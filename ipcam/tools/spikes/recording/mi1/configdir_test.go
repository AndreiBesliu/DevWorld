package mi1

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	d.protectDir = func(dir string) error { return os.MkdirAll(dir, 0o700) } // the PC's DACL: TestHubConfig_AdministratorsOnlyInItsOwnFolder
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

// TestNotAFolder_NamesWhatItIsAndHowToRemoveIt (Final fix C review, minor):
// anything at the protected folder's place that is not a plain folder is
// refused, a plain file too, which any user may put there (ProgramData lets
// every user add files). The line says what lies there, a file or a link,
// and gives the exact command that removes it from an administrator window:
// del for a file or a link to a file, rd for a link or junction to a folder,
// which leaves the folder that it points to as it is.
func TestNotAFolder_NamesWhatItIsAndHowToRemoveIt(t *testing.T) {
	const dir = `C:\ProgramData\ipcam-recspike`
	for _, c := range []struct {
		name          string
		isDir, isLink bool
		what, command string
	}{
		{"a file", false, false, "nu este un folder, ci un fișier", `(del /f /a "C:\ProgramData\ipcam-recspike")`},
		{"a junction", true, true, "nu este un folder obișnuit, ci o legătură spre alt loc", `(rd "C:\ProgramData\ipcam-recspike")`},
		{"a link to a file", false, true, "nu este un folder, ci o legătură spre un fișier", `(del "C:\ProgramData\ipcam-recspike")`},
	} {
		err := notAFolder(dir, c.isDir, c.isLink)
		if err == nil || !strings.HasPrefix(err.Error(), dir+" "+c.what) || !strings.Contains(err.Error(), "dintr-o fereastră de administrator "+c.command) ||
			!strings.HasSuffix(err.Error(), "apoi porniți din nou comanda") {
			t.Errorf("%s: %v; want %q, then the command %s from an administrator window", c.name, err, c.what, c.command)
		}
	}
}

// TestHubConfig_WriteErrorsSayWhy (Final fix C review, minor): a config
// that cannot be written into the protected folder is not a protection
// failure, which would send the owner to the administrator window: a full
// disk, or a file that another program holds open at its name, each has
// its own Romanian line, and any other write error says that the config
// could not be written there.
func TestHubConfig_WriteErrorsSayWhy(t *testing.T) {
	d, st, cur, vol := hubConfigRig(t)
	d.protectDir = func(dir string) error { return os.MkdirAll(dir, 0o700) }
	path := filepath.Join(d.ConfigDir, HubConfigName)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"a full disk", &os.PathError{Op: "write", Path: path, Err: diskFullErrno}, "nu pot scrie setările ipcam-hub în " + path + ": discul este plin"},
		{"another error", &os.PathError{Op: "write", Path: path, Err: errors.New("the device is not ready")},
			"nu pot scrie setările ipcam-hub în " + path + ": write " + path + ": the device is not ready"},
	}
	if inUseErrno != nil {
		cases = append(cases, struct {
			name string
			err  error
			want string
		}{"a file held open", &os.PathError{Op: "remove", Path: path, Err: inUseErrno},
			"nu pot scrie setările ipcam-hub în " + path + ": fișierul este folosit de alt program"})
	}
	for _, c := range cases {
		d.writeProtected = func(string, []byte) error { return c.err }
		if _, err := d.hubConfig(st, cur, vol); err == nil || err.Error() != c.want {
			t.Errorf("%s: hubConfig = %v, want %q", c.name, err, c.want)
		}
	}
}

// TestHubConfig_LabRewritesRunJSONOnlyWhenItChanges (Final fix C review,
// M2): the lab's runs, and a driver without a ConfigDir, start ipcam-hub
// from the run's run.json, which a resume reads again after a reset. Before
// each start hubConfig writes it only when its trusted values change it,
// and then durably, synced, so that a reset during the write cannot leave
// the run without its config; when nothing changed it is not written.
func TestHubConfig_LabRewritesRunJSONOnlyWhenItChanges(t *testing.T) {
	d, _, cur, vol := hubConfigRig(t)
	st := &RunState{Mode: ModeLab}
	path := filepath.Join(d.runDir(cur.RunID), "run.json")
	var synced []string
	realSync := syncPath
	t.Cleanup(func() { syncPath = realSync })
	syncPath = func(p string) error {
		synced = append(synced, p)
		return realSync(p)
	}
	// The run's run.json names no MediaMTX; the driver's release does.
	if got, err := d.hubConfig(st, cur, vol); err != nil || got != path {
		t.Fatalf("hubConfig = %s, %v; want the run's run.json", got, err)
	}
	if !slices.Contains(synced, path) {
		t.Errorf("hubConfig changed %s without syncing it (synced %q)", path, synced)
	}
	checkTrusted(t, path, d, vol)

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	synced = nil
	if _, err := d.hubConfig(st, cur, vol); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) || len(synced) != 0 {
		t.Errorf("hubConfig wrote the unchanged run.json again: %v, %v, synced %q", info.ModTime(), err, synced)
	}
	checkTrusted(t, path, d, vol)
}

// TestExpandSystemFolders (Final fix C review, M3): HKLM keeps ProgramData's
// place as %SystemDrive%\ProgramData. ProgramData expands it with the drive
// and the folder of the system's own Windows folder, never with the process
// environment, and refuses any other variable, which only the environment
// could give.
func TestExpandSystemFolders(t *testing.T) {
	for _, c := range []struct{ raw, want, err string }{
		{`%SystemDrive%\ProgramData`, `C:\ProgramData`, ""},
		{`%SYSTEMDRIVE%\ProgramData`, `C:\ProgramData`, ""},
		{`%SystemRoot%\..\ProgramData`, `C:\Windows\..\ProgramData`, ""},
		{`D:\ProgramData`, `D:\ProgramData`, ""},
		{`%ALLUSERSPROFILE%`, "", "%ALLUSERSPROFILE%"},
		{`%SystemDrive`, "", "%SystemDrive"},
	} {
		got, err := expandSystemFolders(c.raw, `C:\Windows`)
		if got != c.want || (err == nil) != (c.err == "") || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("expandSystemFolders(%q) = %q, %v; want %q, an error naming %q", c.raw, got, err, c.want, c.err)
		}
	}
	if _, err := expandSystemFolders(`%SystemDrive%\ProgramData`, `\\server\share`); err == nil {
		t.Error("a Windows folder without a drive letter gave a ProgramData")
	}
}
