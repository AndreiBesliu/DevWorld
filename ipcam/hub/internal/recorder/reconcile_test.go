package recorder

import (
	"bytes"
	"encoding/json"
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

var t0 = time.Date(2026, 10, 20, 10, 0, 0, 123_456_000, time.Local)

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
// segments would be lost, here 3 unreadable .part files of 5, reconcile
// changes nothing and reports Aborted.
func TestReconcile_OverHalfLost_Aborts(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
	writeFile(t, segPath(recDir, "cam1", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
	writeFile(t, segPath(recDir, "cam2", "main", t0, ".part"), f.data[:f.sampleEnd[1][3]])
	for _, cam := range []string{"cam3", "cam4", "cam5"} {
		writeFile(t, segPath(recDir, cam, "main", t0, ".part"), bytes.Repeat([]byte("not an fMP4 file "), 300))
	}
	before := listFiles(t, recDir)

	report, err := Reconcile(t.Context(), recDir, nil, cat)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !report.Aborted {
		t.Fatalf("report %+v, want Aborted", report)
	}
	if after := listFiles(t, recDir); !maps.EqualFunc(before, after, bytes.Equal) {
		t.Error("an aborted reconcile changed the files")
	}
	if segs, _ := cat.Segments(t.Context()); len(segs) != 0 {
		t.Errorf("an aborted reconcile wrote %d rows", len(segs))
	}
	requireGaps(t, cat, nil)
}

// TestReconcile_HalfLost_Repairs (HUB-09): exactly half, here 2 unreadable
// .part files of 4, is not more than half: reconcile repairs the others and
// reports the unreadable ones, which it leaves as they are.
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
		if got := readFile(t, segPath(recDir, cam, "main", t0, ".part")); !bytes.Equal(got, garbage) {
			t.Errorf("%s: reconcile changed an unreadable file", cam)
		}
	}
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
	segs, _, err := scanRecDir(recDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range segs {
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
// child's first part. A .part that the startup reconcile could not read is
// left as it is and not counted again.
func TestRecorder_RepairsTheDeadChildsPartsWhileTheHubRuns(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000, 1000}})
	writeFile(t, segPath(recDir, "cam2", "main", t0, ""), f.data)
	garbage := bytes.Repeat([]byte("not an fMP4 file "), 300)
	unreadable := segPath(recDir, "cam3", "main", t0, ".part")
	writeFile(t, unreadable, garbage)
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{RecDir: recDir, Observer: obs})
	if rep := reportNow(r); rep != (ReconcileReport{Unreadable: 1}) {
		t.Fatalf("startup report %+v, want 1 unreadable", rep)
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
	want := []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: deadMs + 3000, EndMs: nextStart.UnixMilli(), Reason: catalog.GapRestart}}
	waitFor(t, 10*time.Second, "the dead child's .part has no restart gap that ends at the new segment", func() bool {
		gaps, err := cfg.Catalog.Gaps(t.Context())
		return err == nil && slices.Equal(gaps, want)
	})
	final := strings.TrimSuffix(dead, ".part")
	requireMissing(t, dead)
	requireParts(t, parseRecording(t, readFile(t, final)), f, []int{20, 20, 20})
	requireRow(t, cfg.Catalog, relSeg("cam1", "main", deadStart, ""), deadMs, deadMs+3000, false, final)
	if !bytes.Equal(readFile(t, unreadable), garbage) {
		t.Error("the unreadable .part was changed")
	}
	if got := readFile(t, next); len(got) != 100 {
		t.Errorf("the new child's .part has %d bytes, want its 100", len(got))
	}
	waitFor(t, 10*time.Second, "no report after the repair", func() bool { return len(obs.snapshot().reports) >= 1 })
	wantReport := ReconcileReport{Finalized: 1, Truncated: 1, LostTails: 1, GapsWritten: 1, Unreadable: 1}
	if reps := obs.snapshot().reports; reps[len(reps)-1] != wantReport {
		t.Errorf("report after the repair %+v, want %+v", reps[len(reps)-1], wantReport)
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
