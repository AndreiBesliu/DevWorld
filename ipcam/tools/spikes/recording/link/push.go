// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package link

import (
	"context"
	"net"
	"strconv"
	"time"
)

// pushDialTimeout bounds PushTo's dial of the controller.
const pushDialTimeout = 10 * time.Second

// dial is Dial; tests replace it.
var dial = Dial

// DialError is PushTo's error when it cannot connect to the controller;
// Err says why.
type DialError struct{ Err error }

func (e *DialError) Error() string { return "link: cannot connect to the controller: " + e.Err.Error() }

func (e *DialError) Unwrap() error { return e.Err }

// PushTo is the final collection's push (G15) as `recspike push` and, on
// an armv6 Pi, `recspike-probe --push` run it: it dials the controller at
// addr, on the link's port when addr names none, for at most 10 s, then
// pushes the results folder dir as host (Push). A dial that fails is a
// DialError; the end of ctx, during the dial or after it, is ctx's error,
// so that the owner's Ctrl+C never reads as a connection failure. The
// callers word each error in Romanian.
func PushTo(ctx context.Context, addr, host, dir string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, strconv.Itoa(Port))
	}
	dialCtx, cancel := context.WithTimeout(ctx, pushDialTimeout)
	c, err := dial(dialCtx, addr)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &DialError{Err: err}
	}
	defer func() { _ = c.Close() }()
	return Push(ctx, c, host, dir)
}

// Version is the protocol's version, which a host's hello names.
const Version = "c1"

// NoPullError is Push's error when the link ends, or fails, before the
// controller asks for the results; Err says why.
type NoPullError struct{ Err error }

func (e *NoPullError) Error() string {
	return "link: the controller closed the link before it asked for the results: " + e.Err.Error()
}

func (e *NoPullError) Unwrap() error { return e.Err }

// Push is a host's side of the final collection (G15) on c, a new link to
// the controller: it says hello as host and answers the controller's pull
// with the results folder dir. `reccontroller pull` asks at once; a
// running reccontroller sends its session first, which Push answers with a
// goodbye, and pulls a Pi's folder after it. Push returns once the folder
// went out; the end of ctx closes c. `recspike push` and, on an armv6 Pi,
// `recspike-probe --push` run it.
func Push(ctx context.Context, c *Conn, host, dir string) error {
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	fail := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err := c.Send(Hello{Role: "host", Host: host, Version: Version}); err != nil {
		return fail(err)
	}
	for {
		m, err := c.Receive()
		if err != nil {
			return fail(&NoPullError{Err: err})
		}
		switch m.(type) {
		case Session:
			if err := c.Send(Bye{}); err != nil {
				return fail(err)
			}
		case PullResults:
			if err := c.SendResultsTar(dir); err != nil {
				return fail(err)
			}
			return nil
		}
	}
}
