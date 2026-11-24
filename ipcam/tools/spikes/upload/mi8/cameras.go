// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package mi8 is the MI8 spike (Task 10, G17, HUB-11): the owner's cameras
// subscribed to through ipcam-hub diag onvif for 72 h (P01: the hub's events
// package is used across the process boundary only), each measured against a
// harness frame-diff motion reference that reads the camera itself (P41):
// its substream through gortsplib and an LGPL FFmpeg process, or its ONVIF
// snapshots. A camera's address, user, password, stream and snapshot URIs
// stay in RAM: they reach ipcam-hub only on its stdin, and never a file,
// argv, the environment, a log or the results (P42, PH0-22).
package mi8

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// redacted stands for a camera's address, user or password in printed
// values.
const redacted = "[redacted]"

// CameraRef is a camera of MI8: its pseudonym ID ("real-1"), its device
// service XAddr, and its ONVIF user and password. It is uploadspike's copy of
// the hub's events.CameraRef (P01), with the same redaction: fmt, JSON and
// slog show the ID only.
type CameraRef struct{ ID, XAddr, User, Pass string }

// String is the camera's ID.
func (c CameraRef) String() string { return "camera(" + c.ID + ")" }

// GoString is the camera with its address, user and password redacted.
func (c CameraRef) GoString() string {
	return fmt.Sprintf("mi8.CameraRef{ID:%q, XAddr:%q, User:%q, Pass:%q}", c.ID, redacted, redacted, redacted)
}

// MarshalJSON is {"id":<ID>}.
func (c CameraRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{c.ID})
}

// LogValue is the camera's ID.
func (c CameraRef) LogValue() slog.Value { return slog.GroupValue(slog.String("id", c.ID)) }

// Format prints the camera for every verb as String does, and %#v as
// GoString, so that no verb reaches the fields.
func (c CameraRef) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = fmt.Fprint(f, c.GoString())
		return
	}
	_, _ = fmt.Fprint(f, c.String())
}

// Secrets are the camera's values that the closing scan looks for (P45):
// its address's host, its user and its password.
func (c CameraRef) Secrets() []string {
	var out []string
	if u, err := url.Parse(c.XAddr); err == nil && u.Hostname() != "" {
		out = append(out, u.Hostname())
	}
	for _, v := range []string{c.User, c.Pass} {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// line is the camera's line on diag onvif's stdin.
func (c CameraRef) line() []byte {
	b, _ := json.Marshal(struct { //nolint:errchkjson,gosec // G117: diag onvif's stdin, RAM to RAM
		ID    string `json:"id"`
		XAddr string `json:"xaddr"`
		User  string `json:"user"`
		Pass  string `json:"pass"`
	}{c.ID, c.XAddr, c.User, c.Pass})
	return append(b, '\n')
}

// DefaultOnvifPort is the ONVIF port of an address typed without one.
const DefaultOnvifPort = 80

// ErrBadAddress is XAddrFor's error: the address is not IP[:port].
var ErrBadAddress = errors.New("mi8: the address is not IP[:port]")

// XAddrFor is the device service XAddr of an address typed as IP[:port
// ONVIF] (P42): http://<ip>:<port>/onvif/device_service, port 80 when
// absent. An IPv6 address with a port is written [addr]:port. A host name is
// taken too. The error names nothing of the address.
func XAddrFor(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.ContainsAny(addr, "/?#@ \t") {
		return "", ErrBadAddress
	}
	host, port := addr, strconv.Itoa(DefaultOnvifPort)
	if h, p, err := net.SplitHostPort(addr); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return "", ErrBadAddress
		}
		host, port = h, strconv.Itoa(n)
	} else if strings.Count(addr, ":") == 1 {
		return "", ErrBadAddress // host: without a port
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return "", ErrBadAddress
	}
	if ip := net.ParseIP(host); ip == nil && !validHostName(host) {
		return "", ErrBadAddress
	}
	return "http://" + net.JoinHostPort(host, port) + "/onvif/device_service", nil
}

// validHostName reports whether s is a DNS host name.
func validHostName(s string) bool {
	if len(s) > 253 {
		return false
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// The probe's errors, as diag onvif names them.
const (
	ProbeErrOnvifPort   = "onvif-port"
	ProbeErrAuth        = "auth"
	ProbeErrUnreachable = "unreachable"
)

// Probe is a camera's diag onvif --probe line: its events service,
// PullPoint support, clock offset, login method, brand and model; or Error.
type Probe struct {
	Cam           string `json:"cam"`
	EventsService bool   `json:"eventsService"`
	PullPoint     bool   `json:"pullPoint"`
	ClockOffsetMs int64  `json:"clockOffsetMs"`
	Auth          string `json:"auth"`
	Brand         string `json:"brand"`
	Model         string `json:"model"`
	Error         string `json:"error,omitempty"`
}

// WithEvents reports whether the camera takes part in MI8 by default: it
// answered, with an events service and PullPoint support (D3-A).
func (p Probe) WithEvents() bool { return p.Error == "" && p.EventsService && p.PullPoint }

// SameCamera reports whether p names the brand and model given (--continue).
func (p Probe) SameCamera(brand, model string) bool {
	return p.Error == "" && p.Brand == brand && p.Model == model
}

// Select keeps the cameras of probes that take part (D3): every camera with
// events, or with onePerBrand the first of each brand (D3-B), in order.
func Select(probes []Probe, onePerBrand bool) []Probe {
	var out []Probe
	seen := map[string]bool{}
	for _, p := range probes {
		if !p.WithEvents() {
			continue
		}
		brand := strings.ToLower(strings.TrimSpace(p.Brand))
		if onePerBrand && seen[brand] {
			continue
		}
		seen[brand] = true
		out = append(out, p)
	}
	return out
}

// URIs are a camera's stream and snapshot URIs, from diag onvif --uris: RAM
// only, never logged.
type URIs struct {
	Stream, Snapshot string
}

// Hub runs ipcam-hub's diag onvif. Bin is ipcam-hub; Env, when set, is the
// command's environment; Start starts the long-running subscriber (nil:
// plainly).
type Hub struct {
	Bin   string
	Env   []string
	Start Starter
}

// command is `ipcam-hub diag onvif <args>`.
func (h Hub) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, h.Bin, append([]string{"diag", "onvif"}, args...)...) //nolint:gosec // uploadspike's own ipcam-hub
	cmd.Env = h.Env
	return cmd
}

// run runs diag onvif with args and the cameras' lines on its stdin, and
// returns its JSON lines. Its stderr is dropped: it names no camera value,
// and uploadspike never shows it.
func (h Hub) run(ctx context.Context, cams []CameraRef, args ...string) ([]json.RawMessage, error) {
	var stdin, stdout bytes.Buffer
	for _, c := range cams {
		stdin.Write(c.line())
	}
	cmd := h.command(ctx, args...)
	cmd.Stdin, cmd.Stdout = &stdin, &stdout
	err := cmd.Run()
	var lines []json.RawMessage
	for line := range bytes.SplitSeq(stdout.Bytes(), []byte("\n")) {
		if json.Valid(line) && len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, json.RawMessage(bytes.Clone(line)))
		}
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return lines, fmt.Errorf("mi8: ipcam-hub diag onvif exited with code %d", exit.ExitCode())
		}
		return lines, errors.New("mi8: ipcam-hub diag onvif did not run")
	}
	return lines, nil
}

// Probe runs diag onvif --probe over cams and returns their probes in cams'
// order; a camera without a line has Error "unreachable".
func (h Hub) Probe(ctx context.Context, cams []CameraRef) ([]Probe, error) {
	lines, err := h.run(ctx, cams, "--probe")
	got := map[string]Probe{}
	for _, l := range lines {
		var p struct {
			Type string `json:"type"`
			Probe
		}
		if json.Unmarshal(l, &p) == nil && p.Type == "probe" {
			got[p.Cam] = p.Probe
		}
	}
	out := make([]Probe, 0, len(cams))
	for _, c := range cams {
		p, ok := got[c.ID]
		if !ok {
			p = Probe{Cam: c.ID, Error: ProbeErrUnreachable}
		}
		out = append(out, p)
	}
	return out, err
}

// URIs runs diag onvif --uris --profile sub over cams: each camera's
// substream and snapshot URIs, an empty one when the camera gave none.
func (h Hub) URIs(ctx context.Context, cams []CameraRef) (map[string]URIs, error) {
	lines, err := h.run(ctx, cams, "--uris", "--profile", "sub")
	out := map[string]URIs{}
	for _, l := range lines {
		var u struct {
			Type, Cam, URI string
		}
		if json.Unmarshal(l, &u) != nil || u.URI == "" {
			continue
		}
		v := out[u.Cam]
		switch u.Type {
		case "stream_uri":
			v.Stream = u.URI
		case "snapshot_uri":
			v.Snapshot = u.URI
		}
		out[u.Cam] = v
	}
	return out, err
}

// The kinds of a subscription's lines, as diag onvif prints them.
const (
	KindEvent           = "onvif_event"
	KindUnhealthy       = "events_unhealthy"
	KindLost            = "events_lost"
	KindHealthy         = "events_healthy"
	KindSubscribeFailed = "subscribe_failed"
)

// Line is a line of diag onvif's default mode: an event, a signal or a
// failed subscription.
type Line struct {
	Type   string `json:"type"`
	Cam    string `json:"cam"`
	AtMs   int64  `json:"atMs"`
	Topic  string `json:"topic,omitempty"`
	Motion bool   `json:"motion,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Subscriber is a running diag onvif in its default mode.
type Subscriber struct {
	lines chan Line
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  <-chan struct{}
	ended chan struct{}
	once  sync.Once
}

// Subscribe starts diag onvif in its default mode with cams on its stdin,
// which stays open until Stop: the hub subscribes to each camera until then.
// Lines must be drained until it closes.
func (h Hub) Subscribe(ctx context.Context, cams []CameraRef) (*Subscriber, error) {
	cmd := h.command(context.WithoutCancel(ctx))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	start := h.Start
	if start == nil {
		start = plainStart
	}
	done, err := start(cmd)
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return nil, errors.New("mi8: ipcam-hub diag onvif did not start")
	}
	s := &Subscriber{lines: make(chan Line, 256), cmd: cmd, stdin: stdin, done: done, ended: make(chan struct{})}
	go func() {
		defer close(s.ended)
		defer close(s.lines)
		defer func() { _ = pr.Close() }()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			var l Line
			if json.Unmarshal(sc.Bytes(), &l) == nil && l.Type != "" {
				s.lines <- l
			}
		}
	}()
	for _, c := range cams {
		if _, err := stdin.Write(c.line()); err != nil {
			s.Stop()
			break // diag onvif ended at once: its lines say why
		}
	}
	context.AfterFunc(ctx, s.Stop)
	return s, nil
}

// Lines are the subscription's lines; closed once diag onvif ended.
func (s *Subscriber) Lines() <-chan Line { return s.lines }

// Stop closes diag onvif's stdin, so it unsubscribes and exits 0.
func (s *Subscriber) Stop() { s.once.Do(func() { _ = s.stdin.Close() }) }

// Wait stops diag onvif and waits for its end; it is killed when ctx ends
// first.
func (s *Subscriber) Wait(ctx context.Context) error {
	s.Stop()
	select {
	case <-s.done:
	case <-ctx.Done():
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-s.done
	}
	<-s.ended
	if s.cmd.ProcessState != nil && !s.cmd.ProcessState.Success() {
		return errors.New("mi8: ipcam-hub diag onvif ended with an error")
	}
	return nil
}
