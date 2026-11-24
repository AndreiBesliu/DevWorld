package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMI8_SurvivesHangupAfterPrompts (P42, P49): mi8 answered on a terminal
// (a pty, as over SSH) hands its run to a detached copy and exits; closing
// the terminal and a SIGHUP to its process group leave MI8 running: the
// simulated camera keeps receiving PullMessages for 2 min, the run state's
// heartbeat advances, and status shows the run. The copy's argv and
// environment hold no camera value, and its stdin is closed once read.
func TestMI8_SurvivesHangupAfterPrompts(t *testing.T) {
	s := startOnvifSim(t, "Hikvision/A")
	r := mi8Rig(t, "1h")
	r.stopMI8()
	master, slave := openPTY(t, t)
	cmd := exec.Command(uploadspikeBin(t), "mi8") //nolint:gosec // the test's own build
	cmd.Env = r.env
	cmd.Dir = r.cwd
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })
	out := &syncBuffer{}
	go func() { _, _ = io.Copy(out, master) }()
	steps := []struct{ prompt, answer string }{
		{promptMI8Count, "1"},
		{fmt.Sprintf(promptMI8Address, "real-1"), s.Hosts[0]},
		{fmt.Sprintf(promptMI8User, "real-1"), camUser},
		{fmt.Sprintf(promptMI8Pass, "real-1"), camPass},
	}
	for _, st := range steps {
		until(t, time.Minute, "the prompt "+st.prompt, func() bool { return strings.Contains(out.String(), st.prompt) })
		if _, err := master.WriteString(st.answer + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	until(t, 2*time.Minute, "the background line", func() bool { return strings.Contains(out.String(), msgMI8Background) })
	select {
	case <-exited:
	case <-time.After(time.Minute):
		t.Fatal("mi8 did not exit after handing over")
	}
	if strings.Contains(out.String(), camPass) {
		t.Error("the password was echoed")
	}
	// The SSH window closes: the terminal hangs up, and its session gets a
	// SIGHUP.
	_ = master.Close()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
	st := r.mi8Open()
	if st == nil || st.PID == 0 || st.PID == cmd.Process.Pid {
		t.Fatalf("no detached copy in the run state: %+v", st)
	}
	pid := st.PID
	proc := fmt.Sprintf("/proc/%d", pid)
	cmdline, _ := os.ReadFile(proc + "/cmdline") //nolint:gosec // the copy's
	environ, _ := os.ReadFile(proc + "/environ") //nolint:gosec // the copy's
	fd0, _ := os.Readlink(filepath.Join(proc, "fd", "0"))
	noCameraValue(t, "the copy's argv", string(cmdline), camUser, camPass, s.Hosts[0])
	noCameraValue(t, "the copy's environment", string(environ), camUser, camPass, s.Hosts[0])
	if fd0 != os.DevNull {
		t.Errorf("the copy's stdin is %q, want %s once read", fd0, os.DevNull)
	}
	until(t, time.Minute, "the subscription", func() bool { return s.counts()[0].Subscriptions == 1 })
	pulls0, beat0 := s.counts()[0].Pulls, r.mi8Open().HeartbeatMs
	time.Sleep(2 * time.Minute)
	if c := s.counts()[0]; c.Pulls <= pulls0 || c.Subscriptions != 1 {
		t.Errorf("after 2 min: %+v, want more PullMessages than %d and one subscription", c, pulls0)
	}
	now := r.mi8Open()
	if now == nil || now.PID != pid || now.HeartbeatMs <= beat0 {
		t.Errorf("the heartbeat did not advance: %+v", now)
	}
	p := r.run("", nil, "status")
	if !strings.Contains(p.stdout.String(), "MI8 rulează: ") {
		t.Errorf("status:\n%s", p.stdout)
	}
}
