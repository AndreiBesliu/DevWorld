package main

import (
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
)

func runtimeGOOS() string { return runtime.GOOS }

// holderEndings are Linux's: SIGHUP, SIGINT, SIGTERM, which the holder
// catches, and SIGKILL, which it cannot.
func holderEndings(string) []ending {
	sig := func(s syscall.Signal) func(*testing.T, *rig, *proc) {
		return func(t *testing.T, _ *rig, p *proc) {
			if err := syscall.Kill(p.cmd.Process.Pid, s); err != nil {
				t.Fatal(err)
			}
		}
	}
	return []ending{
		{name: "SIGHUP", end: sig(syscall.SIGHUP)},
		{name: "SIGINT", end: sig(syscall.SIGINT)},
		{name: "SIGTERM", end: sig(syscall.SIGTERM)},
		{name: "SIGKILL", end: sig(syscall.SIGKILL), killed: true},
	}
}

// TestHubLife_HangupReachesOnlyUploadspike (Hub lifetime): a SIGHUP to the
// holder's process group, as a terminal hangup sends it, never reaches the
// stand-in hub, which runs in a session of its own; the hub stops through
// the holder's hand-back, at its stdin's end.
func TestHubLife_HangupReachesOnlyUploadspike(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	m := startHolder(t, r)
	hub := r.startsBy(m.cmd.Process.Pid)[0].PID
	if err := syscall.Kill(-m.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	if !m.wait(time.Minute) {
		t.Fatal("the holder did not end at the hangup")
	}
	until(t, 30*time.Second, "the hub's end", func() bool { return !alive(hub) })
	for _, rec := range r.records(spiketest.RecordSignal) {
		if rec.PID == hub {
			t.Errorf("the hub got %s", rec.Detail)
		}
	}
	if !slices.ContainsFunc(r.records(spiketest.RecordEOFStop), func(rec spiketest.FakeRecord) bool { return rec.PID == hub }) {
		t.Error("the hub did not stop at its stdin's end")
	}
}
