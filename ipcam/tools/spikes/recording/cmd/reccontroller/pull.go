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
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// pullReadTimeout bounds each wait of a connection of pull: for its hello,
// then, once it is the Pi's turn, for its results folder.
var pullReadTimeout = 30 * time.Second

// pull is `reccontroller pull` (G15): it waits on ln for the Pi's `recspike
// push`, stores the Pi's results folder under results/pi, and prints what
// both devices hold and, in Romanian, what is missing. It takes
// connections side by side, each with a read deadline (pullReadTimeout),
// and closes the ones still open once the Pi's folder is here, so that a
// LAN peer that connects and sends nothing holds neither the Pi's push nor
// the end (security-2).
func pull(ctx context.Context, ln net.Listener, resultsDir string, out io.Writer) error {
	defer func() { _ = ln.Close() }()
	_, _ = fmt.Fprintln(out, "Aștept rezultatele Pi-ului: porniți pe Pi ./recspike push --controller <adresa acestui PC>.")
	defer stayAwake(func(format string, args ...any) { _, _ = fmt.Fprintf(out, format+"\n", args...) })()
	// done ends with ctx, or once the Pi's folder is here: it closes the
	// listener and every connection.
	done, finish := context.WithCancel(ctx)
	defer finish()
	stop := context.AfterFunc(done, func() { _ = ln.Close() })
	defer stop()
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex // the screen, and one Pi folder at a time
		pulled bool
	)
	for {
		nc, err := ln.Accept()
		if err != nil {
			finish()
			wg.Wait()
			switch {
			case pulled:
				return nil
			case ctx.Err() != nil:
				return ctx.Err()
			}
			return err
		}
		wg.Go(func() {
			stop := context.AfterFunc(done, func() { _ = nc.Close() })
			defer stop()
			defer func() { _ = nc.Close() }()
			conn := link.NewConn(nc)
			_ = nc.SetReadDeadline(time.Now().Add(pullReadTimeout))
			err := piHello(conn)
			mu.Lock()
			defer mu.Unlock()
			if err == nil && !pulled {
				_ = nc.SetReadDeadline(time.Now().Add(pullReadTimeout))
				if err = pullFrom(conn, resultsDir, out); err == nil {
					pulled = true
					finish() // the Pi's folder is here: no more connections
				}
			}
			if err != nil && done.Err() == nil {
				_, _ = fmt.Fprintf(out, "Transferul nu a reușit: %v. Porniți din nou push pe Pi.\n", err)
			}
		})
	}
}

// piHello reads a connection's hello, which must be the Pi's.
func piHello(conn *link.Conn) error {
	m, err := conn.Receive()
	if err != nil {
		return err
	}
	if hello, ok := m.(link.Hello); !ok || !validHello(hello) || hello.Host != "pi" {
		return errors.New("s-a conectat altă gazdă decât Pi-ul")
	}
	return nil
}

// pullFrom pulls the Pi's results folder over conn, whose hello was the
// Pi's, and prints what both devices hold.
func pullFrom(conn *link.Conn, resultsDir string, out io.Writer) error {
	if err := conn.PullResults(filepath.Join(resultsDir, "pi")); err != nil {
		return err
	}
	_ = conn.Send(link.Bye{})
	_, _ = fmt.Fprintf(out, "Am pus rezultatele Pi-ului în %s.\n", filepath.Join(resultsDir, "pi"))
	piSupported := !unsupportedPi(resultsDir)
	if !piSupported {
		_, _ = fmt.Fprintln(out, "Modelul Pi-ului nu este suportat: nu se așteaptă rezultate cu Pi-ul ca gazdă.")
	}
	_, _ = fmt.Fprint(out, results.InventoryText(results.Inventory(resultsDir, piSupported)))
	return nil
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
