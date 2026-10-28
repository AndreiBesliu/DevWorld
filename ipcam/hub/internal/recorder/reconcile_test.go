package recorder

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/rtsptest"
)

var t0 = time.Date(2026, 10, 20, 10, 0, 0, 123_456_000, time.UTC)

// segPath is where MediaMTX writes the segment of cam/profile that starts at
// start; suffix follows ".mp4": "", ".part", ".age" or ".age.part".
func segPath(recDir, cam, profile string, start time.Time, suffix string) string {
	return filepath.Join(recDir, cam, profile, filepath.FromSlash(segmentName(start))) + suffix
}

// relSeg is segPath relative to the recordings directory, as the catalog has it.
func relSeg(cam, profile string, start time.Time, suffix string) string {
	return cam + "/" + profile + "/" + segmentName(start) + suffix
}

func requireGaps(t *testing.T, cat *catalog.DB, want []catalog.Gap) {
	t.Helper()
	gaps, err := cat.Gaps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gaps, want) {
		t.Errorf("gaps %+v, want %+v", gaps, want)
	}
}

func rowByPath(t *testing.T, cat *catalog.DB) map[string]catalog.Segment {
	t.Helper()
	segs, err := cat.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]catalog.Segment{}
	for _, s := range segs {
		m[s.Path] = s
	}
	return m
}

func requireRow(t *testing.T, cat *catalog.DB, path string, startMs, endMs int64, encrypted bool, file string) {
	t.Helper()
	s, ok := rowByPath(t, cat)[path]
	if !ok {
		t.Fatalf("no catalog row for %s", path)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != catalog.StateComplete || s.StartMs != startMs || s.EndMs != endMs || s.Encrypted != encrypted || s.Bytes != info.Size() {
		t.Errorf("row %+v, want complete, %d–%d ms, encrypted %v, %d bytes", s, startMs, endMs, encrypted, info.Size())
	}
}

func requireMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("%s is still there", path)
	}
}

// listFiles returns every file under dir, relative to it, with its bytes.
func listFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = readFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// alignedTorn is an encrypted recording whose third part is cut after 13 of
// its 20 samples, at the end of the chunk that holds its moof, so the
// salvage lies in the last kept chunk and Reseal does it (C1-R7).
func alignedTorn(t *testing.T, scope crypto.KeyScope) (fmp4File, []byte) {
	t.Helper()
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 4800}, align: true})
	cut := int64(3 * crypto.ChunkSize)
	if f.partStart[2] != 2*crypto.ChunkSize || f.sampleEnd[2][12] > cut || f.sampleEnd[2][13] <= cut {
		t.Fatalf("the test recording's layout moved: part 2 at %d, samples 13 and 14 end at %d and %d", f.partStart[2], f.sampleEnd[2][12], f.sampleEnd[2][13])
	}
	return f, cutAge(t, f.data, scope, 3, 300)
}

// bigTorn is an encrypted recording whose third part, of 400 KB, is cut
// after 13 of its 20 samples, chunks after its moof, so the salvage needs a
// rewrite (C1-R9).
func bigTorn(t *testing.T, scope crypto.KeyScope) (fmp4File, []byte) {
	t.Helper()
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 20000}, align: true})
	cut := int64(6 * crypto.ChunkSize)
	if f.partStart[2] != 2*crypto.ChunkSize || f.sampleEnd[2][12] > cut || f.sampleEnd[2][13] <= cut {
		t.Fatalf("the test recording's layout moved: part 2 at %d, samples 13 and 14 end at %d and %d", f.partStart[2], f.sampleEnd[2][12], f.sampleEnd[2][13])
	}
	return f, cutAge(t, f.data, scope, 6, 300)
}

// TestReconcile_TornClearPart_TruncatesAndFinalizes (§4.5): a clear .part
// cut inside a moof is truncated at its last complete part, gets the mvhd
// duration and its final name, a catalog row and a restart gap.
func TestReconcile_TornClearPart_TruncatesAndFinalizes(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	part := segPath(recDir, "cam1", "main", t0, ".part")
	writeFile(t, part, f.data[:f.partStart[3]+10])

	report, err := Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	final := strings.TrimSuffix(part, ".part")
	requireMissing(t, part)
	data := readFile(t, final)
	if int64(len(data)) != f.partStart[3] {
		t.Errorf("the file has %d bytes, want %d: the end of its last complete part", len(data), f.partStart[3])
	}
	requireParts(t, parseRecording(t, data), f, []int{20, 20, 20})
	if d := mvhdDuration(t, data); d != 3000 {
		t.Errorf("mvhd duration %d, want 3000 ms", d)
	}
	if report != (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}) {
		t.Errorf("report %+v", report)
	}
	start := t0.UnixMilli()
	requireRow(t, cat, relSeg("cam1", "main", t0, ""), start, start+3000, false, final)
	requireGaps(t, cat, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: start + 3000, Reason: catalog.GapRestart}})
}

// TestReconcile_TornAgeTail_StaysDecryptable (Review Focus 3): an .age.part
// whose last chunk is torn is resealed and renamed; it decrypts with age (and
// with the age CLIs and rage when they are there), and its lost tail is one
// restart gap.
func TestReconcile_TornAgeTail_StaysDecryptable(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	scope, riG, rk := newTestScope(t)
	f, file := alignedTorn(t, scope)
	part := segPath(recDir, "cam1", "main", t0, ".age.part")
	writeFile(t, part, file)

	report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	final := strings.TrimSuffix(part, ".part")
	requireMissing(t, part)
	plain := decryptAll(t, readFile(t, final), rk)
	requireParts(t, parseRecording(t, plain), f, []int{20, 20, 13})
	decryptWithAgeTools(t, final, rk, plain)
	if report != (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}) {
		t.Errorf("report %+v", report)
	}
	start := t0.UnixMilli()
	requireRow(t, cat, relSeg("cam1", "main", t0, ".age"), start, start+2650, true, final)
	requireGaps(t, cat, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: start + 2650, Reason: catalog.GapRestart}})
}

// TestReconcile_PartialPart_KeepsWholeSamples (R01): of a last part with 13
// of its 20 samples durable, exactly those 13 are kept, and the file has no
// torn part, clear or encrypted.
func TestReconcile_PartialPart_KeepsWholeSamples(t *testing.T) {
	t.Run("clear", func(t *testing.T) {
		recDir, cat := t.TempDir(), openCatalog(t)
		f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
		part := segPath(recDir, "cam1", "main", t0, ".part")
		writeFile(t, part, f.data[:f.sampleEnd[2][12]+500])
		if _, err := Reconcile(t.Context(), recDir, nil, cat); err != nil {
			t.Fatal(err)
		}
		data := readFile(t, strings.TrimSuffix(part, ".part"))
		if int64(len(data)) != f.sampleEnd[2][12] {
			t.Errorf("%d bytes, want %d: the end of the 13th sample", len(data), f.sampleEnd[2][12])
		}
		requireParts(t, parseRecording(t, data), f, []int{20, 20, 13})
		if d := mvhdDuration(t, data); d != 2650 {
			t.Errorf("mvhd duration %d, want 2650 ms", d)
		}
	})
	t.Run("age", func(t *testing.T) {
		recDir, cat := t.TempDir(), openCatalog(t)
		scope, riG, rk := newTestScope(t)
		f, file := alignedTorn(t, scope)
		part := segPath(recDir, "cam1", "main", t0, ".age.part")
		writeFile(t, part, file)
		if _, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat); err != nil {
			t.Fatal(err)
		}
		plain := decryptAll(t, readFile(t, strings.TrimSuffix(part, ".part")), rk)
		if int64(len(plain)) != f.sampleEnd[2][12] {
			t.Errorf("%d bytes of plaintext, want %d: the end of the 13th sample", len(plain), f.sampleEnd[2][12])
		}
		requireParts(t, parseRecording(t, plain), f, []int{20, 20, 13})
	})
}

// TestReconcile_NewestFinalFileTorn_RepairedLikePart: the newest
// final-named segment of each stream is checked as a .part is, in case its
// last bytes never reached the disk: an .mp4.age whose final chunk is cut
// off and a clear .mp4 with a torn last mdat are repaired, keep their names
// and verify clean. An older torn final file is left alone, and verification
// counts it as unreadable.
func TestReconcile_NewestFinalFileTorn_RepairedLikePart(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	scope, riG, rk := newTestScope(t)
	fa := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}, align: true})
	closed := encryptFMP4(t, fa.data, scope, nil, false)
	older := segPath(recDir, "cam1", "main", t0, ".age")
	newer := segPath(recDir, "cam1", "main", t0.Add(10*time.Second), ".age")
	writeFile(t, older, closed[:len(closed)-1000])
	writeFile(t, newer, closed[:len(closed)-1000])
	fb := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	clearSeg := segPath(recDir, "cam1", "sub", t0.Add(10*time.Second), "")
	writeFile(t, clearSeg, fb.data[:fb.sampleEnd[2][5]+100])
	olderBytes := readFile(t, older)

	report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report.Truncated != 2 || report.Finalized != 0 || report.LostTails != 2 {
		t.Errorf("report %+v, want 2 truncated and 2 lost tails, none finalized", report)
	}
	requireParts(t, parseRecording(t, decryptAll(t, readFile(t, newer), rk)), fa, []int{20, 20})
	data := readFile(t, clearSeg)
	requireParts(t, parseRecording(t, data), fb, []int{20, 20, 6})
	if d := mvhdDuration(t, data); d != 2300 {
		t.Errorf("mvhd duration %d, want 2300 ms", d)
	}
	if !bytes.Equal(readFile(t, older), olderBytes) {
		t.Error("the older torn final file was changed")
	}
	start := t0.Add(10 * time.Second).UnixMilli()
	requireGaps(t, cat, []catalog.Gap{
		{Cam: "cam1", Profile: "main", StartMs: start + 2000, Reason: catalog.GapRestart},
		{Cam: "cam1", Profile: "sub", StartMs: start + 2300, Reason: catalog.GapRestart},
	})

	obs := &testObserver{}
	v := &verifier{recDir: recDir, cat: cat, recording: crypto.NewRecording(riG), obs: obs}
	vr, err := v.pass(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if vr.Unreadable != 1 || vr.TornParts != 0 || vr.CatalogMismatches != 0 {
		t.Errorf("verification %+v, want the older file unreadable and nothing else", vr)
	}
	if got := obs.snapshot().passedSeg; !slices.Equal(slices.Sorted(slices.Values(got)), []string{newer, clearSeg}) {
		t.Errorf("segments verified %q, want the two repaired ones", got)
	}
}

// TestReconcile_StalePartOlderThanAFinalSegment_GapEndsThere (G07): a .part
// that an earlier MediaMTX left behind, older than a final segment of the
// same stream, is repaired, and its restart gap ends where that segment
// starts, when recording had resumed, not at the first part after this
// start. Only the newest .part's gap stays open.
func TestReconcile_StalePartOlderThanAFinalSegment_GapEndsThere(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	stale := segPath(recDir, "cam1", "main", t0, ".part")
	writeFile(t, stale, f.data[:f.partStart[3]+10])
	later := t0.Add(5 * time.Minute)
	writeFile(t, segPath(recDir, "cam1", "main", later, ""), f.data)
	newest := t0.Add(10 * time.Minute)
	writeFile(t, segPath(recDir, "cam1", "main", newest, ".part"), f.data[:f.sampleEnd[1][9]])

	report, err := Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report != (ReconcileReport{Finalized: 2, Truncated: 2, LostTails: 2, GapsWritten: 2}) {
		t.Errorf("report %+v", report)
	}
	final := strings.TrimSuffix(stale, ".part")
	requireMissing(t, stale)
	requireParts(t, parseRecording(t, readFile(t, final)), f, []int{20, 20, 20})
	requireRow(t, cat, relSeg("cam1", "main", t0, ""), t0.UnixMilli(), t0.UnixMilli()+3000, false, final)
	requireGaps(t, cat, []catalog.Gap{
		{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli() + 3000, EndMs: later.UnixMilli(), Reason: catalog.GapRestart},
		{Cam: "cam1", Profile: "main", StartMs: newest.UnixMilli() + 1500, Reason: catalog.GapRestart},
	})
}

// TestReconcile_CompleteNewestAgeIsNotReadAtStartup (§4.5, MI2): the newest
// final .mp4.age of a stream that ends in its final chunk is complete, so the
// startup reconcile, which runs before MediaMTX starts, reads no more of it
// than OpenTorn's scan authenticates. A chunk in its middle that does not
// authenticate is neither read nor reported: walking its parts would decrypt
// a chunk per part (about 20 MB per 5-min segment) on the resume path, and
// would count the file as lost toward HUB-09's abort. The background
// verification reads it whole and reports it.
func TestReconcile_CompleteNewestAgeIsNotReadAtStartup(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	scope, riG, _ := newTestScope(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}, align: true})
	file := encryptFMP4(t, f.data, scope, nil, false)
	if f.partStart[1] != crypto.ChunkSize || len(f.data) != 3*crypto.ChunkSize {
		t.Fatalf("the test recording's layout moved: part 1 at %d, %d bytes", f.partStart[1], len(f.data))
	}
	// A byte of chunk 1, which holds part 1's moof, flipped.
	file[len(headerOf(t, file))+16+crypto.ChunkSize+16+100] ^= 0xff
	final := segPath(recDir, "cam1", "main", t0, ".age")
	writeFile(t, final, file)
	// Its hook gave it its row, as it gives every segment that closed.
	rel, start := relSeg("cam1", "main", t0, ".age"), t0.UnixMilli()
	if err := cat.PutSegment(t.Context(), catalog.Segment{Cam: "cam1", Profile: "main", Path: rel, StartMs: start, EndMs: start + 3000,
		Bytes: int64(len(file)), Encrypted: true, State: catalog.StateComplete}); err != nil {
		t.Fatal(err)
	}
	row := rowByPath(t, cat)[rel]

	report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report != (ReconcileReport{}) {
		t.Errorf("report %+v, want nothing: a complete file is not read at startup", report)
	}
	if !bytes.Equal(readFile(t, final), file) {
		t.Error("reconcile changed a complete file")
	}
	if got := rowByPath(t, cat)[rel]; got != row {
		t.Errorf("row %+v after reconcile, want %+v", got, row)
	}
	requireGaps(t, cat, nil)

	v := &verifier{recDir: recDir, cat: cat, recording: crypto.NewRecording(riG), obs: &testObserver{}}
	vr, err := v.pass(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if vr.Unreadable != 1 || vr.TornParts != 0 || vr.CatalogMismatches != 0 {
		t.Errorf("verification %+v, want the corrupt chunk found: 1 unreadable", vr)
	}
}

// TestReconcile_OverHalfLost_Aborts (HUB-09): when more than half of the
// segments would be lost, here 3 unreadable .part files and a row whose file
// is gone, of 7, reconcile changes nothing and reports Aborted: not a file,
// not a row, the one without its file included, not a gap.
func TestReconcile_OverHalfLost_Aborts(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	ctx := t.Context()
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
	writeFile(t, segPath(recDir, "cam1", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
	writeFile(t, segPath(recDir, "cam2", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
	for _, cam := range []string{"cam3", "cam4", "cam5"} {
		writeFile(t, segPath(recDir, cam, "main", t0, ".part"), bytes.Repeat([]byte("not an fMP4 file "), 300))
	}
	earlier := t0.Add(-5 * time.Minute)
	writeFile(t, segPath(recDir, "cam1", "main", earlier, ""), f.data)
	start := earlier.UnixMilli()
	for _, row := range []catalog.Segment{
		{Cam: "cam1", Profile: "main", Path: relSeg("cam1", "main", earlier, ""), StartMs: start, EndMs: start + 2000, Bytes: int64(len(f.data))},
		// Its file is gone: the abort leaves it complete, not lost.
		{Cam: "cam6", Profile: "main", Path: relSeg("cam6", "main", earlier, ""), StartMs: start, EndMs: start + 2000, Bytes: 12345},
	} {
		row.State = catalog.StateComplete
		if err := cat.PutSegment(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := cat.MarkVerified(ctx, relSeg("cam1", "main", earlier, ""), 42); err != nil {
		t.Fatal(err)
	}
	gap := catalog.Gap{Cam: "cam6", Profile: "main", StartMs: start + 2000, Reason: catalog.GapRestart}
	if err := cat.PutGap(ctx, gap); err != nil {
		t.Fatal(err)
	}
	beforeFiles, beforeRows := listFiles(t, recDir), rowByPath(t, cat)

	report, err := Reconcile(ctx, recDir, nil, cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !report.Aborted {
		t.Fatalf("report %+v, want Aborted", report)
	}
	if after := listFiles(t, recDir); !maps.EqualFunc(beforeFiles, after, bytes.Equal) {
		t.Error("an aborted reconcile changed the files")
	}
	afterRows := rowByPath(t, cat)
	if !maps.EqualFunc(beforeRows, afterRows, func(a, b catalog.Segment) bool {
		return a.ID == b.ID && a.Cam == b.Cam && a.Profile == b.Profile && a.Path == b.Path && a.StartMs == b.StartMs && a.EndMs == b.EndMs &&
			a.Bytes == b.Bytes && a.Encrypted == b.Encrypted && a.State == b.State && (a.VerifiedAtMs == nil) == (b.VerifiedAtMs == nil) &&
			(a.VerifiedAtMs == nil || *a.VerifiedAtMs == *b.VerifiedAtMs)
	}) {
		t.Errorf("an aborted reconcile changed the rows:\nbefore %+v\nafter  %+v", beforeRows, afterRows)
	}
	requireGaps(t, cat, []catalog.Gap{gap})
}

// TestReconcile_HalfLost_Repairs (HUB-09): exactly half, here 2 unreadable
// .part files of 4, is not more than half: reconcile repairs the others and
// reports the unreadable ones, which it sets aside under a name that is no
// segment's, their bytes as they were.
func TestReconcile_HalfLost_Repairs(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
	writeFile(t, segPath(recDir, "cam1", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
	writeFile(t, segPath(recDir, "cam2", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
	garbage := bytes.Repeat([]byte("not an fMP4 file "), 300)
	for _, cam := range []string{"cam3", "cam4"} {
		writeFile(t, segPath(recDir, cam, "main", t0, ".part"), garbage)
	}

	report, err := Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report.Aborted || report.Finalized != 2 || report.Unreadable != 2 {
		t.Errorf("report %+v, want 2 finalized and 2 unreadable, not aborted", report)
	}
	for _, cam := range []string{"cam3", "cam4"} {
		if got := readFile(t, segPath(recDir, cam, "main", t0, ".part.unreadable")); !bytes.Equal(got, garbage) {
			t.Errorf("%s: reconcile changed an unreadable file", cam)
		}
	}
}

// TestReconcile_UnreadablePartIsHandledOnce: an unreadable .part gets a
// restart gap from the start in its name, and is set aside as
// <name>.unreadable, the directory synced, so the next start neither
// counts it again as unreadable nor toward HUB-09's lost segments, nor
// writes its gap again.
func TestReconcile_UnreadablePartIsHandledOnce(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	var synced []string
	defer setSyncDir(func(dir string) error {
		synced = append(synced, dir)
		return nil
	})()
	// Two good segments, so that one lost file of three is no abort.
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
	writeFile(t, segPath(recDir, "cam1", "main", t0.Add(-5*time.Minute), ""), f.data)
	writeFile(t, segPath(recDir, "cam2", "main", t0, ""), f.data)
	part := segPath(recDir, "cam1", "main", t0, ".part")
	garbage := bytes.Repeat([]byte("not an fMP4 file "), 300)
	writeFile(t, part, garbage)

	report, err := Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("the first Reconcile: %v", err)
	}
	if report != (ReconcileReport{Unreadable: 1, LostTails: 1, GapsWritten: 1}) {
		t.Errorf("the first report %+v, want 1 unreadable with its gap", report)
	}
	requireMissing(t, part)
	if got := readFile(t, part+".unreadable"); !bytes.Equal(got, garbage) {
		t.Error("the unreadable file was changed")
	}
	if !slices.Contains(synced, filepath.Dir(part)) {
		t.Errorf("the directory of the unreadable file was not synced (synced: %q)", synced)
	}
	wantGaps := []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli(), Reason: catalog.GapRestart}}
	requireGaps(t, cat, wantGaps)

	report, err = Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("the second Reconcile: %v", err)
	}
	if report != (ReconcileReport{}) {
		t.Errorf("the second report %+v, want nothing: the file was set aside", report)
	}
	requireGaps(t, cat, wantGaps)
}

// TestReconcile_StaleTornPartThenUnreadablePart_GapEndsAtTheNextReadable: a
// restart gap ends where the stream's next readable segment starts. An
// unreadable .part after a torn one holds no recording, so the torn one's
// gap runs past it, and its own gap runs from the start in its name to the
// same segment.
func TestReconcile_StaleTornPartThenUnreadablePart_GapEndsAtTheNextReadable(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	writeFile(t, segPath(recDir, "cam1", "main", t0, ".part"), f.data[:f.partStart[3]+10])
	unreadable := t0.Add(5 * time.Minute)
	writeFile(t, segPath(recDir, "cam1", "main", unreadable, ".part"), bytes.Repeat([]byte("not an fMP4 file "), 300))
	readable := t0.Add(10 * time.Minute)
	writeFile(t, segPath(recDir, "cam1", "main", readable, ".part"), f.data[:f.sampleEnd[1][9]])

	report, err := Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report != (ReconcileReport{Finalized: 2, Truncated: 2, LostTails: 3, GapsWritten: 3, Unreadable: 1}) {
		t.Errorf("report %+v", report)
	}
	requireGaps(t, cat, []catalog.Gap{
		{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli() + 3000, EndMs: readable.UnixMilli(), Reason: catalog.GapRestart},
		{Cam: "cam1", Profile: "main", StartMs: unreadable.UnixMilli(), EndMs: readable.UnixMilli(), Reason: catalog.GapRestart},
		{Cam: "cam1", Profile: "main", StartMs: readable.UnixMilli() + 1500, Reason: catalog.GapRestart},
	})
}

// damageMoof makes the trun of part p of f unparsable: its sample count says
// more entries than the box holds.
func damageMoof(t *testing.T, f fmp4File, p int) []byte {
	t.Helper()
	data := bytes.Clone(f.data)
	at := bytes.Index(data[f.partStart[p]:], []byte("trun"))
	if at < 0 {
		t.Fatalf("no trun in part %d", p)
	}
	binary.BigEndian.PutUint32(data[f.partStart[p]+int64(at)+8:], 0x00ffffff)
	return data
}

// TestReconcile_DamagedLastPartIsTail (M4): bytes that cannot be parsed at
// the end of a recording are a torn tail, as a cut leaves one: a last part
// whose moof is unparsable, or whose mdat header is zeros, as NTFS leaves
// after the valid data of a file, loses only itself. Every complete part
// before it is kept, clear or encrypted. The same damage in a part that
// another part follows is no tail: that file is unreadable.
func TestReconcile_DamagedLastPartIsTail(t *testing.T) {
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}, align: true})
	zeroMdat := bytes.Clone(f.data)
	mdat := f.sampleEnd[2][0] - int64(len(f.payloads[2][0])) - 8
	if string(zeroMdat[mdat+4:mdat+8]) != "mdat" {
		t.Fatalf("no mdat header at %d", mdat)
	}
	clear(zeroMdat[mdat:])
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"unparsable moof", damageMoof(t, f, 2)},
		{"zero mdat", zeroMdat},
	} {
		t.Run(tc.name+"/clear", func(t *testing.T) {
			recDir, cat := t.TempDir(), openCatalog(t)
			part := segPath(recDir, "cam1", "main", t0, ".part")
			writeFile(t, part, tc.data)
			report, err := Reconcile(t.Context(), recDir, nil, cat)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if report != (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}) {
				t.Errorf("report %+v", report)
			}
			data := readFile(t, strings.TrimSuffix(part, ".part"))
			requireParts(t, parseRecording(t, data), f, []int{20, 20})
			if d := mvhdDuration(t, data); d != 2000 {
				t.Errorf("mvhd duration %d, want 2000 ms", d)
			}
			requireGaps(t, cat, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli() + 2000, Reason: catalog.GapRestart}})
		})
		t.Run(tc.name+"/age", func(t *testing.T) {
			recDir, cat := t.TempDir(), openCatalog(t)
			scope, riG, rk := newTestScope(t)
			part := segPath(recDir, "cam1", "main", t0, ".age.part")
			writeFile(t, part, encryptFMP4(t, tc.data, scope, f.partEnd, true))
			report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if report != (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}) {
				t.Errorf("report %+v", report)
			}
			requireParts(t, parseRecording(t, decryptAll(t, readFile(t, strings.TrimSuffix(part, ".part")), rk)), f, []int{20, 20})
			requireGaps(t, cat, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli() + 2000, Reason: catalog.GapRestart}})
		})
	}
	t.Run("damage before the last part", func(t *testing.T) {
		recDir, cat := t.TempDir(), openCatalog(t)
		writeFile(t, segPath(recDir, "cam1", "main", t0.Add(-5*time.Minute), ""), f.data) // so that it is not HUB-09's abort
		part := segPath(recDir, "cam1", "main", t0, ".part")
		damaged := damageMoof(t, f, 1)
		writeFile(t, part, damaged)
		report, err := Reconcile(t.Context(), recDir, nil, cat)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if report.Unreadable != 1 || report.Truncated != 0 || report.Finalized != 0 {
			t.Errorf("report %+v, want the file unreadable, not cut", report)
		}
		if got := readFile(t, part+".unreadable"); !bytes.Equal(got, damaged) {
			t.Error("the unreadable file was changed")
		}
	})
}

// TestReconcile_ZeroFilledPartIsEmpty (M4): a .part whose bytes are all
// zeros, as NTFS can leave after a cut, holds no recording: it is removed,
// as an empty one is, and its stream gets a restart gap from the start in
// its name. Clear or encrypted, an .age.part without a byte, and a clear one
// whose zeros follow its ftyp, where its moov should be.
func TestReconcile_ZeroFilledPartIsEmpty(t *testing.T) {
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000}})
	ftyp := int64(binary.BigEndian.Uint32(f.data))
	for _, tc := range []struct {
		name, suffix string
		data         []byte
	}{
		{"clear", ".part", make([]byte, 200_000)},
		{"age", ".age.part", make([]byte, 200_000)},
		{"age without a byte", ".age.part", nil},
		{"zeros after the ftyp", ".part", slices.Concat(f.data[:ftyp], make([]byte, 200_000))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recDir, cat := t.TempDir(), openCatalog(t)
			_, riG, _ := newTestScope(t)
			part := segPath(recDir, "cam1", "main", t0, tc.suffix)
			writeFile(t, part, tc.data)
			report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if report != (ReconcileReport{LostTails: 1, GapsWritten: 1}) {
				t.Errorf("report %+v, want the file removed with its gap", report)
			}
			if got := listFiles(t, recDir); len(got) != 0 {
				t.Errorf("files after reconcile %q, want none", slices.Sorted(maps.Keys(got)))
			}
			requireGaps(t, cat, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli(), Reason: catalog.GapRestart}})
		})
	}
}

// TestParseSegment_RepeatedHourIsOneInstant: when daylight saving ends in
// Romania (2026-10-25, 04:00 EEST back to 03:00 EET), 03:30 local time names
// two instants an hour apart. Segment names are UTC (MTX-4, TZ=UTC), and
// parseSegment reads them so whatever the host's zone, here Romania's, so
// each name stands for one instant.
func TestParseSegment_RepeatedHourIsOneInstant(t *testing.T) {
	recDir := t.TempDir()
	first := time.Date(2026, 10, 25, 0, 30, 0, 250_000_000, time.UTC) // 03:30 EEST
	for _, want := range []time.Time{first, first.Add(time.Hour)} {   // 03:30 EET
		if h, m, _ := want.In(time.Local).Clock(); h != 3 || m != 30 {
			t.Fatalf("%v is %02d:%02d in the tests' zone, not 03:30 of the repeated hour", want, h, m)
		}
		rel := "cam1/main/" + segmentName(want) + ".part"
		s, ok := parseSegment(recDir, rel)
		if !ok {
			t.Fatalf("%s is not a segment", rel)
		}
		if !s.start.Equal(want) {
			t.Errorf("%s starts at %v, want %v", rel, s.start.UTC(), want)
		}
	}
}

// failingSealer opens torn recordings with sealer, but every Reseal of the
// file named failOn fails, as a write to a failing disk would.
type failingSealer struct {
	crypto.TailSealer
	failOn string
}

func (s failingSealer) OpenTorn(ctx context.Context, f *os.File) (crypto.TornAge, error) {
	torn, err := s.TailSealer.OpenTorn(ctx, f)
	if err != nil || filepath.Base(f.Name()) != s.failOn {
		return torn, err
	}
	return failingReseal{torn}, nil
}

type failingReseal struct{ crypto.TornAge }

func (failingReseal) Reseal(context.Context, int64, []crypto.Patch) (int64, error) {
	return 0, errors.New("injected: the disk refused the write")
}

// TestRecorder_OneFileFailingRepairDoesNotStopRecording: a repair that
// fails on one of three files, because the file cannot be opened or its
// Reseal fails, is handled like an unreadable file: the file is left, it
// counts as unreadable, an error names it by its base name only, and the
// other two are repaired and recording starts. A file that could be read
// has its restart gap written before its repair begins (M3), so a failed
// Reseal leaves that gap too.
func TestRecorder_OneFileFailingRepairDoesNotStopRecording(t *testing.T) {
	scope, riG, _ := newTestScope(t)
	for _, tc := range []struct {
		name, suffix string
		gaps         int
		file         func(t *testing.T) []byte
		inject       func(failOn string) (cfg Config, restore func())
	}{
		{"open", ".part", 2, func(t *testing.T) []byte {
			f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
			return f.data[:f.partStart[2]+10]
		}, func(failOn string) (Config, func()) {
			return Config{}, setOpenSegment(func(path string, flag int) (*os.File, error) {
				if filepath.Base(path) == failOn {
					return nil, &fs.PathError{Op: "open", Path: path, Err: errors.New("injected: input/output error")}
				}
				return os.OpenFile(path, flag, 0) //nolint:gosec // a file of the test
			})
		}},
		{"reseal", ".age.part", 3, func(t *testing.T) []byte {
			_, file := alignedTorn(t, scope)
			return file
		}, func(failOn string) (Config, func()) {
			return Config{Tail: failingSealer{crypto.NewTailSealer(riG), failOn}}, func() {}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recDir := filepath.Join(t.TempDir(), "rec")
			var paths []string
			for i, cam := range []string{"cam1", "cam2", "cam3"} {
				path := segPath(recDir, cam, "main", t0.Add(time.Duration(i)*time.Second), tc.suffix)
				writeFile(t, path, tc.file(t))
				paths = append(paths, path)
			}
			failing := paths[1]
			failingBytes := readFile(t, failing)
			cfg, restore := tc.inject(filepath.Base(failing))
			defer restore()
			obs := &testObserver{}
			cfg.RecDir, cfg.Observer = recDir, obs
			dump := filepath.Join(t.TempDir(), "child.json")
			r, _ := startFake(t, "dump:"+dump, cfg)
			readDump(t, dump) // recording started: the child runs

			if rep := reportNow(r); rep.Finalized != 2 || rep.Truncated != 2 || rep.LostTails != tc.gaps || rep.GapsWritten != tc.gaps || rep.Unreadable != 1 || rep.Aborted {
				t.Errorf("startup report %+v, want 2 repaired, 1 unreadable and %d gaps", rep, tc.gaps)
			}
			for _, path := range []string{paths[0], paths[2]} {
				requireMissing(t, path)
				if _, err := os.Stat(strings.TrimSuffix(path, ".part")); err != nil {
					t.Errorf("%s was not repaired: %v", path, err)
				}
			}
			if !bytes.Equal(readFile(t, failing), failingBytes) {
				t.Error("the file whose repair failed was changed or moved")
			}
			errs, named := obs.snapshot().errs, false
			for _, e := range errs {
				if strings.Contains(e, filepath.Base(failing)) {
					named = true
					if strings.Contains(e, filepath.Dir(failing)) {
						t.Errorf("the error %q names the file's directory", e)
					}
				}
			}
			if !named {
				t.Errorf("errors %q, want one that names %s", errs, filepath.Base(failing))
			}
		})
	}
}

// TestReconcile_FailedRepair_OneGapFromTheFilesStart (7a M1): a repair that
// fails after its restart gap is written (a refused Reseal, as on a failing
// card) leaves the file as it was, so its gap starts at the file's start,
// where the recording that can be read ends; every later start that fails
// the same way keeps that one gap, and so does one cut between the gap's
// write and the repair. The start that finally repairs the file moves the
// gap to where the kept recording ends.
func TestReconcile_FailedRepair_OneGapFromTheFilesStart(t *testing.T) {
	scope, riG, _ := newTestScope(t)
	start := t0.UnixMilli()
	fromStart := []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: start, Reason: catalog.GapRestart}}
	afterKept := []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: start + 2650, Reason: catalog.GapRestart}}
	for _, tc := range []struct {
		name  string
		prior []catalog.Gap // what an earlier start left
	}{
		{"first start", nil},
		{"a start cut between the gap and the repair", afterKept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recDir, cat := t.TempDir(), openCatalog(t)
			_, file := alignedTorn(t, scope)
			part := segPath(recDir, "cam1", "main", t0, ".age.part")
			writeFile(t, part, file)
			for _, g := range tc.prior {
				if err := cat.PutGap(t.Context(), g); err != nil {
					t.Fatal(err)
				}
			}
			failing := failingSealer{crypto.NewTailSealer(riG), filepath.Base(part)}
			wantWritten := 1
			if tc.prior != nil {
				wantWritten = 0
			}
			for i := range 2 {
				report, err := Reconcile(t.Context(), recDir, failing, cat)
				if err != nil {
					t.Fatalf("Reconcile %d: %v", i+1, err)
				}
				if want := (ReconcileReport{LostTails: 1, GapsWritten: wantWritten, Unreadable: 1}); report != want {
					t.Errorf("start %d: report %+v, want %+v", i+1, report, want)
				}
				if !bytes.Equal(readFile(t, part), file) {
					t.Fatalf("start %d changed the file whose repair failed", i+1)
				}
				requireGaps(t, cat, fromStart)
				wantWritten = 0
			}

			report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
			if err != nil {
				t.Fatalf("Reconcile that repairs: %v", err)
			}
			if want := (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1}); report != want {
				t.Errorf("the repairing start's report %+v, want %+v", report, want)
			}
			requireMissing(t, part)
			requireGaps(t, cat, afterKept)
		})
	}
}

// TestRecorder_LiveRepairFails_GapFromTheFilesStart (7a M1): the repair of
// a .part that a dead MediaMTX left, while the hub runs, that fails after
// its gap is written leaves the gap at the file's start, and the next
// start, failing the same way, writes no second gap.
func TestRecorder_LiveRepairFails_GapFromTheFilesStart(t *testing.T) {
	scope, riG, _ := newTestScope(t)
	_, file := alignedTorn(t, scope)
	deadStart := time.Now().Add(-10 * time.Second)
	obs := &testObserver{}
	failOn := filepath.Base(segPath("", "cam1", "main", deadStart, ".age.part"))
	r, cfg := startFake(t, "sleep", Config{Observer: obs, Tail: failingSealer{crypto.NewTailSealer(riG), failOn}})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, segPath(cfg.RecDir, "cam1", "main", deadStart, ".age.part"), file)
	killChild(t, r)

	if rep := waitFinalReport(t, obs); rep != (ReconcileReport{LostTails: 1, GapsWritten: 1, Unreadable: 1}) {
		t.Errorf("report after the failed repair %+v, want its gap and the file unreadable", rep)
	}
	want := []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: deadStart.UnixMilli(), Reason: catalog.GapRestart}}
	requireGaps(t, cfg.Catalog, want)

	if _, err := Reconcile(t.Context(), cfg.RecDir, cfg.Tail, cfg.Catalog); err != nil {
		t.Fatalf("the next start's Reconcile: %v", err)
	}
	requireGaps(t, cfg.Catalog, want)
}

// TestReconcile_AgeTailOnlyInMemory (G08): reconcile decrypts and seals torn
// tails in RAM; it writes no temporary file, in TMPDIR or in the hub's tmp/.
func TestReconcile_AgeTailOnlyInMemory(t *testing.T) {
	tmp := t.TempDir()
	for _, env := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(env, tmp)
	}
	data := t.TempDir()
	recDir, hubTmp := filepath.Join(data, "rec"), filepath.Join(data, "tmp")
	if err := os.MkdirAll(hubTmp, 0o700); err != nil {
		t.Fatal(err)
	}
	cat := openCatalog(t)
	scope, riG, _ := newTestScope(t)
	_, resealed := alignedTorn(t, scope)
	_, interim := bigTorn(t, scope)
	writeFile(t, segPath(recDir, "cam1", "main", t0, ".age.part"), resealed)
	writeFile(t, segPath(recDir, "cam2", "main", t0, ".age.part"), interim)

	if _, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, dir := range []string{tmp, hubTmp} {
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			t.Errorf("%s holds %d entries after reconcile (%v)", dir, len(entries), err)
		}
	}
	got := slices.Sorted(maps.Keys(listFiles(t, recDir)))
	want := []string{relSeg("cam1", "main", t0, ".age"), relSeg("cam2", "main", t0, ".age")}
	if !slices.Equal(got, want) {
		t.Errorf("files after reconcile %q, want only %q", got, want)
	}
}

// TestReconcile_CatalogMatchesFilesAfterwards (G07): after a torn-tail
// reconcile, the background verification finds the catalog and the files in
// agreement; once a file is deleted, the next verification counts one
// mismatch.
func TestReconcile_CatalogMatchesFilesAfterwards(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	part := segPath(recDir, "cam1", "main", t0, ".part")
	writeFile(t, part, f.data[:f.sampleEnd[2][3]+7])
	obs := &testObserver{}
	dump := filepath.Join(t.TempDir(), "child.json")
	r, cfg := startFake(t, "dump:"+dump, Config{RecDir: recDir, Observer: obs})

	waitFor(t, 10*time.Second, "no verification after startup", func() bool { return len(obs.snapshot().verified) >= 1 })
	if v := obs.snapshot().verified[0]; v.CatalogMismatches != 0 || v.Unreadable != 0 || v.TornParts != 0 {
		t.Fatalf("the verification after startup reported %+v, want all zero", v)
	}
	if err := os.Remove(strings.TrimSuffix(part, ".part")); err != nil {
		t.Fatal(err)
	}
	next := segPath(cfg.RecDir, "cam1", "sub", t0.Add(time.Minute), "")
	writeFile(t, next, f.data)
	env := hookEnv(readDump(t, dump), "cam1", "sub", next, t0.Add(time.Minute), "3")
	if err := PostSegmentComplete(t.Context(), func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	receive(t, r)
	waitFor(t, 10*time.Second, "no verification after the segment closed", func() bool { return len(obs.snapshot().verified) >= 2 })
	if v := obs.snapshot().verified[1]; v.CatalogMismatches != 1 {
		t.Errorf("the verification after the deletion reported %+v, want 1 catalog mismatch", v)
	}
}

// TestReconcile_RewriteRunsAfterResume (C1-R9): a salvage that changes bytes
// chunks before keep is not resealed in place. Reconcile leaves the file cut
// at its last complete part under its final name, a valid interim state, and
// recording resumes at once; then, in the background, the salvaged plaintext
// is rewritten under a fresh file key over the interim file, and the catalog
// row, the gap and the report follow.
func TestReconcile_RewriteRunsAfterResume(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	scope, riG, rk := newTestScope(t)
	f, file := bigTorn(t, scope)
	part := segPath(recDir, "cam1", "main", t0, ".age.part")
	writeFile(t, part, file)
	final := strings.TrimSuffix(part, ".part")

	started := make(chan time.Time, 1)
	release := make(chan struct{})
	defer setRewriteHook(func() {
		started <- time.Now()
		<-release
	})()
	obs := &testObserver{}
	dump := filepath.Join(t.TempDir(), "child.json")
	r, cfg := startFake(t, "dump:"+dump, Config{RecDir: recDir, Tail: crypto.NewTailSealer(riG), Recording: crypto.NewRecording(riG), Observer: obs})

	// Resumed: the child runs, with the interim file under its final name.
	readDump(t, dump)
	interim := readFile(t, final)
	if got := decryptAll(t, interim, rk); !bytes.Equal(got, f.data[:f.partEnd[1]]) {
		t.Fatal("the interim file does not decrypt to the recording up to its last complete part")
	}
	start := t0.UnixMilli()
	requireGaps(t, cfg.Catalog, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: start + 2000, Reason: catalog.GapRestart}})
	select {
	case <-started:
		t.Fatal("the rewrite started before a job gave it the stream's recipients")
	case <-time.After(300 * time.Millisecond):
	}

	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", &scope)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the rewrite did not start")
	}
	close(release)
	waitFor(t, 10*time.Second, "no report after the rewrite", func() bool { return len(obs.snapshot().reports) >= 1 })

	rewritten := readFile(t, final)
	requireParts(t, parseRecording(t, decryptAll(t, rewritten, rk)), f, []int{20, 20, 13})
	requireParts(t, parseRecording(t, decryptAll(t, rewritten, riG)), f, []int{20, 20, 13})
	oldKey, err := age.DecryptHeader(headerOf(t, interim), riG)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := age.DecryptHeader(headerOf(t, rewritten), riG)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldKey, newKey) {
		t.Error("the rewrite kept the file key")
	}
	requireMissing(t, final+".rewrite")
	requireRow(t, cfg.Catalog, relSeg("cam1", "main", t0, ".age"), start, start+2650, true, final)
	requireGaps(t, cfg.Catalog, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: start + 2650, Reason: catalog.GapRestart}})
	if rep := obs.snapshot().reports[0]; rep.Rewritten != 1 || rep.Finalized != 1 {
		t.Errorf("the report after the rewrite %+v, want 1 finalized and 1 rewritten", rep)
	}
}

// TestReconcile_RewriteWaitsForVerificationToCloseTheFile: the rewrite
// renames over the interim file only once a verification that has it open
// is done with it: Windows refuses to rename over a file that is open.
func TestReconcile_RewriteWaitsForVerificationToCloseTheFile(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	scope, riG, rk := newTestScope(t)
	f, file := bigTorn(t, scope)
	part := segPath(recDir, "cam1", "main", t0, ".age.part")
	writeFile(t, part, file)
	final := strings.TrimSuffix(part, ".part")

	// The hooks stay until the recorder has stopped (cleanups run last
	// first); the verification is released before that, when the test
	// returns.
	opened := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(setVerifyOpenHook(func(path string) {
		if path == final {
			once.Do(func() {
				close(opened)
				<-release
			})
		}
	}))
	defer once.Do(func() {}) // after a failure before the verification opened the file
	defer close(release)
	rewriting := make(chan struct{}, 1)
	t.Cleanup(setRewriteHook(func() { rewriting <- struct{}{} }))
	obs := &testObserver{}
	r, _ := startFake(t, "sleep", Config{RecDir: recDir, Tail: crypto.NewTailSealer(riG), Recording: crypto.NewRecording(riG), Observer: obs})

	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("the verification did not open the interim file")
	}
	interim := readFile(t, final)
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", &scope)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rewriting:
	case <-time.After(10 * time.Second):
		t.Fatal("the rewrite did not start")
	}
	time.Sleep(500 * time.Millisecond)
	if !bytes.Equal(readFile(t, final), interim) {
		t.Fatal("the rewrite renamed over the interim file while a verification had it open")
	}
	release <- struct{}{}
	waitFor(t, 10*time.Second, "no report after the rewrite", func() bool { return len(obs.snapshot().reports) >= 1 })
	requireParts(t, parseRecording(t, decryptAll(t, readFile(t, final), rk)), f, []int{20, 20, 13})
	requireMissing(t, final+".rewrite")
}

func headerOf(t *testing.T, file []byte) []byte {
	t.Helper()
	hdr, err := age.ExtractHeader(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	return hdr
}

// TestReconcile_CutDuringRewriteLeavesInterim (C1-R9): a cut during the
// background rewrite leaves the interim file valid and a partial sibling,
// which the next reconcile removes; the interim file is complete, so it is
// never resealed or rewritten.
func TestReconcile_CutDuringRewriteLeavesInterim(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	scope, riG, rk := newTestScope(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}, align: true})
	final := segPath(recDir, "cam1", "main", t0, ".age")
	closed := encryptFMP4(t, f.data, scope, nil, false)
	writeFile(t, final, closed)
	writeFile(t, final+".rewrite", closed[:5000])

	report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
	if err != nil {
		t.Fatal(err)
	}
	requireMissing(t, final+".rewrite")
	if !bytes.Equal(readFile(t, final), closed) {
		t.Error("the interim file was changed")
	}
	requireParts(t, parseRecording(t, decryptAll(t, readFile(t, final), rk)), f, []int{20, 20})
	if report.Truncated != 0 || report.LostTails != 0 {
		t.Errorf("report %+v, want nothing truncated", report)
	}
	requireGaps(t, cat, nil)
}

// TestReconcile_CatalogsFinalFilesWithoutRows: a segment that was renamed
// but never reached the catalog (the directory sync after its rename failed,
// so MediaMTX ran no hook, or the hub stopped first) gets a row, with its
// start from its name and its end from its contents.
func TestReconcile_CatalogsFinalFilesWithoutRows(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	scope, riG, _ := newTestScope(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	clearSeg := segPath(recDir, "cam1", "main", t0, "")
	writeFile(t, clearSeg, f.data)
	encrypted := segPath(recDir, "cam1", "sub", t0, ".age")
	writeFile(t, encrypted, encryptFMP4(t, f.data, scope, nil, false))
	newer := segPath(recDir, "cam1", "sub", t0.Add(5*time.Minute), ".age")
	writeFile(t, newer, encryptFMP4(t, f.data, scope, nil, false))

	report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
	if err != nil {
		t.Fatal(err)
	}
	if report != (ReconcileReport{}) {
		t.Errorf("report %+v, want nothing repaired", report)
	}
	start := t0.UnixMilli()
	requireRow(t, cat, relSeg("cam1", "main", t0, ""), start, start+3000, false, clearSeg)
	requireRow(t, cat, relSeg("cam1", "sub", t0, ".age"), start, start+3000, true, encrypted)
	later := t0.Add(5 * time.Minute).UnixMilli()
	requireRow(t, cat, relSeg("cam1", "sub", t0.Add(5*time.Minute), ".age"), later, later+3000, true, newer)
	requireGaps(t, cat, nil)
}

// TestReconcile_AfterTheHubIsKilled: the real MediaMTX records a clear and
// an encrypted stream for a process that is then killed, as MI2 kills
// ipcam-hub; MediaMTX dies with it (C07), leaving torn .part files. Reconcile
// gives both their final names, and the verification finds them readable,
// with no torn part and the catalog in agreement; each stream gets one
// restart gap.
func TestReconcile_AfterTheHubIsKilled(t *testing.T) {
	bin := rtsptest.MediaMTX(t)
	srv := rtsptest.Start(t, rtsptest.MainH264(t))
	scope, riG, rk := newTestScope(t)
	recipients, err := scope.Strings()
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := json.Marshal([]helperJob{
		{Cam: "cam1", Profile: "main", URL: srv.URL("cam1/main"), Recipients: recipients[:]},
		{Cam: "cam1", Profile: "sub", URL: srv.URL("cam1/sub")},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	recDir := filepath.Join(dir, "rec")
	helper := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // the test binary itself
	helper.Env = append(withoutEnv(os.Environ(), fakeChildEnv), helperEnv+"="+dir, helperBinEnv+"="+bin, helperJobsEnv+"="+string(jobs))
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = helper.Process.Kill()
		_ = helper.Wait()
	}()
	for _, p := range []string{"main", "sub"} {
		waitFor(t, 30*time.Second, "no growing .part for cam1/"+p, func() bool {
			_, size, ok := newestPart(filepath.Join(recDir, "cam1", p))
			return ok && size > 600_000
		})
	}
	pidFile, err := os.ReadFile(filepath.Join(dir, "mediamtx.pid")) //nolint:gosec // a file in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidFile))
	if err != nil || pid == 0 {
		t.Fatalf("MediaMTX's PID %q: %v", pidFile, err)
	}
	time.Sleep(1500 * time.Millisecond) // into a part that is not durable yet
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
	// MediaMTX dies with it, and only then has it closed its files.
	waitFor(t, 5*time.Second, "MediaMTX outlived the hub", func() bool { return !processAlive(t, pid) })

	cat, err := catalog.Open(t.Context(), filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cat.Close() }()
	report, err := Reconcile(t.Context(), recDir, crypto.NewTailSealer(riG), cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report.Finalized != 2 || report.LostTails != 2 || report.Unreadable != 0 {
		t.Errorf("report %+v, want 2 finalized with 2 lost tails", report)
	}
	l, err := scanRecDir(recDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range l.segs {
		if s.part {
			t.Errorf("%s is still a .part", s.rel)
		}
		if s.encrypted {
			decryptAll(t, readFile(t, s.abs), rk)
		}
	}
	v := &verifier{recDir: recDir, cat: cat, recording: crypto.NewRecording(riG), obs: &testObserver{}}
	if vr, err := v.pass(t.Context()); err != nil || vr != (ReconcileReport{}) {
		t.Errorf("verification after reconcile: %+v, %v; want all zero", vr, err)
	}
	gaps, err := cat.Gaps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 2 || gaps[0].Reason != catalog.GapRestart || gaps[1].Reason != catalog.GapRestart {
		t.Errorf("gaps %+v, want one restart gap per stream", gaps)
	}
	t.Logf("reconcile %+v", report)
}

// killChild kills the recorder's MediaMTX child.
func killChild(t *testing.T, r *rec) {
	t.Helper()
	pid := r.childPID()
	if pid == 0 {
		t.Fatal("no child runs")
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
}

func reportNow(r *rec) ReconcileReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.report
}

// TestRecorder_RepairsTheDeadChildsPartsWhileTheHubRuns (§4.5, G07): when
// the MediaMTX child dies while the hub runs, the .part files it left are
// repaired in the background once it is gone, as reconcile repairs them at
// startup (final name, catalog row, restart gap), without waiting for the
// next hub start. The gap ends where recording resumed, at the restarted
// child's first part. A .part that the startup reconcile could not read was
// set aside then, and is not counted again.
func TestRecorder_RepairsTheDeadChildsPartsWhileTheHubRuns(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	writeFile(t, segPath(recDir, "cam2", "main", t0, ""), f.data)
	garbage := bytes.Repeat([]byte("not an fMP4 file "), 300)
	unreadable := segPath(recDir, "cam3", "main", t0, ".part")
	writeFile(t, unreadable, garbage)
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{RecDir: recDir, Observer: obs})
	if rep := reportNow(r); rep != (ReconcileReport{LostTails: 1, GapsWritten: 1, Unreadable: 1}) {
		t.Fatalf("startup report %+v, want 1 unreadable with its gap", rep)
	}
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}

	// The child writes a segment and dies in its fourth part.
	deadStart := time.Now().Add(-10 * time.Second)
	dead := segPath(recDir, "cam1", "main", deadStart, ".part")
	writeFile(t, dead, f.data[:f.partStart[3]+10])
	killChild(t, r)
	waitFor(t, 10*time.Second, "the child was not started again", func() bool { return len(obs.snapshot().restarts) >= 1 })
	// The new child starts a segment.
	nextStart := time.Now()
	next := segPath(recDir, "cam1", "main", nextStart, ".part")
	writeFile(t, next, make([]byte, 100))

	deadMs := deadStart.UnixMilli()
	want := []catalog.Gap{
		{Cam: "cam1", Profile: "main", StartMs: deadMs + 3000, EndMs: nextStart.UnixMilli(), Reason: catalog.GapRestart},
		{Cam: "cam3", Profile: "main", StartMs: t0.UnixMilli(), Reason: catalog.GapRestart},
	}
	// The final report comes once the batch's repairs are done; the gap is
	// written before the file changes, so it alone says nothing about the file.
	wantReport := ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 2, GapsWritten: 2, Unreadable: 1}
	if rep := waitFinalReport(t, obs); rep != wantReport {
		t.Errorf("report after the repair %+v, want %+v", rep, wantReport)
	}
	waitFor(t, 10*time.Second, "the dead child's .part has no restart gap that ends at the new segment", func() bool {
		gaps, err := cfg.Catalog.Gaps(t.Context())
		return err == nil && slices.Equal(gaps, want)
	})
	final := strings.TrimSuffix(dead, ".part")
	requireMissing(t, dead)
	requireParts(t, parseRecording(t, readFile(t, final)), f, []int{20, 20, 20})
	requireRow(t, cfg.Catalog, relSeg("cam1", "main", deadStart, ""), deadMs, deadMs+3000, false, final)
	if !bytes.Equal(readFile(t, unreadable+".unreadable"), garbage) {
		t.Error("the unreadable .part was changed")
	}
	if got := readFile(t, next); len(got) != 100 {
		t.Errorf("the new child's .part has %d bytes, want its 100", len(got))
	}
}

// TestRecorder_RepairsAPartLeftBehindWhileTheChildRuns (MTX-1, C1-R9): when
// a write or an fsync fails, MediaMTX's recorder closes the segment under its
// .part name and starts again, in the same process, with a new segment. Once
// that newer .part is there, the older one is no longer written: it is
// repaired while the child runs, its restart gap ends where the newer
// segment starts, and a salvage that needs a rewrite gets it. The newer
// .part is left alone, and the child is not restarted.
func TestRecorder_RepairsAPartLeftBehindWhileTheChildRuns(t *testing.T) {
	scope, riG, rk := newTestScope(t)
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{Tail: crypto.NewTailSealer(riG), Recording: crypto.NewRecording(riG), Observer: obs})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", &scope)}); err != nil {
		t.Fatal(err)
	}

	f, file := bigTorn(t, scope)
	leftStart := time.Now().Add(-10 * time.Second)
	left := segPath(cfg.RecDir, "cam1", "main", leftStart, ".age.part")
	writeFile(t, left, file)
	waitFor(t, 10*time.Second, "the .part was not seen", func() bool { return len(obs.snapshot().firsts) >= 1 })
	newerStart := time.Now()
	newer := segPath(cfg.RecDir, "cam1", "main", newerStart, ".age.part")
	newerBytes := []byte("the segment that MediaMTX writes now")
	writeFile(t, newer, newerBytes)

	waitFor(t, 10*time.Second, "no report after the rewrite", func() bool {
		reps := obs.snapshot().reports
		return len(reps) > 0 && reps[len(reps)-1].Rewritten == 1
	})
	final := strings.TrimSuffix(left, ".part")
	leftMs := leftStart.UnixMilli()
	requireMissing(t, left)
	requireParts(t, parseRecording(t, decryptAll(t, readFile(t, final), rk)), f, []int{20, 20, 13})
	requireRow(t, cfg.Catalog, relSeg("cam1", "main", leftStart, ".age"), leftMs, leftMs+2650, true, final)
	requireGaps(t, cfg.Catalog, []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: leftMs + 2650, EndMs: newerStart.UnixMilli(), Reason: catalog.GapRestart}})
	if !bytes.Equal(readFile(t, newer), newerBytes) {
		t.Error("the .part that MediaMTX writes was changed")
	}
	wantReport := ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1, Rewritten: 1}
	if reps := obs.snapshot().reports; reps[len(reps)-1] != wantReport {
		t.Errorf("report after the rewrite %+v, want %+v", reps[len(reps)-1], wantReport)
	}
	if n := len(obs.snapshot().restarts); n != 0 {
		t.Errorf("the child was started again %d times", n)
	}
}

// TestRecorder_ReconcileUpdatesAreNumbered (M13): every reconcile report
// that the recorder gives out carries a Seq, 1 for the startup's and one
// more for each later one, and is Final when no background rewrite or
// repair is pending, so that a reader keeps the one with the highest Seq.
// A salvage that waits for its rewrite makes the startup's not final; the
// update after the rewrite is. A start with nothing pending is final at once.
func TestRecorder_ReconcileUpdatesAreNumbered(t *testing.T) {
	t.Run("a rewrite pending", func(t *testing.T) {
		recDir := filepath.Join(t.TempDir(), "rec")
		scope, riG, _ := newTestScope(t)
		_, file := bigTorn(t, scope)
		writeFile(t, segPath(recDir, "cam1", "main", t0, ".age.part"), file)
		obs := &testObserver{}
		r, _ := startFake(t, "sleep", Config{RecDir: recDir, Tail: crypto.NewTailSealer(riG), Recording: crypto.NewRecording(riG), Observer: obs})
		ups := obs.snapshot().updates
		if len(ups) != 1 || ups[0].Seq != 1 || ups[0].Final || ups[0].Finalized != 1 {
			t.Fatalf("updates at startup %+v, want one, seq 1, not final, with the .part finalized", ups)
		}
		if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", &scope)}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 10*time.Second, "no update after the rewrite", func() bool { return len(obs.snapshot().updates) >= 2 })
		ups = obs.snapshot().updates
		if last := ups[len(ups)-1]; len(ups) != 2 || last.Seq != 2 || !last.Final || last.Rewritten != 1 || last.Finalized != 1 {
			t.Errorf("updates %+v, want a second one, seq 2 and final, with the rewrite", ups)
		}
	})
	t.Run("nothing pending", func(t *testing.T) {
		recDir := filepath.Join(t.TempDir(), "rec")
		f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
		writeFile(t, segPath(recDir, "cam1", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
		obs := &testObserver{}
		startFake(t, "sleep", Config{RecDir: recDir, Observer: obs})
		want := ReconcileUpdate{ReconcileReport: ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}, Seq: 1, Final: true}
		if ups := obs.snapshot().updates; !slices.Equal(ups, []ReconcileUpdate{want}) {
			t.Errorf("updates %+v, want only %+v", ups, want)
		}
	})
}

// TestRecorder_UnreadableDirectoryDoesNotStopRecording (M1): a directory
// under rec/ that cannot be read, such as a date directory with an I/O
// error after a cut on an SD card, does not fail New: it is skipped, an
// error names it by its base name only, the other streams are repaired and
// recording starts.
func TestRecorder_UnreadableDirectoryDoesNotStopRecording(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	torn := segPath(recDir, "cam1", "main", t0, ".part")
	writeFile(t, torn, f.data[:f.partStart[2]+10])
	hidden := segPath(recDir, "cam2", "main", t0, ".part")
	hiddenBytes := f.data[:f.partStart[2]+10]
	writeFile(t, hidden, hiddenBytes)
	bad := filepath.Dir(hidden)
	defer setWalkRecDir(unreadableDir(bad))()
	obs := &testObserver{}
	dump := filepath.Join(t.TempDir(), "child.json")
	r, _ := startFake(t, "dump:"+dump, Config{RecDir: recDir, Observer: obs})
	readDump(t, dump) // recording started: the child runs

	if rep := reportNow(r); rep != (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}) {
		t.Errorf("startup report %+v, want cam1's .part repaired", rep)
	}
	requireMissing(t, torn)
	if !bytes.Equal(readFile(t, hidden), hiddenBytes) {
		t.Error("the .part in the directory that could not be read was changed")
	}
	named := false
	for _, e := range obs.snapshot().errs {
		if strings.Contains(e, filepath.Base(bad)) {
			named = true
			if strings.Contains(e, filepath.Dir(bad)) {
				t.Errorf("the error %q names the directory's parents", e)
			}
		}
	}
	if !named {
		t.Errorf("errors %q, want one that names %s", obs.snapshot().errs, filepath.Base(bad))
	}
}

// putRows writes a complete catalog row for each segment file of paths, as
// its hook would have, with the file's size.
func putRows(t *testing.T, cat *catalog.DB, recDir string, paths ...string) {
	t.Helper()
	for _, path := range paths {
		rel, err := filepath.Rel(recDir, path)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := parseSegment(recDir, rel)
		if !ok {
			t.Fatalf("%s is no segment", rel)
		}
		start := s.start.UnixMilli()
		if err := cat.PutSegment(t.Context(), catalog.Segment{Cam: string(s.key.cam), Profile: string(s.key.profile), Path: s.rel,
			StartMs: start, EndMs: start + 3000, Bytes: int64(len(readFile(t, path))), Encrypted: s.encrypted, State: catalog.StateComplete}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestReconcile_UnreadableDirectoryLeavesItsRows (7a M2): the rows of the
// files under a directory that cannot be read are neither lost nor counted
// toward HUB-09's abort: those files may well be there. With two of three
// cataloged segments under it, the start does not abort, and every row
// stays complete.
func TestReconcile_UnreadableDirectoryLeavesItsRows(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	seen := segPath(recDir, "cam1", "main", t0, "")
	hidden := []string{segPath(recDir, "cam2", "main", t0, ""), segPath(recDir, "cam2", "main", t0.Add(5*time.Minute), "")}
	for _, path := range append([]string{seen}, hidden...) {
		writeFile(t, path, f.data)
	}
	putRows(t, cat, recDir, append([]string{seen}, hidden...)...)
	defer setWalkRecDir(unreadableDir(filepath.Dir(hidden[0])))()

	res, err := reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.report.Aborted {
		t.Error("reconcile aborted: the rows under the directory that cannot be read counted as lost")
	}
	for path, row := range rowByPath(t, cat) {
		if row.State != catalog.StateComplete {
			t.Errorf("%s is %s, want complete", path, row.State)
		}
	}
	if len(res.errs) != 1 || !strings.Contains(res.errs[0].Error(), filepath.Base(filepath.Dir(hidden[0]))) {
		t.Errorf("errors %q, want the listing's, naming the directory", res.errs)
	}
}

// TestReconcile_DirSyncFailsAfterTheChange_KeepsTheGap (M3): when the
// rename of a repaired .part, or the removal of an empty one, succeeds and
// the sync of its directory then fails, the file is repaired, not
// unreadable: its restart gap is written, before the change that a cut
// could make durable, and the sync's error names the file.
func TestReconcile_DirSyncFailsAfterTheChange_KeepsTheGap(t *testing.T) {
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	for _, tc := range []struct {
		name    string
		data    []byte
		report  ReconcileReport
		keptMs  int64
		removed bool
	}{
		{"rename", f.data[:f.partStart[3]+10], ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}, 3000, false},
		{"remove", make([]byte, 200_000), ReconcileReport{LostTails: 1, GapsWritten: 1}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recDir, cat := t.TempDir(), openCatalog(t)
			part := segPath(recDir, "cam1", "main", t0, ".part")
			writeFile(t, part, tc.data)
			wantGap := catalog.Gap{Cam: "cam1", Profile: "main", StartMs: t0.UnixMilli() + tc.keptMs, Reason: catalog.GapRestart}
			gapBeforeSync := false
			defer setSyncDir(func(dir string) error {
				if dir != filepath.Dir(part) {
					return nil
				}
				gaps, err := cat.Gaps(context.Background())
				gapBeforeSync = err == nil && slices.Equal(gaps, []catalog.Gap{wantGap})
				return &fs.PathError{Op: "sync", Path: dir, Err: errors.New("injected: input/output error")}
			})()

			res, err := reconcile(t.Context(), recDir, nil, cat)
			if err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if res.report != tc.report {
				t.Errorf("report %+v, want %+v", res.report, tc.report)
			}
			requireMissing(t, part)
			if !tc.removed {
				requireRow(t, cat, relSeg("cam1", "main", t0, ""), t0.UnixMilli(), t0.UnixMilli()+tc.keptMs, false, strings.TrimSuffix(part, ".part"))
			}
			requireGaps(t, cat, []catalog.Gap{wantGap})
			if !gapBeforeSync {
				t.Error("the gap was not in the catalog when the directory was synced")
			}
			if len(res.errs) != 1 || !strings.Contains(res.errs[0].Error(), filepath.Base(part)) {
				t.Errorf("errors %q, want the sync's, naming %s", res.errs, filepath.Base(part))
			}
		})
	}
}

// TestRecorder_LiveRepairDirSyncFails_KeepsTheGap (M3): the repair of a
// .part that a dead MediaMTX left, while the hub runs, keeps its gap and
// its catalog row when the sync of its directory fails after the rename,
// and does not count the file as unreadable.
func TestRecorder_LiveRepairDirSyncFails_KeepsTheGap(t *testing.T) {
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{Observer: obs})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	deadStart := time.Now().Add(-10 * time.Second)
	dead := segPath(cfg.RecDir, "cam1", "main", deadStart, ".part")
	defer setSyncDir(func(dir string) error {
		if dir == filepath.Dir(dead) {
			return &fs.PathError{Op: "sync", Path: dir, Err: errors.New("injected: input/output error")}
		}
		return nil
	})()
	writeFile(t, dead, f.data[:f.partStart[3]+10])
	killChild(t, r)

	if rep := waitFinalReport(t, obs); rep != (ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1}) {
		t.Errorf("report after the repair %+v, want the .part repaired, not unreadable", rep)
	}
	deadMs := deadStart.UnixMilli()
	requireRow(t, cfg.Catalog, relSeg("cam1", "main", deadStart, ""), deadMs, deadMs+3000, false, strings.TrimSuffix(dead, ".part"))
	gaps, err := cfg.Catalog.Gaps(t.Context())
	if err != nil || len(gaps) != 1 || gaps[0].StartMs != deadMs+3000 || gaps[0].Reason != catalog.GapRestart {
		t.Errorf("gaps %+v (%v), want the restart gap from %d", gaps, err, deadMs+3000)
	}
	if !slices.ContainsFunc(obs.snapshot().errs, func(e string) bool { return strings.Contains(e, filepath.Base(dead)) }) {
		t.Errorf("errors %q, want the sync's, naming %s", obs.snapshot().errs, filepath.Base(dead))
	}
}

// ioErrorSealer opens torn recordings with its TailSealer, but for the file
// named failOn, OpenTorn fails with an I/O error (openErr), or the reads of
// what it opened do (readErr), as a failing disk's would.
type ioErrorSealer struct {
	crypto.TailSealer
	failOn           string
	openErr, readErr bool
}

func (s ioErrorSealer) OpenTorn(ctx context.Context, f *os.File) (crypto.TornAge, error) {
	if filepath.Base(f.Name()) != s.failOn {
		return s.TailSealer.OpenTorn(ctx, f)
	}
	if s.openErr {
		return nil, &fs.PathError{Op: "read", Path: f.Name(), Err: errors.New("injected: input/output error")}
	}
	torn, err := s.TailSealer.OpenTorn(ctx, f)
	return failingReads{torn}, err
}

type failingReads struct{ crypto.TornAge }

func (failingReads) ReadAt([]byte, int64) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: "file", Err: errors.New("injected: input/output error")}
}

// TestReconcile_IOErrorsLeaveTheFile (M4): an I/O error while reconcile
// reads a .part, from OpenTorn, from a read of the walk or from its Stat,
// leaves the file as it is: it is not set aside as unreadable, gets no gap,
// counts as unreadable, and an error names it. A final file whose size the
// catalog step cannot read keeps its row as it is.
func TestReconcile_IOErrorsLeaveTheFile(t *testing.T) {
	scope, riG, _ := newTestScope(t)
	injected := func(path string) error {
		return &fs.PathError{Op: "read", Path: path, Err: errors.New("injected: input/output error")}
	}
	clearTorn := func(t *testing.T) []byte {
		f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
		return f.data[:f.partStart[2]+10]
	}
	ageTorn := func(t *testing.T) []byte {
		_, file := alignedTorn(t, scope)
		return file
	}
	for _, tc := range []struct {
		name, suffix string
		file         func(t *testing.T) []byte
		inject       func(failOn string) (tail crypto.TailSealer, restore func())
	}{
		{"OpenTorn", ".age.part", ageTorn, func(failOn string) (crypto.TailSealer, func()) {
			return ioErrorSealer{TailSealer: crypto.NewTailSealer(riG), failOn: failOn, openErr: true}, func() {}
		}},
		{"walk read, encrypted", ".age.part", ageTorn, func(failOn string) (crypto.TailSealer, func()) {
			return ioErrorSealer{TailSealer: crypto.NewTailSealer(riG), failOn: failOn, readErr: true}, func() {}
		}},
		{"walk read, clear", ".part", clearTorn, func(failOn string) (crypto.TailSealer, func()) {
			// A file that is open for writing only fails every read with a *fs.PathError.
			return nil, setOpenSegment(func(path string, flag int) (*os.File, error) {
				if filepath.Base(path) == failOn {
					flag = os.O_WRONLY
				}
				return os.OpenFile(path, flag, 0) //nolint:gosec // a file of the test
			})
		}},
		{"Stat", ".part", clearTorn, func(failOn string) (crypto.TailSealer, func()) {
			// The Stat of a closed file fails with a *fs.PathError.
			return nil, setOpenSegment(func(path string, flag int) (*os.File, error) {
				f, err := os.OpenFile(path, flag, 0) //nolint:gosec // a file of the test
				if err == nil && filepath.Base(path) == failOn {
					_ = f.Close()
				}
				return f, err
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recDir, cat := t.TempDir(), openCatalog(t)
			good := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
			for _, cam := range []string{"cam2", "cam3"} { // so that it is not HUB-09's abort
				writeFile(t, segPath(recDir, cam, "main", t0, ""), good.data)
			}
			part := segPath(recDir, "cam1", "main", t0, tc.suffix)
			data := tc.file(t)
			writeFile(t, part, data)
			tail, restore := tc.inject(filepath.Base(part))
			defer restore()

			res, err := reconcile(t.Context(), recDir, tail, cat)
			if err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if res.report != (ReconcileReport{Unreadable: 1}) {
				t.Errorf("report %+v, want the file counted unreadable, and nothing else", res.report)
			}
			if !bytes.Equal(readFile(t, part), data) {
				t.Error("the file was changed or moved")
			}
			requireMissing(t, part+".unreadable")
			requireGaps(t, cat, nil)
			if len(res.errs) != 1 || !strings.Contains(res.errs[0].Error(), filepath.Base(part)) {
				t.Errorf("errors %q, want one that names %s", res.errs, filepath.Base(part))
			}
		})
	}

	t.Run("catalog stat", func(t *testing.T) {
		recDir, cat := t.TempDir(), openCatalog(t)
		good := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
		older, newer := segPath(recDir, "cam1", "main", t0, ""), segPath(recDir, "cam1", "main", t0.Add(5*time.Minute), "")
		writeFile(t, older, good.data)
		writeFile(t, newer, good.data)
		row := catalog.Segment{Cam: "cam1", Profile: "main", Path: relSeg("cam1", "main", t0, ""), StartMs: t0.UnixMilli(), EndMs: t0.UnixMilli() + 2000,
			Bytes: int64(len(good.data)), State: catalog.StateComplete}
		if err := cat.PutSegment(t.Context(), row); err != nil {
			t.Fatal(err)
		}
		defer setStatSegment(func(path string) (os.FileInfo, error) {
			if path == older {
				return nil, injected(path)
			}
			return os.Stat(path)
		})()

		res, err := reconcile(t.Context(), recDir, nil, cat)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if res.report != (ReconcileReport{}) {
			t.Errorf("report %+v, want nothing repaired", res.report)
		}
		got, ok := rowByPath(t, cat)[row.Path]
		if !ok || got.State != catalog.StateComplete || got.Bytes != row.Bytes || got.StartMs != row.StartMs || got.EndMs != row.EndMs {
			t.Errorf("row %+v, want it kept as %+v", got, row)
		}
		if _, ok := rowByPath(t, cat)[relSeg("cam1", "main", t0.Add(5*time.Minute), "")]; !ok {
			t.Error("the other final file has no row")
		}
		if len(res.errs) != 1 || !strings.Contains(res.errs[0].Error(), filepath.Base(older)) {
			t.Errorf("errors %q, want one that names %s", res.errs, filepath.Base(older))
		}
	})
}

// TestRecorder_LiveRepair_TornThenUnreadablePart_GapEndsAtTheNextReadable
// (M5): while the hub runs, the .part files that a dead MediaMTX left are
// planned together, so that a gap ends where its stream's next segment that
// holds a recording starts, as at startup: a torn .part's gap runs past an
// unreadable .part that follows it, to the restarted child's segment.
func TestRecorder_LiveRepair_TornThenUnreadablePart_GapEndsAtTheNextReadable(t *testing.T) {
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{Observer: obs})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	// The child leaves a segment that it tore and one that cannot be read, and
	// dies. Both appear at once, in a date directory of their own (the hub
	// makes today's and tomorrow's), so that the watcher sees them in one
	// poll and finds neither left behind while the child runs: the child's
	// exit hands both to one repair.
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	day := time.Now().UTC().AddDate(0, 0, 2).Truncate(24 * time.Hour).Add(12 * time.Hour)
	tornStart, unreadableStart, nextStart := day, day.Add(10*time.Second), day.Add(20*time.Second)
	staging := filepath.Join(t.TempDir(), "day")
	writeFile(t, filepath.Join(staging, filepath.Base(segPath(cfg.RecDir, "cam1", "main", tornStart, ".part"))), f.data[:f.partStart[3]+10])
	writeFile(t, filepath.Join(staging, filepath.Base(segPath(cfg.RecDir, "cam1", "main", unreadableStart, ".part"))),
		bytes.Repeat([]byte("not an fMP4 file "), 300))
	if err := os.Rename(staging, filepath.Dir(segPath(cfg.RecDir, "cam1", "main", tornStart, ".part"))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "the .part files were not seen", func() bool { return len(obs.snapshot().firsts) >= 1 })
	killChild(t, r)
	waitFor(t, 10*time.Second, "the child was not started again", func() bool { return len(obs.snapshot().restarts) >= 1 })
	// The new child starts a segment.
	writeFile(t, segPath(cfg.RecDir, "cam1", "main", nextStart, ".part"), make([]byte, 100))

	want := []catalog.Gap{
		{Cam: "cam1", Profile: "main", StartMs: tornStart.UnixMilli() + 3000, EndMs: nextStart.UnixMilli(), Reason: catalog.GapRestart},
		{Cam: "cam1", Profile: "main", StartMs: unreadableStart.UnixMilli(), EndMs: nextStart.UnixMilli(), Reason: catalog.GapRestart},
	}
	waitFor(t, 10*time.Second, "the gaps do not end at the new segment", func() bool {
		gaps, err := cfg.Catalog.Gaps(t.Context())
		return err == nil && len(gaps) == 2 && gaps[0].EndMs != 0 && gaps[1].EndMs != 0
	})
	requireGaps(t, cfg.Catalog, want)
}
