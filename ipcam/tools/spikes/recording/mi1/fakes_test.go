package mi1

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The test binary stands in for ipcam-hub when fakeHubEnv is set: "run
// --config <run.json> --stop-on-stdin-eof" appends "hub <pid>" to the file
// that fakeLogEnv names, creates one new segment file per job under its
// data directory (named by the time, so that a later start's sorts later),
// prints a final reconcile, ready (once the file that fakeReadyAfterEnv
// names exists, if it names one), each job's `recording` and a verify, and
// on the end of its stdin appends "stopped" and exits 0. It ignores
// signals: only a kill ends it otherwise.
const (
	fakeHubEnv        = "MI1_FAKE_HUB"
	fakeLogEnv        = "MI1_FAKE_LOG"
	fakeReadyAfterEnv = "MI1_FAKE_READY_AFTER"
)

// fakeReadyWait bounds the fake ipcam-hub's wait for fakeReadyAfterEnv's
// file.
const fakeReadyWait = 30 * time.Second

// fakeRecordingAfter is how long after its ready the fake ipcam-hub prints
// its jobs' `recording` events.
const fakeRecordingAfter = 50 * time.Millisecond

func TestMain(m *testing.M) {
	if os.Getenv(fakeHubEnv) == "1" {
		os.Exit(fakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeHub(args []string) int {
	if len(args) != 4 || args[0] != "run" || args[1] != "--config" || args[3] != "--stop-on-stdin-eof" {
		fmt.Fprintln(os.Stderr, "fake ipcam-hub: usage: run --config <run.json> --stop-on-stdin-eof")
		return 1
	}
	cfg, err := hubproc.ReadConfig(args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake ipcam-hub:", err)
		return 1
	}
	signal.Ignore(os.Interrupt, syscall.SIGTERM)
	logLine(fmt.Sprintf("hub %d", os.Getpid()))
	emit := func(fields map[string]any) {
		fields["atMs"] = time.Now().UnixMilli()
		b, _ := json.Marshal(fields)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	name := time.Now().UTC().Format("150405.000000")
	name = strings.Replace(name, ".", "_", 1)
	for _, j := range cfg.Jobs {
		dir := filepath.Join(cfg.DataDir, "rec", j.Cam, j.Profile, "2026-10-25")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			fmt.Fprintln(os.Stderr, "fake ipcam-hub:", err)
			return 1
		}
		suffix := ".mp4.part"
		if j.Recipients != nil {
			suffix = ".mp4.age.part"
		}
		if err := os.WriteFile(filepath.Join(dir, name+suffix), []byte("a fake segment"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fake ipcam-hub:", err)
			return 1
		}
	}
	emit(map[string]any{"type": "reconcile", "seq": 1, "final": true, "aborted": false, "unreadable": 0, "tornParts": 0, "catalogMismatches": 0})
	if after := os.Getenv(fakeReadyAfterEnv); after != "" {
		for deadline := time.Now().Add(fakeReadyWait); ; time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(after); err == nil { //nolint:gosec // the test's own signal file
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, "fake ipcam-hub: no", after, "after", fakeReadyWait)
				return 1
			}
		}
	}
	emit(map[string]any{"type": "ready", "jobs": []any{}})
	time.Sleep(fakeRecordingAfter)
	for _, j := range cfg.Jobs {
		emit(map[string]any{"type": "recording", "cam": j.Cam, "profile": j.Profile})
	}
	emit(map[string]any{"type": "verify", "unreadable": 0, "tornParts": 0, "catalogMismatches": 0})
	_, _ = io.Copy(io.Discard, os.Stdin)
	logLine("stopped")
	return 0
}

// logLine appends line to the fake log, if one is named.
func logLine(line string) {
	path := os.Getenv(fakeLogEnv)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's log
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

// readLog returns the lines of the fake log.
func readLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // the test's log
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return lines(string(data))
}

// logEntries returns the lines of the fake log, with "hub <pid>" read as
// "hub".
func logEntries(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, l := range readLog(t, path) {
		out = append(out, strings.Fields(l)[0])
	}
	return out
}

// killFakeHubs kills every fake ipcam-hub that the log names, as a power
// cut would.
func killFakeHubs(t *testing.T, path string) {
	t.Helper()
	for _, l := range readLog(t, path) {
		f := strings.Fields(l)
		if len(f) != 2 || f[0] != "hub" {
			continue
		}
		if pid, err := strconv.Atoi(f[1]); err == nil {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	}
}

// fakeController is reccontroller as the host sees it, on loopback: it
// answers hello with a session, collects run_start, ready, report and
// refused, sends `cut` when a run is ready (or lets onReady do it),
// answers a report with a valid verdict (or verdict's), serves sendResults
// on pull_results, and, with pullAtBye, pulls the host's results into
// pulled when it says bye. It keeps every beacon it receives, and drops the
// link instead of answering the first dropReports reports. It loses the
// first loseStarts run_starts and drops the link at their run's ready, as
// a link that died after the run_start would, and the first dropAtStarts
// run_starts with the link at once, as a link that died as recspike sent
// it, then creates the file droppedSignal, if set. Like reccontroller, it
// refuses the ready of a run that it never kept (unknownRun). dropLinks
// drops every link it has. Every message it gets is also an event.
type fakeController struct {
	t           *testing.T
	ln          net.Listener
	beacon      net.PacketConn
	sendResults string
	pulled      string
	pullAtBye   bool
	onReady     func(c *link.Conn, start link.RunStart)
	verdict     func(start link.RunStart, r link.Report) results.Run
	dropReports int

	mu            sync.Mutex
	loseStarts    int
	dropAtStarts  int
	droppedSignal string
	lost          map[string]bool // the run_starts it lost, until their ready
	conns         map[*link.Conn]bool
	hellos        int
	starts        []link.RunStart
	reports       []link.Report
	refused       []link.Refused
	beacons       map[uint64][]uint64 // each run's beacon sequence numbers, as they arrived
	events        chan string
}

// unknownRun is reccontroller's refusal of a ready for a run that it does
// not know.
const unknownRun = "rularea nu a fost anunțată controlorului"

func startFakeController(t *testing.T) *fakeController {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	beacon, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeController{t: t, ln: ln, beacon: beacon, sendResults: t.TempDir(), pulled: t.TempDir(),
		lost: map[string]bool{}, conns: map[*link.Conn]bool{}, beacons: map[uint64][]uint64{}, events: make(chan string, 256)}
	t.Cleanup(func() { _, _ = ln.Close(), beacon.Close() })
	go func() {
		buf := make([]byte, 64)
		for {
			n, _, err := beacon.ReadFrom(buf)
			if err != nil {
				return
			}
			if run, seq, ok := link.ParseBeacon(buf[:n]); ok {
				f.mu.Lock()
				f.beacons[run] = append(f.beacons[run], seq)
				f.mu.Unlock()
			}
		}
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(link.NewConn(c))
		}
	}()
	return f
}

func (f *fakeController) addr() string { return f.ln.Addr().String() }

func (f *fakeController) event(e string) {
	select {
	case f.events <- e:
	default:
	}
}

func (f *fakeController) serve(c *link.Conn) {
	f.mu.Lock()
	f.conns[c] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.conns, c)
		f.mu.Unlock()
		_ = c.Close()
	}()
	runs := map[string]link.RunStart{}
	for {
		m, err := c.Receive()
		if err != nil {
			return
		}
		switch m := m.(type) {
		case link.Hello:
			port := f.beacon.LocalAddr().(*net.UDPAddr).Port
			_ = c.Send(link.Session{RTSPBase: "rtsp://127.0.0.1:1", User: "u", Pass: "p", Streams: []string{"cam01_main", "cam99_main"},
				BeaconPort: port, Needles: [][]byte{[]byte("ftypmp42")}})
			f.mu.Lock()
			f.hellos++
			f.mu.Unlock()
			f.event("hello " + m.Host)
		case link.RunStart:
			f.mu.Lock()
			if f.dropAtStarts > 0 {
				f.dropAtStarts--
				signal := f.droppedSignal
				f.mu.Unlock()
				_ = c.Close() // the link dies with the run_start
				if signal != "" {
					if err := os.WriteFile(signal, nil, 0o600); err != nil {
						f.t.Errorf("fake controller: %v", err)
					}
				}
				f.event("dropped at run_start " + m.RunID)
				return
			}
			lose := f.loseStarts > 0
			if lose {
				f.loseStarts--
				f.lost[m.RunID] = true
			} else {
				f.starts = append(f.starts, m)
			}
			f.mu.Unlock()
			if lose {
				f.event("lost run_start " + m.RunID)
				continue
			}
			runs[m.RunID] = m
			f.event("run_start " + m.RunID)
		case link.Ready:
			f.event("ready " + m.RunID)
			start, ok := runs[m.RunID]
			if !ok { // announced on an earlier connection, before a reboot
				f.mu.Lock()
				for _, s := range f.starts {
					if s.RunID == m.RunID {
						start, ok = s, true
					}
				}
				lost := f.lost[m.RunID]
				delete(f.lost, m.RunID)
				f.mu.Unlock()
				switch {
				case !ok && lost:
					f.event("dropped at ready " + m.RunID)
					return // the link that lost the run_start dies
				case !ok:
					_ = c.Send(link.Refused{RunID: m.RunID, Reason: unknownRun})
					f.event("unknown " + m.RunID)
					continue
				}
			}
			if f.onReady != nil {
				f.onReady(c, start)
			} else {
				_ = c.Send(link.Cut{RunID: m.RunID, Kind: start.Kind, PromptAt: time.Now()})
				f.event("cut " + m.RunID)
			}
		case link.Report:
			f.mu.Lock()
			f.reports = append(f.reports, m)
			if f.dropReports > 0 {
				f.dropReports--
				f.mu.Unlock()
				f.event("dropped " + m.RunID)
				return // the link drops before the verdict
			}
			var start link.RunStart
			for _, s := range f.starts {
				if s.RunID == m.RunID {
					start = s
				}
			}
			f.mu.Unlock()
			run := results.Run{RunID: m.RunID, Valid: true, Slot: start.Slot, Attempt: start.Attempt, ShutdownKind: start.Kind,
				Encrypted: start.Encrypted, PadLowBitrate: start.PadLowBitrate, Reconciled: true, Hub04Clear: m.Hub04Clear}
			if f.verdict != nil {
				run = f.verdict(start, m)
			}
			_ = c.Send(link.Verdict{Run: run})
			f.event("report " + m.RunID)
		case link.Refused:
			f.mu.Lock()
			f.refused = append(f.refused, m)
			f.mu.Unlock()
			f.event("refused " + m.RunID)
		case link.PullResults:
			_ = c.SendResultsTar(f.sendResults)
			f.event("pull_results")
		case link.Bye:
			if f.pullAtBye {
				if err := c.PullResults(f.pulled); err != nil {
					f.t.Errorf("pull at bye: %v", err)
				}
			}
			f.event("bye")
			return
		}
	}
}

// dropLinks drops every link that the fake controller has.
func (f *fakeController) dropLinks() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.conns {
		_ = c.Close()
	}
}

// helloCount is how many times a host connected and said hello.
func (f *fakeController) helloCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hellos
}

func (f *fakeController) runStarts() []link.RunStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]link.RunStart(nil), f.starts...)
}

// runStartsOf is the run_starts kept of runID. A test that cancels the set
// at a run's report checks that run's: the set announces its next run
// before it sees the cancel when the verdict reaches it first.
func (f *fakeController) runStartsOf(runID string) []link.RunStart {
	var out []link.RunStart
	for _, s := range f.runStarts() {
		if s.RunID == runID {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeController) runReports() []link.Report {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]link.Report(nil), f.reports...)
}

// beaconSeqs is the sequence numbers of the run's beacons, as they arrived.
func (f *fakeController) beaconSeqs(runID string) []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint64(nil), f.beacons[link.RunHash(runID)]...)
}

// waitEvent waits up to 30 s for an event that starts with prefix. It
// returns an error instead of failing the test, so that a goroutine may
// call it; the test's goroutine fails on it.
func (f *fakeController) waitEvent(prefix string) error {
	return f.waitEventWithin(prefix, 30*time.Second)
}

func (f *fakeController) waitEventWithin(prefix string, d time.Duration) error {
	timeout := time.After(d)
	for {
		select {
		case e := <-f.events:
			if strings.HasPrefix(e, prefix) {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("no %q event from the fake controller in %v", prefix, d)
		}
	}
}

// mustEvent waits for an event that starts with prefix, on the test's
// goroutine.
func (f *fakeController) mustEvent(t *testing.T, prefix string) {
	t.Helper()
	if err := f.waitEvent(prefix); err != nil {
		t.Fatal(err)
	}
}

// boots numbers the test drivers' boots.
var boots atomic.Int64

// testDriver is a driver for a host with the given pseudonym, with the fake
// ipcam-hub, the fake controller, its own folders, a synced clock and no
// preflight; its screen is out. Each test driver is a boot of its own, as
// a recspike started after a reboot is.
func testDriver(t *testing.T, f *fakeController, host, results, work string, out io.Writer) *Driver {
	t.Helper()
	t.Setenv(fakeHubEnv, "1")
	boot := Boot{ID: fmt.Sprintf("boot-%d", boots.Add(1))}
	return &Driver{
		Controller: f.addr(),
		Hub:        os.Args[0],
		Results:    results,
		Work:       work,
		Host:       HostState{Pseudonym: host, OS: "linux", Storage: "ssd", WriteCache: "unknown", Cameras: 1},
		Out:        out,
		GOOS:       "linux",
		NTP:        func(context.Context) (bool, error) { return true, nil },
		Uptime:     func() (time.Duration, error) { return time.Minute, nil },
		ThisBoot:   func() (Boot, error) { return boot, nil },
		OSStop:     func(context.Context, controller.ShutdownKind) error { return errors.New("no OS stop in this test") },
		LastRecorded: func(context.Context, lastindex.Before, age.Identity) (uint64, error) {
			return 100, nil
		},
		Preflight: func(context.Context, string, LinkProbes) error { return nil },
	}
}

// syncBuffer is a goroutine-safe screen.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// windowAroundNow is a window that holds the next two hours.
func windowAroundNow(t *testing.T) *Window {
	t.Helper()
	now := time.Now()
	w, err := ParseWindow(now.Add(-time.Hour).Format("15:04") + "-" + now.Add(2*time.Hour).Format("15:04"))
	if err != nil {
		t.Fatal(err)
	}
	return &w
}

// lines returns the lines of s.
func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// TestFakeController_WaitEventReturnsAnError: the fake controller's wait
// for an event returns an error when the event does not come, so that the
// goroutines that wait (a power cut at the cut, a cancel at the next run)
// never call t.Fatal; the test's goroutine fails on the error.
func TestFakeController_WaitEventReturnsAnError(t *testing.T) {
	f := startFakeController(t)
	f.event("hello pc")
	if err := f.waitEventWithin("hello", time.Second); err != nil {
		t.Errorf("waitEvent for an event that came = %v", err)
	}
	err := f.waitEventWithin("run_start pc-kill9-01-1", 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), `"run_start pc-kill9-01-1"`) {
		t.Errorf("waitEvent for an event that never comes = %v, want an error that names it", err)
	}
}
