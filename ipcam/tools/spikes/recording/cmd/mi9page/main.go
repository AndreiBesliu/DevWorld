// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command mi9page is MI9's Chrome leg in the recorder spike's mi9 job (G26
// leg 2), a CI tool that never ships. `record` records the job's segment:
// main.h264 served as cam01_main on 127.0.0.1, recorded encrypted through
// ipcam-hub (hubrec.Record) into one closed segment, whose path and RI_g
// identity file it prints as one JSON line. `serve` serves the page that
// plays that segment in Chrome (web/index.html) and the hub side that the
// page connects to (mi9.Hub, its plaintext over bulk/<id> channels), on
// 127.0.0.1 only.
//
//	mi9page record -media DIR -hub IPCAM-HUB [-mediamtx MEDIAMTX] -out DIR [-secs 300]
//	mi9page serve -segment FILE -identity FILE [-listen 127.0.0.1:7090]
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

//go:embed web/index.html
var page []byte

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "mi9page:", err)
		os.Exit(1)
	}
}

const usage = `usage:
  mi9page record -media DIR -hub IPCAM-HUB [-mediamtx MEDIAMTX] -out DIR [-secs 300]
  mi9page serve -segment FILE -identity FILE [-listen 127.0.0.1:7090]`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "record":
		return record(ctx, args[1:], stdout, stderr)
	case "serve":
		return serve(ctx, args[1:], stdout, stderr)
	}
	return errors.New(usage)
}

// record records the segment of the job from main.h264 (cam01_main) and
// prints {"segment":…,"identity":…}.
func record(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(stderr)
	media := flags.String("media", "", "the media folder (main.h264)")
	hub := flags.String("hub", "", "ipcam-hub")
	mediamtx := flags.String("mediamtx", "", "the MediaMTX fork, if not next to ipcam-hub")
	out := flags.String("out", "", "the recording's folder")
	secs := flags.Int("secs", mi9.SegmentSecs, "the segment's length in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *media == "" || *hub == "" || *out == "" || flags.NArg() != 0 {
		return errors.New(usage)
	}
	var streams []controller.Stream
	for _, s := range controller.Streams(*media) {
		if s.Path == hubrec.Camera+"_"+hubrec.Profile {
			streams = append(streams, s)
		}
	}
	creds, err := controller.NewCredentials()
	if err != nil {
		return err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	srv, err := controller.ServePrecodedOn(ctx, ln, streams, creds, controller.NewSendLog(), source.SystemClock{})
	if err != nil {
		return err
	}
	defer srv.Close()
	camera := url.URL{Scheme: "rtsp", User: url.UserPassword(creds.User, creds.Pass), Host: ln.Addr().String(), Path: "/" + streams[0].Path}
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", args...) }
	rec, err := hubrec.Record(ctx, hubrec.Options{Hub: *hub, MediaMTX: *mediamtx, CameraURL: camera.String(),
		Run: filepath.Dir(*out), Name: filepath.Base(*out), SegmentSecs: *secs, Say: say})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]string{"segment": rec.Path, "identity": rec.Identity})
}

// serve serves the page and its hub side until ctx ends.
func serve(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	segment := flags.String("segment", "", "the encrypted segment, a .mp4.age")
	identity := flags.String("identity", "", "the file of its RI_g identity")
	listen := flags.String("listen", "127.0.0.1:7090", "where the page is served")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *segment == "" || *identity == "" || flags.NArg() != 0 {
		return errors.New(usage)
	}
	id, err := hubrec.ReadIdentity(*identity)
	if err != nil {
		return err
	}
	seg, err := mi9.OpenSegment(*segment, id)
	if err != nil {
		return err
	}
	defer func() { _ = seg.Close() }()
	api, err := mi9.NewAPI(mi9.Chrome)
	if err != nil {
		return err
	}
	h := &hubs{}
	defer h.closeAll()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("POST /offer", func(w http.ResponseWriter, r *http.Request) {
		offer, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answer, err := h.answer(r.Context(), api, seg, string(offer))
		if err != nil {
			slog.Error("mi9page: answer", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = io.WriteString(w, answer)
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", *listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	_, _ = fmt.Fprintf(stdout, "mi9page: http://%s/\n", ln.Addr())
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// hubs holds the hub side of the page's session: a page loaded again
// replaces the one before.
type hubs struct {
	mu  sync.Mutex
	cur *mi9.Hub
}

func (h *hubs) answer(ctx context.Context, api *webrtc.API, seg *mi9.Segment, offer string) (string, error) {
	hub, err := mi9.NewHub(api, mi9.Configuration(mi9.Chrome, nil), seg)
	if err != nil {
		return "", err
	}
	answer, err := hub.Answer(ctx, offer)
	if err != nil {
		return "", errors.Join(err, hub.Close())
	}
	h.mu.Lock()
	old := h.cur
	h.cur = hub
	h.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return answer, nil
}

func (h *hubs) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur != nil {
		_ = h.cur.Close()
	}
}
