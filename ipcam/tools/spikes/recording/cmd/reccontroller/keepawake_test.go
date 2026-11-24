package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
)

// awakeLine is what the controller says while it keeps the PC awake.
const awakeLine = "Cât timp această fereastră este deschisă, PC-ul nu intră în repaus."

// fakeAwake stands in for Windows' keep-awake request: it counts the
// requests that are held.
type fakeAwake struct {
	mu                 sync.Mutex
	requested, holding int
}

func (f *fakeAwake) keep() (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested++
	f.holding++
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.holding--
		})
	}, nil
}

func (f *fakeAwake) counts() (requested, holding int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requested, f.holding
}

// useKeepAwake makes keep the controller's keep-awake request for the test.
func useKeepAwake(t *testing.T, keep func() (func(), error)) {
	t.Helper()
	old := keepAwake
	keepAwake = keep
	t.Cleanup(func() { keepAwake = old })
}

// TestPull_KeepsThePCAwake (report-2): `reccontroller pull` on the PC
// waits for the Pi's push for as long as the owner needs; Windows must not
// sleep meanwhile. The request is held from the start of the wait to its
// end, and the owner reads that the PC stays awake.
func TestPull_KeepsThePCAwake(t *testing.T) {
	awake := &fakeAwake{}
	useKeepAwake(t, awake.keep)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var screen syncBuffer
	done := make(chan error, 1)
	go func() { done <- pull(ctx, ln, filepath.Join(t.TempDir(), "results"), &screen) }()
	waitFor(t, "the keep-awake line", func() bool { return strings.Contains(screen.String(), awakeLine) })
	if requested, holding := awake.counts(); requested != 1 || holding != 1 {
		t.Errorf("while pull waits: %d requests, %d held; want one, held", requested, holding)
	}
	cancel()
	<-done
	if _, holding := awake.counts(); holding != 0 {
		t.Errorf("after pull: %d requests still held, want none", holding)
	}
}

// TestServe_KeepsThePCAwake (report-2): a PC that is the Pi's controller is
// its camera source and its beacon's receiver, all night, with nobody at
// it; Windows sleeps on keyboard and mouse idleness whatever the network
// does. serve holds the keep-awake request until it stops and says so;
// outside Windows (no request) it says nothing of sleep, and a request
// that Windows refuses is named, with the setting to change instead.
func TestServe_KeepsThePCAwake(t *testing.T) {
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	defaultLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLog) })
	serveWith := func(t *testing.T, keep func() (func(), error)) *syncBuffer {
		t.Helper()
		useKeepAwake(t, keep)
		ctx, cancel := context.WithCancel(t.Context())
		lns, err := controller.ListenAll(ctx, controller.ListenConfig{Host: "127.0.0.1"})
		if err != nil {
			t.Fatal(err)
		}
		screen := &syncBuffer{}
		served := make(chan error, 1)
		go func() { served <- serveOn(ctx, serveConfig{dir: t.TempDir(), media: media, local: true}, lns, screen) }()
		t.Cleanup(func() {
			cancel()
			if err := <-served; err != nil {
				t.Errorf("serveOn: %v", err)
			}
			_ = lns.Close()
		})
		waitFor(t, "the controller's start", func() bool { return strings.Contains(screen.String(), "Rezultatele se păstrează") })
		return screen
	}

	awake := &fakeAwake{}
	t.Run("on Windows", func(t *testing.T) {
		screen := serveWith(t, awake.keep)
		if requested, holding := awake.counts(); requested != 1 || holding != 1 || !strings.Contains(screen.String(), awakeLine) {
			t.Errorf("while serving: %d requests, %d held, screen:\n%s\nwant one held and «%s»", requested, holding, screen.String(), awakeLine)
		}
	})
	if _, holding := awake.counts(); holding != 0 { // the subtest's cleanup stopped serve
		t.Errorf("after serve: %d requests still held, want none", holding)
	}
	t.Run("elsewhere", func(t *testing.T) {
		if screen := serveWith(t, nil); strings.Contains(screen.String(), "repaus") {
			t.Errorf("without a keep-awake request the screen says:\n%s", screen.String())
		}
	})
	t.Run("refused", func(t *testing.T) {
		screen := serveWith(t, func() (func(), error) { return nil, errors.New("SetThreadExecutionState: refused") })
		if s := screen.String(); strings.Contains(s, awakeLine) || !strings.Contains(s, "nu poate împiedica intrarea PC-ului în repaus") {
			t.Errorf("a refused request: the screen says:\n%s\nwant the warning, not «%s»", s, awakeLine)
		}
	})
}
