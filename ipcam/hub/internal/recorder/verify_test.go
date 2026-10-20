package recorder

import (
	"os"
	"slices"
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
