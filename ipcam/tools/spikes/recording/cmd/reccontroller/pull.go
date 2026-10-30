// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// pull is `reccontroller pull` (G15): it waits on ln for the Pi's `recspike
// push`, stores the Pi's results folder under results/pi, and prints what
// both devices hold and, in Romanian, what is missing.
func pull(ctx context.Context, ln net.Listener, resultsDir string, out io.Writer) error {
	defer func() { _ = ln.Close() }()
	_, _ = fmt.Fprintln(out, "Aștept rezultatele Pi-ului: porniți pe Pi ./recspike push --controller <adresa acestui PC>.")
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		done, err := pullFrom(link.NewConn(nc), resultsDir, out)
		_ = nc.Close()
		if err != nil {
			_, _ = fmt.Fprintf(out, "Transferul nu a reușit: %v. Porniți din nou push pe Pi.\n", err)
		}
		if done {
			return nil
		}
	}
}

// pullFrom pulls one host's results; done is true when the host was the Pi
// and its folder arrived.
func pullFrom(conn *link.Conn, resultsDir string, out io.Writer) (done bool, err error) {
	m, err := conn.Receive()
	if err != nil {
		return false, err
	}
	if hello, ok := m.(link.Hello); !ok || pseudonym(hello.Host) != "pi" {
		return false, errors.New("s-a conectat altă gazdă decât Pi-ul")
	}
	if err := conn.PullResults(filepath.Join(resultsDir, "pi")); err != nil {
		return false, err
	}
	_ = conn.Send(link.Bye{})
	_, _ = fmt.Fprintf(out, "Am pus rezultatele Pi-ului în %s.\n", filepath.Join(resultsDir, "pi"))
	piSupported := !unsupportedPi(resultsDir)
	if !piSupported {
		_, _ = fmt.Fprintln(out, "Modelul Pi-ului nu este suportat: nu se așteaptă rezultate cu Pi-ul ca gazdă.")
	}
	_, _ = fmt.Fprint(out, results.InventoryText(results.Inventory(resultsDir, piSupported)))
	return true, nil
}

// unsupportedPi reports whether an envelope of the results says that the
// Pi's model is known and unsupported.
func unsupportedPi(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" || found {
			return nil
		}
		if e, err := results.Read(path); err == nil && e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported {
			found = true
		}
		return nil
	})
	return found
}
