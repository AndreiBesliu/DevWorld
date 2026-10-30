package mi1

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
)

// plantedConfig is what another account may put at the config's name
// before install: a config that runs its own MediaMTX.
var plantedConfig = []byte(`{"dataDir":"C:\\Users\\Public\\data","mediamtx":"C:\\Users\\Public\\mediamtx.exe","sdCardConfirmed":false,"jobs":[]}` + "\n")

// hubConfigRig is a driver of the owner's runs, with a run of its own in
// Work, and that run's state and volume: what hubConfig needs.
func hubConfigRig(t *testing.T) (*Driver, *RunState, *CurrentRun, Volume) {
	t.Helper()
	work := t.TempDir()
	const id = "pc-kill9-01-1"
	pad := true
	saved := hubproc.Config{DataDir: filepath.Join(work, id, "data"), Jobs: []hubproc.Job{{Cam: "cam01", Profile: "main",
		RestreamURL: "rtsp://127.0.0.1:8554/cam01_main", Recipients: nil, PadLowBitrate: &pad}}}
	if err := os.MkdirAll(filepath.Join(work, id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := hubproc.WriteConfig(filepath.Join(work, id, "run.json"), saved); err != nil {
		t.Fatal(err)
	}
	d := &Driver{Work: work, MediaMTX: filepath.Join(t.TempDir(), "proba-inregistrare", "mediamtx"),
		ConfigDir: filepath.Join(t.TempDir(), "ProgramData", "ipcam-recspike")}
	return d, &RunState{Mode: ModeAutomated}, &CurrentRun{RunID: id}, dirVolume(filepath.Join(work, id, "data"))
}

// checkTrusted checks that the config at path is the run's own, with the
// release's MediaMTX and the run's data directory.
func checkTrusted(t *testing.T, path string, d *Driver, vol Volume) {
	t.Helper()
	cfg, err := hubproc.ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MediaMTX != d.MediaMTX || cfg.DataDir != vol.DataDir() || cfg.IdentityFile != "" || len(cfg.Jobs) != 1 {
		t.Errorf("ipcam-hub starts with %+v, want MediaMTX %s, data directory %s, no RI_g file and the run's one job", cfg, d.MediaMTX, vol.DataDir())
	}
}

// TestHubConfig_ReplacesAFilePlantedAtItsName (Final fix C review, I2):
// the config that the owner's elevated ipcam-hub starts with is never
// written through a file that was already at its name in the protected
// folder, which another account may have put there before install. On the
// PC such a file would keep its owner, who may change its DACL and then the
// config, between hubConfig and ipcam-hub's start; a link would carry the
// write to wherever it points. hubConfig removes what is there and creates
// the config anew.
func TestHubConfig_ReplacesAFilePlantedAtItsName(t *testing.T) {
	d, st, cur, vol := hubConfigRig(t)
	d.protectDir = func(dir string) error { return os.MkdirAll(dir, 0o700) } // the PC's DACL: TestHubConfig_AdministratorsOnlyAfterAPlant
	if err := os.MkdirAll(d.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(elsewhere, plantedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(elsewhere, filepath.Join(d.ConfigDir, HubConfigName)); err != nil {
		t.Fatal(err)
	}
	path, err := d.hubConfig(st, cur, vol)
	if err != nil {
		t.Fatalf("hubConfig: %v", err)
	}
	if path != filepath.Join(d.ConfigDir, HubConfigName) {
		t.Fatalf("hubConfig wrote %s, want the config in the protected folder", path)
	}
	if got, err := os.ReadFile(elsewhere); err != nil || !bytes.Equal(got, plantedConfig) { //nolint:gosec // the test's file
		t.Errorf("hubConfig wrote through the file planted at the config's name, into %s: %q (%v)", elsewhere, got, err)
	}
	planted, err := os.Stat(elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(planted, written) {
		t.Errorf("the config is still the file planted at its name")
	}
	checkTrusted(t, path, d, vol)
}
