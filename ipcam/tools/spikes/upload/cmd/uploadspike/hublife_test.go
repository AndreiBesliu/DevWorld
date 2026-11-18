package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
)

// ending ends an mi6 holder one way: a signal, a console event, or a kill.
type ending struct {
	name string
	end  func(t *testing.T, r *rig, p *proc)
	// killed: the holder had no handler, so its hub stops on its stdin's
	// end alone.
	killed bool
}

// TestHubLife_HolderEndsAnyWay_OneHubUnderResume (Hub lifetime, Self-healing
// autostart): a resume runs as the autostart runs it, and an mi6 holder
// takes the hub from it through the handover; the holder is then ended in
// turn by each of the platform's endings (Linux: SIGHUP, SIGINT, SIGTERM,
// SIGKILL; Windows: CTRL_BREAK_EVENT, SIGTERM injected into the handler,
// which is what Go makes of CTRL_CLOSE_EVENT, and TerminateProcess). After
// each, within seconds, exactly one stand-in hub runs, resume is its
// parent and hub.pid names it; the holder's hub has exited, after a kill
// too, through its closed stdin (on Linux the stand-in recorded its stdin
// stop; a Pdeathsig SIGTERM may be recorded too). The stand-in records
// SIGTERM but stops only at its stdin's end.
func TestHubLife_HolderEndsAnyWay_OneHubUnderResume(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	res := r.start(nil, "resume")
	until(t, time.Minute, "resume's first hub", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) == 1 })
	signalFile := filepath.Join(t.TempDir(), "signal")
	for _, tc := range holderEndings(signalFile) {
		t.Run(tc.name, func(t *testing.T) {
			resumeStarts := len(r.startsBy(res.cmd.Process.Pid))
			m := startHolder(t, r, envSignalFile+"="+signalFile)
			mHub := r.startsBy(m.cmd.Process.Pid)[0].PID
			if live := r.liveHubs(); !slices.Equal(live, []int{mHub}) {
				t.Fatalf("live hubs %v under the holder; want its own %d alone", live, mHub)
			}
			tc.end(t, r, m)
			if !m.wait(time.Minute) {
				t.Fatalf("the holder did not end\nstdout:\n%s", m.stdout)
			}
			until(t, 2*time.Minute, "resume's hub after the holder's end", func() bool {
				return len(r.startsBy(res.cmd.Process.Pid)) == resumeStarts+1
			})
			until(t, 30*time.Second, "one hub", func() bool { return len(r.liveHubs()) == 1 })
			resHub := r.startsBy(res.cmd.Process.Pid)[resumeStarts].PID
			if live := r.liveHubs(); !slices.Equal(live, []int{resHub}) || r.hubPID() != resHub {
				t.Errorf("live hubs %v, hub.pid %d; want resume's hub %d alone", live, r.hubPID(), resHub)
			}
			if alive(mHub) {
				t.Errorf("the holder's hub %d still runs", mHub)
			}
			stopped := slices.ContainsFunc(r.records(spiketest.RecordEOFStop), func(rec spiketest.FakeRecord) bool { return rec.PID == mHub })
			if !stopped && (!tc.killed || runtimeGOOS() == "linux") {
				t.Errorf("the holder's hub %d recorded no stop at its stdin's end", mHub)
			}
			for _, rec := range r.records(spiketest.RecordSignal) {
				if rec.PID == mHub && !strings.Contains(rec.Detail, "terminated") {
					t.Errorf("the holder's hub got %s", rec.Detail)
				}
			}
		})
	}
	if alive(res.cmd.Process.Pid) != true {
		t.Error("resume ended")
	}
	_ = os.Remove(signalFile)
}
