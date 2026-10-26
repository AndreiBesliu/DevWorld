// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// answerPull serves the far side's pull_results with the results folder,
// until it closes the link.
func answerPull(ctx context.Context, s *session, dir string, out io.Writer) error {
	timeout := time.NewTimer(verdictTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return nil
		case r, ok := <-s.in:
			if !ok || r.err != nil {
				return nil // the controller closed the link
			}
			if _, ok := r.m.(link.PullResults); ok {
				if err := s.c.SendResultsTar(dir); err != nil {
					return err
				}
				if out != nil {
					_, _ = fmt.Fprintln(out, "Am trimis rezultatele.")
				}
			}
		}
	}
}

// geteuid, fileOwner and dialLink are the system's; tests replace them.
var (
	geteuid   = os.Geteuid
	fileOwner = ownerUID
	dialLink  = link.Dial
)

// pushNeedsSudo is push's answer when root owns the results.
const pushNeedsSudo = "rezultatele sunt ale lui root, fiindcă proba a rulat cu sudo: " +
	"porniți și trimiterea cu sudo (sudo ./recspike push --controller <adresa PC-ului>)"

// needsRoot reports whether recspike, run as a user other than root, cannot
// read the results folder dir: on the Pi, MI1 runs as root, so root owns its
// results, which only root may read. On Windows (no euid) it is false.
func needsRoot(dir string) (bool, error) {
	if geteuid() <= 0 {
		return false, nil
	}
	root := false
	err := filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrPermission):
			root = true
			return filepath.SkipAll
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if fileOwner(info) == 0 {
			root = true
			return filepath.SkipAll
		}
		return nil
	})
	return root, err
}

// Push is `recspike push --controller <PC>` on the Pi, the final collection
// (G15): it connects to the PC's reccontroller, which pulls the results
// folder dir, and returns when the PC has it. Started without sudo on
// results that root owns, it says so in Romanian before it connects.
func Push(ctx context.Context, addr, dir, host string, out io.Writer) error {
	if root, err := needsRoot(dir); err != nil {
		return err
	} else if root {
		return errors.New(pushNeedsSudo)
	}
	s, err := dialSession(ctx, addr, host, 0, nil)
	if err != nil {
		return err
	}
	defer s.close()
	// `reccontroller pull` asks at once; a running reccontroller sends its
	// session first and pulls a Pi's folder after the bye.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r, ok := <-s.in:
			if !ok || r.err != nil {
				return fmt.Errorf("controlorul a închis legătura înainte să ceară rezultatele: %v", r.err)
			}
			switch r.m.(type) {
			case link.Session:
				if err := s.send(link.Bye{}); err != nil {
					return err
				}
			case link.PullResults:
				if err := s.c.SendResultsTar(dir); err != nil {
					return err
				}
				_, _ = fmt.Fprintln(out, "Am trimis rezultatele.")
				return nil
			}
		}
	}
}

// session is the host's link to the controller: one goroutine reads its
// messages into in, and closes broken when the link fails.
type session struct {
	c          *link.Conn
	in         chan received
	closed     chan struct{}
	broken     chan struct{}
	info       link.Session
	beaconAddr string
}

type received struct {
	m   link.Message
	err error
}

// errLinkDown is a failed link to the controller, which an unattended set
// connects again.
var errLinkDown = errors.New("legătura cu controlorul s-a întrerupt")

// connect connects to the controller, retrying for a while after a boot,
// when the network may not be up yet, and receives the session.
func (d *Driver) connect(ctx context.Context) (*session, error) {
	return d.connectWithin(ctx, connectTimeout)
}

// reconnect replaces the dropped link of s with a new one, in an unattended
// set (automated or lab): it dials again for up to reconnectWait. A real
// set's owner is at the host, and a drop ends it, as before.
func (d *Driver) reconnect(ctx context.Context, s *session, st *RunState, cause error) error {
	if st.Mode == ModeReal {
		return cause
	}
	d.say("%v; mă reconectez…", cause)
	fresh, err := d.connectWithin(ctx, reconnectWait)
	if err != nil {
		return fmt.Errorf("%w, și nu s-a refăcut: %w", cause, err)
	}
	s.close()
	*s = *fresh
	d.say("Legătura cu controlorul s-a refăcut.")
	return nil
}

// connectWithin connects to the controller, dialing again for up to retry,
// and receives the session.
func (d *Driver) connectWithin(ctx context.Context, retry time.Duration) (*session, error) {
	s, err := dialSession(ctx, d.Controller, d.Host.Pseudonym, retry, d.linkDown)
	if err != nil {
		return nil, err
	}
	m, err := s.await(ctx, time.Minute, "session")
	if err != nil {
		s.close()
		return nil, fmt.Errorf("controlorul nu a trimis sesiunea: %w", err)
	}
	s.info = m.(link.Session)
	host, _, err := net.SplitHostPort(d.Controller)
	if err != nil {
		s.close()
		return nil, err
	}
	port := s.info.BeaconPort
	if port == 0 {
		port = link.BeaconPort
	}
	s.beaconAddr = net.JoinHostPort(host, strconv.Itoa(port))
	return s, nil
}

// dialSession dials the controller, retrying for up to retry, and says
// hello as the host. down, when not nil, runs once the link has failed and
// the session reports it down.
func dialSession(ctx context.Context, addr, host string, retry time.Duration, down func()) (*session, error) {
	deadline := time.Now().Add(retry)
	for {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		c, err := dialLink(dialCtx, addr)
		cancel()
		if err == nil {
			s := &session{c: c, in: make(chan received, 16), closed: make(chan struct{}), broken: make(chan struct{})}
			go read(c, s.in, s.closed, s.broken, down)
			if err := s.send(link.Hello{Role: "host", Host: host, Version: "c1"}); err != nil {
				s.close()
				return nil, err
			}
			return s, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("nu mă pot conecta la controlor (portul %d): %w", link.Port, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// read delivers the controller's messages on c into in, then its error,
// and closes in; it closes broken as soon as the link fails, then runs
// down, if any. It holds its own channels: a reconnection gives the session
// new ones.
func read(c *link.Conn, in chan<- received, closed <-chan struct{}, broken chan<- struct{}, down func()) {
	defer close(in)
	for {
		m, err := c.Receive()
		if err != nil {
			close(broken)
			if down != nil {
				down()
			}
			err = fmt.Errorf("%w: %w", errLinkDown, err)
		}
		select {
		case in <- received{m, err}:
		case <-closed:
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *session) send(m link.Message) error {
	if err := s.c.Send(m); err != nil {
		return fmt.Errorf("%w: %w", errLinkDown, err)
	}
	return nil
}

// sendAll sends msgs in order, until one fails.
func (s *session) sendAll(msgs []link.Message) error {
	for _, m := range msgs {
		if err := s.send(m); err != nil {
			return err
		}
	}
	return nil
}

// up reports whether the link has not failed.
func (s *session) up() bool {
	select {
	case <-s.broken:
		return false
	default:
		return true
	}
}

func (s *session) close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	_ = s.c.Close()
}

// await returns the next message of type typ, dropping others, within d.
func (s *session) await(ctx context.Context, d time.Duration, typ string) (link.Message, error) {
	timeout := time.NewTimer(d)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			return nil, fmt.Errorf("controlorul nu a răspuns (%s) în %v", typ, d)
		case r, ok := <-s.in:
			if !ok {
				return nil, fmt.Errorf("%w: %w", errLinkDown, io.EOF)
			}
			if r.err != nil {
				return nil, r.err
			}
			if r.m.Type() == typ {
				return r.m, nil
			}
		}
	}
}

// report sends a run's report and returns the controller's verdict.
func (s *session) report(ctx context.Context, r link.Report) (link.Verdict, error) {
	if err := s.send(r); err != nil {
		return link.Verdict{}, err
	}
	for {
		m, err := s.await(ctx, verdictTimeout, "verdict")
		if err != nil {
			return link.Verdict{}, err
		}
		if v := m.(link.Verdict); v.Run.RunID == r.RunID {
			return v, nil
		}
	}
}

// probes are the link's preflight probes (R08): the beacon acknowledged
// over TCP, an RTSP pull of cam01_main, and the NTP check.
func (s *session) probes(ntp NTPCheck) LinkProbes {
	return LinkProbes{
		Beacon: s.checkBeacon,
		RTSP: func(ctx context.Context) error {
			base, err := url.Parse(s.info.RTSPBase)
			if err != nil {
				return err
			}
			u := url.URL{Scheme: base.Scheme, User: url.UserPassword(s.info.User, s.info.Pass), Host: base.Host, Path: "/" + MainStream}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			src, err := source.NewRTSP(ctx, "preflight", u.String())
			if err != nil {
				return err
			}
			cached, live, stop := src.Subscribe()
			defer stop()
			if len(cached) > 0 {
				return nil
			}
			select {
			case _, ok := <-live:
				if !ok {
					return errors.New("the camera stopped")
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		NTP: ntp,
	}
}

// checkBeacon sends beacons of a check id while it asks the controller
// whether they arrive.
func (s *session) checkBeacon(ctx context.Context) error {
	id := fmt.Sprintf("verificare-%d", time.Now().UnixNano())
	conn, err := net.Dial("udp4", s.beaconAddr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := s.send(link.BeaconCheck{ID: id}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go func() {
		tick := time.NewTicker(controller.BeaconEvery)
		defer tick.Stop()
		for seq := uint64(0); ctx.Err() == nil; seq++ {
			_, _ = conn.Write(link.BeaconPacket(id, seq))
			select {
			case <-ctx.Done():
			case <-tick.C:
			}
		}
	}()
	m, err := s.await(ctx, 10*time.Second, "beacon_ack")
	if err != nil {
		return err
	}
	if !m.(link.BeaconAck).Received {
		return errors.New("no beacon arrived")
	}
	return nil
}
