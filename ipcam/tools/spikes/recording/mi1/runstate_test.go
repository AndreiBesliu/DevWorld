package mi1

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// TestRunState_ResumesAfterReboot (G14): the run state is written before
// each step that a reboot can cut, durably and whole, and read back after
// the reboot as it was: the set, its mode, kind and window, the runs done,
// and the run in progress with the files listed at its stop. From it, the
// next run is the one after the runs that count, a redo of a run that did
// not count keeps its position with the next attempt, and no state means
// no set in progress. It names no address and no absolute path, because
// it lives in results/, which is published (PH0-22).
func TestRunState_ResumesAfterReboot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results", "state", "run-state.json")
	if st, err := LoadRunState(path); err != nil || st != nil {
		t.Fatalf("LoadRunState of no file = %+v, %v; want nil, nil", st, err)
	}
	counted := func(id string) results.Run { return results.Run{RunID: id, Valid: true, Attempt: 1} }
	st := &RunState{
		Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: "22:00-06:30",
		Host: HostState{Pseudonym: "pi", OS: "linux", Storage: "sd", WriteCache: "unknown",
			SdConfirmation: &results.SdConfirmation{Text: "DA", AtMs: 1792610000000}},
		Done: []DoneRun{
			{Index: 1, Run: counted("pi-sysrq-01-1")},
			{Index: 2, Run: results.Run{RunID: "pi-sysrq-02-1", Valid: false, Attempt: 1}},
			{Index: 2, Run: counted("pi-sysrq-02-2")},
		},
		Current: &CurrentRun{
			RunID: "pi-sysrq-03-1", Index: 3, Attempt: 1, Encrypted: true, PadLowBitrate: true, Streams: []string{MainStream},
			Phase: PhaseStopping, Before: map[string][]string{MainStream: {"rec/cam01/main/2026-10-25/220512_000001.mp4.age.part"}},
		},
	}
	if err := SaveRunState(path, st); err != nil {
		t.Fatalf("SaveRunState: %v", err)
	}
	// The reboot: a new process reads the file.
	got, err := LoadRunState(path)
	if err != nil {
		t.Fatalf("LoadRunState: %v", err)
	}
	if !reflect.DeepEqual(got, st) {
		t.Errorf("after the reboot\n%+v\nwant\n%+v", got, st)
	}
	if got.Current.Phase != PhaseStopping || got.Current.RunID != "pi-sysrq-03-1" {
		t.Errorf("the run in progress is %+v", got.Current)
	}
	plan, err := Plan(SetPiSysrq, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next, attempt, ok := got.nextAutomated(plan); !ok || next.Index != 3 || attempt != 1 {
		t.Errorf("next = run %d, attempt %d, %v; want run 3, attempt 1", next.Index, attempt, ok)
	}
	got.Done = append(got.Done, DoneRun{Index: 3, Run: results.Run{RunID: "pi-sysrq-03-1", Valid: true, Attempt: 1, PacingP99Ms: 140}})
	if next, attempt, ok := got.nextAutomated(plan); !ok || next.Index != 3 || attempt != 2 {
		t.Errorf("after a run with uneven pacing: run %d, attempt %d, %v; want run 3 again, attempt 2", next.Index, attempt, ok)
	}
	data, err := os.ReadFile(path) //nolint:gosec // the test's file
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{filepath.Dir(filepath.Dir(path)), "rtsp://", "127.0.0.1", ":7081"} {
		if strings.Contains(string(data), private) {
			t.Errorf("the run state holds %q:\n%s", private, data)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("run state mode %v (%v), want readable by its owner only", info.Mode().Perm(), err)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

// TestRunState_OldFormat_SaysTheStateFolderCanBeDeleted (Task 7.2c
// review): a run-state.json of an older recspike (one set's state, not the
// sets by results set) refuses every load and save; the owner reads in
// Romanian that its state folder can be deleted to start over, the folder
// that the run state is in, wherever the results folder was given.
func TestRunState_OldFormat_SaysTheStateFolderCanBeDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proba-pc", "state", "run-state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"set":"pi-sysrq","mode":"automated","kind":"sysrq","done":[]}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Dir(path)
	_, loadErr := LoadRunState(path)
	saveErr := SaveRunState(path, &RunState{Set: SetPiKill9, Mode: ModeAutomated, Kind: "kill9"})
	for name, err := range map[string]error{"LoadRunState": loadErr, "SaveRunState": saveErr} {
		if err == nil || !strings.HasPrefix(err.Error(), "starea probelor") || !strings.Contains(err.Error(), "ștergeți dosarul "+folder) {
			t.Errorf("%s of an old run state = %v; want a Romanian line that says the folder %s can be deleted", name, err, folder)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != old { //nolint:gosec // the test's file
		t.Errorf("the old run state changed: %q, %v", data, err)
	}
}

// TestRunState_RunIDIsAFileName (Final fix A review, Minor 1): the run
// state lies in results/, which the owner's own token may change, and the
// elevated resume joins its run id to work/. A run id that is not one file
// name refuses the load, as a damaged state does.
func TestRunState_RunIDIsAFileName(t *testing.T) {
	for _, id := range []string{`..\..\Windows\Temp\x`, "../../etc", "a/b", ""} {
		path := filepath.Join(t.TempDir(), "state", "run-state.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		states := `{"active":"pc-bugcheck","sets":{"pc-bugcheck":{"set":"pc-bugcheck","mode":"automated","kind":"bugcheck","done":[],` +
			`"current":{"runId":` + strconv.Quote(id) + `,"index":1,"attempt":1,"encrypted":false,"padLowBitrate":true,"streams":["cam01_main"],"phase":"recording"}}}}` + "\n"
		if err := os.WriteFile(path, []byte(states), 0o600); err != nil {
			t.Fatal(err)
		}
		// The reason inside it is Romanian too (Final fix B review, M6).
		want := "identificatorul rulării " + strconv.Quote(id) + " nu este un nume de fișier"
		if st, err := LoadRunState(path); err == nil || !strings.HasPrefix(err.Error(), "starea probelor nu se poate citi") || !strings.Contains(err.Error(), want) {
			t.Errorf("run id %q: LoadRunState = %+v, %v; want the Romanian refusal of a damaged state, with «%s»", id, st, err, want)
		}
	}
}

// recordSyncs records each file and folder that the driver syncs, and
// still syncs them.
func recordSyncs(t *testing.T) *[]string {
	t.Helper()
	var synced []string
	realPath, realDir := syncPath, syncDir
	t.Cleanup(func() { syncPath, syncDir = realPath, realDir })
	syncPath = func(path string) error {
		synced = append(synced, "file "+path)
		return realPath(path)
	}
	syncDir = func(dir string) error {
		synced = append(synced, "folder "+dir)
		return realDir(dir)
	}
	return &synced
}

// TestSaveRunState_FlushesTheRenamedFile (mi1-3): the run state is renamed
// over the old one (with write-through on Windows), and the renamed file is
// flushed before its folder: the state saved just before NotMyFault or
// sysrq must be on disk when the host goes down.
func TestSaveRunState_FlushesTheRenamedFile(t *testing.T) {
	synced := recordSyncs(t)
	path := filepath.Join(t.TempDir(), "state", "run-state.json")
	st := &RunState{Set: SetPCBugcheck, Mode: ModeAutomated, Kind: "bugcheck",
		Current: &CurrentRun{RunID: "pc-bugcheck-02-1", Index: 2, Attempt: 1, Streams: []string{MainStream}, Phase: PhaseStopping}}
	realPath := syncPath
	syncPath = func(p string) error {
		if p == path {
			if got, err := LoadRunState(path); err != nil || got == nil || got.Current == nil || got.Current.Phase != PhaseStopping {
				t.Errorf("the file flushed holds %+v (%v), want the state just saved: the rename comes first", got, err)
			}
		}
		return realPath(p)
	}
	if err := SaveRunState(path, st); err != nil {
		t.Fatal(err)
	}
	if want := []string{"file " + path, "folder " + filepath.Dir(path)}; !reflect.DeepEqual(*synced, want) {
		t.Errorf("synced %q\nwant %q", *synced, want)
	}
}

// TestWriteRun_TheRunsFilesOutliveAPowerCut (G09, G14): the run's run.json
// and RI_g, which recspike reads again after the host's reboot, reach the
// disk before the run starts, and so do the entries of the run's folder and
// of the folder in work/. Otherwise a stop that comes before the system has
// written them out, a qemu-reset or sysrq 20 s into a smoke run, leaves
// run.json empty after the reboot ("hubproc: run.json: EOF" in the
// mi1-smoke guest).
func TestWriteRun_TheRunsFilesOutliveAPowerCut(t *testing.T) {
	synced := recordSyncs(t)
	work := t.TempDir()
	d := &Driver{Work: work}
	cur := &CurrentRun{RunID: "pi-sysrq-01-1", Encrypted: true, PadLowBitrate: true, Streams: []string{MainStream}}
	vol, err := Dirs{Root: work}.Prepare(t.Context(), cur.RunID, true)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{info: link.Session{RTSPBase: "rtsp://127.0.0.1:8554", User: "u", Pass: "p"}, controllerHost: "127.0.0.1"}
	config, err := d.writeRun(cur, vol, s)
	if err != nil {
		t.Fatalf("writeRun: %v", err)
	}
	run := filepath.Join(work, cur.RunID)
	want := []string{"file " + filepath.Join(run, "ri_g.key"), "file " + config, "folder " + run, "folder " + work}
	if !reflect.DeepEqual(*synced, want) {
		t.Errorf("synced %q\nwant %q", *synced, want)
	}
}

// TestWriteDurably_SyncsTheFileAndItsFolder: a file that recspike reads
// after a reboot, such as the controller's address, is written, synced and
// its folder's entry synced.
func TestWriteDurably_SyncsTheFileAndItsFolder(t *testing.T) {
	synced := recordSyncs(t)
	path := filepath.Join(t.TempDir(), "work", "controller")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurably(path, []byte("192.0.2.1:7081\n"), 0o600); err != nil {
		t.Fatalf("WriteDurably: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "192.0.2.1:7081\n" { //nolint:gosec // the test's own file
		t.Errorf("the file holds %q (%v)", data, err)
	}
	if want := []string{"file " + path, "folder " + filepath.Dir(path)}; !reflect.DeepEqual(*synced, want) {
		t.Errorf("synced %q, want %q", *synced, want)
	}
}
