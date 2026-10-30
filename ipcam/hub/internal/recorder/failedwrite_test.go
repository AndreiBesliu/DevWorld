package recorder

import (
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
)

// requireTailReason fails t unless the .part at rel has a recorded card
// reason exactly when want.
func requireTailReason(t *testing.T, cat *catalog.DB, rel string, want bool) {
	t.Helper()
	reason, found, err := cat.TailReason(t.Context(), rel)
	if err != nil || found != want || (found && reason != catalog.GapCard) {
		t.Errorf("%s: tail reason %q, %v, %v; want a card reason %v", rel, reason, found, err, want)
	}
}

// TestRecorder_WriteFailed_NotedOnceAndNotAfterAStop (T40; Task 8b review
// M2): after a FAILED stop on a card that a read-only remount left, Stop
// could not write MediaMTX's configuration, so MediaMTX keeps the stream's
// path and logs a failed write about every 2 s. The recorder notes none of
// those that came after the stream's Stop, whose card gap covers the hole,
// and a .part's reason once, not again at each failed write that finds the
// same file: no write of the catalog, on the same card, per retry. A
// failed write from before the Stop is still noted.
func TestRecorder_WriteFailed_NotedOnceAndNotAfterAStop(t *testing.T) {
	r, cfg := startFake(t, "sleep", Config{})
	ctx := t.Context()
	if err := r.Apply(ctx, []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil), NewJob("cam2", "main", "rtsp://127.0.0.1:1/cam2/main", nil)}); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-10 * time.Second)
	for _, cam := range []string{"cam1", "cam2"} {
		writeFile(t, segPath(cfg.RecDir, cam, "main", start, ".part"), []byte("the segment that failed"))
	}
	cam1, cam2 := relSeg("cam1", "main", start, ".part"), relSeg("cam2", "main", start, ".part")
	note := func(cam CameraID, at time.Time) {
		t.Helper()
		if err := r.noteFailedWrite(ctx, failedWrite{key: streamKey{cam, "main"}, reason: catalog.GapCard, at: at}); err != nil {
			t.Fatalf("noteFailedWrite: %v", err)
		}
	}

	note("cam1", time.Now())
	requireTailReason(t, cfg.Catalog, cam1, true)
	// A second write of the same reason would show.
	if err := cfg.Catalog.DeleteTailReason(ctx, cam1); err != nil {
		t.Fatal(err)
	}
	note("cam1", time.Now())
	requireTailReason(t, cfg.Catalog, cam1, false)

	beforeStop := time.Now().Add(-time.Second)
	if err := r.Stop(ctx, "cam2", "main", catalog.GapCard); err != nil {
		t.Fatal(err)
	}
	note("cam2", time.Now())
	requireTailReason(t, cfg.Catalog, cam2, false)
	note("cam2", beforeStop)
	requireTailReason(t, cfg.Catalog, cam2, true)
}

// TestRecorder_StopNotesFailedWritesForAtMostTheTimeout (T40; Task 8b review
// M4): the failed writes still queued when the hub stops are noted before
// the recorder ends, on the card; a card that answers slowly holds the stop
// for no longer than shutdownNoteTimeout, and the writes left unnoted are
// reported.
func TestRecorder_StopNotesFailedWritesForAtMostTheTimeout(t *testing.T) {
	t.Cleanup(setShutdownNoteTimeout(300 * time.Millisecond))
	var slow atomic.Bool
	t.Cleanup(setReadDir(func(dir string) ([]os.DirEntry, error) {
		if slow.Load() {
			time.Sleep(100 * time.Millisecond)
		}
		return os.ReadDir(dir)
	}))
	obs := &testObserver{}
	r, cfg, stop := startFakeStoppable(t, "sleep", Config{Observer: obs})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, segPath(cfg.RecDir, "cam1", "main", time.Now().Add(-10*time.Second), ".part"), []byte("the segment that failed"))
	// MediaMTX's failed writes, queued without the kick that would note them
	// now: the stop finds them.
	const queued = 20
	r.mu.Lock()
	for range queued {
		r.failedWrites = append(r.failedWrites, failedWrite{key: streamKey{"cam1", "main"}, reason: catalog.GapCard, at: time.Now()})
	}
	r.mu.Unlock()

	slow.Store(true)
	began := time.Now()
	stop()
	if took := time.Since(began); took > 3*time.Second {
		t.Errorf("the stop took %v with %d failed writes on a slow card, want about the %v that bounds their notes", took, queued, shutdownNoteTimeout)
	}
	unnoted := false
	for _, e := range obs.snapshot().errs {
		unnoted = unnoted || strings.Contains(e, "failed writes of MediaMTX not noted")
	}
	if !unnoted {
		t.Errorf("errors %q, want the failed writes left unnoted reported", obs.snapshot().errs)
	}
}
