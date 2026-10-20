package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// The names of the segment files: rec/<cam>/<profile>/<YYYY-MM-DD>/
// <HHMMSS>_<µs>.mp4, with .age when encrypted (MTX-2), and .part while
// MediaMTX writes it (MTX-1). A .rewrite is the sibling that a background
// rewrite writes (C1-R9).
const (
	suffixMP4     = ".mp4"
	suffixAge     = ".age"
	suffixPart    = ".part"
	suffixRewrite = ".rewrite"
)

// segFile is a segment file under rec/.
type segFile struct {
	abs, rel  string // rel is relative to rec/, with "/", as the catalog has it
	key       streamKey
	start     time.Time
	encrypted bool
	part      bool // a .part, not complete yet
}

// final is the segment's final name.
func (s segFile) final() string { return strings.TrimSuffix(s.abs, suffixPart) }

func (s segFile) finalRel() string { return strings.TrimSuffix(s.rel, suffixPart) }

// parseSegment reads a path relative to rec/. ok is false for a file that is
// not a segment.
func parseSegment(recDir, rel string) (s segFile, ok bool) {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) != 4 || !namePattern.MatchString(parts[0]) || parts[0] == "." || parts[0] == ".." {
		return segFile{}, false
	}
	p := Profile(parts[1])
	if p != ProfileMain && p != ProfileSub {
		return segFile{}, false
	}
	name := parts[3]
	s.part = strings.HasSuffix(name, suffixPart)
	name = strings.TrimSuffix(name, suffixPart)
	s.encrypted = strings.HasSuffix(name, suffixAge)
	name = strings.TrimSuffix(name, suffixAge)
	stem, isMP4 := strings.CutSuffix(name, suffixMP4)
	hms, micros, found := strings.Cut(stem, "_")
	if !isMP4 || !found || len(hms) != 6 || len(micros) != 6 {
		return segFile{}, false
	}
	start, err := time.ParseInLocation("2006-01-02 150405.000000", parts[2]+" "+hms+"."+micros, time.Local)
	if err != nil {
		return segFile{}, false
	}
	s.abs, s.rel, s.key, s.start = filepath.Join(recDir, filepath.FromSlash(rel)), rel, streamKey{CameraID(parts[0]), p}, start
	return s, true
}

// scanRecDir returns the segment files under recDir, sorted by path, which
// is by stream, then by start, and the .rewrite siblings that a cut left.
func scanRecDir(recDir string) (segs []segFile, rewrites []string, err error) {
	err = filepath.WalkDir(recDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == recDir {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(recDir, path)
		if err != nil {
			return err
		}
		if base, ok := strings.CutSuffix(rel, suffixRewrite); ok {
			if _, isSeg := parseSegment(recDir, base); isSeg {
				rewrites = append(rewrites, path)
			}
			return nil
		}
		if s, ok := parseSegment(recDir, rel); ok {
			segs = append(segs, s)
		}
		return nil
	})
	slices.SortFunc(segs, func(a, b segFile) int { return strings.Compare(a.rel, b.rel) })
	return segs, rewrites, err
}

// pendingRewrite is a salvage that changes bytes before the last kept chunk
// of an encrypted file, which Reseal must never do (C1-R7). Reconcile cut the
// file at the end of its last complete part and kept the cut-off chunks'
// ciphertext in RAM; once recording has resumed, the salvaged plaintext is
// rewritten under a fresh file key over that interim file (C1-R9).
type pendingRewrite struct {
	seg           segFile // under its final name
	keep          int64
	patches       []crypto.Patch
	kept          *crypto.KeptTail
	interimEndMs  int64
	salvagedEndMs int64
}

// repair is reconcile's plan for one file: a .part, or the newest
// final-named file of a stream, whose last bytes may not have reached the
// disk.
type repair struct {
	seg        segFile
	f          *os.File
	torn       crypto.TornAge // an encrypted file
	complete   bool           // an encrypted file with its final chunk, never resealed or rewritten
	rec        *recording     // nil when the file is unreadable or complete
	salvage    salvage
	unreadable bool
}

// needed reports whether the newest final file must be repaired.
func (rp *repair) needed() bool {
	return rp.seg.part || rp.unreadable || (!rp.complete && rp.salvage.cut)
}

// Reconcile repairs the recordings under recDir after a cut or a crash
// (§4.5, R01, C12, G07, G08), before MediaMTX starts:
//
//   - each .part, and the newest final-named segment of each stream in case
//     its last bytes did not reach the disk, keeps every complete part plus
//     the whole samples of a torn last part, and is renamed to its final
//     name; a clear file gets its mvhd duration;
//   - an encrypted file is resealed in place when every byte that the
//     salvage changes lies in its last kept chunk; otherwise it is cut at
//     its last complete part, and New rewrites the salvage in the background
//     (C1-R9; Reconcile alone leaves the cut file). A file with its final
//     chunk is never resealed or rewritten;
//   - each lost tail is an open restart gap;
//   - the catalog is made to match the files.
//
// When more than half of the segments would be lost, it changes nothing
// and reports Aborted (HUB-09). It decrypts in RAM only and writes no
// temporary file (G08).
func Reconcile(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB) (ReconcileReport, error) {
	report, _, _, err := reconcile(ctx, recDir, tail, cat)
	return report, err
}

func reconcile(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB) (ReconcileReport, []*pendingRewrite, []catalog.Gap, error) {
	var report ReconcileReport
	segs, rewrites, err := scanRecDir(recDir)
	if err != nil {
		return report, nil, nil, fmt.Errorf("recorder: reconcile: %w", err)
	}
	rows, err := cat.Segments(ctx)
	if err != nil {
		return report, nil, nil, err
	}

	repairs, err := planRepairs(ctx, segs, tail)
	defer func() {
		for _, rp := range repairs {
			if rp.f != nil {
				_ = rp.f.Close()
			}
		}
	}()
	if err != nil {
		return report, nil, nil, err
	}
	if aborts(segs, rows, repairs) {
		report.Aborted = true
		return report, nil, nil, nil
	}
	for _, path := range rewrites {
		if err := os.Remove(path); err != nil {
			return report, nil, nil, fmt.Errorf("recorder: remove the partial rewrite %s: %w", path, err)
		}
	}

	durations := map[string]int64{} // final path → ms, of the files repaired
	var pending []*pendingRewrite
	var gaps []catalog.Gap
	for _, rp := range repairs {
		if rp.unreadable {
			report.Unreadable++
			continue
		}
		durationMs, p, err := rp.apply(ctx, &report)
		if err != nil {
			return report, nil, nil, fmt.Errorf("recorder: reconcile %s: %w", rp.seg.rel, err)
		}
		if p != nil {
			pending = append(pending, p)
		}
		durations[rp.seg.final()] = durationMs
		if rp.seg.part || rp.salvage.cut {
			g := catalog.Gap{Cam: string(rp.seg.key.cam), Profile: string(rp.seg.key.profile), StartMs: rp.seg.start.UnixMilli() + durationMs, Reason: catalog.GapRestart}
			if err := cat.PutGap(ctx, g); err != nil {
				return report, nil, nil, err
			}
			gaps = append(gaps, g)
			report.LostTails++
			report.GapsWritten++
		}
	}

	if err := reconcileCatalog(ctx, recDir, tail, cat, rows, durations); err != nil {
		return report, nil, nil, err
	}
	return report, pending, gaps, nil
}

// planRepairs opens and reads, without changing them, every .part and the
// newest final file of each stream.
func planRepairs(ctx context.Context, segs []segFile, tail crypto.TailSealer) ([]*repair, error) {
	newest := map[streamKey]segFile{}
	for _, s := range segs {
		if !s.part {
			newest[s.key] = s // segs are sorted by start
		}
	}
	var repairs []*repair
	for _, s := range segs {
		if !s.part && newest[s.key].abs != s.abs {
			continue
		}
		rp, err := planRepair(ctx, s, tail)
		if err != nil {
			return repairs, err
		}
		if rp.needed() {
			repairs = append(repairs, rp)
		} else if rp.f != nil {
			_ = rp.f.Close()
		}
	}
	return repairs, nil
}

func planRepair(ctx context.Context, s segFile, tail crypto.TailSealer) (*repair, error) {
	rp := &repair{seg: s}
	f, err := os.OpenFile(s.abs, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("recorder: reconcile: %w", err)
	}
	rp.f = f
	info, err := f.Stat()
	if err != nil {
		return rp, err
	}
	var data io.ReaderAt = f
	size := info.Size()
	if s.encrypted {
		if tail == nil {
			rp.unreadable = true // there is no RI_g to read it with
			return rp, nil
		}
		torn, err := tail.OpenTorn(ctx, f)
		if err != nil {
			if ctx.Err() != nil {
				return rp, ctx.Err()
			}
			rp.unreadable = true
			return rp, nil
		}
		rp.torn, rp.complete = torn, torn.Complete()
		data, size = torn, torn.DurableSize()
	}
	rec, err := walk(data, size)
	if err != nil {
		// errNotRecording, or a chunk that does not authenticate: an
		// unreadable file is reported, and left as it is.
		rp.unreadable = true
		return rp, nil //nolint:nilerr // reported through rp.unreadable
	}
	rp.rec, rp.salvage = rec, rec.planSalvage(size)
	if rp.torn != nil && !rp.complete {
		// The plaintext after the durable chunks is lost, even when the
		// durable bytes end on a box.
		rp.salvage.cut = true
	}
	return rp, nil
}

// aborts reports whether more than half of the segments would be lost: the
// files that cannot be read and the rows whose file is gone (HUB-09).
func aborts(segs []segFile, rows []catalog.Segment, repairs []*repair) bool {
	files := map[string]bool{}
	for _, s := range segs {
		files[s.finalRel()] = true
	}
	total, lost := len(segs), 0
	for _, r := range rows {
		if r.State != catalog.StateLost && !files[r.Path] {
			total++
			lost++
		}
	}
	for _, rp := range repairs {
		if rp.unreadable {
			lost++
		}
	}
	return total > 0 && 2*lost > total
}

// apply repairs the file and gives a .part its final name. It returns the
// duration of what the file now holds, and the rewrite that a salvage still
// needs.
func (rp *repair) apply(ctx context.Context, report *ReconcileReport) (int64, *pendingRewrite, error) {
	durationMs := rp.salvage.durationMs
	var pending *pendingRewrite
	switch {
	case rp.complete:
		// Its final chunk is there: it is never resealed or rewritten.
	case rp.salvage.empty:
		// Not one sample reached the disk: there is no recording to keep.
		_ = rp.f.Close()
		rp.f = nil
		if err := os.Remove(rp.seg.abs); err != nil {
			return 0, nil, err
		}
		return 0, nil, syncDir(filepath.Dir(rp.seg.abs))
	case rp.torn != nil:
		s := rp.salvage
		lastChunk := (s.keep - 1) / crypto.ChunkSize * crypto.ChunkSize
		inLastChunk := !slices.ContainsFunc(s.patches, func(p crypto.Patch) bool { return p.Off < lastChunk })
		if inLastChunk {
			if _, err := rp.torn.Reseal(ctx, s.keep, s.patches); err != nil {
				return 0, nil, err
			}
		} else {
			kept, err := crypto.KeepTail(rp.torn, s.partEnd)
			if err != nil {
				return 0, nil, err
			}
			if _, err := rp.torn.Reseal(ctx, s.partEnd, nil); err != nil {
				return 0, nil, err
			}
			start := rp.seg.start.UnixMilli()
			seg := rp.seg
			seg.abs, seg.rel, seg.part = seg.final(), seg.finalRel(), false
			pending = &pendingRewrite{seg: seg, keep: s.keep, patches: s.patches, kept: kept,
				interimEndMs: start + s.partEndMs, salvagedEndMs: start + s.durationMs}
			durationMs = s.partEndMs
		}
		if s.cut {
			report.Truncated++
		}
	default:
		s := rp.salvage
		patches := append(slices.Clone(s.patches), rp.rec.mvhdPatch(s.durationMs))
		for _, p := range patches {
			if _, err := rp.f.WriteAt(p.Data, p.Off); err != nil {
				return 0, nil, err
			}
		}
		if err := rp.f.Truncate(s.keep); err != nil {
			return 0, nil, err
		}
		if err := rp.f.Sync(); err != nil {
			return 0, nil, err
		}
		if s.cut {
			report.Truncated++
		}
	}
	if rp.seg.part {
		if err := rp.f.Close(); err != nil {
			return 0, nil, err
		}
		rp.f = nil
		if err := renameReplace(rp.seg.abs, rp.seg.final()); err != nil {
			return 0, nil, err
		}
		if err := syncDir(filepath.Dir(rp.seg.abs)); err != nil {
			return 0, nil, err
		}
		report.Finalized++
	}
	return durationMs, pending, nil
}

// reconcileCatalog makes the catalog's rows match the final files (G07):
// rows for files that have none, with the start from the file's name and
// the end from its contents, the facts of files that changed, and lost
// rows for files that are gone.
func reconcileCatalog(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB, rows []catalog.Segment, durations map[string]int64) error {
	segs, _, err := scanRecDir(recDir)
	if err != nil {
		return err
	}
	known := map[string]catalog.Segment{}
	for _, r := range rows {
		known[r.Path] = r
	}
	var facts []catalog.FileFact
	for _, s := range segs {
		if s.part {
			continue
		}
		info, err := os.Stat(s.abs)
		if err != nil {
			return err
		}
		fact := catalog.FileFact{Cam: string(s.key.cam), Profile: string(s.key.profile), Path: s.rel, Bytes: info.Size(), Encrypted: s.encrypted}
		if row, ok := known[s.rel]; !ok || row.Bytes != fact.Bytes || row.State != catalog.StateComplete {
			d, ok := durations[s.abs]
			if !ok {
				d = fileDurationMs(ctx, s, tail)
			}
			fact.StartMs = s.start.UnixMilli()
			fact.EndMs = fact.StartMs + d
		}
		facts = append(facts, fact)
	}
	_, err = cat.ReconcileFiles(ctx, facts)
	return err
}

// fileDurationMs reads the duration of a final file from its parts, or 0
// when it cannot be read: verification then reports it.
func fileDurationMs(ctx context.Context, s segFile, tail crypto.TailSealer) int64 {
	f, err := os.OpenFile(s.abs, os.O_RDWR, 0)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	var rec *recording
	if s.encrypted {
		if tail == nil {
			return 0
		}
		t, err := tail.OpenTorn(ctx, f)
		if err != nil {
			return 0
		}
		rec, err = walk(t, t.DurableSize())
		if err != nil {
			return 0
		}
	} else {
		info, err := f.Stat()
		if err != nil {
			return 0
		}
		if rec, err = walk(f, info.Size()); err != nil {
			return 0
		}
	}
	return rec.durationMs(nil)
}
