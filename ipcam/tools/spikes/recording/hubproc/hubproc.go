// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package hubproc runs ipcam-hub for recspike, across the process boundary
// (C01): it writes run.json, starts `ipcam-hub run`, reads its JSON-lines
// events, sends the run's liveness beacon from ipcam-hub's `ready` until the
// child exits, kills it as a crash would (SIGKILL on Linux, TerminateProcess
// on Windows), stops it cleanly by ending its stdin, restarts it, and
// measures the resume on the monotonic clock (C07, G13).
package hubproc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// Config is run.json as `ipcam-hub run` reads it (hub/cmd/ipcam-hub/run.go).
type Config struct {
	DataDir string `json:"dataDir"`
	// MediaMTX is the fork's binary; empty is the one next to ipcam-hub.
	MediaMTX string `json:"mediamtx,omitempty"`
	// IdentityFile is RI_g's identity, a 0600 file in the run directory.
	IdentityFile    string `json:"identityFile,omitempty"`
	SDCardConfirmed bool   `json:"sdCardConfirmed"`
	Jobs            []Job  `json:"jobs"`
}

// Job is one recording job of run.json.
type Job struct {
	Cam         string `json:"cam"`
	Profile     string `json:"profile"`
	RestreamURL string `json:"restreamUrl"`
	SegmentSecs int    `json:"segmentSecs,omitempty"`
	// Recipients are RI_g then RK; nil records in clear (it is written as
	// null), and ipcam-hub refuses any other count, an empty list included.
	Recipients []string `json:"recipients"`
	// PadLowBitrate is the O17 setting; nil leaves it out, which ipcam-hub
	// takes as on.
	PadLowBitrate *bool `json:"padLowBitrate,omitempty"`
}

// WriteConfig writes run.json at path, readable by its owner only.
func WriteConfig(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// ReadConfig reads run.json at path.
func ReadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path) //nolint:gosec // the run's own run.json
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("hubproc: %s: %w", filepath.Base(path), err)
	}
	return c, nil
}

// Event is one of ipcam-hub's events (hub/cmd/ipcam-hub/events.go), with
// the fields that recspike reads; Line holds the whole event as printed.
// AtMs is the host's wall clock, information only (Review Focus 2); At is
// when hubproc read the line, on the monotonic clock.
type Event struct {
	Type              string `json:"type"`
	AtMs              int64  `json:"atMs"`
	Cam               string `json:"cam"`
	Profile           string `json:"profile"`
	Seq               int64  `json:"seq"`
	Final             bool   `json:"final"`
	Aborted           bool   `json:"aborted"`
	Unreadable        int    `json:"unreadable"`
	TornParts         int    `json:"tornParts"`
	CatalogMismatches int    `json:"catalogMismatches"`
	Message           string `json:"message"`
	// Path is a segment_verified's segment; State a card event's state, and
	// Kind an alert's kind (T40).
	Path  string `json:"path"`
	State string `json:"state"`
	Kind  string `json:"kind"`
	// LastPartAgoMs is a status event's time since the stream's open part
	// last grew, -1 before its first part since ipcam-hub started.
	LastPartAgoMs int64 `json:"lastPartAgoMs"`
	// StartMs and EndMs are a segment_closed's segment's start and end, or
	// a gap's, on the host's wall clock (Unix ms).
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`

	Line json.RawMessage `json:"-"`
	At   time.Time       `json:"-"`
}

// Beacon is where the run's liveness beacon goes: the controller's beacon
// address (host:port, IPv4 UDP, R08) and the run.
type Beacon struct {
	Addr, RunID string
}

// Options start one ipcam-hub.
type Options struct {
	// Binary is ipcam-hub; Config is the run.json that it runs.
	Binary, Config string
	// Stderr receives the child's stderr (its errors and MediaMTX's log);
	// nil discards it.
	Stderr io.Writer
	// OnEvent, when set, is called with every event of this child, in
	// order, from the goroutine that reads them, also after the child has
	// exited. The child that Restart starts calls no OnEvent.
	OnEvent func(Event)
	// Beacon, when set, is beaconed from the child's `ready` until it exits.
	// Start dials its address and fails when it cannot.
	Beacon *Beacon
}

// ErrExited is returned when the child has exited: before an event that a
// caller waits for, or before a kill.
var ErrExited = errors.New("hubproc: ipcam-hub exited")

// maxEventLine bounds one event line; ipcam-hub's are far shorter.
const maxEventLine = 1 << 20

// Hub is one ipcam-hub child.
type Hub struct {
	opts  Options
	cmd   *exec.Cmd
	stdin *os.File // the write end of the child's stdin; Stop closes it
	jobs  []jobKey

	exited   chan struct{} // closed when the process has exited
	exitCode int           // valid once exited is closed

	stopBeacon context.CancelFunc
	beaconDone chan struct{} // closed when the beacon goroutine has returned

	mu        sync.Mutex
	changed   chan struct{} // closed and replaced at each event and at EOF
	eof       bool          // stdout ended
	readErr   error         // the events could not be read; stdout is drained
	current   map[string]Event
	recording map[jobKey]time.Time
}

type jobKey struct{ cam, profile string }

// Start starts `ipcam-hub run --config <Config> --stop-on-stdin-eof`; the
// jobs of run.json are the ones whose `recording` events WaitRecording waits
// for. With a Beacon, it first resolves and dials the beacon's address, and
// an address that does not work is its error: no child runs without its
// beacon. The child's stdin is a pipe that only Stop closes: a stop that
// needs neither a signal nor a console, as recspike's boot task has none
// on Windows (G14); if recspike itself dies, ipcam-hub stops cleanly too.
func Start(o Options) (*Hub, error) {
	cfg, err := ReadConfig(o.Config)
	if err != nil {
		return nil, err
	}
	var beaconConn net.Conn
	if o.Beacon != nil {
		if beaconConn, err = net.Dial("udp4", o.Beacon.Addr); err != nil {
			return nil, fmt.Errorf("hubproc: beacon: %w", err)
		}
	}
	h := &Hub{
		opts: o, exited: make(chan struct{}), exitCode: -1, beaconDone: make(chan struct{}),
		changed: make(chan struct{}), current: map[string]Event{}, recording: map[jobKey]time.Time{},
	}
	for _, j := range cfg.Jobs {
		h.jobs = append(h.jobs, jobKey{j.Cam, j.Profile})
	}
	// The child writes its events into a pipe of ours rather than through
	// exec's copying goroutine, and WaitDelay bounds the copy of its stderr,
	// so that Exited follows the process's exit even if a grandchild still
	// held an end.
	r, w, err := os.Pipe()
	if err != nil {
		closeConn(beaconConn)
		return nil, err
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		_, _ = r.Close(), w.Close()
		closeConn(beaconConn)
		return nil, err
	}
	h.stdin = stdinW
	h.cmd = exec.Command(o.Binary, "run", "--config", o.Config, "--stop-on-stdin-eof") //nolint:gosec // recspike's own ipcam-hub
	h.cmd.Stdin, h.cmd.Stdout, h.cmd.Stderr, h.cmd.WaitDelay = stdinR, w, o.Stderr, time.Second
	ownGroup(h.cmd)
	if err := h.cmd.Start(); err != nil {
		_, _, _, _ = r.Close(), w.Close(), stdinR.Close(), stdinW.Close()
		closeConn(beaconConn)
		return nil, err
	}
	_, _ = w.Close(), stdinR.Close() // the child holds these ends
	beaconCtx, stopBeacon := context.WithCancel(context.Background())
	h.stopBeacon = stopBeacon
	go h.read(r)
	go func() {
		_ = h.cmd.Wait()
		if h.cmd.ProcessState != nil {
			h.exitCode = h.cmd.ProcessState.ExitCode()
		}
		_ = h.stdin.Close()
		close(h.exited)
		stopBeacon() // the beacon ends with the child
	}()
	go func() {
		defer close(h.beaconDone)
		if beaconConn != nil {
			h.beacon(beaconCtx, beaconConn, o.Beacon.RunID)
		}
	}()
	return h, nil
}

// closeConn closes the beacon's connection, if any.
func closeConn(c net.Conn) {
	if c != nil {
		_ = c.Close()
	}
}

// read parses the child's events until its stdout ends. A line that is not
// an event is dropped. When the events cannot be read (a read error, or a
// line longer than maxEventLine), the error is every later wait's, and read
// keeps draining stdout until it ends, so that ipcam-hub never writes into
// a closed pipe (EPIPE; on Linux, SIGPIPE would kill it).
func (h *Hub) read(r *os.File) {
	defer func() { _ = r.Close() }()
	lines := bufio.NewScanner(r)
	lines.Buffer(make([]byte, 64<<10), maxEventLine)
	for lines.Scan() {
		var e Event
		if err := json.Unmarshal(lines.Bytes(), &e); err != nil || e.Type == "" {
			continue
		}
		e.Line, e.At = json.RawMessage(bytes.Clone(lines.Bytes())), time.Now()
		h.keep(e)
		if h.opts.OnEvent != nil {
			h.opts.OnEvent(e)
		}
	}
	if err := lines.Err(); err != nil {
		h.mu.Lock()
		h.readErr = fmt.Errorf("hubproc: reading ipcam-hub's events: %w", err)
		h.signal()
		h.mu.Unlock()
		_, _ = io.Copy(io.Discard, r)
	}
	h.mu.Lock()
	h.eof = true
	h.signal()
	h.mu.Unlock()
}

// keep records e as the current event of its type: the latest, except for
// reconcile, whose current one has the highest seq (events.go: they can
// come out of order). A job's first `recording` event is kept by job.
func (h *Hub) keep(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.current[e.Type]; !ok || e.Type != "reconcile" || e.Seq > cur.Seq {
		h.current[e.Type] = e
	}
	if k := (jobKey{e.Cam, e.Profile}); e.Type == "recording" {
		if _, ok := h.recording[k]; !ok {
			h.recording[k] = e.At
		}
	}
	h.signal()
}

// signal wakes the waiters; h.mu is held.
func (h *Hub) signal() {
	close(h.changed)
	h.changed = make(chan struct{})
}

// wait calls check under h.mu at each change until it reports done, the
// child's stdout ends (ErrExited), or ctx is done. Once the events could not
// be read, it returns that error instead, the child running or not.
func (h *Hub) wait(ctx context.Context, check func() bool) error {
	for {
		h.mu.Lock()
		readErr, done, eof, changed := h.readErr, check(), h.eof, h.changed
		h.mu.Unlock()
		switch {
		case readErr != nil:
			return readErr
		case done:
			return nil
		case eof:
			return ErrExited
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// WaitEvent waits until the current event of typ satisfies ok (nil: any)
// and returns it. The current event is the latest of its type, and for
// reconcile the one with the highest seq. It gives ErrExited when stdout
// ended first, and the read error once the events could not be read.
func (h *Hub) WaitEvent(ctx context.Context, typ string, ok func(Event) bool) (Event, error) {
	var found Event
	err := h.wait(ctx, func() bool {
		e, seen := h.current[typ]
		if seen && (ok == nil || ok(e)) {
			found = e
			return true
		}
		return false
	})
	return found, err
}

// WaitRecording waits until every job of run.json has sent its first
// `recording` event and returns when the last of them arrived.
func (h *Hub) WaitRecording(ctx context.Context) (time.Time, error) {
	var last time.Time
	err := h.wait(ctx, func() bool {
		last = time.Time{}
		for _, k := range h.jobs {
			at, ok := h.recording[k]
			if !ok {
				return false
			}
			if at.After(last) {
				last = at
			}
		}
		return true
	})
	return last, err
}

// ResumeMs is how long after killedAtPlusScan every job of this child,
// restarted after the kill, was recording again: the arrival of the last
// job's `recording` event, on the monotonic clock (C07, G13).
// killedAtPlusScan is the kill's moment moved later by the time the caller
// spent between the kill and Restart on its own work, mi1's raw HUB-04 scan
// at the stop, which is not ipcam-hub's resume.
func (h *Hub) ResumeMs(ctx context.Context, killedAtPlusScan time.Time) (int64, error) {
	last, err := h.WaitRecording(ctx)
	if err != nil {
		return 0, err
	}
	return last.Sub(killedAtPlusScan).Milliseconds(), nil
}

// beacon sends the beacon of runID on conn every controller.BeaconEvery
// from the child's `ready` until ctx is done, and closes conn: Kill and Stop
// end it before they act, and the child's exit ends it. It never starts
// again under the run.
func (h *Hub) beacon(ctx context.Context, conn net.Conn, runID string) {
	defer closeConn(conn)
	if _, err := h.WaitEvent(ctx, "ready", nil); err != nil {
		return
	}
	tick := time.NewTicker(controller.BeaconEvery)
	defer tick.Stop()
	for seq := uint64(0); ctx.Err() == nil; seq++ {
		_, _ = conn.Write(link.BeaconPacket(runID, seq)) // a lost beacon is the controller's to notice
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}

// Exited is closed when the child has exited.
func (h *Hub) Exited() <-chan struct{} { return h.exited }

// Pid is the child's process id: MI3 measures its CPU and memory, with
// MediaMTX's, its own child (G20).
func (h *Hub) Pid() int { return h.cmd.Process.Pid }

// ExitCode is the child's exit code once it has exited (-1 when a signal
// ended it), else -1.
func (h *Hub) ExitCode() int {
	select {
	case <-h.exited:
		return h.exitCode
	default:
		return -1
	}
}

// Kill ends the beacon, then kills the child as a crash would (SIGKILL,
// TerminateProcess), waits until it has exited, and returns the moment of
// the kill on the monotonic clock. A child that had already exited gives
// ErrExited.
func (h *Hub) Kill() (time.Time, error) {
	h.stopBeacon()
	<-h.beaconDone
	select {
	case <-h.exited:
		return time.Time{}, ErrExited
	default:
	}
	at := time.Now()
	if err := kill(h.cmd.Process); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			// cmd.Wait has reaped the child and Start's goroutine is
			// about to close exited.
			<-h.exited
			return time.Time{}, ErrExited
		}
		select {
		case <-h.exited:
			return time.Time{}, ErrExited
		default:
			return time.Time{}, err
		}
	}
	<-h.exited
	return at, nil
}

// Stop asks the child to stop cleanly, as recspike does after a run's
// report, by ending its stdin (--stop-on-stdin-eof), and kills it when ctx
// is done first. A child that has exited by itself needs no stop: nil.
func (h *Hub) Stop(ctx context.Context) error {
	h.stopBeacon()
	<-h.beaconDone
	select {
	case <-h.exited:
		return nil
	default:
	}
	if err := h.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		// The pipe would not close, so the child cannot see the request: it
		// is killed, and the failed clean stop is the error. A child that
		// exited meanwhile needs neither.
		_, kerr := h.Kill()
		if errors.Is(kerr, ErrExited) {
			return nil
		}
		return errors.Join(err, kerr)
	}
	select {
	case <-h.exited:
		return nil
	case <-ctx.Done():
		if _, err := h.Kill(); err != nil && !errors.Is(err, ErrExited) {
			return err
		}
		return ctx.Err()
	}
}

// Restart starts ipcam-hub again on the same run.json, without the beacon
// and without OnEvent: the run's beacon ended with this child, and OnEvent
// watches this child's events, which its reader may still be handing over
// after the restart. The child must have exited.
func (h *Hub) Restart() (*Hub, error) {
	select {
	case <-h.exited:
	default:
		return nil, errors.New("hubproc: restart of a running ipcam-hub")
	}
	o := h.opts
	o.Beacon, o.OnEvent = nil, nil
	return Start(o)
}
