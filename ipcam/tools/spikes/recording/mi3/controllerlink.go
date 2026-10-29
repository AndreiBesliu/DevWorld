// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// controllerLink is recspike's link to reccontroller for an MI3 run: one
// connection for every step, whose messages one goroutine reads. recspike
// connects out; only reccontroller listens (R08).
type controllerLink struct {
	c       *link.Conn
	in      chan received
	done    chan struct{}
	session link.Session
	// host is the controller's host as recspike dialed it, which reaches
	// its RTSP server and its sink too.
	host string
}

type received struct {
	m   link.Message
	err error
}

// dialTimeout bounds the dial of the controller.
const dialTimeout = 15 * time.Second

// dialController connects to the controller at addr, a host:port, as host,
// says hello and receives its session.
func dialController(ctx context.Context, addr, host string) (*controllerLink, error) {
	hostPart, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	c, err := link.Dial(dialCtx, addr)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("nu mă pot conecta la controlor: %w", err)
	}
	l := &controllerLink{c: c, in: make(chan received, 16), done: make(chan struct{}), host: hostPart}
	go l.read()
	if err := c.Send(link.Hello{Role: "host", Host: host, Version: "c1"}); err != nil {
		_ = l.close("", false)
		return nil, err
	}
	m, err := l.await(ctx, time.Minute, "session", "")
	if err != nil {
		_ = l.close("", false)
		return nil, fmt.Errorf("controlorul nu a trimis sesiunea: %w", err)
	}
	l.session = m.(link.Session)
	return l, nil
}

// read delivers the controller's messages, then the link's error.
func (l *controllerLink) read() {
	defer close(l.in)
	for {
		m, err := l.c.Receive()
		select {
		case l.in <- received{m, err}:
		case <-l.done:
			return
		}
		if err != nil {
			return
		}
	}
}

// pullWait bounds the wait for the PC's pull after a Pi's goodbye.
const pullWait = time.Minute

// close says goodbye and closes the link. On the Pi (pi), the PC's
// reccontroller then pulls the Pi's results folder, as at the end of every
// MI1 session (G15): close answers it with results, so that the Pi's MI3
// envelope reaches the PC at once.
func (l *controllerLink) close(results string, pi bool) error {
	defer func() {
		close(l.done)
		_ = l.c.Close()
	}()
	if err := l.c.Send(link.Bye{}); err != nil || !pi {
		return err
	}
	if _, err := l.await(context.Background(), pullWait, "pull_results", ""); err != nil {
		return err
	}
	return l.c.SendResultsTar(results)
}

// request sends m, of the step runID, and returns the controller's answer
// of type typ to it within timeout; the controller's refusal of the step
// is an error with its reason.
func (l *controllerLink) request(ctx context.Context, m link.Message, runID, typ string, timeout time.Duration) (link.Message, error) {
	if err := l.c.Send(m); err != nil {
		return nil, fmt.Errorf("legătura cu controlorul s-a întrerupt: %w", err)
	}
	return l.await(ctx, timeout, typ, runID)
}

// await returns the next message of type typ for runID, dropping others,
// within timeout.
func (l *controllerLink) await(ctx context.Context, timeout time.Duration, typ, runID string) (link.Message, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("controlorul nu a răspuns (%s) în %v", typ, timeout)
		case r, ok := <-l.in:
			switch {
			case !ok:
				return nil, errors.New("legătura cu controlorul s-a închis")
			case r.err != nil:
				return nil, fmt.Errorf("legătura cu controlorul s-a întrerupt: %w", r.err)
			}
			if refused, ok := r.m.(link.Refused); ok && runID != "" && refused.RunID == runID {
				return nil, fmt.Errorf("controlorul a refuzat pasul: %s", refused.Reason)
			}
			if r.m.Type() == typ && stepOf(r.m) == runID {
				return r.m, nil
			}
		}
	}
}

// stepOf is the step that an MI3 answer is of, "" for any other message.
func stepOf(m link.Message) string {
	switch m := m.(type) {
	case link.Mi3Offers:
		return m.RunID
	case link.Mi3Live:
		return m.RunID
	case link.Mi3Result:
		return m.RunID
	}
	return ""
}

// cameraURL is the RTSP address of stream, with the session's credentials,
// at the controller's host as recspike dialed it. It holds the
// credentials: it goes only to the private run.json and to a load's stdin.
func (l *controllerLink) cameraURL(stream string) (string, error) {
	base, err := url.Parse(l.session.RTSPBase)
	if err != nil || base.Port() == "" {
		return "", fmt.Errorf("controlorul a trimis o adresă RTSP greșită: %q", l.session.RTSPBase)
	}
	u := url.URL{Scheme: base.Scheme, User: url.UserPassword(l.session.User, l.session.Pass), Host: net.JoinHostPort(l.host, base.Port()), Path: "/" + stream}
	return u.String(), nil
}

// sinkURL is the controller's upload sink, to which the upload adds each
// PUT's name.
func (l *controllerLink) sinkURL() string {
	port := l.session.SinkPort
	if port == 0 {
		port = controller.UploadPort
	}
	return "https://" + net.JoinHostPort(l.host, strconv.Itoa(port)) + "/sink/"
}
