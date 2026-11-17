package main

import (
	"os"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func runtimeGOOS() string { return runtime.GOOS }

// holderEndings are Windows's: CTRL_BREAK_EVENT to the holder's own
// process group; SIGTERM injected into its handler through the signal
// file, which is what Go makes of CTRL_CLOSE_EVENT; and TerminateProcess,
// which it cannot catch.
func holderEndings(signalFile string) []ending {
	return []ending{
		{name: "CTRL_BREAK_EVENT", end: func(t *testing.T, _ *rig, p *proc) {
			if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.cmd.Process.Pid)); err != nil { //nolint:gosec // a PID
				t.Fatal(err)
			}
		}},
		{name: "SIGTERM injected", end: func(t *testing.T, _ *rig, _ *proc) {
			if err := os.WriteFile(signalFile, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "TerminateProcess", end: func(t *testing.T, _ *rig, p *proc) {
			if err := p.cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
		}, killed: true},
	}
}
