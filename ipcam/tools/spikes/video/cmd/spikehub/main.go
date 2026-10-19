// Command spikehub is the hub of the Phase 0 (b) video spike: it serves the
// spike app, simulated cameras over WebRTC, the HLS recording, and stores the
// results that the app uploads.
//
//	spikehub [-media DIR] [-sims 16] [-port 7080] [-results DIR] [-mdns=true]
//	spikehub -real
//
// With -real it serves nothing: it probes up to four of the owner's real
// cameras, prints what each sends and exits (R43, real.go).
//
// See ipcam/docs/superpowers/plans/2026-10-18-phase0b-video-spike.md, Task 6.
// The routes are:
//
//	GET  /                      the embedded spike app (and /assets/*)
//	GET  /spike/v1/cameras      the cameras and their profiles
//	POST /spike/v1/offer        SDP offer in, SDP answer out (application/sdp)
//	GET  /media/hls/*           the 10 min VOD, with byte ranges
//	POST /spike/v1/results      a scenario result, stored in the results folder
//
// The camera numbers that the list and the "sent" messages carry are 1 to 16
// for sim-01 to sim-16, 17 for mx-h265 and 18 for mx-high51 (R1, R3).
package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/session"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/webrtc/v4"
	"golang.org/x/term"
)

// webFiles is the spike app. The repository holds only web/.gitkeep (all: so
// that a dot file counts); a release build copies the app's dist/ into web/.
//
//go:embed all:web
var webFiles embed.FS

// config is what the flags set.
type config struct {
	media   string
	sims    int
	port    int
	results string
	mdns    bool
	real    bool
}

// parseFlags reads the command line. The usage and the flag errors go to out.
func parseFlags(args []string, out io.Writer) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("spikehub", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&cfg.media, "media", "./media", "folder with the files that cmd/mediagen wrote")
	flags.IntVar(&cfg.sims, "sims", maxSims, "number of simulated cameras, sim-01 to sim-NN (0 to 16)")
	flags.IntVar(&cfg.port, "port", 7080, "TCP port of the hub (0 picks a free one)")
	flags.StringVar(&cfg.results, "results", "./results", "folder for the results files")
	flags.BoolVar(&cfg.mdns, "mdns", true, "resolve the .local candidates of browsers with mDNS")
	flags.BoolVar(&cfg.real, "real", false, "only probe up to 4 real cameras, whose RTSP URLs are typed hidden, and exit; serves nothing")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() > 0 {
		// The argument is not echoed: it may be a camera URL with a password.
		return config{}, errors.New("nu primește argumente; adresele camerelor reale se introduc cu -real, ascuns")
	}
	if cfg.sims < 0 || cfg.sims > maxSims {
		return config{}, fmt.Errorf("-sims trebuie să fie între 0 și %d (camerele %d și %d sunt ale matricei de coduri)", maxSims, cameraMatrixH265, cameraMatrixHigh51)
	}
	if cfg.port < 0 || cfg.port > 65535 {
		return config{}, errors.New("-port trebuie să fie între 0 și 65535")
	}
	return cfg, nil
}

// env is what run needs from the process around it; tests replace the parts.
// A nil field takes the default.
type env struct {
	stdout, stderr io.Writer
	// askSecret asks for the RTSP URLs of -real, and for its final Enter. The
	// default reads the terminal, hidden.
	askSecret askFunc
	// listen opens the hub's port. The default listens on all IPv4 interfaces:
	// a phone needs every interface, and IPv4 only matches the hub's ICE (R25).
	// Listening dual-stack, Firefox at http://localhost reaches the hub over
	// [::1]; without a camera permission it then gathers ICE candidates on its
	// default IPv6 address only, and on a host without an IPv6 route it finds
	// none and never connects (MV13). Refused on [::1], it falls back to
	// 127.0.0.1 and gathers on its default IPv4 address.
	listen func(port int) (net.Listener, error)
	// lan lists the private addresses for the banner.
	lan func() ([]lanAddr, error)
	// newAPI is session.NewAPI.
	newAPI func(mdns bool) (*webrtc.API, bool, error)
	// clock paces the simulated cameras; the default is the system clock.
	clock source.Clock
	// keepAwake keeps the PC from sleeping while the hub serves. The default is
	// defaultKeepAwake, which is nil, and nothing is asked, outside Windows.
	keepAwake keepAwakeFunc
	// realFrameTimeout is how long -real waits for a camera's first frame.
	realFrameTimeout time.Duration
}

func (e env) withDefaults() env {
	if e.stdout == nil {
		e.stdout = os.Stdout
	}
	if e.stderr == nil {
		e.stderr = os.Stderr
	}
	if e.listen == nil {
		e.listen = func(port int) (net.Listener, error) { return net.Listen("tcp4", ":"+strconv.Itoa(port)) }
	}
	if e.lan == nil {
		e.lan = lanAddrs
	}
	if e.newAPI == nil {
		e.newAPI = session.NewAPI
	}
	if e.clock == nil {
		e.clock = source.SystemClock{}
	}
	if e.keepAwake == nil {
		e.keepAwake = defaultKeepAwake
	}
	if e.realFrameTimeout == 0 {
		e.realFrameTimeout = realFrameTimeout
	}
	return e
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Args[1:], env{})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "spikehub:", err)
		waitForEnter()
		stop()
		os.Exit(1)
	}
}

// waitForEnter keeps the window open on Windows, where the owner starts
// spikehub.exe with a double click and the window closes with the process,
// before the message can be read.
func waitForEnter() {
	if runtime.GOOS != "windows" || !term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // a file descriptor fits in an int
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "Apăsați Enter pentru a închide.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// missingMediaError explains in Romanian that the media folder cannot be read,
// and what to do about it on the OS goos. The Windows owner has
// spikehub-windows.zip, whose media folder is next to spikehub.exe once the
// archive is extracted (hub-7); the Linux hubs have the spike-video-media
// archive, or cmd/mediagen.
func missingMediaError(goos, dir string, err error) error {
	remedy := "rulați cmd/mediagen sau despachetați arhiva spike-video-media; folderul media stă lângă spikehub"
	if goos == "windows" {
		remedy = "extrageți toată arhiva spikehub-windows.zip, cu clic dreapta → «Extragere totală», " +
			"și porniți spikehub.exe din folderul extras, unde stă și folderul media"
	}
	return fmt.Errorf("nu pot citi fișierele media din %q: %w (%s)", dir, err, remedy)
}

// run starts the hub and serves until ctx is done, or with -real probes the
// real cameras and returns.
func run(ctx context.Context, args []string, e env) error {
	e = e.withDefaults()
	cfg, err := parseFlags(args, e.stderr)
	if err != nil {
		return err
	}
	// source and session log through the default logger.
	slog.SetDefault(slog.New(slog.NewTextHandler(e.stderr, nil)))
	log := slog.Default()
	if cfg.real {
		return runRealProbe(ctx, e)
	}

	m, err := loadMedia(cfg.media)
	if err != nil {
		return missingMediaError(runtime.GOOS, cfg.media, err)
	}
	api, mdnsUnavailable, err := e.newAPI(cfg.mdns)
	if err != nil {
		return fmt.Errorf("pornirea WebRTC: %w", err)
	}
	cat, err := newCatalog(m, cfg.sims, e.clock)
	if err != nil {
		return err
	}

	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	h := newHub(hubConfig{
		cat:             cat,
		api:             api,
		mdnsUnavailable: mdnsUnavailable,
		hlsDir:          filepath.Join(cfg.media, "hls"),
		resultsDir:      cfg.results,
		web:             web,
		now:             time.Now,
		log:             log,
		console:         e.stdout,
	})
	defer h.Close()

	ln, err := e.listen(cfg.port)
	if err != nil {
		return fmt.Errorf("nu pot folosi portul %d (îl folosește alt program?): %w", cfg.port, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// The PC stays awake from here until run returns (R49).
	var keepsAwake bool
	var awakeErr error
	if e.keepAwake != nil {
		var release func()
		release, awakeErr = e.keepAwake()
		if awakeErr != nil {
			log.Warn("cannot keep the PC from sleeping", "err", awakeErr)
		} else {
			keepsAwake = true
			defer release()
		}
	}
	lan, lanErr := e.lan()
	writeBanner(e.stdout, bannerInfo{
		port: port, lan: lan, lanErr: lanErr, sims: cfg.sims, resultsDir: cfg.results, mdnsUnavailable: mdnsUnavailable,
		keepsAwake: keepsAwake, awakeErr: awakeErr,
	})
	log.Info("spikehub listens", "port", port, "sims", cfg.sims, "mdnsUnavailable", mdnsUnavailable, "keepsAwake", keepsAwake)

	srv := &http.Server{
		Handler:           h.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		_ = srv.Close()
	}
	_, _ = fmt.Fprintln(e.stdout, "spikehub s-a oprit.")
	return nil
}
