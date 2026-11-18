package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

func TestMain(m *testing.M) {
	if os.Getenv(spiketest.FakeHubEnv) == "1" {
		os.Exit(spiketest.FakeHub(os.Args[1:]))
	}
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// The binaries that the tests build once.
var (
	buildMu sync.Mutex
	binDir  string
	built   = map[string]string{}
)

// binary builds the command pkg once per test binary.
func binary(t *testing.T, name, pkg string) string {
	t.Helper()
	buildMu.Lock()
	defer buildMu.Unlock()
	if p, ok := built[name]; ok {
		return p
	}
	if binDir == "" {
		d, err := os.MkdirTemp("", "uploadspike-bin-")
		if err != nil {
			t.Fatal(err)
		}
		binDir = d
	}
	p := spiketest.BuildBinary(t, binDir, name, pkg)
	built[name] = p
	return p
}

// uploadspikeBin is uploadspike, built from this package.
func uploadspikeBin(t *testing.T) string {
	return binary(t, "uploadspike", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/cmd/uploadspike")
}

// syncBuffer is a buffer that a process writes while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// rig is one device of a test: its base, its hub (the stand-in, or a real
// one), its port and uploadspike's environment.
type rig struct {
	t         *testing.T
	base, hub string
	cwd       string // the commands' current folder: results' zip lands there
	log       string // the stand-in's records
	port      int
	env       []string
	// started are the PIDs of the rig's commands and their starts.
	mu      sync.Mutex
	started map[int]int64
}

// freePort is a TCP port that nothing listens on now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // a TCP listener's
}

// newRig is a device with the stand-in hub.
func newRig(t *testing.T) *rig {
	t.Helper()
	hub, log := spiketest.InstallFakeHub(t, t.TempDir())
	return newRigWith(t, hub, log)
}

// newRigWith is a device with the hub at hub.
func newRigWith(t *testing.T, hub, log string) *rig {
	t.Helper()
	r := &rig{t: t, base: t.TempDir(), hub: hub, log: log, port: freePort(t), cwd: t.TempDir()}
	host := "lab-linux"
	if runtime.GOOS == "windows" {
		host = "lab-windows"
	}
	r.env = append(os.Environ(), envBase+"="+r.base, envHubBin+"="+hub, envPort+"="+fmt.Sprint(r.port), envNoBrowser+"=1",
		envFast+"=1", envRunAs+"=-", envHost+"="+host)
	// Registered after the rig's folders and before any command's kill,
	// so it runs between them (cleanups run last first).
	t.Cleanup(r.waitHubsGone)
	return r
}

// hubsGoneWait bounds how long a test's end waits for its hubs.
const hubsGoneWait = 30 * time.Second

// waitHubsGone waits, at a test's end and for at most hubsGoneWait, until
// no hub that the rig's commands started runs, nor any other process that
// they started. A command that the test's cleanup killed leaves its hub
// to end through its closed stdin a moment later, and may leave a
// PowerShell of its own (the firewall's check of a LAN login) running in
// its current folder; on Windows a running program's .exe, its open files
// and its current folder cannot be removed, so without the wait the
// test's folders failed testing's 2-s removal (TestStatus_IsAClient on the
// windows job: «TempDir RemoveAll cleanup: … The process cannot access the
// file because it is being used by another process», on the commands'
// current folder). A process still running after the bound is a leak,
// which that removal then reports.
func (r *rig) waitHubsGone() {
	deadline := time.Now().Add(hubsGoneWait)
	r.mu.Lock()
	parents := maps.Clone(r.started)
	r.mu.Unlock()
	for len(r.runningHubs())+len(childrenOf(parents)) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
}

// runningHubs are the PIDs of the rig's hubs that run now: the stand-ins
// that its log records (on Windows, which reuses PIDs soon, only a
// process that started before its record, as its creation time is wall
// time there), and the real hub that hub.pid names, with its start.
func (r *rig) runningHubs() []int {
	var live []int
	for _, rec := range r.records(spiketest.RecordStart) {
		start, err := runstate.ProcStartMs(rec.PID)
		if err != nil || runtime.GOOS == "windows" && start > rec.AtMs+100 || slices.Contains(live, rec.PID) {
			continue
		}
		live = append(live, rec.PID)
	}
	if pid, start := r.hubPIDStart(); pid != 0 && runstate.Alive(pid, start) && !slices.Contains(live, pid) {
		live = append(live, pid)
	}
	return live
}

// proc is an uploadspike process of a test.
type proc struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout, stderr *syncBuffer
	done           chan struct{}
	code           int
}

// start starts uploadspike with args and extra variables.
func (r *rig) start(extra []string, args ...string) *proc {
	r.t.Helper()
	cmd := exec.Command(uploadspikeBin(r.t), args...) //nolint:gosec // the test's own build
	cmd.Env = append(append([]string{}, r.env...), extra...)
	cmd.Dir = r.cwd
	p := &proc{cmd: cmd, stdout: &syncBuffer{}, stderr: &syncBuffer{}, done: make(chan struct{}), code: -1}
	cmd.Stdout, cmd.Stderr = p.stdout, p.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	p.stdin = stdin
	ownGroupForTest(cmd)
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	if start, err := runstate.ProcStartMs(cmd.Process.Pid); err == nil {
		r.mu.Lock()
		if r.started == nil {
			r.started = map[int]int64{}
		}
		r.started[cmd.Process.Pid] = start
		r.mu.Unlock()
	}
	go func() {
		_ = cmd.Wait()
		p.code = cmd.ProcessState.ExitCode()
		close(p.done)
	}()
	r.t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

// run runs uploadspike with args and stdin to its end.
func (r *rig) run(stdin string, extra []string, args ...string) *proc {
	r.t.Helper()
	p := r.start(extra, args...)
	_, _ = io.WriteString(p.stdin, stdin)
	_ = p.stdin.Close()
	if !p.wait(2 * time.Minute) {
		r.t.Fatalf("uploadspike %v did not end; stdout:\n%s\nstderr:\n%s", args, p.stdout, p.stderr)
	}
	return p
}

// wait waits for the process's end.
func (p *proc) wait(d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

// until polls cond every 100 ms for d.
func until(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", d, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// state reads the rig's run state.
func (r *rig) state() *runstate.State {
	r.t.Helper()
	s, err := runstate.Load(filepath.Join(r.base, "state", runstate.StateFile))
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

// update changes the rig's run state.
func (r *rig) update(fn func(*runstate.State)) {
	r.t.Helper()
	if err := runstate.Update(filepath.Join(r.base, "state", runstate.StateFile), func(s *runstate.State) error {
		fn(s)
		return nil
	}); err != nil {
		r.t.Fatal(err)
	}
}

// openMI6 opens an MI6 run on the platforms in the run state, as `mi6`
// does on day 0, and returns it for edits.
func (r *rig) openMI6(platforms ...string) {
	r.t.Helper()
	r.update(func(s *runstate.State) {
		s.Open["MI6"] = mi6.NewRun("mi6-test", time.Now(), "test", false, platforms).Encode()
	})
}

// editMI6 changes the open MI6 run.
func (r *rig) editMI6(fn func(*mi6.Run)) {
	r.t.Helper()
	r.update(func(s *runstate.State) {
		run, err := mi6.Decode(s.Open["MI6"])
		if err != nil {
			r.t.Fatal(err)
		}
		fn(run)
		s.Open["MI6"] = run.Encode()
	})
}

// records are the stand-in's records of event.
func (r *rig) records(event string) []spiketest.FakeRecord {
	var out []spiketest.FakeRecord
	for _, rec := range spiketest.FakeRecords(r.t, r.log) {
		if rec.Event == event {
			out = append(out, rec)
		}
	}
	return out
}

// startsBy are the stand-in's starts whose parent is pid.
func (r *rig) startsBy(pid int) []spiketest.FakeRecord {
	var out []spiketest.FakeRecord
	for _, rec := range r.records(spiketest.RecordStart) {
		if rec.PPID == pid {
			out = append(out, rec)
		}
	}
	return out
}

// alive reports whether the process pid runs.
func alive(pid int) bool {
	_, err := runstate.ProcStartMs(pid)
	return err == nil
}

// liveHubs are the PIDs of the stand-in hubs that run now.
func (r *rig) liveHubs() []int {
	var out []int
	for _, rec := range r.records(spiketest.RecordStart) {
		if alive(rec.PID) {
			out = append(out, rec.PID)
		}
	}
	return out
}

// hubPID is hub.pid's PID, 0 without one.
func (r *rig) hubPID() int {
	pid, _ := r.hubPIDStart()
	return pid
}

// hubPIDStart is hub.pid's PID and its process's start, 0 without one.
func (r *rig) hubPIDStart() (int, int64) {
	b, err := os.ReadFile(filepath.Join(r.base, "state", runstate.HubPID)) //nolint:gosec // the test's own file
	if err != nil {
		return 0, 0
	}
	var rec struct {
		PID         int   `json:"pid"`
		ProcStartMs int64 `json:"procStartMs"`
	}
	_ = json.Unmarshal(b, &rec)
	return rec.PID, rec.ProcStartMs
}

// get answers a GET of the rig's holder.
func (r *rig) get(path string, headers ...string) (int, string) {
	r.t.Helper()
	return r.do(http.MethodGet, path, "", headers...)
}

// do sends a request to the rig's holder.
func (r *rig) do(method, path, body string, headers ...string) (int, string) {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.t.Context(), method, fmt.Sprintf("http://127.0.0.1:%d%s", r.port, path), strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// clientsInput is setup's terminal input of the four Google clients and no
// Microsoft one; the IDs are at least 16 bytes, the secrets obviously fake.
func clientsInput() string {
	return strings.Join([]string{
		"android-client-0001.apps.example",
		`{"installed":{"client_id":"desktop-client-0001.apps.example","client_secret":"` + "SENT" + `INEL-desktop-secret-01"}}`,
		"tv-client-0001.apps.example",
		"SENT" + "INEL-tv-secret-000001",
		`{"web":{"client_id":"web-client-0001.apps.example"}}`,
		"",
	}, "\n") + "\n"
}
