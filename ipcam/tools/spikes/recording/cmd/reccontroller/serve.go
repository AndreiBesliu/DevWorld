// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// serveConfig is where reccontroller serves and keeps its results.
type serveConfig struct {
	dir, media string
	local      bool
	listen     controller.ListenConfig
}

// serve serves until ctx is done.
func serve(ctx context.Context, cfg serveConfig, out io.Writer) error {
	logFile, err := os.OpenFile(filepath.Join(cfg.dir, "reccontroller.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // next to the binary
	if err != nil {
		return fmt.Errorf("nu pot deschide jurnalul: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))

	creds, err := controller.NewCredentials()
	if err != nil {
		return err
	}
	lns, err := controller.ListenAll(ctx, cfg.listen)
	if err != nil {
		return fmt.Errorf("nu pot asculta pe porturile probei (rulează deja un reccontroller?): %w", err)
	}
	defer func() { _ = lns.Close() }()

	clock := source.SystemClock{}
	sendLog := controller.NewSendLog()
	streams := controller.Streams(cfg.media)
	srv, err := controller.ServePrecodedOn(ctx, lns.RTSP, streams, creds, sendLog, clock)
	if err != nil {
		return fmt.Errorf("lipsesc sau nu se pot citi fișierele video din %s: %w", cfg.media, err)
	}
	defer srv.Close()
	needles, err := mi1.MediaNeedles(mediaFiles(streams)...)
	if err != nil {
		return fmt.Errorf("nu pot citi fișierele video din %s: %w", cfg.media, err)
	}
	c := newCtl(ctx, ctlConfig{
		pacer: srv, sendLog: sendLog, clock: clock, out: out, results: filepath.Join(cfg.dir, "results"), local: cfg.local,
		rtspPort: lns.RTSP.Addr().(*net.TCPAddr).Port, beaconPort: lns.Beacon.LocalAddr().(*net.UDPAddr).Port,
		creds: creds, streams: streams, needles: needles,
	})
	go superviseBeacons(ctx, func(ctx context.Context) error {
		return controller.ReceiveBeacons(ctx, lns.Beacon, c.beacons, clock)
	}, c.say)
	// Tasks 9 to 11 serve the app endpoints and the upload sink here.
	for _, ln := range []net.Listener{lns.App, lns.Upload} {
		hs := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = hs.Serve(ln) }()
		defer func() { _ = hs.Close() }()
	}
	where := "pe toate interfețele IPv4"
	if cfg.local {
		where = "doar pe acest dispozitiv (127.0.0.1)"
	}
	c.say("reccontroller rulează %s: RTSP %d, legătură %d, semnal %d, aplicație %d, încărcare %d.",
		where, cfg.listen.RTSPPort, cfg.listen.LinkPort, cfg.listen.BeaconPort, cfg.listen.AppPort, cfg.listen.UploadPort)
	c.say("Rezultatele se păstrează în %s. Oprire: Ctrl+C.", c.results)

	go func() {
		<-ctx.Done()
		_ = lns.Link.Close()
	}()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := lns.Link.Accept()
		if err != nil {
			if ctx.Err() != nil {
				c.say("reccontroller se oprește.")
				return nil
			}
			return err
		}
		wg.Go(func() { c.serve(ctx, link.NewConn(conn), conn.LocalAddr()) })
	}
}

// mediaFiles is each media file of the streams, once.
func mediaFiles(streams []controller.Stream) []string {
	var files []string
	seen := map[string]bool{}
	for _, s := range streams {
		if !seen[s.File] {
			seen[s.File] = true
			files = append(files, s.File)
		}
	}
	return files
}

// beaconRestartDelay is how long superviseBeacons waits before it starts a
// failed receiver again.
var beaconRestartDelay = time.Second

// superviseBeacons runs the beacon receiver until ctx ends. A receiver
// that fails is shown on the screen and started again after a second, since
// a run measured without beacons is invalid; after 10 failures in a row it
// stops for good and says so.
func superviseBeacons(ctx context.Context, receive func(context.Context) error, say func(string, ...any)) {
	const maxFailures = 10
	for failures := 1; ; failures++ {
		err := receive(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		if failures >= maxFailures {
			say("Recepția semnalului de la gazdă s-a oprit definitiv: %v. Reporniți reccontroller; rulările de acum nu se pot măsura.", err)
			return
		}
		say("Recepția semnalului de la gazdă s-a oprit: %v. O repornesc.", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(beaconRestartDelay):
		}
	}
}
