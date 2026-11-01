// Command fakeproviders serves the hub's fake storage providers over plain
// HTTP, for the upload spike's lab (P04, Task 8): fakegdrive under /gdrive/
// (the Drive API) and /gdrive-upload/ (its upload URL, a BaseURL apart as the
// live API's), and fakegraph under /graph/.
//
//	fakeproviders -listen <ip:port> [-throttle <bytes/s>]
//
// At start it prints one line, the URLs to give the drivers:
//
//	{"schema":"ipcam.fakeproviders/1","gdrive":"http://…/gdrive","gdriveUpload":"http://…/gdrive-upload","graph":"http://…/graph"}
//
// and then serves until SIGTERM or an interrupt, when it exits 0. With
// -throttle it reads each request's body at that many bytes a second, the
// uploads' chunks and fragments included, so that a chunk's upload_progress
// and the next commit are seconds apart, not milliseconds (the lab uses
// 1 MiB/s, Task 8's restart). It is test infrastructure that no release
// ships, so it lives outside hub/cmd/ (§3.5).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	fakegraph "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/graph"
)

const (
	// schema names the format of the line that fakeproviders prints.
	schema = "ipcam.fakeproviders/1"
	// throttleSteps is how many reads a throttled body takes a second, so
	// that its pace is even.
	throttleSteps = 16
	// readHeaderTimeout bounds a request's header.
	readHeaderTimeout = 10 * time.Second
)

// urls is the line that fakeproviders prints at start.
type urls struct {
	Schema       string `json:"schema"`
	GDrive       string `json:"gdrive"`
	GDriveUpload string `json:"gdriveUpload"`
	Graph        string `json:"graph"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is fakeproviders with args: 0 once a signal ends it, 1 when it cannot
// serve, 2 for a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fakeproviders", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "", "the address to serve on, `ip:port`")
	throttle := flags.Int64("throttle", 0, "read each request's body at this many `bytes/s`; 0: no limit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *listen == "" || *throttle < 0 || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: fakeproviders -listen <ip:port> [-throttle <bytes/s>]")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fakeproviders: %v\n", err)
		return 1
	}
	base := "http://" + ln.Addr().String()
	line, err := json.Marshal(urls{Schema: schema, GDrive: base + "/gdrive", GDriveUpload: base + "/gdrive-upload", Graph: base + "/graph"})
	if err == nil {
		_, err = fmt.Fprintf(stdout, "%s\n", line)
	}
	if err != nil {
		_ = ln.Close()
		_, _ = fmt.Fprintf(stderr, "fakeproviders: %v\n", err)
		return 1
	}
	srv := &http.Server{Handler: handler(*throttle), ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		_ = srv.Close()
		return 0
	case err := <-served:
		_, _ = fmt.Fprintf(stderr, "fakeproviders: %v\n", err)
		return 1
	}
}

// handler serves a fakegdrive under /gdrive/ and /gdrive-upload/, and a
// fakegraph under /graph/, each request's body read at throttle bytes a
// second when throttle is positive.
func handler(throttle int64) http.Handler {
	drive := fakegdrive.New(fakegdrive.Options{}).Handler()
	mux := http.NewServeMux()
	mux.Handle("/gdrive/", http.StripPrefix("/gdrive", drive))
	mux.Handle("/gdrive-upload/", http.StripPrefix("/gdrive-upload", drive))
	mux.Handle("/graph/", http.StripPrefix("/graph", fakegraph.New(fakegraph.Options{}).Handler()))
	if throttle == 0 {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = &slowBody{ReadCloser: r.Body, ctx: r.Context(), rate: throttle}
		}
		mux.ServeHTTP(w, r)
	})
}

// slowBody is a request's body read at rate bytes a second from its first
// read: each read takes at most a throttleSteps-th of a second's bytes, and
// returns once the bytes read so far are due.
type slowBody struct {
	io.ReadCloser
	ctx   context.Context
	rate  int64
	start time.Time
	read  int64
}

func (b *slowBody) Read(p []byte) (int, error) {
	if b.start.IsZero() {
		b.start = time.Now()
	}
	if step := max(b.rate/throttleSteps, 1); int64(len(p)) > step {
		p = p[:step]
	}
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	due := b.start.Add(time.Duration(float64(b.read) / float64(b.rate) * float64(time.Second)))
	if wait := time.Until(due); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-b.ctx.Done():
			return n, b.ctx.Err()
		}
	}
	return n, err
}
