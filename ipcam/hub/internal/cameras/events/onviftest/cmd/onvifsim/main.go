// Command onvifsim serves simulated ONVIF cameras (onviftest) over plain
// HTTP on 127.0.0.1, for the tests of another process: uploadspike's MI8
// tests (Task 10) run the real ipcam-hub diag onvif against it across the
// process boundary (P01), as they run fakeproviders for the storage
// providers. It is test infrastructure that no release ships, so it lives
// outside hub/cmd/ (§3.5).
//
//	onvifsim -user <u> -pass <p> -cam <brand>/<model> [-cam …] [-stream <rtsp URL>] [-snapshot <URL>]
//
// At start it prints one line, the cameras' device service addresses:
//
//	{"type":"ready","cameras":[{"xaddr":"http://127.0.0.1:…/onvif/device_service","hostPort":"127.0.0.1:…"}]}
//
// then reads commands on stdin, one per line, each answered by one JSON line:
//
//	emit <i> <0|1>    a motion notification (IsMotion) on camera i's subscriptions
//	silent <i> <0|1>  camera i stops answering, or answers again
//	expire <i>        camera i drops every subscription
//	counts            {"type":"counts","cameras":[{"subscriptions","creates","renews","pulls","unsubscribes","expired"}]}
//
// It exits 0 at the end of its stdin, or on SIGTERM or an interrupt.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events/onviftest"
)

// motionTopic is the topic of the motion notifications that emit queues.
const motionTopic = "tns1:RuleEngine/CellMotionDetector/Motion"

// camFlags collects the repeated -cam flags.
type camFlags []string

func (c *camFlags) String() string     { return strings.Join(*c, ",") }
func (c *camFlags) Set(v string) error { *c = append(*c, v); return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run is onvifsim's main; it returns the exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("onvifsim", flag.ContinueOnError)
	flags.SetOutput(stderr)
	user := flags.String("user", "", "the cameras' ONVIF user")
	pass := flags.String("pass", "", "the cameras' ONVIF password")
	stream := flags.String("stream", "", "the stream URI of every profile")
	snapshot := flags.String("snapshot", "", "the snapshot URI")
	var cams camFlags
	flags.Var(&cams, "cam", "a camera, <brand>/<model>; repeat for each")
	if err := flags.Parse(args); err != nil || len(cams) == 0 || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: onvifsim -user <u> -pass <p> -cam <brand>/<model> [-cam …] [-stream <URI>] [-snapshot <URI>]")
		return 2
	}
	var sims []*onviftest.Sim
	defer func() {
		for _, s := range sims {
			s.Close()
		}
	}()
	type ready struct {
		XAddr    string `json:"xaddr"`
		HostPort string `json:"hostPort"`
	}
	var out []ready
	for _, c := range cams {
		brand, model, _ := strings.Cut(c, "/")
		s := onviftest.New(onviftest.Config{User: *user, Pass: *pass, Brand: brand, Model: model, Serial: "SIM-SERIAL-" + strconv.Itoa(len(sims)+1),
			StreamURIs: map[string]string{"main": *stream, "sub": *stream}, SnapshotURI: *snapshot})
		sims = append(sims, s)
		u, err := url.Parse(s.XAddr())
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "onvifsim:", err)
			return 1
		}
		out = append(out, ready{XAddr: s.XAddr(), HostPort: u.Host})
	}
	var mu sync.Mutex
	say := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := json.Marshal(v) //nolint:errchkjson // plain values
		_, _ = stdout.Write(append(b, '\n'))
	}
	say(map[string]any{"type": "ready", "cameras": out})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdin)
		for sc.Scan() {
			if err := command(sims, sc.Text(), say); err != nil {
				say(map[string]string{"type": "error", "error": err.Error()})
			}
		}
	}()
	select {
	case <-done:
	case <-sig:
	}
	return 0
}

// command runs one line of stdin.
func command(sims []*onviftest.Sim, line string, say func(any)) error {
	f := strings.Fields(line)
	if len(f) == 0 {
		return nil
	}
	if f[0] == "counts" {
		type counts struct {
			Subscriptions int `json:"subscriptions"`
			Creates       int `json:"creates"`
			Renews        int `json:"renews"`
			Pulls         int `json:"pulls"`
			Unsubscribes  int `json:"unsubscribes"`
			Expired       int `json:"expired"`
		}
		var cs []counts
		for _, s := range sims {
			c := s.Counts()
			cs = append(cs, counts{Subscriptions: s.Subscriptions(), Creates: c.Creates, Renews: c.Renews, Pulls: c.Pulls,
				Unsubscribes: c.Unsubscribes, Expired: c.Expired})
		}
		say(map[string]any{"type": "counts", "cameras": cs})
		return nil
	}
	if len(f) < 2 {
		return errors.New("a command needs a camera index")
	}
	i, err := strconv.Atoi(f[1])
	if err != nil || i < 0 || i >= len(sims) {
		return errors.New("no such camera")
	}
	s := sims[i]
	switch {
	case f[0] == "emit" && len(f) == 3:
		s.Emit(motionTopic, f[2] == "1")
	case f[0] == "silent" && len(f) == 3:
		s.SetSilent(f[2] == "1")
	case f[0] == "expire" && len(f) == 2:
		s.ExpireAll()
	default:
		return errors.New("unknown command")
	}
	say(map[string]string{"type": "ok"})
	return nil
}
