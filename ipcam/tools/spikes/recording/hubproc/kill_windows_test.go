package hubproc

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestKill_AccessDeniedNeedsASignalledHandle: TerminateProcess's
// ERROR_ACCESS_DENIED means "already exited" only once the process handle
// is signalled. A process still running behind that error must come back as
// the error, never as os.ErrProcessDone: the driver would start a second
// ipcam-hub on the same data folder.
func TestKill_AccessDeniedNeedsASignalledHandle(t *testing.T) {
	denied := os.NewSyscallError("TerminateProcess", windows.ERROR_ACCESS_DENIED)
	other := os.NewSyscallError("TerminateProcess", windows.ERROR_INVALID_HANDLE)
	waitFailed := errors.New("OpenProcess: invalid parameter")
	for _, tc := range []struct {
		name      string
		killErr   error
		signalled bool
		waitErr   error
		wantWait  bool
		wantDone  bool
		wantErr   error
	}{
		{name: "killed", killErr: nil},
		{name: "denied, handle signalled", killErr: denied, signalled: true, wantWait: true, wantDone: true},
		{name: "denied, still running", killErr: denied, signalled: false, wantWait: true, wantErr: windows.ERROR_ACCESS_DENIED},
		{name: "denied, wait failed", killErr: denied, waitErr: waitFailed, wantWait: true, wantErr: windows.ERROR_ACCESS_DENIED},
		{name: "released by cmd.Wait", killErr: syscall.EINVAL, wantDone: true},
		{name: "another error", killErr: other, wantErr: windows.ERROR_INVALID_HANDLE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var waited []time.Duration
			err := killResult(tc.killErr, func(d time.Duration) (bool, error) {
				waited = append(waited, d)
				return tc.signalled, tc.waitErr
			})
			if tc.wantWait != (len(waited) == 1) || len(waited) > 1 {
				t.Errorf("waits = %v, want one: %v", waited, tc.wantWait)
			}
			if tc.wantWait && len(waited) == 1 && waited[0] != exitWait {
				t.Errorf("waited %v, want exitWait (%v)", waited[0], exitWait)
			}
			if got := errors.Is(err, os.ErrProcessDone); got != tc.wantDone {
				t.Errorf("killResult = %v; ErrProcessDone: %v, want %v", err, got, tc.wantDone)
			}
			switch {
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Errorf("killResult = %v, want %v", err, tc.wantErr)
			case tc.wantErr == nil && !tc.wantDone && err != nil:
				t.Errorf("killResult = %v, want nil", err)
			}
		})
	}
}

// TestKill_ProcessEndedWaitsOnTheRealHandle: the waiter kill uses reads
// the real process object: a running child is not signalled within a short
// wait, and a killed one that the caller has not reaped yet is.
func TestKill_ProcessEndedWaitsOnTheRealHandle(t *testing.T) {
	t.Setenv(fakeEnv, "1")
	// The fake runs until its stdin ends: the pipe stays open.
	cmd := exec.Command(os.Args[0], "run", "--config", writeRunJSON(t), "--stop-on-stdin-eof") //nolint:gosec // the test binary as the fake ipcam-hub
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })

	if ended, err := processEnded(cmd.Process.Pid, 50*time.Millisecond); err != nil || ended {
		t.Fatalf("processEnded(running) = %v, %v; want false, nil", ended, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if ended, err := processEnded(cmd.Process.Pid, exitWait); err != nil || !ended {
		t.Fatalf("processEnded(killed) = %v, %v; want true, nil", ended, err)
	}
}
