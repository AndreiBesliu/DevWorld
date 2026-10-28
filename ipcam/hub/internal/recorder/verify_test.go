package recorder

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// TestReconcile_ReportsUnreadableAndTornParts (C12): the background
// verification parses every closed clear segment and decrypts every closed
// .age through OpenReaderAt. Of two corrupt .age files (one where only a
// chunk inside a free box is corrupt, which no parser reads), one clear
// file with a torn part and one good file, it reports 2 unreadable and 1
// torn part through Observer.Verified, marks only the good one verified,
// and checks the others again in the next pass.
func TestReconcile_ReportsUnreadableAndTornParts(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	scope, riG, _ := newTestScope(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	ctx := t.Context()

	corrupt := encryptFMP4(t, f.data, scope, nil, false)
	corrupt[len(corrupt)/2] ^= 0x40
	// A free box of three chunks after the first part: the chunk after the
	// one where its payload starts holds only padding.
	padded := encryptFMP4(t, slices.Concat(f.data[:f.partEnd[0]], freeBox(3*crypto.ChunkSize), f.data[f.partEnd[0]:]), scope, nil, false)
	chunk := (f.partEnd[0]+8)/crypto.ChunkSize + 1
	padded[int64(len(headerOf(t, padded)))+16+chunk*(crypto.ChunkSize+16)+100] ^= 0x40
	files := []struct {
		cam, suffix string
		start       time.Time
		data        []byte
	}{
		{"cam1", ".age", t0, corrupt},
		{"cam3", ".age", t0, padded},
		{"cam2", "", t0, f.data[:f.sampleEnd[2][9]]},
		{"cam2", "", t0.Add(5 * time.Minute), f.data},
	}
	for _, file := range files {
		path := segPath(recDir, file.cam, "main", file.start, file.suffix)
		writeFile(t, path, file.data)
		if err := cat.PutSegment(ctx, catalog.Segment{Cam: file.cam, Profile: "main", Path: relSeg(file.cam, "main", file.start, file.suffix),
			StartMs: file.start.UnixMilli(), EndMs: file.start.UnixMilli() + 3000, Bytes: int64(len(file.data)), Encrypted: file.suffix != "", State: catalog.StateComplete}); err != nil {
			t.Fatal(err)
		}
	}

	obs := &testObserver{}
	v := &verifier{recDir: recDir, cat: cat, recording: crypto.NewRecording(riG), obs: obs}
	for pass := range 2 {
		report, err := v.pass(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if report != (ReconcileReport{Unreadable: 2, TornParts: 1}) {
			t.Errorf("pass %d: %+v, want 2 unreadable and 1 torn part", pass+1, report)
		}
		if got := obs.snapshot().verified; len(got) != pass+1 || got[pass] != report {
			t.Errorf("pass %d: Observer.Verified got %+v", pass+1, got)
		}
	}
	good := segPath(recDir, "cam2", "main", t0.Add(5*time.Minute), "")
	if got := obs.snapshot().passedSeg; !slices.Equal(got, []string{good}) {
		t.Errorf("segments verified %q, want only %s, once", got, good)
	}
	for path, s := range rowByPath(t, cat) {
		if verified := s.VerifiedAtMs != nil; verified != (path == relSeg("cam2", "main", t0.Add(5*time.Minute), "")) {
			t.Errorf("%s: verified %v", path, verified)
		}
	}
	if _, err := os.Stat(good); err != nil {
		t.Fatal(err)
	}
}

// countingRecording opens recordings with Recording, and counts the bytes of
// ciphertext that their plaintext readers read.
type countingRecording struct {
	crypto.Recording
	read atomic.Int64
}

func (c *countingRecording) OpenReaderAt(ctx context.Context, src io.ReaderAt, size int64) (io.ReaderAt, int64, error) {
	return c.Recording.OpenReaderAt(ctx, countingReaderAt{src, &c.read}, size)
}

type countingReaderAt struct {
	r io.ReaderAt
	n *atomic.Int64
}

func (c countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n.Add(int64(n))
	return n, err
}

// TestVerify_DecryptsEachAgeSegmentOnce (MI3): the background verification
// checks a closed .age segment in one pass, parsing its parts from the
// stream that it decrypts once, so it reads the ciphertext about once: each
// chunk, plus the header and the final chunk that opening it authenticates.
// Decrypting it whole and then reading it again for the parse would read it
// twice, and double the CPU of every closed segment on a Pi.
func TestVerify_DecryptsEachAgeSegmentOnce(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	ctx := t.Context()
	scope, riG, _ := newTestScope(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{20000, 20000, 20000, 20000}})
	file := encryptFMP4(t, f.data, scope, f.partEnd, false)
	path := segPath(recDir, "cam1", "main", t0, ".age")
	writeFile(t, path, file)
	if err := cat.PutSegment(ctx, catalog.Segment{Cam: "cam1", Profile: "main", Path: relSeg("cam1", "main", t0, ".age"),
		StartMs: t0.UnixMilli(), EndMs: t0.UnixMilli() + 4000, Bytes: int64(len(file)), Encrypted: true, State: catalog.StateComplete}); err != nil {
		t.Fatal(err)
	}

	obs := &testObserver{}
	rec := &countingRecording{Recording: crypto.NewRecording(riG)}
	v := &verifier{recDir: recDir, cat: cat, recording: rec, obs: obs}
	report, err := v.pass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report != (ReconcileReport{}) || !slices.Equal(obs.snapshot().passedSeg, []string{path}) {
		t.Fatalf("verification %+v, passed %q; want the segment passed", report, obs.snapshot().passedSeg)
	}
	limit := int64(len(file)) + 2*(crypto.ChunkSize+16)
	if got := rec.read.Load(); got > limit {
		t.Errorf("the verification read %d bytes of ciphertext for a file of %d, want at most %d: each chunk decrypted once", got, len(file), limit)
	}
}

// TestVerify_AuthenticatesTheTrailingPadding (C12): the one pass reads the
// plaintext to its end, so a chunk that does not authenticate inside the
// free box after the last part, which no box header read reaches, still
// makes the segment unreadable.
func TestVerify_AuthenticatesTheTrailingPadding(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	ctx := t.Context()
	scope, riG, _ := newTestScope(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
	file := encryptFMP4(t, slices.Concat(f.data, freeBox(3*crypto.ChunkSize)), scope, nil, false)
	// A chunk inside the free box, neither the one with its header nor the final one.
	chunk := int64(len(f.data))/crypto.ChunkSize + 1
	file[int64(len(headerOf(t, file)))+16+chunk*(crypto.ChunkSize+16)+100] ^= 0x40
	path := segPath(recDir, "cam1", "main", t0, ".age")
	writeFile(t, path, file)
	if err := cat.PutSegment(ctx, catalog.Segment{Cam: "cam1", Profile: "main", Path: relSeg("cam1", "main", t0, ".age"),
		StartMs: t0.UnixMilli(), EndMs: t0.UnixMilli() + 2000, Bytes: int64(len(file)), Encrypted: true, State: catalog.StateComplete}); err != nil {
		t.Fatal(err)
	}
	v := &verifier{recDir: recDir, cat: cat, recording: crypto.NewRecording(riG), obs: &testObserver{}}
	report, err := v.pass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report != (ReconcileReport{Unreadable: 1}) {
		t.Errorf("verification %+v, want the corrupt padding found: 1 unreadable", report)
	}
}

// TestVerify_UnreadableDirectoryLeavesItsRows (7a M2): a verification pass
// while a directory under rec/ cannot be read goes out, and counts the rows
// under it neither as mismatches nor as lost: their files may well be
// there.
func TestVerify_UnreadableDirectoryLeavesItsRows(t *testing.T) {
	recDir, cat := t.TempDir(), openCatalog(t)
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000, 1000}})
	seen, hidden := segPath(recDir, "cam1", "main", t0, ""), segPath(recDir, "cam2", "main", t0, "")
	writeFile(t, seen, f.data)
	writeFile(t, hidden, f.data)
	putRows(t, cat, recDir, seen, hidden)
	defer setWalkRecDir(unreadableDir(filepath.Dir(hidden)))()

	obs := &testObserver{}
	v := &verifier{recDir: recDir, cat: cat, obs: obs}
	report, err := v.pass(t.Context())
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if report != (ReconcileReport{}) {
		t.Errorf("report %+v, want no mismatch", report)
	}
	if got := obs.snapshot().verified; len(got) != 1 {
		t.Errorf("%d verify reports, want 1", len(got))
	}
}
