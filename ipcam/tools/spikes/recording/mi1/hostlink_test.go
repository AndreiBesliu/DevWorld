package mi1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHostLink_OneLinkForTheWholeProbe (G15): a probe that holds one link
// to the PC's controller, as MI4's, reads its camera's address at its start
// without a goodbye, which the controller would answer with a pull of a
// Pi's results that are not there yet; at its end, the goodbye's pull gets
// the results folder, and the screen says so.
func TestHostLink_OneLinkForTheWholeProbe(t *testing.T) {
	f := startFakeController(t)
	f.pullAtBye = true
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "MI4", "pi-mi4"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MI4", "pi-mi4", "pi-mi4-1.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h, err := OpenHostLink(t.Context(), f.addr(), "pi")
	if err != nil {
		t.Fatal(err)
	}
	f.mustEvent(t, "hello pi")
	url, err := h.CameraURL("cam01_main")
	if err != nil || url != "rtsp://u:p@127.0.0.1:1/cam01_main" { //nolint:gosec // the fake controller's test credentials
		t.Errorf("CameraURL = %q, %v", url, err)
	}
	if _, err := h.CameraURL("cam42_main"); err == nil || !strings.Contains(err.Error(), "nu servește camera cam42_main") {
		t.Errorf("CameraURL of a stream the controller lacks = %v", err)
	}
	if err := f.waitEventWithin("bye", 200*time.Millisecond); err == nil {
		t.Fatal("the link said goodbye before the probe's end")
	}

	var out syncBuffer
	if err := h.End(t.Context(), dir, &out); err != nil {
		t.Fatalf("End: %v", err)
	}
	f.mustEvent(t, "bye")
	if _, err := os.Stat(filepath.Join(f.pulled, "MI4", "pi-mi4", "pi-mi4-1.json")); err != nil {
		t.Errorf("the PC's controller did not get the Pi's results: %v", err)
	}
	if !strings.Contains(out.String(), "Am trimis rezultatele.") {
		t.Errorf("screen %q, want the results sent", out.String())
	}
}

// TestHostLink_EndWithoutAPull: a controller that does not pull (the PC as
// the host, or a controller on the same device) closes the link at the
// goodbye, and End returns at once.
func TestHostLink_EndWithoutAPull(t *testing.T) {
	f := startFakeController(t)
	h, err := OpenHostLink(t.Context(), f.addr(), "pc")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var out syncBuffer
	if err := h.End(t.Context(), t.TempDir(), &out); err != nil || time.Since(start) > 5*time.Second || out.String() != "" {
		t.Errorf("End = %v after %v, screen %q; want it at once, with nothing sent", err, time.Since(start), out.String())
	}
	f.mustEvent(t, "bye")
}
