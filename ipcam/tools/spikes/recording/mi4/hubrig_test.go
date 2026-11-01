package mi4

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// The test binary stands in for ipcam-hub when spiketest.FakeHubEnv is set
// (spiketest.FakeHub).
func TestMain(m *testing.M) {
	if os.Getenv(spiketest.FakeHubEnv) == "1" {
		os.Exit(spiketest.FakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// TestHubRig_ElevatedHubStartsFromTrustedValues (Final fix C review, M9):
// the guide runs MI4 from an administrator window, and its recordings lie
// in work/, which the owner's own token may change. Each recording's
// ipcam-hub starts from MI4's own config in the protected folder
// (Options.ConfigDir, mi1.HubConfigMI4), with the recording's trusted
// values, never from a run.json in work/.
func TestHubRig_ElevatedHubStartsFromTrustedValues(t *testing.T) {
	dir := t.TempDir()
	hub, _ := spiketest.InstallFakeHub(t, dir)
	run := filepath.Join(dir, "work", "mi4", "20261029-090000")
	o := Options{Hub: hub, MediaMTX: filepath.Join(dir, "proba-inregistrare", "mediamtx"), ConfigDir: filepath.Join(dir, "ProgramData", "ipcam-recspike")}
	h := &hubRig{o: o, dir: run, link: &mi1.HostLink{}, url: "rtsp://192.0.2.1:8554/cam01_main"}
	if _, err := h.record(t.Context(), hubrec.FiveMin, 300); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(run, hubrec.FiveMin)
	want := spiketest.HubStart{Config: filepath.Join(o.ConfigDir, mi1.HubConfigMI4), MediaMTX: o.MediaMTX, DataDir: filepath.Join(rec, hubrec.RecordingDir),
		IdentityFile: filepath.Join(rec, hubrec.IdentityFile)}
	if starts := spiketest.HubStarts(t, dir); len(starts) != 1 || starts[0] != want {
		t.Errorf("ipcam-hub started %+v, want once %+v", starts, want)
	}
}
