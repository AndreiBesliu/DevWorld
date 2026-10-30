// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package link

import (
	"context"
)

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
