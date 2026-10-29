package mi3

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// The test binary stands in for a load when helperEnv is set: "burn" spins
// on a CPU until its stdin ends; "parent" starts a burning child of its own
// and waits for its stdin's end too.
const helperEnv = "MI3_TEST_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "burn":
		var done atomic.Bool
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			done.Store(true)
		}()
		for !done.Load() {
			runtime.Gosched() // nothing else runs: this spins on a CPU
		}
		os.Exit(0)
	case "parent":
		child := exec.Command(os.Args[0]) //nolint:gosec // the test binary itself
		child.Env = append(os.Environ(), helperEnv+"=burn")
		stdin, err := child.StdinPipe()
		if err != nil || child.Start() != nil {
			os.Exit(1)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = stdin.Close()
		_ = child.Wait()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// helper starts the test binary as kind, and ends it when the test does.
func helper(t *testing.T, kind string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0]) //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), helperEnv+"="+kind)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	return cmd
}

// TestCPU_MeasuresAProcessTreeAndTheMachine (G20): a load's CPU is its
// process's and its descendants', as ipcam-hub's includes MediaMTX's; the
// machine's busy time and the tree's memory are read from the OS
// (/proc on Linux, GetSystemTimes, GetProcessTimes and the working set on
// Windows).
func TestCPU_MeasuresAProcessTreeAndTheMachine(t *testing.T) {
	parent := helper(t, "parent")
	pid := parent.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for len(tree(pid)) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("tree(%d) = %v, want the parent and its burning child", pid, tree(pid))
		}
		time.Sleep(20 * time.Millisecond)
	}
	roots := map[string]int{"load": pid}
	start, err := sampleUsage(roots)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	end, err := sampleUsage(roots)
	if err != nil {
		t.Fatal(err)
	}
	used := end.cpuSince(start, "load")
	core := end.pctOfCore(start, used)
	machine := end.pctOfMachine(start, used)
	total := end.pctOfMachine(start, end.busy-start.busy)
	t.Logf("the tree used %v: %.0f %% of a core, %.1f %% of the machine; the machine %.1f %%", used, core, machine, total)
	if core < 30 || core > 105*float64(len(end.procs["load"])) {
		t.Errorf("a spinning child used %.0f %% of a core, want most of one", core)
	}
	// The machine's busy time and a process's are different counters: on
	// Linux /proc/stat counts ticks and /proc/<pid>/stat the time run. Over
	// 1.5 s on a CI runner that also runs other tests, they can differ by a
	// few percent of the machine, so the tree may exceed the machine's busy
	// share by that much.
	if machine <= 0 || machine > total+5 {
		t.Errorf("the tree used %.1f %% of the machine, which was %.1f %% busy", machine, total)
	}
	if !slices.Contains(tree(pid), pid) || len(end.procs["load"]) < 2 {
		t.Errorf("the tree's processes %v, want the parent and its child", end.procs["load"])
	}
	if mem := memory(roots); mem < 1<<20 {
		t.Errorf("memory = %d bytes, want the two processes' resident memory", mem)
	}

	_ = parent.Process.Kill()
	_ = parent.Wait()
	if _, err := sampleUsage(roots); err == nil {
		t.Error("a load that ended was sampled without an error")
	}
}
