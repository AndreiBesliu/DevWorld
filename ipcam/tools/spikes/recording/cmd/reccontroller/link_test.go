package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// mi9Envelope is an MI9 LAN envelope as a host's recspike sends it.
func mi9Envelope(t *testing.T, runID string, seekMs float64) []byte {
	t.Helper()
	e := results.Envelope{Schema: results.Schema, Code: "MI9", RunID: runID, Set: "lan", Host: "pc", OS: "windows", Storage: "ssd",
		MI9: &results.Mi9Data{Legs: []results.Mi9Leg{{Transport: "lan", ThroughputMbps: 90, SeekMs: []float64{seekMs}, Source: "lan"}}}}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestLink_MessagesBeforeHelloAreRefused (security-2): the link takes any
// LAN peer, so nothing is served before a valid hello: a peer that asks
// for the results folder, announces a run or sends a file first has its
// link closed, and none of it is done.
func TestLink_MessagesBeforeHelloAreRefused(t *testing.T) {
	for _, first := range []link.Message{
		link.PullResults{},
		link.RunStart{RunID: "pc-kill9-01-1", Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", Attempt: 1},
		link.Results{Name: "MI9/lan/pc-mi9-1.json", Data: mi9Envelope(t, "pc-mi9-1", 300)},
		link.Hello{Role: "host", Host: "pc", Version: "c0"},         // another protocol
		link.Hello{Role: "host", Host: "laptop-ana", Version: "c1"}, // no device of the probe
	} {
		r := newRig(t)
		writeEnvelope(t, r.results, "MI1", "pc-kill9", "pc", nil)
		a, b := net.Pipe()
		go r.c.serve(t.Context(), link.NewConn(a), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: link.Port}, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000})
		peer := link.NewConn(b)
		r.send(peer, first)
		answer := make(chan error, 1)
		go func() {
			m, err := peer.Receive()
			if err == nil {
				err = fmt.Errorf("the controller answered %s", m.Type())
			} else {
				err = nil
			}
			answer <- err
		}()
		select {
		case err := <-answer:
			if err != nil {
				t.Errorf("%s first: %v, want the link closed", first.Type(), err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s first: the link is still open 5 s later, want it closed", first.Type())
		}
		_ = peer.Close()
		r.c.mu.Lock()
		runs := len(r.c.runs)
		r.c.mu.Unlock()
		if _, err := os.Stat(filepath.Join(r.results, "MI9")); runs != 0 || !os.IsNotExist(err) {
			t.Errorf("%s first: %d runs, MI9 folder %v; want nothing done", first.Type(), runs, err)
		}
	}
}

// TestResults_OnlyMI9EnvelopesNeverReplaced (security-2): a host sends the
// controller one kind of file, MI9's LAN envelope, at most 64 KiB, named
// MI9/lan/<its run id>.json. The controller keeps exactly that, and never
// replaces an envelope that it holds: an envelope sent again under a taken
// name, a counted MI1 envelope, oversize data, a name that is not the
// envelope's and data that is no envelope are all refused.
func TestResults_OnlyMI9EnvelopesNeverReplaced(t *testing.T) {
	r := newRig(t)
	writeEnvelope(t, r.results, "MI1", "pc-kill9", "pc", nil)
	counted, err := os.ReadFile(filepath.Join(r.results, "MI1", "pc-kill9", "pc-kill9-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	conn := r.connect("pc")
	accepted := mi9Envelope(t, "pc-mi9-1", 300)
	r.send(conn, link.Results{Name: "MI9/lan/pc-mi9-1.json", Data: accepted})
	waitFor(t, "the MI9 envelope", func() bool { return strings.Contains(r.screen.String(), "Am primit MI9/lan/pc-mi9-1.json.") })

	mi1Env, err := json.Marshal(results.Envelope{Schema: results.Schema, Code: "MI1", RunID: "pc-kill9-1", Set: "pc-kill9", Host: "pc", OS: "windows",
		Runs: []results.Run{{RunID: "pc-kill9-1", Valid: false, InvalidReason: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []link.Results{
		{Name: "MI9/lan/pc-mi9-1.json", Data: mi9Envelope(t, "pc-mi9-1", 9000)},                                              // the same name again
		{Name: "MI1/pc-kill9/pc-kill9-1.json", Data: mi1Env},                                                                 // a counted envelope
		{Name: "MI9/lan/pc-mi9-2.json", Data: append(mi9Envelope(t, "pc-mi9-2", 300), bytes.Repeat([]byte(" "), 64<<10)...)}, // over 64 KiB
		{Name: "MI9/lan/pc-mi9-3.json", Data: mi9Envelope(t, "pc-mi9-4", 300)},                                               // not the envelope's name
		{Name: "MI9/lan/pc-mi9-5.json", Data: []byte(`{"not":"an envelope"}`)},                                               // no envelope
	} {
		before := strings.Count(r.screen.String(), "Am refuzat")
		r.send(conn, m)
		waitFor(t, "the refusal of "+m.Name, func() bool { return strings.Count(r.screen.String(), "Am refuzat") > before })
	}
	if got, err := os.ReadFile(filepath.Join(r.results, "MI9", "lan", "pc-mi9-1.json")); err != nil {
		t.Errorf("the MI9 envelope: %v", err)
	} else if e, err := results.Parse(got); err != nil || e.MI9.Legs[0].SeekMs[0] != 300 {
		t.Errorf("the MI9 envelope holds %s (%v), want the first one", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(r.results, "MI1", "pc-kill9", "pc-kill9-1.json")); err != nil || !bytes.Equal(got, counted) {
		t.Errorf("the counted MI1 envelope changed: %s (%v)", got, err)
	}
	for _, name := range []string{"pc-mi9-2.json", "pc-mi9-3.json", "pc-mi9-4.json", "pc-mi9-5.json"} {
		if _, err := os.Stat(filepath.Join(r.results, "MI9", "lan", name)); !os.IsNotExist(err) {
			t.Errorf("MI9/lan/%s was kept (%v)", name, err)
		}
	}
}

// TestResults_TwoSendsOfOneNameKeepOne (Final fix A review, Minor 6): two
// connections that send an envelope of the same name at the same moment
// never both see it kept: one is kept, the other refused, and the file is
// the kept one's, never replaced by the other.
func TestResults_TwoSendsOfOneNameKeepOne(t *testing.T) {
	r := newRig(t)
	a, b := r.connect("pc"), r.connect("pc")
	for i := range 20 {
		name := fmt.Sprintf("MI9/lan/pc-mi9-%d.json", i)
		runID := fmt.Sprintf("pc-mi9-%d", i)
		before := strings.Count(r.screen.String(), "Am primit") + strings.Count(r.screen.String(), "Am refuzat")
		var wg sync.WaitGroup
		for k, conn := range []*link.Conn{a, b} {
			wg.Go(func() {
				if err := conn.Send(link.Results{Name: name, Data: mi9Envelope(t, runID, float64(100+k))}); err != nil {
					t.Errorf("send: %v", err)
				}
			})
		}
		wg.Wait()
		waitFor(t, "both answers to "+name, func() bool {
			return strings.Count(r.screen.String(), "Am primit")+strings.Count(r.screen.String(), "Am refuzat") >= before+2
		})
		if kept := strings.Count(r.screen.String(), "Am primit "+name+"."); kept != 1 {
			t.Fatalf("%s was kept %d times, want once; screen:\n%s", name, kept, r.screen.String())
		}
	}
}

// TestPull_ASilentConnectionBlocksNothing (security-2): `reccontroller
// pull` takes connections side by side, so a LAN peer that connects and
// sends nothing neither holds the Pi's push nor the end: once the Pi's
// folder is here, pull ends at once and closes the silent connection,
// long before its read deadline.
func TestPull_ASilentConnectionBlocksNothing(t *testing.T) {
	old := pullReadTimeout
	pullReadTimeout = time.Hour
	t.Cleanup(func() { pullReadTimeout = old })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pcResults := filepath.Join(t.TempDir(), "results")
	piResults := t.TempDir()
	writeEnvelope(t, piResults, "MI2", "mi2", "pi", nil)
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- pull(t.Context(), ln, pcResults, &out) }()

	silent := dialSilent(t, ln.Addr().String())
	time.Sleep(50 * time.Millisecond) // the silent one is first

	var piOut syncBuffer
	if err := mi1.Push(t.Context(), ln.Addr().String(), piResults, "pi", &piOut); err != nil {
		t.Fatalf("push beside a silent connection: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pull: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("pull did not end within 10 s of the Pi's push; screen:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(pcResults, "pi", "MI2", "mi2", "mi2-1.json")); err != nil {
		t.Errorf("the Pi's results: %v", err)
	}
	select {
	case <-silent:
	case <-time.After(10 * time.Second):
		t.Error("the silent connection is still open after pull ended")
	}
}

// TestPull_ASilentConnectionTimesOut (security-2): each connection of
// `reccontroller pull` has a read deadline, so a peer that sends nothing
// is closed while pull goes on waiting for the Pi.
func TestPull_ASilentConnectionTimesOut(t *testing.T) {
	old := pullReadTimeout
	pullReadTimeout = 300 * time.Millisecond
	t.Cleanup(func() { pullReadTimeout = old })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- pull(ctx, ln, filepath.Join(t.TempDir(), "results"), &out) }()

	select {
	case <-dialSilent(t, ln.Addr().String()):
	case <-time.After(10 * time.Second):
		t.Fatal("the silent connection is still open 10 s after its 300 ms deadline")
	}
	select {
	case err := <-done:
		t.Fatalf("pull ended with the silent connection (%v), want it waiting for the Pi", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("pull after Ctrl+C: %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pull did not end after Ctrl+C")
	}
}

// TestPull_AStalledPiDelaysNothing (Final fix A review, Minor 7): a peer
// whose hello says it is the Pi, and that then sends nothing, does not
// hold the real Pi's push for its read deadline: pull reads each folder
// apart, and only stores one at a time.
func TestPull_AStalledPiDelaysNothing(t *testing.T) {
	old := pullReadTimeout
	pullReadTimeout = time.Hour
	t.Cleanup(func() { pullReadTimeout = old })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pcResults := filepath.Join(t.TempDir(), "results")
	piResults := t.TempDir()
	writeEnvelope(t, piResults, "MI2", "mi2", "pi", nil)
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- pull(t.Context(), ln, pcResults, &out) }()

	stalled, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stalled.Close() })
	fake := link.NewConn(stalled)
	if err := fake.Send(link.Hello{Role: "host", Host: "pi", Version: link.Version}); err != nil {
		t.Fatal(err)
	}
	if m, err := fake.Receive(); err != nil || m.Type() != "pull_results" {
		t.Fatalf("the stalled Pi got %v, %v; want pull_results", m, err)
	}

	pushed := make(chan error, 1)
	go func() {
		var piOut syncBuffer
		pushed <- mi1.Push(t.Context(), ln.Addr().String(), piResults, "pi", &piOut)
	}()
	select {
	case err := <-pushed:
		if err != nil {
			t.Fatalf("the Pi's push beside a stalled one: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the Pi's push waited behind a stalled Pi; screen:\n%s", out.String())
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pull: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pull did not end after the Pi's push")
	}
	if _, err := os.Stat(filepath.Join(pcResults, "pi", "MI2", "mi2", "mi2-1.json")); err != nil {
		t.Errorf("the Pi's results: %v", err)
	}
}

// TestPull_OnlyThePiHearsThatItsTransferFailed (Final fix A review, Minor
// 7): a connection that is not the Pi's, a port scan, a silent peer or
// another host, prints nothing on pull's screen; a Pi whose transfer fails
// after its hello is told to push again.
func TestPull_OnlyThePiHearsThatItsTransferFailed(t *testing.T) {
	old := pullReadTimeout
	pullReadTimeout = 200 * time.Millisecond
	t.Cleanup(func() { pullReadTimeout = old })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- pull(ctx, ln, filepath.Join(t.TempDir(), "results"), &out) }()
	const failed = "Transferul nu a reușit"

	scan, err := net.Dial("tcp4", ln.Addr().String()) // a port scan
	if err != nil {
		t.Fatal(err)
	}
	_ = scan.Close()
	<-dialSilent(t, ln.Addr().String()) // closed at its deadline
	pc, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := link.NewConn(pc).Send(link.Hello{Role: "host", Host: "pc", Version: link.Version}); err != nil {
		t.Fatal(err)
	}
	_, _ = pc.Read(make([]byte, 1)) // closed by pull
	_ = pc.Close()
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(out.String(), failed) {
		t.Errorf("pull printed a failed transfer for peers that are not the Pi:\n%s", out.String())
	}

	pi, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	piConn := link.NewConn(pi)
	if err := piConn.Send(link.Hello{Role: "host", Host: "pi", Version: link.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := piConn.Receive(); err != nil {
		t.Fatal(err)
	}
	_ = pi.Close() // the Pi's link drops before its folder
	waitFor(t, "the failed transfer's line", func() bool { return strings.Contains(out.String(), failed) })
	if n := strings.Count(out.String(), failed); n != 1 {
		t.Errorf("pull printed %d failed transfers, want the Pi's one:\n%s", n, out.String())
	}
	cancel()
	<-done
}

// dialSilent connects to addr and sends nothing; the channel is closed
// when the far side closes the connection.
func dialSilent(t *testing.T, addr string) <-chan struct{} {
	t.Helper()
	c, err := net.Dial("tcp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_, _ = c.Read(make([]byte, 1))
	}()
	return closed
}
