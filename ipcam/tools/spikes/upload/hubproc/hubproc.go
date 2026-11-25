// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package hubproc runs ipcam-hub for uploadspike across the process
// boundary (P01): it writes the hub's files into its data directory
// without following a link (files.go), starts `ipcam-hub run --config
// <run.json> --stop-on-stdin-eof` with a stdin pipe whose write end only
// this process holds, reads its JSON-lines events with c1's reader, which
// keeps draining after an error so that ipcam-hub never gets EPIPE, talks to
// its control endpoint (control.go), stops it cleanly by ending its stdin,
// kills it as c1's hubproc does, waits for a previous holder's hub to end
// (WaitGone), and runs its diag subcommands (Diag). The hub never outlives
// this process: when it ends, however it ends, the kernel closes the pipe
// and the hub stops cleanly, MediaMTX included (One hub per device).
package hubproc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// The owner's texts of the hub's start errors.
const (
	sdCardUnconfirmedText = "Hub-ul nu pornește: cardul SD nu este confirmat. Rulați sudo ./uploadspike setup și scrieți DA."
	noSpikeUserText       = "Rulați întâi sudo ./uploadspike install."
)

// ownerError is an error whose text is a whole Romanian sentence for the
// owner.
type ownerError string

func (e ownerError) Error() string { return string(e) }

var (
	// ErrSdCardUnconfirmed is a start that ipcam-hub's SD gate refused
	// (run.go: sd_card_unconfirmed, exit 4).
	ErrSdCardUnconfirmed error = ownerError(sdCardUnconfirmedText)
	// ErrNoSpikeUser is a start as root without the hub's identity, which
	// Task 8's install creates.
	ErrNoSpikeUser error = ownerError(noSpikeUserText)
	// ErrExited is a wait that the hub's exit ended.
	ErrExited = errors.New("hubproc: ipcam-hub exited")
)

// exitSdCardUnconfirmed is ipcam-hub run's exit code at its SD gate.
const exitSdCardUnconfirmed = 4

// SpikeUser is the hub's identity on Linux when uploadspike runs as root.
const SpikeUser = "ipcam-spike"

// stopWait is how long Stop waits for the hub's clean stop before it kills
// it.
const stopWait = 30 * time.Second

// maxEventLine bounds one event line; ipcam-hub's are far shorter.
const maxEventLine = 1 << 20

// controlTokenEnv is ipcam-hub's control token variable.
const controlTokenEnv = "IPCAM_CONTROL_TOKEN" //nolint:gosec // G101: the name of a variable

// Config starts one ipcam-hub.
type Config struct {
	// Bin is ipcam-hub; RunJSON its run.json, <base>/hub/spike/run.json,
	// inside the hub's data directory <base>/hub.
	Bin, RunJSON string
	// Files are written into RunJSON's folder before the start, by name:
	// run.json and any identity file, 0600, without following a link.
	Files map[string][]byte
	// RunAsUser is the hub's identity on Linux when uploadspike runs as
	// root: SpikeUser. Empty runs the hub as uploadspike.
	RunAsUser string
	// ControlToken is the control endpoint's bearer, passed in the hub's
	// environment; empty serves no control endpoint.
	ControlToken string
	// Env adds variables to the hub's environment.
	Env []string
	// PIDFile is <base>/state/hub.pid: the hub's PID and start time.
	PIDFile string
	// Stderr receives the hub's stderr; nil discards it.
	Stderr io.Writer
}

// NewControlToken is a fresh control token: 32 random bytes, hex.
func NewControlToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Event is one of ipcam-hub's events: its type, its time (the hub's wall
// clock) and the whole line.
type Event struct {
	Type string          `json:"type"`
	AtMs int64           `json:"atMs"`
	Raw  json.RawMessage `json:"-"`
}

// Hub is one ipcam-hub child.
type Hub struct {
	cfg   Config
	cmd   *exec.Cmd
	stdin *os.File // the write end of the child's stdin; Stop closes it
	pid   int

	exited   chan struct{} // closed when the process has exited
	exitCode int           // valid once exited is closed
	killed   bool          // Kill ended it

	events chan Event // Events' channel, fed in order from queue

	mu      sync.Mutex
	queue   []Event
	changed chan struct{} // closed and replaced at each event and at EOF
	eof     bool
	readErr error
	current map[string]Event
}

// pidRecord is hub.pid's content.
type pidRecord struct {
	PID         int   `json:"pid"`
	ProcStartMs int64 `json:"procStartMs"`
}

// Start prepares the hub's files (files.go) and starts `ipcam-hub run
// --config <RunJSON> --stop-on-stdin-eof` in a session of its own on Linux,
// as RunAsUser when set, with Pdeathsig, and in a process group of its own
// on Windows, then records its PID and start time in PIDFile, and waits
// until the hub is ready (its `ready` event) or ctx ends. A hub that ends
// first at its SD gate gives ErrSdCardUnconfirmed; any other end, an error
// with its exit code.
func Start(ctx context.Context, cfg Config) (*Hub, error) {
	owner, err := spikeOwner(cfg.RunAsUser)
	if err != nil {
		return nil, err
	}
	if err := prepareFiles(cfg, owner); err != nil {
		return nil, err
	}
	h := &Hub{cfg: cfg, exited: make(chan struct{}), exitCode: -1, events: make(chan Event, 64),
		changed: make(chan struct{}), current: map[string]Event{}}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		_, _ = r.Close(), w.Close()
		return nil, err
	}
	h.stdin = stdinW
	h.cmd = exec.Command(cfg.Bin, "run", "--config", cfg.RunJSON, "--stop-on-stdin-eof") //nolint:gosec // uploadspike's own ipcam-hub
	h.cmd.Env = hubEnv(cfg)
	h.cmd.Stdin, h.cmd.Stdout, h.cmd.Stderr, h.cmd.WaitDelay = stdinR, w, cfg.Stderr, time.Second
	setProcAttr(h.cmd, owner)
	if err := startChild(h.cmd, func() { h.waited() }); err != nil {
		_, _, _, _ = r.Close(), w.Close(), stdinR.Close(), stdinW.Close()
		return nil, err
	}
	h.pid = h.cmd.Process.Pid
	_, _ = w.Close(), stdinR.Close() // the child holds these ends
	go h.read(r)
	go h.pump()
	if cfg.PIDFile != "" {
		if err := writePIDFile(cfg.PIDFile, h.pid); err != nil {
			_ = h.Kill()
			return nil, err
		}
	}
	if err := h.waitReady(ctx); err != nil {
		_ = h.Kill()
		return nil, err
	}
	return h, nil
}

// hubEnv is the hub's environment: this process's, without a control
// token of its own, with cfg's variables and cfg's control token.
func hubEnv(cfg Config) []string {
	env := make([]string, 0, len(os.Environ())+len(cfg.Env)+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, controlTokenEnv+"=") {
			env = append(env, kv)
		}
	}
	env = append(env, cfg.Env...)
	if cfg.ControlToken != "" {
		env = append(env, controlTokenEnv+"="+cfg.ControlToken)
	}
	return env
}

// waited runs once the child has exited: it records the exit code, closes
// the stdin pipe and removes the PID file, which names this hub.
func (h *Hub) waited() {
	if h.cmd.ProcessState != nil {
		h.exitCode = h.cmd.ProcessState.ExitCode()
	}
	_ = h.stdin.Close()
	if h.cfg.PIDFile != "" {
		if rec, err := readPIDFile(h.cfg.PIDFile); err == nil && rec.PID == h.pid {
			_ = os.Remove(h.cfg.PIDFile)
		}
	}
	close(h.exited)
}

// waitReady waits for the hub's ready event.
func (h *Hub) waitReady(ctx context.Context) error {
	_, err := h.WaitEvent(ctx, "ready")
	if !errors.Is(err, ErrExited) {
		return err
	}
	<-h.exited
	if h.exitCode == exitSdCardUnconfirmed {
		return ErrSdCardUnconfirmed
	}
	return fmt.Errorf("hubproc: ipcam-hub exited with code %d before it was ready", h.exitCode)
}

// read parses the child's events until its stdout ends. A line that is not
// an event is dropped. When the events cannot be read, the error is every
// later wait's, and read keeps draining stdout until it ends, so that
// ipcam-hub never writes into a closed pipe.
func (h *Hub) read(r *os.File) {
	defer func() { _ = r.Close() }()
	lines := bufio.NewScanner(r)
	lines.Buffer(make([]byte, 64<<10), maxEventLine)
	for lines.Scan() {
		var e Event
		if err := json.Unmarshal(lines.Bytes(), &e); err != nil || e.Type == "" {
			continue
		}
		e.Raw = json.RawMessage(bytes.Clone(lines.Bytes()))
		h.mu.Lock()
		h.current[e.Type] = e
		h.queue = append(h.queue, e)
		h.signal()
		h.mu.Unlock()
	}
	h.mu.Lock()
	if err := lines.Err(); err != nil {
		h.readErr = fmt.Errorf("hubproc: reading ipcam-hub's events: %w", err)
	}
	h.signal()
	h.mu.Unlock()
	_, _ = io.Copy(io.Discard, r)
	h.mu.Lock()
	h.eof = true
	h.signal()
	h.mu.Unlock()
}

// pump hands the queued events to Events' channel in order, and closes it
// after the last one: the reader never waits on a consumer.
func (h *Hub) pump() {
	defer close(h.events)
	for {
		h.mu.Lock()
		for len(h.queue) == 0 && !h.eof {
			changed := h.changed
			h.mu.Unlock()
			<-changed
			h.mu.Lock()
		}
		if len(h.queue) == 0 {
			h.mu.Unlock()
			return
		}
		e := h.queue[0]
		h.queue = h.queue[1:]
		h.mu.Unlock()
		h.events <- e
	}
}

// signal wakes the waiters; h.mu is held.
func (h *Hub) signal() {
	close(h.changed)
	h.changed = make(chan struct{})
}

// Events is the hub's events, in order, closed after its stdout ended.
// Every event goes to it, so its one reader must keep reading.
func (h *Hub) Events() <-chan Event { return h.events }

// WaitEvent waits for the latest event of typ: ErrExited when the hub's
// stdout ended first, the read error once the events could not be read.
func (h *Hub) WaitEvent(ctx context.Context, typ string) (Event, error) {
	for {
		h.mu.Lock()
		e, seen := h.current[typ]
		readErr, eof, changed := h.readErr, h.eof, h.changed
		h.mu.Unlock()
		switch {
		case seen:
			return e, nil
		case readErr != nil:
			return Event{}, readErr
		case eof:
			return Event{}, ErrExited
		}
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-changed:
		}
	}
}

// Pid is the hub's process id.
func (h *Hub) Pid() int { return h.pid }

// Exited is closed once the hub has exited.
func (h *Hub) Exited() <-chan struct{} { return h.exited }

// ExitCode is the hub's exit code once it has exited, else -1.
func (h *Hub) ExitCode() int {
	select {
	case <-h.exited:
		return h.exitCode
	default:
		return -1
	}
}

// Stop stops the hub cleanly, as c1's hubproc.Stop: it closes the stdin
// pipe, which the hub reads as its stop request (with --stop-on-stdin-eof
// it has the console that MediaMTX's clean stop needs, under the S4U task
// and the systemd unit too), and kills it when it has not ended after 30 s
// or when ctx ends first. A hub that has exited needs no stop.
func (h *Hub) Stop(ctx context.Context) error {
	select {
	case <-h.exited:
		return nil
	default:
	}
	if err := h.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return errors.Join(err, h.Kill())
	}
	timer := time.NewTimer(stopWait)
	defer timer.Stop()
	select {
	case <-h.exited:
		return nil
	case <-timer.C:
		return errors.Join(errors.New("hubproc: ipcam-hub did not stop within 30 s, killed"), h.Kill())
	case <-ctx.Done():
		return errors.Join(ctx.Err(), h.Kill())
	}
}

// Kill ends the hub at once (SIGKILL, TerminateProcess with c1's
// killResult), and waits until it has exited; MediaMTX dies with it (c1
// C07). A hub that had exited already gives nil.
func (h *Hub) Kill() error {
	select {
	case <-h.exited:
		return nil
	default:
	}
	h.killed = true
	if err := kill(h.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		select {
		case <-h.exited:
			return nil
		default:
			return err
		}
	}
	<-h.exited
	return nil
}

// writePIDFile records pid and its start time at path, 0600.
func writePIDFile(path string, pid int) error {
	start, err := runstate.ProcStartMs(pid)
	if err != nil {
		return err
	}
	b, err := json.Marshal(pidRecord{PID: pid, ProcStartMs: start})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readPIDFile reads hub.pid.
func readPIDFile(path string) (pidRecord, error) {
	var rec pidRecord
	b, err := os.ReadFile(path) //nolint:gosec // uploadspike's own hub.pid
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, fmt.Errorf("hubproc: %s: %w", filepath.Base(path), err)
	}
	return rec, nil
}

// gonePoll is how often WaitGone looks at the recorded hub.
const gonePoll = 200 * time.Millisecond

// WaitGone is a new holder's first step after it takes hub.lock: while the
// process that pidFile names is still that ipcam-hub (same PID and start
// time), it waits, and after grace it kills it and waits until it has
// ended. A missing pidFile, or a process that has ended, is gone; the file
// is then removed.
func WaitGone(ctx context.Context, pidFile string, grace time.Duration) error {
	rec, err := readPIDFile(pidFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		_ = os.Remove(pidFile) // unreadable: it names no process we could wait for
		return nil
	}
	deadline := time.Now().Add(grace)
	killed := false
	for runstate.Alive(rec.PID, rec.ProcStartMs) {
		if !killed && !time.Now().Before(deadline) {
			killed = true
			if p, err := os.FindProcess(rec.PID); err == nil {
				_ = kill(p)
			}
			deadline = time.Now().Add(10 * time.Second)
		} else if killed && !time.Now().Before(deadline) {
			return fmt.Errorf("hubproc: the previous hub (pid %d) did not end after a kill", rec.PID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(gonePoll):
		}
	}
	_ = os.Remove(pidFile)
	return nil
}

// DiagError is a diag subcommand that exited with a code other than 0.
type DiagError struct {
	Code   int
	Stderr string
}

func (e *DiagError) Error() string {
	return fmt.Sprintf("hubproc: ipcam-hub diag exited with code %d: %s", e.Code, e.Stderr)
}

// Diag runs `ipcam-hub diag <args>` with stdin (nil: none), as RunAsUser
// when set (diag reach runs as the blocked identity, diag visibility as
// the catalog's owner), else as uploadspike (diag scan-secrets), and
// returns the JSON lines that it printed. An exit code other than 0 is a
// *DiagError, with the lines printed before it.
func Diag(ctx context.Context, cfg Config, args []string, stdin io.Reader) ([]json.RawMessage, error) {
	owner, err := spikeOwner(cfg.RunAsUser)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, cfg.Bin, append([]string{"diag"}, args...)...) //nolint:gosec // uploadspike's own ipcam-hub
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &stdout, &stderr
	cmd.Env = hubEnv(Config{Env: cfg.Env})
	setDiagAttr(cmd, owner)
	runErr := cmd.Run()
	var lines []json.RawMessage
	for line := range bytes.SplitSeq(stdout.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 && json.Valid(line) {
			lines = append(lines, json.RawMessage(bytes.Clone(line)))
		}
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return lines, &DiagError{Code: exit.ExitCode(), Stderr: msg}
	}
	return lines, runErr
}
