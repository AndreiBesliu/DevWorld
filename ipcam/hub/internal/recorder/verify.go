package recorder

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// unhookedGrace is how long a final file without a catalog row is taken
// for a segment whose hook is still on its way, not for a mismatch.
const unhookedGrace = 30 * time.Second

// verifier is the background verification (C12, G07): it parses each
// closed clear segment whole and decrypts each closed .age through
// OpenReaderAt, then compares the catalog with the files.
type verifier struct {
	recDir    string
	cat       *catalog.DB
	recording crypto.Recording // nil: .age files cannot be read, so they count as unreadable
	obs       Observer
	// busy, when set, reports the paths (relative to rec/) that a rewrite
	// is changing, which a pass leaves for the next one.
	busy func(rel string) bool
	// open, when set, is held from the opening of a segment until its row
	// is marked verified, so that a rewrite does not rename over it
	// meanwhile.
	open sync.Locker
}

// pass checks every complete segment that is not verified yet, marks those
// that pass, and reports through Observer.Verified how many could not be
// read, how many end in a torn part, and how many rows and files disagree.
func (v *verifier) pass(ctx context.Context) (ReconcileReport, error) {
	var report ReconcileReport
	segs, err := v.cat.Segments(ctx)
	if err != nil {
		return report, err
	}
	for _, s := range segs {
		if s.State != catalog.StateComplete || s.VerifiedAtMs != nil {
			continue
		}
		if err := v.verifySegment(ctx, s, &report); err != nil {
			return report, err
		}
	}

	// The rows first, then the files: a row means that its hook came, which
	// is after the rename, so the scan finds its file under its final name;
	// a file without a row may still wait for its hook.
	if segs, err = v.cat.Segments(ctx); err != nil {
		return report, err
	}
	rows := map[string]bool{}
	for _, s := range segs {
		rows[s.Path] = true
	}
	files, _, err := scanRecDir(v.recDir)
	if err != nil {
		return report, err
	}
	var facts []catalog.FileFact
	for _, f := range files {
		if f.part || v.isBusy(f.rel) {
			continue
		}
		info, err := os.Stat(f.abs)
		if err != nil {
			continue // renamed or removed since the scan
		}
		if !rows[f.rel] && time.Since(info.ModTime()) < unhookedGrace {
			continue
		}
		facts = append(facts, catalog.FileFact{Cam: string(f.key.cam), Profile: string(f.key.profile), Path: f.rel, Bytes: info.Size(), Encrypted: f.encrypted})
	}
	d := catalog.Diff(segs, facts)
	for _, path := range d.Lost {
		if v.isBusy(path) {
			continue
		}
		report.CatalogMismatches++
	}
	report.CatalogMismatches += len(d.Added) + len(d.Updated)
	v.obs.Verified(report)
	return report, nil
}

// verifySegment checks one segment and marks its row verified when it
// passes, or counts it in report.
func (v *verifier) verifySegment(ctx context.Context, s catalog.Segment, report *ReconcileReport) error {
	if v.open != nil {
		v.open.Lock()
		defer v.open.Unlock()
	}
	if v.isBusy(s.Path) {
		return nil
	}
	abs := filepath.Join(v.recDir, filepath.FromSlash(s.Path))
	switch err := v.check(ctx, abs, s.Encrypted); {
	case errors.Is(err, os.ErrNotExist):
		// The comparison of the catalog with the files counts it.
	case errors.Is(err, errTornPart):
		report.TornParts++
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		report.Unreadable++
	default:
		if err := v.cat.MarkVerified(ctx, s.Path, time.Now().UnixMilli()); err != nil {
			return err
		}
		if o, ok := v.obs.(SegmentVerifiedObserver); ok {
			o.SegmentVerified(abs)
		}
	}
	return nil
}

func (v *verifier) isBusy(rel string) bool { return v.busy != nil && v.busy(rel) }

// verifyOpenHook, when a test sets it, runs each time a verification has
// opened a segment.
var verifyOpenHook = func(string) {}

// errTornPart is a recording that ends in a torn part, or in bytes that are
// no complete box.
var errTornPart = errors.New("the recording ends in a torn part")

// check reads the segment at path whole.
func (v *verifier) check(ctx context.Context, path string, encrypted bool) error {
	f, err := os.Open(path) //nolint:gosec // a segment under rec/
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	verifyOpenHook(path)
	info, err := f.Stat()
	if err != nil {
		return err
	}
	var data io.ReaderAt = f
	size := info.Size()
	if encrypted {
		if v.recording == nil {
			return errors.New("no identity to decrypt the segment with")
		}
		if data, size, err = v.recording.OpenReaderAt(ctx, f, size); err != nil {
			return err
		}
		// Decrypting every byte authenticates every chunk, also those that
		// hold only padding.
		if _, err := io.Copy(io.Discard, io.NewSectionReader(data, 0, size)); err != nil {
			return err
		}
	}
	return checkRecording(data, size)
}

// checkRecording parses the size bytes of a recording's plaintext: walk
// reads the init's boxes (mediacommon's init parser would refuse MediaMTX's
// own mtxi box), and mediacommon's fMP4 parser reads each part, which
// checks that every sample lies in its mdat.
func checkRecording(r io.ReaderAt, size int64) error {
	rec, err := walk(r, size)
	if err != nil {
		return err
	}
	if rec.initEnd == 0 {
		return errors.New("the init is not whole")
	}
	if rec.torn != nil || rec.tail {
		return errTornPart
	}
	for _, p := range rec.parts {
		data := make([]byte, p.mdat.end()-p.moof.off)
		if err := readAt(r, data, p.moof.off); err != nil {
			return err
		}
		var parts fmp4.Parts
		if err := parts.Unmarshal(data); err != nil {
			return err
		}
	}
	return nil
}
