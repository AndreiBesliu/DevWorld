package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	onvif "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events"
)

// The types of diag onvif's output lines.
const (
	onvifTypeProbe           = "probe"
	onvifTypeStreamURI       = "stream_uri"
	onvifTypeSnapshotURI     = "snapshot_uri"
	onvifTypeEvent           = "onvif_event"
	onvifTypeSubscribeFailed = "subscribe_failed"
	onvifTypeSubscribed      = "subscribed"
)

// onvifOptions are the subscriptions' options; the tests shorten them.
var onvifOptions = onvif.Options{}

// onvifCloseTimeout bounds the unsubscribes at the end of diag onvif.
const onvifCloseTimeout = 10 * time.Second

// onvifMaxLine bounds a camera line of stdin.
const onvifMaxLine = 64 << 10

// onvifCamID is a camera's pseudonym: "real-1", never an address.
var onvifCamID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// onvifCameraLine is a camera line of stdin. It lives in RAM only.
type onvifCameraLine struct {
	ID    string `json:"id"`
	XAddr string `json:"xaddr"`
	User  string `json:"user"`
	Pass  string `json:"pass"`
}

// onvifProbeLine is a camera's probe.
type onvifProbeLine struct {
	Type          string `json:"type"`
	Cam           string `json:"cam"`
	EventsService bool   `json:"eventsService"`
	PullPoint     bool   `json:"pullPoint"`
	ClockOffsetMs int64  `json:"clockOffsetMs"`
	Auth          string `json:"auth"`
	Brand         string `json:"brand"`
	Model         string `json:"model"`
}

// onvifErrorLine is a probe or a URI that failed, or a subscription that
// could not be created, with its reason.
type onvifErrorLine struct {
	Type   string `json:"type"`
	Cam    string `json:"cam"`
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// onvifURILine is a stream or snapshot URI. uploadspike reads it over the
// pipe into RAM and never logs it (P41).
type onvifURILine struct {
	Type string `json:"type"`
	Cam  string `json:"cam"`
	URI  string `json:"uri"`
}

// onvifEventLine is a camera's notification, at the hub's receipt time.
type onvifEventLine struct {
	Type   string `json:"type"`
	Cam    string `json:"cam"`
	AtMs   int64  `json:"atMs"`
	Topic  string `json:"topic"`
	Motion bool   `json:"motion"`
}

// onvifSignalLine is a subscription's state change.
type onvifSignalLine struct {
	Type string `json:"type"`
	Cam  string `json:"cam"`
	AtMs int64  `json:"atMs"`
}

// lineWriter prints JSON lines from several goroutines.
type lineWriter struct {
	mu             sync.Mutex
	stdout, stderr io.Writer
}

// print prints v as one JSON line.
func (w *lineWriter) print(v any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = printJSONLine(w.stdout, w.stderr, v)
}

// diagOnvifCommand is "ipcam-hub diag onvif [--probe | --uris [--profile
// sub]]" (P01 item 4, P41, P42): the MI8 spike's ONVIF calls, run by the
// hub's events package. The cameras arrive only as JSON lines on stdin,
// {"id":"real-1","xaddr":…,"user":…,"pass":…}, never through a file or the
// command line, and stay in RAM. Each line is served as it arrives, and the
// output is JSON lines on stdout:
//   - --probe: {"type":"probe","cam",…,"brand","model"} per camera, or
//     {"type":"probe","cam","error":"onvif-port"|"auth"|"unreachable"};
//   - --uris: {"type":"stream_uri","cam","uri"} and {"type":"snapshot_uri",…}
//     of --profile (sub by default), or the same types with "error";
//   - default: subscribe to each camera until stdin closes or SIGTERM, and
//     print subscribed {cam, atMs} once its subscription is created, then
//     onvif_event {cam, atMs, topic, motion}, events_unhealthy, events_lost
//     and events_healthy {cam, atMs}; or subscribe_failed {cam, reason:
//     auth|no-pullpoint|unreachable}.
//
// No line, and nothing on stderr, holds a camera's address, user, password
// or serial number; only --uris prints the URIs the camera returns. It exits
// 1 on a usage error or a line that is not a camera, 0 otherwise.
func diagOnvifCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag onvif", flag.ContinueOnError)
	flags.SetOutput(stderr)
	probe := flags.Bool("probe", false, "probe each camera: events service, PullPoint, clock offset, login method, brand and model")
	uris := flags.Bool("uris", false, "print each camera's stream and snapshot URIs")
	profile := flags.String("profile", onvif.ProfileSub, "the profile of --uris: main, sub or a profile token")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if flags.NArg() != 0 || (*probe && *uris) || stdin == nil {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	out := &lineWriter{stdout: stdout, stderr: stderr}
	var serve func(ctx context.Context, cam onvif.CameraRef)
	switch {
	case *probe:
		serve = func(ctx context.Context, cam onvif.CameraRef) { onvifProbe(ctx, cam, out) }
	case *uris:
		serve = func(ctx context.Context, cam onvif.CameraRef) { onvifURIs(ctx, cam, *profile, out) }
	default:
		serve = func(ctx context.Context, cam onvif.CameraRef) { onvifSubscribe(ctx, cam, out) }
	}
	// In the default mode a camera is served until stdin closes; the other
	// modes finish each camera's lines, so stdin's end only waits for them.
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A signal can end the command while the reader still waits on stdin:
	// once stopped, the reader starts no camera, so wg.Wait has the last word.
	var (
		mu      sync.Mutex
		stopped bool
		wg      sync.WaitGroup
	)
	readErr := make(chan error, 1)
	go func() {
		readErr <- readCameraLines(stdin, func(cam onvif.CameraRef) {
			mu.Lock()
			defer mu.Unlock()
			if !stopped {
				wg.Go(func() { serve(subCtx, cam) })
			}
		})
	}()
	var err error
	select {
	case err = <-readErr:
	case <-ctx.Done():
	}
	if (!*probe && !*uris) || err != nil || ctx.Err() != nil {
		cancel()
	}
	mu.Lock()
	stopped = true
	mu.Unlock()
	wg.Wait()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag onvif:", err)
		return exitFatal
	}
	return exitOK
}

// readCameraLines reads stdin's camera lines and passes each to serve, until
// stdin ends. A line that is not a camera stops it with an error that quotes
// nothing of the line, which may hold a password.
func readCameraLines(stdin io.Reader, serve func(onvif.CameraRef)) error {
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 4096), onvifMaxLine)
	n := 0
	for sc.Scan() {
		n++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var line onvifCameraLine
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		if dec.Decode(&line) != nil {
			return fmt.Errorf("stdin line %d is not a camera line {\"id\",\"xaddr\",\"user\",\"pass\"}", n)
		}
		if !onvifCamID.MatchString(line.ID) {
			return fmt.Errorf("stdin line %d: the id is not a pseudonym such as real-1", n)
		}
		if u, err := url.Parse(line.XAddr); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("stdin line %d (%s): the xaddr is not an http URL", n, line.ID)
		}
		serve(onvif.CameraRef{ID: line.ID, XAddr: line.XAddr, User: line.User, Pass: line.Pass})
	}
	if err := sc.Err(); err != nil {
		return errors.New("stdin: a line is too long or unreadable")
	}
	return nil
}

// onvifProbe prints a camera's probe line.
func onvifProbe(ctx context.Context, cam onvif.CameraRef, out *lineWriter) {
	res, err := onvif.Probe(ctx, cam, onvifOptions)
	if err != nil {
		out.print(onvifErrorLine{Type: onvifTypeProbe, Cam: cam.ID, Error: onvif.Reason(err)})
		return
	}
	out.print(onvifProbeLine{Type: onvifTypeProbe, Cam: cam.ID, EventsService: res.EventsService, PullPoint: res.PullPoint,
		ClockOffsetMs: res.ClockOffsetMs, Auth: res.Auth, Brand: res.Brand, Model: res.Model})
}

// onvifURIs prints a camera's stream and snapshot URI lines.
func onvifURIs(ctx context.Context, cam onvif.CameraRef, profile string, out *lineWriter) {
	stream, snapshot, streamErr, snapshotErr := onvif.MediaURIs(ctx, cam, profile, onvifOptions)
	for _, u := range []struct {
		typ, uri string
		err      error
	}{{onvifTypeStreamURI, stream, streamErr}, {onvifTypeSnapshotURI, snapshot, snapshotErr}} {
		if u.err != nil {
			out.print(onvifErrorLine{Type: u.typ, Cam: cam.ID, Error: onvif.Reason(u.err)})
			continue
		}
		out.print(onvifURILine{Type: u.typ, Cam: cam.ID, URI: u.uri})
	}
}

// onvifSubscribe subscribes to a camera and prints its events and signals
// until ctx ends, then unsubscribes.
func onvifSubscribe(ctx context.Context, cam onvif.CameraRef, out *lineWriter) {
	sub, err := onvif.Subscribe(ctx, cam, onvifOptions)
	if err != nil {
		reason := onvif.Reason(err)
		if reason == onvif.ReasonOnvifPort {
			reason = onvif.ReasonUnreachable
		}
		out.print(onvifErrorLine{Type: onvifTypeSubscribeFailed, Cam: cam.ID, Reason: reason})
		return
	}
	// A quiet camera prints nothing more for hours: MI8 must know that it
	// subscribed, so that a later failure does not hide its series.
	out.print(onvifSignalLine{Type: onvifTypeSubscribed, Cam: cam.ID, AtMs: time.Now().UnixMilli()})
	evs, sigs := sub.Events(), sub.Signals()
	for evs != nil || sigs != nil {
		select {
		case e, ok := <-evs:
			if !ok {
				evs = nil
				continue
			}
			out.print(onvifEventLine{Type: onvifTypeEvent, Cam: e.Cam, AtMs: e.AtMs, Topic: e.Topic, Motion: e.Motion})
		case s, ok := <-sigs:
			if !ok {
				sigs = nil
				continue
			}
			out.print(onvifSignalLine{Type: s.Kind, Cam: s.Cam, AtMs: s.AtMs})
		case <-ctx.Done():
			closeCtx, cancel := context.WithTimeout(context.Background(), onvifCloseTimeout)
			_ = sub.Close(closeCtx)
			cancel()
			// Close ended the loop: drain what it delivered before.
			ctx = context.Background()
		}
	}
}
