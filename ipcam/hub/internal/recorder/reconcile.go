package recorder

import (
	"bytes"
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
// <HHMMSS>_<µs>.mp4, in UTC (MTX-4), with .age when encrypted (MTX-2), and
// .part while MediaMTX writes it (MTX-1). A .rewrite is the sibling that a
// background rewrite writes (C1-R9), and .unreadable is added to a .part
// that reconcile could not read, which sets it aside.
const (
	suffixMP4        = ".mp4"
	suffixAge        = ".age"
	suffixPart       = ".part"
	suffixRewrite    = ".rewrite"
	suffixUnreadable = ".unreadable"
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
// not a segment. The name is the start in UTC, so it stands for one instant
// also in the hour that repeats when daylight saving time ends.
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
	start, err := time.ParseInLocation("2006-01-02 150405.000000", parts[2]+" "+hms+"."+micros, time.UTC)
	if err != nil {
		return segFile{}, false
	}
	s.abs, s.rel, s.key, s.start = filepath.Join(recDir, filepath.FromSlash(rel)), rel, streamKey{CameraID(parts[0]), p}, start
	return s, true
}

// walkRecDir walks the recordings directory; tests replace it.
var walkRecDir = filepath.WalkDir

// listing is what scanRecDir found under rec/.
type listing struct {
	segs     []segFile // sorted by path: by stream, then by start
	rewrites []string  // the .rewrite siblings that a cut left
	// unreadable are the directories that could not be read, relative to
	// rec/ with "/", and errs the errors of the listing, each naming its
	// directory by its base name.
	unreadable []string
	errs       []error
}

// hidden reports whether rel, a path relative to rec/ with "/", lies in a
// directory that could not be read: whether its file is there is unknown,
// so its row is left as it is (7a M2).
func (l listing) hidden(rel string) bool {
	return slices.ContainsFunc(l.unreadable, func(dir string) bool { return strings.HasPrefix(rel, dir+"/") })
}

// scanRecDir lists the segment files under recDir and the .rewrite siblings
// that a cut left. A directory under recDir that cannot be read, such as a
// date directory with an I/O error after a cut, is skipped as if it were
// empty and listed as unreadable, with its error: one bad directory does not
// keep the others from being listed. err is recDir's own.
func scanRecDir(recDir string) (l listing, err error) {
	err = walkRecDir(recDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			switch {
			case path == recDir && errors.Is(err, fs.ErrNotExist):
				return fs.SkipAll
			case path == recDir:
				return err
			}
			l.errs = append(l.errs, fileError("list", path, err))
			if d != nil && d.IsDir() {
				if rel, rerr := filepath.Rel(recDir, path); rerr == nil {
					l.unreadable = append(l.unreadable, filepath.ToSlash(rel))
				}
				return fs.SkipDir
			}
			return nil
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
				l.rewrites = append(l.rewrites, path)
			}
			return nil
		}
		if s, ok := parseSegment(recDir, rel); ok {
			l.segs = append(l.segs, s)
		}
		return nil
	})
	slices.SortFunc(l.segs, func(a, b segFile) int { return strings.Compare(a.rel, b.rel) })
	return l, err
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
	rec        *recording     // nil when the file is unreadable, empty or complete
	salvage    salvage
	unreadable bool // its bytes are no recording that can be read
	// failed is the I/O error that kept the file from being read: it is left
	// as it is, for the next start.
	failed error
}

// needed reports whether the newest final file must be repaired.
func (rp *repair) needed() bool {
	return rp.seg.part || rp.unreadable || rp.failed != nil || (!rp.complete && rp.salvage.cut)
}

// lost reports whether the file counts as lost toward HUB-09's abort.
func (rp *repair) lost() bool { return rp.unreadable || rp.failed != nil }

// noRecording reports whether the file holds no recording that can be read:
// a stream's restart gap does not end where it starts.
func (rp *repair) noRecording() bool { return rp.lost() || rp.salvage.empty }

// lostTail reports whether the repair loses the end of the recording that
// the file was to hold, which a restart gap then covers: a .part's, or a
// salvage's that cuts the file.
func (rp *repair) lostTail() bool { return rp.seg.part || rp.salvage.cut }

// resealsInPlace reports whether every byte that the salvage of an
// encrypted file changes lies in its last kept chunk, which Reseal may
// re-encrypt as the final chunk (C1-R7).
func (rp *repair) resealsInPlace() bool {
	s := rp.salvage
	lastChunk := (s.keep - 1) / crypto.ChunkSize * crypto.ChunkSize
	return !slices.ContainsFunc(s.patches, func(p crypto.Patch) bool { return p.Off < lastChunk })
}

// keptMs is the duration of what the file holds once apply has repaired
// it: nothing when it is removed or cannot be read, the end of its last
// complete part when its salvage waits for a rewrite (C1-R9), else the
// salvage's.
func (rp *repair) keptMs() int64 {
	switch {
	case rp.complete:
	case rp.salvage.empty:
		return 0
	case rp.torn != nil && !rp.resealsInPlace():
		return rp.salvage.partEndMs
	}
	return rp.salvage.durationMs
}

// dirSyncError is apply's error when the file was repaired and renamed or
// removed, and only the sync of its directory failed: the repair stands,
// and its gap is written already.
type dirSyncError struct{ err error }

func (e *dirSyncError) Error() string { return "sync the directory: " + e.err.Error() }

func (e *dirSyncError) Unwrap() error { return e.err }

func (rp *repair) close() {
	if rp.f != nil {
		_ = rp.f.Close()
		rp.f = nil
	}
}

// cannotRead sorts out a file of size bytes that OpenTorn or walk could not
// read with err: an I/O error is a failure; a .part of zeros only, as NTFS
// can leave after a cut, holds no recording and is empty; anything else is
// unreadable.
func (rp *repair) cannotRead(err error, size int64) {
	if isIOError(err) {
		rp.failed = err
		return
	}
	if rp.seg.part {
		zero, err := allZero(rp.f, size)
		if err != nil {
			rp.failed = err
			return
		}
		if zero {
			rp.salvage = salvage{cut: true, empty: true}
			return
		}
	}
	rp.unreadable = true
}

// isIOError reports whether err is an error of the file system rather than
// of the bytes that it read.
func isIOError(err error) bool {
	var pe *fs.PathError
	return errors.As(err, &pe)
}

// allZero reports whether the size bytes of r are all zeros.
func allZero(r io.ReaderAt, size int64) (bool, error) {
	buf, zeros := make([]byte, 64<<10), make([]byte, 64<<10)
	sr := io.NewSectionReader(r, 0, size)
	for {
		n, err := sr.Read(buf)
		if !bytes.Equal(buf[:n], zeros[:n]) {
			return false, nil
		}
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// fileError is the error of a file that could not be repaired. It names the
// file by its base name only: the directories above it can hold the name of
// the owner's account, and the events reach published results.
func fileError(what, path string, err error) error {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		err = fmt.Errorf("%s: %w", pe.Op, pe.Err)
	case errors.As(err, &le):
		err = fmt.Errorf("%s: %w", le.Op, le.Err)
	}
	return fmt.Errorf("recorder: %s %s: %w", what, filepath.Base(path), err)
}

// quarantine sets an unreadable .part aside as <name>.unreadable, which is no
// segment's name, so that a later start neither counts it again nor writes
// its gap again.
func quarantine(s segFile) error {
	if err := renameReplace(s.abs, s.abs+suffixUnreadable); err != nil {
		return err
	}
	return syncDir(filepath.Dir(s.abs))
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
//   - each lost tail is a restart gap, which ends where the stream's next
//     segment that holds a recording starts, or stays open when there is
//     none: the first part after the start closes it. It is written before
//     the file changes, so that no cut loses it; the next start finds it
//     again after a cut before the change, and writes no second one;
//   - an unreadable .part gets a restart gap from the start in its name, and
//     is set aside as <name>.unreadable;
//   - the catalog is made to match the files.
//
// A file that cannot be repaired, through an I/O error or a failed Reseal,
// is left as it is and counted as unreadable: one bad file never stops the
// others' repair. Its restart gap, when it has one, then starts at its
// start: none of it can be read until a later start repairs it. A file
// whose directory sync fails after its rename or removal is repaired, and
// the sync's error is reported. A directory under recDir that cannot be
// read is skipped and reported, and the rows under it are left as they
// are. When more than half of the segments would be lost, it changes
// nothing and reports Aborted (HUB-09). It returns an error only for ctx,
// the listing of recDir itself and the catalog. It decrypts in RAM only and
// writes no temporary file (G08).
func Reconcile(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB) (ReconcileReport, error) {
	res, err := reconcile(ctx, recDir, tail, cat)
	return res.report, err
}

// reconciled is what reconcile did: its report, the rewrites that its
// salvages still need, the gaps that it wrote, and the errors of the files
// that it could not repair.
type reconciled struct {
	report  ReconcileReport
	pending []*pendingRewrite
	gaps    []catalog.Gap
	errs    []error
}

func reconcile(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB) (reconciled, error) {
	var res reconciled
	l, err := scanRecDir(recDir)
	if err != nil {
		return res, fmt.Errorf("recorder: reconcile: %w", err)
	}
	segs := l.segs
	res.errs = l.errs
	rows, err := cat.Segments(ctx)
	if err != nil {
		return res, err
	}

	repairs, err := planRepairs(ctx, segs, tail)
	defer func() {
		for _, rp := range repairs {
			rp.close()
		}
	}()
	if err != nil {
		return res, err
	}
	if aborts(l, rows, repairs) {
		res.report.Aborted = true
		return res, nil
	}
	for _, path := range l.rewrites {
		if err := os.Remove(path); err != nil {
			res.errs = append(res.errs, fileError("remove the partial rewrite", path, err))
		}
	}

	durations := map[string]int64{} // final path → ms, of the files repaired
	// noRecording holds the files that hold no recording that can be read:
	// a stream's restart gap does not end where one of them starts.
	noRecording := map[string]bool{}
	for _, rp := range repairs {
		if rp.noRecording() {
			noRecording[rp.seg.abs] = true
		}
	}
	var written []*catalog.Gap // the restart gaps that this start wrote, as they end up
	for _, rp := range repairs {
		if rp.failed != nil {
			res.report.Unreadable++
			res.errs = append(res.errs, fileError("reconcile", rp.seg.abs, rp.failed))
			continue
		}
		var g catalog.Gap
		if rp.lostTail() {
			// The gap first, before the file changes: a cut before the change
			// finds the gap again at the next start, one after it would lose
			// the gap. A .part that an earlier MediaMTX left, older than other
			// segments of its stream, has a gap that ended when the next one
			// started.
			g = restartGap(rp.seg, rp.keptMs(), nextStartMs(segs, rp.seg, noRecording))
			isNew, err := putRestartGap(ctx, cat, rp.seg, g)
			if err != nil {
				return res, err
			}
			if isNew {
				written = append(written, &g)
				res.report.GapsWritten++
			}
			res.report.LostTails++
		}
		if rp.unreadable {
			res.report.Unreadable++
			if rp.seg.part {
				rp.close() // Windows renames only a file that nobody has open
				if err := quarantine(rp.seg); err != nil {
					res.errs = append(res.errs, fileError("set aside", rp.seg.abs, err))
				}
			}
			continue
		}
		durationMs, p, err := rp.apply(ctx, &res.report)
		if err != nil {
			res.errs = append(res.errs, fileError("reconcile", rp.seg.abs, err))
			if !errors.As(err, new(*dirSyncError)) {
				res.report.Unreadable++
				if rp.lostTail() {
					if err := failedRepairGap(ctx, cat, rp.seg, &g); err != nil {
						return res, err
					}
				}
				continue
			}
		}
		if p != nil {
			res.pending = append(res.pending, p)
		}
		durations[rp.seg.final()] = durationMs
	}
	for _, g := range written {
		res.gaps = append(res.gaps, *g)
	}

	errs, err := reconcileCatalog(ctx, recDir, tail, cat, rows, durations)
	res.errs = append(res.errs, errs...)
	return res, err
}

// restartGap is the gap after the lost tail of s, whose file now holds
// durationMs: it ends at nextMs, where the stream's next segment starts, or
// stays open when nextMs is 0 (the first part after a restart closes it).
func restartGap(s segFile, durationMs, nextMs int64) catalog.Gap {
	g := catalog.Gap{Cam: string(s.key.cam), Profile: string(s.key.profile), StartMs: s.start.UnixMilli() + durationMs, Reason: catalog.GapRestart}
	if nextMs != 0 {
		g.EndMs = max(nextMs, g.StartMs)
	}
	return g
}

// putRestartGap writes g, the restart gap of the lost tail of s, unless an
// earlier start wrote it: a restart gap of the stream that starts where g
// does, or at the start of s, where a failed repair left it
// (failedRepairGap), is that gap, and one at the start of s moves to where
// g starts, as the repair to come expects. isNew reports whether g is a new
// row. So however many starts a cut ends between the gap and the repair, or
// a repair that keeps failing, the file has one gap (PutGap is a plain
// INSERT).
func putRestartGap(ctx context.Context, cat *catalog.DB, s segFile, g catalog.Gap) (isNew bool, err error) {
	cam, profile, fileStart := string(s.key.cam), string(s.key.profile), s.start.UnixMilli()
	found, err := cat.HasGap(ctx, cam, profile, g.StartMs, catalog.GapRestart)
	if err != nil || found {
		return false, err
	}
	if fileStart != g.StartMs {
		if found, err = cat.HasGap(ctx, cam, profile, fileStart, catalog.GapRestart); err != nil {
			return false, err
		}
		if found {
			return false, cat.MoveGapStart(ctx, cam, profile, fileStart, g.StartMs)
		}
	}
	return true, cat.PutGap(ctx, g)
}

// failedRepairGap moves g, the restart gap of s that putRestartGap left at
// g's start, to the start of s once the repair of s failed for another
// reason than its directory's sync: the file stays as it was, so none of its
// recording can be read until a later start repairs it, and the gap covers
// it whole.
func failedRepairGap(ctx context.Context, cat *catalog.DB, s segFile, g *catalog.Gap) error {
	fileStart := s.start.UnixMilli()
	if g.StartMs == fileStart {
		return nil
	}
	if err := cat.MoveGapStart(ctx, string(s.key.cam), string(s.key.profile), g.StartMs, fileStart); err != nil {
		return err
	}
	g.StartMs = fileStart
	return nil
}

// nextStartMs returns the start, in ms, of the first of segs that belongs
// to the stream of s, starts after it and is not in none, the files that
// hold no recording, or 0 when there is none.
func nextStartMs(segs []segFile, s segFile, none map[string]bool) int64 {
	var next time.Time
	for _, o := range segs {
		if o.key == s.key && !none[o.abs] && o.start.After(s.start) && (next.IsZero() || o.start.Before(next)) {
			next = o.start
		}
	}
	if next.IsZero() {
		return 0
	}
	return next.UnixMilli()
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
			rp.close()
			return repairs, err
		}
		if rp.needed() {
			repairs = append(repairs, rp)
		} else {
			rp.close()
		}
	}
	return repairs, nil
}

// openSegment opens a segment that reconcile repairs; tests replace it.
var openSegment = func(path string, flag int) (*os.File, error) { return os.OpenFile(path, flag, 0) } //nolint:gosec // a segment under rec/

// planRepair reads s, without changing it, and plans its repair. Its error
// is ctx's alone: a file that cannot be read is reported in the plan.
func planRepair(ctx context.Context, s segFile, tail crypto.TailSealer) (*repair, error) {
	rp := &repair{seg: s}
	f, err := openSegment(s.abs, os.O_RDWR)
	if err != nil {
		rp.failed = err
		return rp, nil
	}
	rp.f = f
	info, err := f.Stat()
	if err != nil {
		rp.failed = err
		return rp, nil
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
			rp.cannotRead(err, info.Size())
			return rp, nil
		}
		rp.torn, rp.complete = torn, torn.Complete()
		if rp.complete && !s.part {
			// A final file that ends in its final chunk needs no repair, and
			// is read no further than OpenTorn's scan, which authenticated
			// that chunk: walking its parts would decrypt a chunk per part on
			// the synchronous path before MediaMTX starts. The background
			// verification reads it whole. A complete .part is still walked:
			// its duration is its row's end and its restart gap's start.
			return rp, nil
		}
		data, size = torn, torn.DurableSize()
	}
	rec, err := walk(data, size)
	if err != nil {
		// errNotRecording, a chunk that does not authenticate, or a read that
		// failed: reported through rp.
		rp.cannotRead(err, info.Size())
		return rp, nil
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
// files that cannot be read and the rows whose file is gone (HUB-09). A row
// under a directory that could not be read counts neither way.
func aborts(l listing, rows []catalog.Segment, repairs []*repair) bool {
	files := map[string]bool{}
	for _, s := range l.segs {
		files[s.finalRel()] = true
	}
	total, lost := len(l.segs), 0
	for _, r := range rows {
		if r.State != catalog.StateLost && !files[r.Path] && !l.hidden(r.Path) {
			total++
			lost++
		}
	}
	for _, rp := range repairs {
		if rp.lost() {
			lost++
		}
	}
	return total > 0 && 2*lost > total
}

// apply repairs the file and gives a .part its final name. It returns the
// duration of what the file now holds (keptMs), and the rewrite that a
// salvage still needs. Its error is a *dirSyncError when only the sync of
// the directory failed, after the rename or the removal: the file is
// repaired then.
func (rp *repair) apply(ctx context.Context, report *ReconcileReport) (int64, *pendingRewrite, error) {
	durationMs := rp.keptMs()
	var pending *pendingRewrite
	switch {
	case rp.complete:
		// Its final chunk is there: it is never resealed or rewritten.
	case rp.salvage.empty:
		// Not one sample reached the disk: there is no recording to keep.
		rp.close()
		if err := os.Remove(rp.seg.abs); err != nil {
			return 0, nil, err
		}
		if err := syncDir(filepath.Dir(rp.seg.abs)); err != nil {
			return 0, nil, &dirSyncError{err}
		}
		return 0, nil, nil
	case rp.torn != nil:
		s := rp.salvage
		if rp.resealsInPlace() {
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
		report.Finalized++
		if err := syncDir(filepath.Dir(rp.seg.abs)); err != nil {
			return durationMs, pending, &dirSyncError{err}
		}
	}
	return durationMs, pending, nil
}

// leftParts lists the .part files in the three newest date directories of
// every stream under recDir. Once the child has exited, none of them is
// written.
func leftParts(recDir string) []string {
	cams, err := os.ReadDir(recDir)
	if err != nil {
		return nil
	}
	var parts []string
	for _, cam := range cams {
		if !cam.IsDir() {
			continue
		}
		for _, p := range []Profile{ProfileMain, ProfileSub} {
			parts = append(parts, partFiles(filepath.Join(recDir, cam.Name(), string(p)))...)
		}
	}
	return parts
}

// streamSegments lists the segment files in the three newest date
// directories of the stream k.
func streamSegments(recDir string, k streamKey) []segFile {
	var segs []segFile
	for _, path := range recentFiles(filepath.Join(recDir, string(k.cam), string(k.profile))) {
		if rel, err := filepath.Rel(recDir, path); err == nil {
			if s, ok := parseSegment(recDir, rel); ok {
				segs = append(segs, s)
			}
		}
	}
	return segs
}

// repairParts repairs, while the hub runs, the .part files that MediaMTX
// left and no longer writes: those of a child that exited, and one that a
// recorder left when a failed write or fsync made it start again in the
// same process (MTX-1). Each is repaired as Reconcile repairs a .part at
// startup, without its abort, which judges a start (HUB-09): it keeps its
// complete parts and the whole samples of a torn last one, gets its final
// name, a catalog row and a restart gap that ends where the stream's next
// segment that holds a recording starts, and a salvage that changes bytes
// before its last kept chunk gets the background rewrite (C1-R9); an
// unreadable one gets its gap and is set aside. The whole batch is planned
// first, so that, as at startup, no gap ends where a file of the batch that
// holds no recording starts. Without this, such a file would wait for the
// next hub start, outside the catalog, and its gap would then be closed by
// the first part after that start. The report adds what was done and goes
// to ReconcileObserver as the next update; verification follows. It ends
// the piece of background work that repairLoop began.
func (r *rec) repairParts(ctx context.Context, paths []string) {
	var segs []segFile
	for _, path := range paths {
		if rel, err := filepath.Rel(r.cfg.RecDir, path); err == nil {
			if s, ok := parseSegment(r.cfg.RecDir, rel); ok && s.part {
				segs = append(segs, s)
			}
		}
	}
	// The oldest first, so that the next segment, where a gap ends, is still
	// there under the name it started with.
	slices.SortFunc(segs, func(a, b segFile) int { return a.start.Compare(b.start) })
	var plans []*repair
	for _, s := range segs {
		if ctx.Err() != nil {
			break
		}
		if _, err := os.Lstat(s.abs); errors.Is(err, fs.ErrNotExist) {
			r.forgetPart(s.abs) // repaired already
			continue
		}
		r.setBusy(s.finalRel(), true) // verification leaves it until its row is there
		rp, err := planRepair(ctx, s, r.cfg.Tail)
		if err != nil { // ctx's: nothing has changed
			rp.close()
			r.setBusy(s.finalRel(), false)
			break
		}
		plans = append(plans, rp)
	}
	none := map[string]bool{}
	for _, rp := range plans {
		if rp.noRecording() {
			none[rp.seg.abs] = true
		}
	}
	var report ReconcileReport
	var pending []*pendingRewrite
	for _, rp := range plans {
		s := rp.seg
		if ctx.Err() == nil {
			// A repair that has begun is finished, also when the hub stops: a
			// file renamed without its row and gap would lose the gap.
			p, err := r.repairPart(context.WithoutCancel(ctx), rp, none, &report)
			if p != nil {
				pending = append(pending, p)
			}
			if err != nil {
				r.notifyError(fileError("repair", s.abs, err))
			}
		}
		rp.close()
		r.setBusy(s.finalRel(), false)
		if _, err := os.Lstat(s.abs); errors.Is(err, fs.ErrNotExist) {
			// It has its final name, or was empty: no listing gives it again.
			// One that is still there stays tried, until the next start.
			r.forgetPart(s.abs)
		}
	}
	r.finishWork(report, pending)
	if report == (ReconcileReport{}) && pending == nil {
		return
	}
	r.kickVerify()
	select {
	case r.jobsKick <- struct{}{}:
	default:
	}
}

// repairPart repairs one planned .part for repairParts, and returns the
// rewrite that its salvage still needs; none holds the batch's files that
// hold no recording. A file that it cannot repair counts as unreadable, and
// its error is returned; so is the error of a directory sync that failed
// after the rename or the removal, which leaves the file repaired.
func (r *rec) repairPart(ctx context.Context, rp *repair, none map[string]bool, report *ReconcileReport) (*pendingRewrite, error) {
	s := rp.seg
	if rp.failed != nil {
		report.Unreadable++
		return nil, rp.failed
	}
	// The gap first, before the file changes, as at startup. It ends where the
	// stream's next segment that holds a recording starts when that segment
	// is there; otherwise the stream's first part closes it. gapMu keeps that
	// first part from closing the stream's gaps between the two steps.
	r.gapMu.Lock()
	g := restartGap(s, rp.keptMs(), nextStartMs(streamSegments(r.cfg.RecDir, s.key), s, none))
	isNew, err := putRestartGap(ctx, r.cfg.Catalog, s, g)
	r.gapMu.Unlock()
	if err != nil {
		return nil, err
	}
	if isNew {
		defer func() { r.notifyGap(g) }() // as it ends up: a failed repair moves it
		report.GapsWritten++
	}
	report.LostTails++
	if rp.unreadable {
		report.Unreadable++
		rp.close() // Windows renames only a file that nobody has open
		return nil, quarantine(s)
	}
	durationMs, pending, err := rp.apply(ctx, report)
	if err != nil && !errors.As(err, new(*dirSyncError)) {
		report.Unreadable++
		return nil, errors.Join(err, failedRepairGap(ctx, r.cfg.Catalog, s, &g))
	}
	if removed := !rp.complete && rp.salvage.empty; !removed {
		info, serr := os.Stat(s.final())
		if serr != nil {
			return pending, serr
		}
		start, k := s.start.UnixMilli(), s.key
		if perr := r.cfg.Catalog.PutSegment(ctx, catalog.Segment{Cam: string(k.cam), Profile: string(k.profile), Path: s.finalRel(),
			StartMs: start, EndMs: start + durationMs, Bytes: info.Size(), Encrypted: s.encrypted, State: catalog.StateComplete}); perr != nil {
			return pending, perr
		}
	}
	return pending, err
}

// statSegment reads a final file's size for the catalog; tests replace it.
var statSegment = os.Stat

// reconcileCatalog makes the catalog's rows match the final files (G07):
// rows for files that have none, with the start from the file's name and
// the end from its contents, the facts of files that changed, and lost
// rows for files that are gone. A file whose size cannot be read keeps its
// row as it is, and its error is returned with the others. A row under a
// directory that this listing could not read stays as it is too; the first
// listing reported the directory.
func reconcileCatalog(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB, rows []catalog.Segment, durations map[string]int64) ([]error, error) {
	l, err := scanRecDir(recDir)
	if err != nil {
		return nil, err
	}
	known := map[string]catalog.Segment{}
	var facts []catalog.FileFact
	for _, r := range rows {
		known[r.Path] = r
		if l.hidden(r.Path) && r.State == catalog.StateComplete {
			facts = append(facts, rowFact(r))
		}
	}
	var errs []error
	for _, s := range l.segs {
		if s.part {
			continue
		}
		info, err := statSegment(s.abs)
		if err != nil {
			errs = append(errs, fileError("catalog", s.abs, err))
			if row, ok := known[s.rel]; ok && row.State == catalog.StateComplete {
				facts = append(facts, rowFact(row))
			}
			continue
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
	return errs, err
}

// rowFact is the fact that leaves the complete row r as it is.
func rowFact(r catalog.Segment) catalog.FileFact {
	return catalog.FileFact{Cam: r.Cam, Profile: r.Profile, Path: r.Path, Bytes: r.Bytes, Encrypted: r.Encrypted}
}

// fileDurationMs reads the duration of a final file from its parts, or 0
// when it cannot be read: verification then reports it. It opens the file
// read-only.
func fileDurationMs(ctx context.Context, s segFile, tail crypto.TailSealer) int64 {
	f, err := os.Open(s.abs)
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
