package mi1

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

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
