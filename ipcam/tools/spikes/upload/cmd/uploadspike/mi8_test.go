package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi8"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// The test cameras' account, assembled at run time.
var (
	camUser = "onvif" + "user-4417"
	camPass = "SENT" + "INEL-onvifpass-2"
)

// onvifSim is an onvifsim process: simulated ONVIF cameras that the real
// ipcam-hub diag onvif talks to.
type onvifSim struct {
	t     *testing.T
	stdin io.WriteCloser
	lines *bufio.Scanner
	mu    sync.Mutex
	Hosts []string // 127.0.0.1:port, as the owner types an address
}

// startOnvifSim starts onvifsim with the cameras (brand/model).
func startOnvifSim(t *testing.T, cams ...string) *onvifSim {
	t.Helper()
	args := []string{"-user", camUser, "-pass", camPass}
	for _, c := range cams {
		args = append(args, "-cam", c)
	}
	cmd := exec.Command(binary(t, "onvifsim", "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events/onviftest/cmd/onvifsim"), args...) //nolint:gosec // the test's own build
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })
	s := &onvifSim{t: t, stdin: stdin, lines: bufio.NewScanner(out)}
	var ready struct {
		Cameras []struct {
			HostPort string `json:"hostPort"`
		} `json:"cameras"`
	}
	if !s.lines.Scan() || json.Unmarshal(s.lines.Bytes(), &ready) != nil {
		t.Fatal("onvifsim gave no ready line")
	}
	for _, c := range ready.Cameras {
		s.Hosts = append(s.Hosts, c.HostPort)
	}
	return s
}

// simCamCounts are a simulated camera's subscription counts.
type simCamCounts struct {
	Subscriptions, Creates, Renews, Pulls, Unsubscribes int
}

// counts are the simulated cameras' counts.
func (s *onvifSim) counts() []simCamCounts {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := io.WriteString(s.stdin, "counts\n"); err != nil {
		s.t.Fatal(err)
	}
	var c struct {
		Cameras []simCamCounts `json:"cameras"`
	}
	if !s.lines.Scan() || json.Unmarshal(s.lines.Bytes(), &c) != nil {
		s.t.Fatal("onvifsim gave no counts")
	}
	return c.Cameras
}

// mi8Rig is a device with the real ipcam-hub, MI8 on the snapshot diff
// without snapshots (no FFmpeg, no reference), closing after target.
func mi8Rig(t *testing.T, target string) *rig {
	t.Helper()
	r := newRigWith(t, binary(t, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub"), "")
	r.env = append(r.env, envFFmpeg+"=-", envMI8Timing+"=target="+target+",progress=500ms")
	return r
}

// answers is the prompts' input for cameras given as host, user, password.
func answers(cams ...[3]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d\n", len(cams))
	for _, c := range cams {
		fmt.Fprintf(&b, "%s\n%s\n%s\n", c[0], c[1], c[2])
	}
	return b.String()
}

// mi8Open is the rig's open MI8 run, nil without one.
func (r *rig) mi8Open() *mi8.State {
	r.t.Helper()
	raw, ok := r.state().Open["MI8"]
	if !ok {
		return nil
	}
	s, err := mi8.Decode(raw)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

// stopMI8 ends the rig's MI8 process at the test's end, if one runs: its
// detached copy (Linux), which unsubscribes and ends on SIGTERM.
func (r *rig) stopMI8() {
	r.t.Cleanup(func() {
		raw, ok := r.state().Open["MI8"]
		if !ok {
			return
		}
		s, err := mi8.Decode(raw)
		if err != nil || !runstate.Alive(s.PID, s.ProcStartMs) {
			return
		}
		if p, err := os.FindProcess(s.PID); err == nil {
			_ = terminate(p)
		}
		deadline := time.Now().Add(30 * time.Second)
		for runstate.Alive(s.PID, s.ProcStartMs) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	})
}

// noCameraValue fails when text holds one of the cameras' values.
func noCameraValue(t *testing.T, what, text string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(text, v) {
			t.Errorf("%s holds a camera value %q", what, v)
		}
	}
}

// TestMI8_CameraAddressWithOnvifPort (P42): an address typed as IP:port
// reaches the camera on that port, and --probe-only prints each camera's
// brand and model, or «portul ONVIF nu răspunde» for a closed port.
func TestMI8_CameraAddressWithOnvifPort(t *testing.T) {
	s := startOnvifSim(t, "Hikvision/DS-2CD2043")
	r := mi8Rig(t, "1m")
	closed := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p := r.run(answers([3]string{s.Hosts[0], camUser, camPass}, [3]string{closed, camUser, camPass}), nil, "mi8", "--probe-only")
	out := p.stdout.String()
	if p.exitCode() != 0 {
		t.Fatalf("exit %d: %s", p.exitCode(), p.errorLine())
	}
	if want := fmt.Sprintf(msgMI8ProbeOK, "real-1", "Hikvision", "DS-2CD2043", msgMI8Yes, msgMI8Yes); !strings.Contains(out, want) {
		t.Errorf("stdout lacks %q:\n%s", want, out)
	}
	if want := fmt.Sprintf(msgMI8ProbePort, "real-2"); !strings.Contains(out, want) || !strings.Contains(want, "portul ONVIF nu răspunde") {
		t.Errorf("stdout lacks %q:\n%s", want, out)
	}
	noCameraValue(t, "the output", out+p.stderr.String(), camPass, camUser, s.Hosts[0], closed)
	if r.mi8Open() != nil {
		t.Error("--probe-only opened a run")
	}
	if c := s.counts(); c[0].Creates != 0 {
		t.Errorf("--probe-only subscribed: %+v", c[0])
	}
}

// TestMI8_SecondInstanceRefused (Single instance): while MI8 runs (Linux:
// its detached copy; Windows: the window's process), a second mi8 and an mi8
// --continue each print «MI8 rulează deja; …» and exit 1 before any prompt,
// and the simulated camera holds one subscription; on Linux a second mi8
// started before the copy's first heartbeat is refused too.
func TestMI8_SecondInstanceRefused(t *testing.T) {
	s := startOnvifSim(t, "Hikvision/A")
	r := mi8Rig(t, "1h")
	r.env = append(r.env, envMI8Timing+"=target=1h,progress=500ms,beat=4s")
	r.stopMI8()
	first := r.start(nil, "mi8")
	_, _ = io.WriteString(first.stdin, answers([3]string{s.Hosts[0], camUser, camPass}))
	refused := func(when string, args ...string) {
		t.Helper()
		p := r.run("", nil, args...)
		if p.exitCode() != 1 || !strings.Contains(p.stdout.String(), msgMI8Running) || strings.Contains(p.stdout.String(), promptMI8Count) ||
			strings.Contains(p.stdout.String(), "Camera real-1") {
			t.Errorf("%s, %v: exit %d, stdout:\n%s", when, args, p.exitCode(), p.stdout)
		}
	}
	until(t, time.Minute, "the prompts' end", func() bool { return strings.Contains(first.stdout.String(), "Intră în proba MI8") })
	if runtime.GOOS == "linux" {
		refused("before the copy's first heartbeat", "mi8")
		if !first.wait(2 * time.Minute) {
			t.Fatalf("mi8 did not hand over to its copy:\n%s", first.stdout)
		}
		if first.exitCode() != 0 || !strings.Contains(first.stdout.String(), msgMI8Background) {
			t.Fatalf("mi8: exit %d, stdout:\n%s", first.exitCode(), first.stdout)
		}
	} else {
		until(t, time.Minute, "the window's text", func() bool { return strings.Contains(first.stdout.String(), msgMI8KeepWindow) })
	}
	until(t, time.Minute, "the subscription", func() bool { return s.counts()[0].Subscriptions == 1 })
	refused("while it runs", "mi8")
	refused("while it runs", "mi8", "--continue")
	time.Sleep(2 * time.Second)
	if c := s.counts()[0]; c.Subscriptions != 1 || c.Creates != 1 {
		t.Errorf("the camera holds %d subscriptions after %d creates, want 1 and 1", c.Subscriptions, c.Creates)
	}
}

// openMI8Run writes an open MI8 run into the rig's run state: 40 observed
// hours that ended an hour ago on another boot, closing at 40 h + extra.
func (r *rig) openMI8Run(extra time.Duration, cams ...mi8.CameraState) *mi8.State {
	r.t.Helper()
	now := time.Now().UnixMilli()
	h := time.Hour.Milliseconds()
	st := &mi8.State{RunID: "mi8-continue", StartMs: now - 41*h, QuietGapSecs: 10, TargetMs: 40*h + extra.Milliseconds(),
		Sessions: []mi8.Session{{StartMs: now - 41*h, EndMs: now - h}}, BootID: "another-boot", HeartbeatMs: now - h,
		Cameras: cams, PID: 1 << 22, ProcStartMs: 1}
	r.update(func(s *runstate.State) { s.Open["MI8"] = st.Encode() })
	return st
}

// TestMI8_ContinueAfterRebootAddsHours (P49): after a reboot, mi8 --continue
// lists real-1 and real-2 with their brand and model from the run state,
// asks each one's address, user and password, and adds the new session's
// hours to the run's 40, with one reboot gap; the run state holds no
// address, user or password at any time. Sessions of 40 h and 33 h make 73
// (mi8's TestMI8_SessionsAddUpAcrossReboot).
func TestMI8_ContinueAfterRebootAddsHours(t *testing.T) {
	s := startOnvifSim(t, "Hikvision/A", "Dahua/B")
	r := mi8Rig(t, "1h")
	r.stopMI8()
	r.openMI8Run(4*time.Second, mi8.CameraState{Camera: "real-1", Brand: "Hikvision", Model: "A",
		Episodes: []results.EpisodeTime{{StartMs: 1, EndMs: 2, Matched: true}}},
		mi8.CameraState{Camera: "real-2", Brand: "Dahua", Model: "B"})
	secrets := []string{camUser, camPass, s.Hosts[0], s.Hosts[1]}
	in := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n", s.Hosts[0], camUser, camPass, s.Hosts[1], camUser, camPass)
	p := r.start(nil, "mi8", "--continue")
	_, _ = io.WriteString(p.stdin, in)
	stateText := func() string {
		b, _ := os.ReadFile(filepath.Join(r.base, "state", runstate.StateFile)) //nolint:gosec // the test's own file
		return string(b)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for r.mi8Open() != nil && time.Now().Before(deadline) {
		noCameraValue(t, "the run state", stateText(), secrets...)
		time.Sleep(200 * time.Millisecond)
	}
	if r.mi8Open() != nil {
		t.Fatalf("the run did not close; stdout:\n%s\nstderr:\n%s", p.stdout, p.stderr)
	}
	if !p.wait(time.Minute) {
		t.Fatal("mi8 --continue did not end")
	}
	out := p.stdout.String()
	for _, want := range []string{fmt.Sprintf(msgMI8ContinueCam, "real-1", "Hikvision A"), fmt.Sprintf(msgMI8ContinueCam, "real-2", "Dahua B"),
		fmt.Sprintf(promptMI8Again, "real-1"), fmt.Sprintf(promptMI8Pass, "real-2"), fmt.Sprintf(msgMI8ContinueHours, 40.0)} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	noCameraValue(t, "the run state", stateText(), secrets...)
	files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI8", "*", "mi8-continue.json"))
	if len(files) != 1 {
		t.Fatalf("envelopes = %v", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	noCameraValue(t, "the envelope", string(b), secrets...)
	var envl results.Envelope
	if err := json.Unmarshal(b, &envl); err != nil {
		t.Fatal(err)
	}
	if envl.MI8.Hours <= 40 || envl.MI8.Hours > 40.1 {
		t.Errorf("hours = %v, want 40 h plus the new session's seconds", envl.MI8.Hours)
	}
	if gaps := envl.Runs[0].Gaps; len(gaps) != 1 || gaps[0].Reason != results.GapReboot {
		t.Errorf("gaps = %+v, want one reboot gap", gaps)
	}
	if len(envl.MI8.Cameras) != 2 || envl.MI8.Cameras[0].Episodes != 1 || envl.MI8.Cameras[0].Brand != "Hikvision" {
		t.Errorf("cameras = %+v, want real-1's earlier episode kept", envl.MI8.Cameras)
	}
	if !envl.TestSeams {
		t.Error("an envelope written with test seams is not marked")
	}
	if c := s.counts(); c[0].Creates == 0 || c[1].Creates == 0 {
		t.Errorf("the cameras were not subscribed to again: %+v", c)
	}
	if lines, _ := statusLines(&env{base: r.base, now: time.Now}); !strings.Contains(strings.Join(lines, "\n"), msgMI8Done) {
		t.Errorf("status after the close = %q", lines)
	}
}

// terminate asks a process to end: SIGTERM, or a kill on Windows.
func terminate(p *os.Process) error {
	if runtime.GOOS == "windows" {
		return p.Kill()
	}
	return p.Signal(syscall.SIGTERM)
}

// TestMI8_ContinueRefusesAnotherCamera (P42): under --continue an address
// whose probe gives another brand or model than real-1's is refused with
// «Aceasta nu este camera real-1 (…). Verificați adresa.», the camera is
// asked again, and the other camera is never subscribed to nor adds hours.
func TestMI8_ContinueRefusesAnotherCamera(t *testing.T) {
	s := startOnvifSim(t, "Dahua/B")
	r := mi8Rig(t, "1h")
	r.stopMI8()
	before := r.openMI8Run(time.Hour, mi8.CameraState{Camera: "real-1", Brand: "Hikvision", Model: "A"})
	p := r.run(fmt.Sprintf("%s\n%s\n%s\n\n", s.Hosts[0], camUser, camPass), nil, "mi8", "--continue")
	out := p.stdout.String()
	if want := fmt.Sprintf(msgMI8NotThisCamera, "real-1", "Hikvision A"); !strings.Contains(out, want) {
		t.Errorf("stdout lacks %q:\n%s", want, out)
	}
	if strings.Count(out, fmt.Sprintf(promptMI8Again, "real-1")) != 2 {
		t.Errorf("real-1 was not asked again:\n%s", out)
	}
	if p.exitCode() != 1 {
		t.Errorf("exit %d, want 1 (no camera left)", p.exitCode())
	}
	after := r.mi8Open()
	if after == nil || after.ObservedMs() != before.ObservedMs() || len(after.Sessions) != 1 {
		t.Errorf("the run changed: %+v", after)
	}
	if c := s.counts(); c[0].Creates != 0 {
		t.Errorf("the other camera was subscribed to: %+v", c[0])
	}
	// A new mi8 leaves the stopped run to --continue, before any prompt.
	p = r.run("", nil, "mi8")
	if p.exitCode() != 1 || !strings.Contains(p.stderr.String(), msgMI8OpenRun) || strings.Contains(p.stdout.String(), promptMI8Count) {
		t.Errorf("mi8 over a stopped run: exit %d, stdout:\n%s\nstderr:\n%s", p.exitCode(), p.stdout, p.stderr)
	}
}
