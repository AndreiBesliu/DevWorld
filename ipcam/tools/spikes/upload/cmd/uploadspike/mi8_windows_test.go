package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

// TestMI8_WindowsKeepsWindowText (P49): on Windows the console window is
// MI8's process: the prompts end with «Lăsați fereastra deschisă 72 de
// ore.», no detached copy starts, and the window's process runs the
// subscription.
func TestMI8_WindowsKeepsWindowText(t *testing.T) {
	s := startOnvifSim(t, "Hikvision/A")
	r := mi8Rig(t, "1h")
	p := r.start(nil, "mi8")
	_, _ = io.WriteString(p.stdin, answers([3]string{s.Hosts[0], camUser, camPass}))
	until(t, time.Minute, "the window's text", func() bool { return strings.Contains(p.stdout.String(), msgMI8KeepWindow) })
	if strings.Contains(p.stdout.String(), msgMI8Background) {
		t.Errorf("Windows printed the background line:\n%s", p.stdout)
	}
	until(t, time.Minute, "the subscription", func() bool { return s.counts()[0].Subscriptions == 1 })
	until(t, time.Minute, "the run's heartbeat", func() bool { st := r.mi8Open(); return st != nil && st.HeartbeatMs > 0 })
	if st := r.mi8Open(); st.PID != p.cmd.Process.Pid {
		t.Errorf("the run's process is %d, want the window's %d", st.PID, p.cmd.Process.Pid)
	}
	if p.ended() {
		t.Errorf("mi8 ended: exit %d", p.exitCode())
	}
}
