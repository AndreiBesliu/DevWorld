// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"flag"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// runStatus prints each open run's state from the run state: a client,
// which opens no listener and takes no lock (Inbound listeners).
func runStatus(_ context.Context, e *env, args []string) error {
	if err := cli.ParseFlags(flag.NewFlagSet("status", flag.ContinueOnError), args, e.out, usage); err != nil {
		return err
	}
	lines, err := statusLines(e)
	if err != nil {
		return err
	}
	for _, l := range lines {
		e.printf("%s", l)
	}
	return nil
}

// statusLines are the open runs' lines: MI6's pairs and days, with the B1
// reconnections that a failed day asks for (Task 8 adds MI5's phase, Task
// 10 MI8's hours). The holder's status.html shows the same.
func statusLines(e *env) ([]string, error) {
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return nil, err
	}
	var lines []string
	if raw, ok := st.Open["MI6"]; ok {
		run, err := mi6.Decode(raw)
		if err != nil {
			return nil, err
		}
		mi5Open, _ := mi5State(st)
		lines = append(lines, mi6.StatusLines(run, st.Gaps, e.now(), mi5Open)...)
	}
	if len(lines) == 0 {
		lines = []string{msgNoOpenRun}
	}
	return lines, nil
}
