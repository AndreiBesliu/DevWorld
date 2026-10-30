package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

// With fakeMediaMTXEnv set, the test binary started on a MediaMTX
// configuration (<run dir>/mediamtx.yml) plays MediaMTX (fakeMediaMTX):
// ipcam-hub, which the variable reaches too, starts it as its child. The
// fake writes what it did to the file that fakeOutEnv names, and waits for
// the test's signals beside it (fakeSignal).
const (
	fakeMediaMTXEnv = "IPCAM_HUB_TEST_FAKE_MEDIAMTX"
	fakeOutEnv      = "IPCAM_HUB_TEST_FAKE_OUT"
)

// isFakeMediaMTX reports whether args are MediaMTX's, as the recorder starts
// it: one argument, its configuration.
func isFakeMediaMTX(args []string) bool {
	return os.Getenv(fakeMediaMTXEnv) != "" && len(args) == 1 && filepath.Base(args[0]) == "mediamtx.yml"
}

// fakeOut is what the fake MediaMTX of mode "writeerror" did: the starts of
// the two segments that it wrote, in Unix ms, as their names say.
type fakeOut struct {
	FirstStartMs, NextStartMs int64
}

// fakeMediaMTX plays MediaMTX on the configuration at config, in one of
// these modes, once the configuration names a path:
//   - "sleep": it records nothing;
//   - "eio:<n>": it writes n failed writes of the first path to stderr, as
//     MediaMTX logs a recorder's error, an I/O error each;
//   - "writeerror": for the first path, it writes a .part of two whole 1 s
//     parts and a torn third, logs its failed write, then writes the next
//     segment's .part, as MediaMTX does when a write fails and it starts the
//     recorder again 2 s later; each step waits for the test's signal.
//
// Then it waits until it is stopped.
func fakeMediaMTX(mode, config string) int {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	path, recDir, err := firstPath(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake MediaMTX:", err)
		return 2
	}
	// On the child's stderr, which the recorder reads with its stdout, where
	// MediaMTX logs.
	failedWrite := func(file string) {
		fmt.Fprintf(os.Stderr, "%s ERR [path %s] [recorder] write %s: input/output error\n", time.Now().Format("2006/01/02 15:04:05"), path, file)
	}
	switch {
	case mode == "sleep":
	case strings.HasPrefix(mode, "eio:"):
		var n int
		if _, err := fmt.Sscanf(mode, "eio:%d", &n); err != nil {
			return 2
		}
		for range n {
			failedWrite(filepath.Join(recDir, filepath.FromSlash(path), "x.mp4.part"))
			time.Sleep(50 * time.Millisecond)
		}
	case mode == "writeerror":
		if err := writeErrorSegments(path, recDir, failedWrite); err != nil {
			fmt.Fprintln(os.Stderr, "fake MediaMTX:", err)
			return 2
		}
	default:
		return 2
	}
	<-stop
	return 0
}

// firstPath waits until the configuration at config names a path and
// returns the first one and the recordings directory of its recordPath.
func firstPath(config string) (path, recDir string, err error) {
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		b, err := os.ReadFile(config) //nolint:gosec // the configuration that the recorder wrote
		if err != nil {
			continue
		}
		var c struct {
			Paths map[string]struct {
				RecordPath string `json:"recordPath"`
			} `json:"paths"`
		}
		if json.Unmarshal(b, &c) != nil || len(c.Paths) == 0 {
			continue
		}
		names := make([]string, 0, len(c.Paths))
		for name := range c.Paths {
			names = append(names, name)
		}
		slices.Sort(names)
		rec, _, ok := strings.Cut(c.Paths[names[0]].RecordPath, "/%path/")
		if !ok {
			return "", "", fmt.Errorf("recordPath %q", c.Paths[names[0]].RecordPath)
		}
		return names[0], filepath.FromSlash(rec), nil
	}
	return "", "", errors.New("no path in the configuration after 30 s")
}

// The signals of the test to the "writeerror" fake, each sent once
// ipcam-hub's events show that it took the fake's step before.
const (
	signalSeen   = "seen"   // a poll saw the first segment's .part: its recording event
	signalFailed = "failed" // the failed write reached the recorder: the card's degraded event
)

// fakeSignal is the file that sends signal to the fake MediaMTX whose out
// file is out.
func fakeSignal(out, signal string) string { return out + "." + signal }

// awaitSignal waits until the test sends signal.
func awaitSignal(signal string) error {
	path := fakeSignal(os.Getenv(fakeOutEnv), signal)
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: beside the file that the test named
			return nil
		}
	}
	return fmt.Errorf("no %s signal after 60 s", signal)
}

// writeErrorSegments writes the segments of mode "writeerror" for path,
// each step once the test has seen ipcam-hub take the one before, never
// after a fixed time.
func writeErrorSegments(path, recDir string, failedWrite func(file string)) error {
	init, parts, err := fakeRecording(3)
	if err != nil {
		return err
	}
	// The repair keeps the two whole parts: the third is cut inside its moof.
	torn := parts[2][:20]
	firstStart := time.Now().UTC().Truncate(time.Microsecond)
	first, err := fakeSegment(recDir, path, firstStart, init, parts[0])
	if err != nil {
		return err
	}
	// The next segment's .part leaves this one behind only for a hub that saw
	// this one first.
	if err := awaitSignal(signalSeen); err != nil {
		return err
	}
	if err := appendFile(first, slices.Concat(parts[1], torn)); err != nil {
		return err
	}
	failedWrite(first)
	// The hub notes the failed write before the next segment makes it repair
	// the file.
	if err := awaitSignal(signalFailed); err != nil {
		return err
	}
	// The next segment starts after MediaMTX's 2 s pause, as its name says:
	// after the failed file's two parts, however soon the signals came.
	nextStart := time.Now().Add(2 * time.Second).UTC().Truncate(time.Microsecond)
	// What the test reads goes out first: the next segment's .part makes the
	// hub repair the failed one.
	b, err := json.Marshal(fakeOut{FirstStartMs: firstStart.UnixMilli(), NextStartMs: nextStart.UnixMilli()})
	if err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv(fakeOutEnv), b, 0o600); err != nil { //nolint:gosec // the file that the test named
		return err
	}
	_, err = fakeSegment(recDir, path, nextStart, init, parts[0])
	return err
}

// fakeSegment creates the .part of a segment of path that starts at start,
// a UTC time in µs, with init and its first part, named as MediaMTX names
// it (MTX-4).
func fakeSegment(recDir, path string, start time.Time, init, part []byte) (string, error) {
	dir := filepath.Join(recDir, filepath.FromSlash(path), start.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	file := filepath.Join(dir, fmt.Sprintf("%s_%06d.mp4.part", start.Format("150405"), start.Nanosecond()/1000))
	return file, os.WriteFile(file, slices.Concat(init, part), 0o600)
}

func appendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // the fake's own segment
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

// fakeRecording is an fMP4 init and n parts of one 1 s sample each.
func fakeRecording(n int) (init []byte, parts [][]byte, err error) {
	var buf seekablebuffer.Buffer
	i := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{
		SPS: []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20},
		PPS: []byte{0x08, 0x06, 0x07, 0x08},
	}}}}
	if err := i.Marshal(&buf); err != nil {
		return nil, nil, err
	}
	init = bytes.Clone(buf.Bytes())
	for k := range n {
		var pb seekablebuffer.Buffer
		p := fmp4.Part{SequenceNumber: uint32(k + 1), Tracks: []*fmp4.PartTrack{{ID: 1, BaseTime: uint64(k) * 90000, //nolint:gosec // a few parts
			Samples: []*fmp4.Sample{{Duration: 90000, Payload: bytes.Repeat([]byte{1}, 5000)}}}}}
		if err := p.Marshal(&pb); err != nil {
			return nil, nil, err
		}
		parts = append(parts, bytes.Clone(pb.Bytes()))
	}
	return init, parts, nil
}

// sdCardHub starts ipcam-hub on jobs, recorded by the fake MediaMTX in mode,
// with its data directory on an SD card that the owner confirmed, by the
// mount table, whose data mount is read-only when readOnly. It returns the
// hub and its data directory.
func sdCardHub(t *testing.T, mode string, readOnly bool, jobs ...string) (*hub, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the card monitor runs where rec/ is on an SD card, from Linux's mount table (T40); Windows has none")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	line := mountInfoLine(22, "179:2", dataDir, "/dev/mmcblk0p2")
	if readOnly {
		line = strings.Replace(line, " - ext4 /dev/mmcblk0p2 rw", " - ext4 /dev/mmcblk0p2 ro,errors=remount-ro", 1)
	}
	mountinfo := filepath.Join(dir, "mountinfo")
	if err := os.WriteFile(mountinfo, []byte(mountInfoLine(21, "259:2", "/", "/dev/nvme0n1p2")+line), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mountInfoEnv, mountinfo)
	// The probe's fsync would make the card DEGRADED on a slow test disk: these
	// tests watch MediaMTX's log and the mount table.
	t.Setenv(probeEveryEnv, "1h")
	t.Setenv(fakeMediaMTXEnv, mode)
	t.Setenv(fakeOutEnv, filepath.Join(dir, "fake.json"))
	var defs []map[string]any
	for _, job := range jobs {
		cam, profile, _ := strings.Cut(job, "/")
		defs = append(defs, map[string]any{"cam": cam, "profile": profile, "restreamUrl": "rtsp://127.0.0.1:1/" + job, "recipients": nil})
	}
	config := filepath.Join(dir, "run.json")
	writeJSON(t, config, map[string]any{"dataDir": dataDir, "mediamtx": os.Args[0], "sdCardConfirmed": true, "jobs": defs})
	return startHub(t, "run", "--config", config), dataDir
}

// cardEvents collects ipcam-hub's events until done accepts them all.
type cardEvents struct {
	mu  sync.Mutex
	all []map[string]any
}

// until reads h's events until done holds for those read, failing after d.
func (c *cardEvents) until(t *testing.T, h *hub, d time.Duration, what string, done func([]map[string]any) bool) {
	t.Helper()
	timeout := time.After(d)
	for {
		c.mu.Lock()
		ok := done(c.all)
		c.mu.Unlock()
		if ok {
			return
		}
		select {
		case e, open := <-h.events:
			if !open {
				t.Fatalf("ipcam-hub ended before %s", what)
			}
			c.mu.Lock()
			c.all = append(c.all, e)
			c.mu.Unlock()
		case <-timeout:
			t.Fatalf("after %v: no %s; events %v", d, what, c.all)
		}
	}
}

// ofTypeIn returns the events of all of type typ whose field key is value.
func ofTypeIn(all []map[string]any, typ, key string, value any) []map[string]any {
	var out []map[string]any
	for _, e := range all {
		if e["type"] == typ && (key == "" || e[key] == value) {
			out = append(out, e)
		}
	}
	return out
}

// cardGaps are the gap events of reason card.
func cardGaps(all []map[string]any) []map[string]any { return ofTypeIn(all, "gap", "reason", "card") }

// TestSdCard_IoErrors_RaiseAlertThenStop (Review Focus 4, T40, HUB-17, G17):
// I/O errors that MediaMTX logs, while rec/ is on an SD card, move the card
// from OK to DEGRADED and then FAILED (3 in 60 s), and the alert follows
// within 60 s; at FAILED, ipcam-hub stops every job with a card gap
// (recorder.Stop): recording stops with a gap, never with a silent loss. A
// read-only remount of the card is FAILED at once, and stops the jobs the
// same way.
func TestSdCard_IoErrors_RaiseAlertThenStop(t *testing.T) {
	jobs := []string{"cam1/main", "cam2/main"}
	for _, tc := range []struct {
		name, mode string
		readOnly   bool
		states     []any
	}{
		{"three EIO lines of MediaMTX", "eio:3", false, []any{"ok", "degraded", "failed"}},
		{"a read-only remount", "sleep", true, []any{"failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := sdCardHub(t, tc.mode, tc.readOnly, jobs...)
			var c cardEvents
			c.until(t, h, 60*time.Second, "card gap of every job", func(all []map[string]any) bool {
				return len(cardGaps(all)) == len(jobs)
			})
			var states []any
			for _, e := range ofTypeIn(c.all, "card", "", nil) {
				states = append(states, e["state"])
			}
			if !slices.Equal(states, tc.states) {
				t.Errorf("card states %v, want %v", states, tc.states)
			}
			failed := ofTypeIn(c.all, "card", "state", "failed")
			alerts := ofTypeIn(c.all, "alert", "kind", "card")
			if len(failed) != 1 || len(alerts) != 1 {
				t.Fatalf("%d failed card events and %d card alerts, want one each", len(failed), len(alerts))
			}
			failedAt := failed[0]["atMs"].(float64)
			if at := alerts[0]["atMs"].(float64); at > failedAt+60_000 {
				t.Errorf("the alert came %v ms after FAILED, want ≤ 60 s", at-failedAt)
			}
			var stopped []string
			for _, g := range cardGaps(c.all) {
				stopped = append(stopped, g["cam"].(string)+"/"+g["profile"].(string))
				if g["endMs"] != float64(0) {
					t.Errorf("gap %v is closed, want open: the job stopped", g)
				}
				if at := g["atMs"].(float64); at > failedAt+60_000 {
					t.Errorf("%v came %v ms after FAILED, want ≤ 60 s", g, at-failedAt)
				}
			}
			slices.Sort(stopped)
			if !slices.Equal(stopped, jobs) {
				t.Errorf("card gaps for %v, want one for each of %v", stopped, jobs)
			}
		})
	}
}

// TestSdCard_DegradedKeepsRecording (T40, G17): below FAILED, recording
// continues: one write that MediaMTX could not make on the card is
// DEGRADED and raises the alert, stops no job, and writes one card gap
// row, from the end of the failed segment's last good part to the start
// of the next segment.
func TestSdCard_DegradedKeepsRecording(t *testing.T) {
	h, dataDir := sdCardHub(t, "writeerror", false, "cam1/main")
	out := filepath.Join(filepath.Dir(dataDir), "fake.json")
	signal := func(name string) {
		if err := os.WriteFile(fakeSignal(out, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var c cardEvents
	c.until(t, h, 60*time.Second, "recording event", func(all []map[string]any) bool { return len(ofTypeIn(all, "recording", "", nil)) > 0 })
	signal(signalSeen)
	c.until(t, h, 60*time.Second, "degraded card", func(all []map[string]any) bool { return len(ofTypeIn(all, "card", "state", "degraded")) > 0 })
	signal(signalFailed)
	c.until(t, h, 60*time.Second, "card gap", func(all []map[string]any) bool { return len(cardGaps(all)) > 0 })
	c.until(t, h, 10*time.Second, "running status after the gap", func(all []map[string]any) bool {
		return ofTypeIn(all[len(all)-1:], "status", "running", true) != nil
	})

	var states []any
	for _, e := range ofTypeIn(c.all, "card", "", nil) {
		states = append(states, e["state"])
	}
	if !slices.Equal(states, []any{"ok", "degraded"}) {
		t.Errorf("card states %v, want ok then degraded", states)
	}
	if n := len(ofTypeIn(c.all, "alert", "kind", "card")); n != 1 {
		t.Errorf("%d card alerts, want 1", n)
	}
	b, err := os.ReadFile(out) //nolint:gosec // the fake's own output
	if err != nil {
		t.Fatal(err)
	}
	var segments fakeOut
	if err := json.Unmarshal(b, &segments); err != nil {
		t.Fatal(err)
	}

	if err := h.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-h.done
	_ = h.cmd.Wait()
	cat, err := catalog.Open(t.Context(), filepath.Join(dataDir, "state", "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cat.Close() }()
	gaps, err := cat.Gaps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []catalog.Gap{{Cam: "cam1", Profile: "main", StartMs: segments.FirstStartMs + 2000, EndMs: segments.NextStartMs, Reason: catalog.GapCard}}
	if !slices.Equal(gaps, want) {
		t.Errorf("gaps %+v, want %+v", gaps, want)
	}
}

// TestCardSampler_ProbeTimesTheFsyncAndCountsItsErrors (T40, §13.1): every
// probe writes and fsyncs rec/.health/probe and times it; a slow one moves
// the card to DEGRADED, and EIO or EROFS from it count as I/O errors and a
// read-only card. Each change of state is a card event, and the alert goes
// out once.
func TestCardSampler_ProbeTimesTheFsyncAndCountsItsErrors(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	var out bytes.Buffer
	s := newCardSampler(recDir, "", &events{out: &out})
	s.probe()
	if _, err := os.Stat(filepath.Join(recDir, ".health", "probe")); err != nil {
		t.Fatalf("the probe wrote no rec/.health/probe: %v", err)
	}

	now := time.Now()
	s.now = func() time.Time { return now }
	var result struct {
		took time.Duration
		err  error
	}
	s.probeFile = func(string) (time.Duration, error) { return result.took, result.err }
	step := func(took time.Duration, err error) {
		now = now.Add(probeEvery)
		result.took, result.err = took, err
		s.probe()
	}
	step(20*time.Millisecond, nil)
	step(600*time.Millisecond, nil)
	step(0, &fs.PathError{Op: "write", Path: "probe", Err: syscall.EIO})
	step(0, &fs.PathError{Op: "sync", Path: "probe", Err: syscall.EROFS})

	var types, states []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var e struct{ Type, State, Kind string }
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("event %q: %v", line, err)
		}
		types = append(types, e.Type)
		if e.Type == "card" {
			states = append(states, e.State)
		}
	}
	if !slices.Equal(states, []string{"degraded", "failed"}) {
		t.Errorf("card states %v, want degraded (a 600 ms fsync), then failed (EROFS)", states)
	}
	if n := slices.Index(types, "alert"); n < 0 || slices.Index(types[n+1:], "alert") >= 0 {
		t.Errorf("events %v, want one alert", types)
	}
	if st := s.stats(); st.IOErrors24h != 1 || !st.ReadOnly || st.FsyncP99_10m != 600*time.Millisecond {
		t.Errorf("stats %+v, want one I/O error, read-only, p99 600 ms", st)
	}
	select {
	case <-s.failed:
	default:
		t.Error("FAILED did not ask for the jobs' stop")
	}
}

// TestCardSampler_CountsIOErrorsOfMediaMTXAndTheHub (T40): a line of
// MediaMTX's log, and an error of ipcam-hub's own, that holds
// «input/output error» or «read-only file system» is one I/O error of the
// card; other lines are not.
func TestCardSampler_CountsIOErrorsOfMediaMTXAndTheHub(t *testing.T) {
	var out bytes.Buffer
	s := newCardSampler(t.TempDir(), "", &events{out: &out})
	for _, line := range []string{
		"2026/10/28 12:00:00 INF [path cam1/main] [recorder] recording 1 track (H264)",
		"2026/10/28 12:00:00 ERR [path cam1/main] [recorder] write /data/rec/cam1/main/x.mp4.part: input/output error",
		"2026/10/28 12:00:00 ERR [path cam2/main] [recorder] open /data/rec/cam2/main/y.mp4.part: Read-only file system",
	} {
		s.childLog(line)
	}
	s.hubError(errors.New("recorder: error that names no I/O"))
	s.hubError(fmt.Errorf("recorder: verification: list 2026-10-28: %w", &fs.PathError{Op: "readdirent", Path: "x", Err: syscall.EIO}))
	if st := s.stats(); st.IOErrors24h != 3 {
		t.Errorf("stats %+v, want 3 I/O errors: two lines of MediaMTX and one error of ipcam-hub", st)
	}
}

// fakeCardRecorder records what the card sampler asks of the recorder.
type fakeCardRecorder struct {
	stops chan string // "<cam>/<profile>" of each Stop
	// hold, when set, holds each Stop until it is closed, as a card that
	// holds the stop's writes would.
	hold chan struct{}
	// failed, when set, gets "<cam>/<profile>" of each WriteFailed.
	failed chan string
}

func (f *fakeCardRecorder) Stop(_ context.Context, cam recorder.CameraID, p recorder.Profile, reason catalog.GapReason) error {
	if reason != catalog.GapCard {
		return fmt.Errorf("stop with reason %s, not card", reason)
	}
	f.stops <- string(cam) + "/" + string(p)
	if f.hold != nil {
		<-f.hold
	}
	return nil
}

func (f *fakeCardRecorder) WriteFailed(cam recorder.CameraID, p recorder.Profile, _ catalog.GapReason) {
	if f.failed != nil {
		f.failed <- string(cam) + "/" + string(p)
	}
}

// eioLine is a line of MediaMTX's log for a write that failed on the card.
const eioLine = "2026/10/28 12:00:00 ERR [path cam1/main] [recorder] write /data/rec/cam1/main/x.mp4.part: input/output error"

// TestCardSampler_HungProbe_FailedStillStopsTheJobs (T40, Review Focus 4;
// Task 8 review M2; Task 8b review): the probe's write and fsync run on a
// goroutine of their own, and the stop at FAILED on another: a probe that
// hangs on a dying card keeps neither MediaMTX's I/O errors from making the
// card FAILED nor every job from stopping with a card gap at once. The
// probe hangs between two intervals, so it never counts: the three lines
// of MediaMTX alone make the card FAILED.
func TestCardSampler_HungProbe_FailedStillStopsTheJobs(t *testing.T) {
	s := newCardSampler(t.TempDir(), "", &events{out: io.Discard})
	s.probeEvery = time.Hour // no interval comes that would count the hung probe
	hung, probing := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.probeFile = func(string) (time.Duration, error) {
		once.Do(func() { close(probing) })
		<-hung
		return 0, nil
	}
	rec := &fakeCardRecorder{stops: make(chan string, 4)}
	s.attach(rec)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		close(hung)
	})
	s.probeTick() // the probe starts, and hangs
	select {
	case <-probing:
	case <-time.After(10 * time.Second):
		t.Fatal("no probe started")
	}
	jobs := []recorder.Job{recorder.NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil), recorder.NewJob("cam2", "main", "rtsp://127.0.0.1:1/cam2/main", nil)}
	go s.run(ctx, jobs)

	var states []string
	for range 3 {
		s.childLog(eioLine)
		s.mu.Lock()
		states = append(states, s.state.String())
		s.mu.Unlock()
	}
	if want := []string{"degraded", "degraded", "failed"}; !slices.Equal(states, want) {
		t.Errorf("after each line of MediaMTX, states %v, want %v", states, want)
	}
	s.mu.Lock()
	stalled := s.stalled
	s.mu.Unlock()
	if st := s.stats(); st.IOErrors60s != 3 || stalled != 0 {
		t.Errorf("stats %+v and %d intervals of the hung probe counted, want the 3 I/O errors of MediaMTX's lines and none of the probe", st, stalled)
	}
	var stopped []string
	for len(stopped) < len(jobs) {
		select {
		case job := <-rec.stops:
			stopped = append(stopped, job)
		case <-time.After(10 * time.Second):
			t.Fatalf("10 s after FAILED, with the probe hung, Stop for %v; want every job", stopped)
		}
	}
	slices.Sort(stopped)
	if want := []string{"cam1/main", "cam2/main"}; !slices.Equal(stopped, want) {
		t.Errorf("stopped %v, want %v", stopped, want)
	}
}

// TestCardSampler_RunJoinsItsGoroutines (T40; Task 8b review M4): run
// returns only once the goroutines that it started have ended, so that
// nothing the card sampler finds goes out after ipcam-hub's stop: the stop
// of the jobs at FAILED, whose writes are on the card, and the probe in
// flight, whose result is still observed. A probe that the card holds for
// longer than joinWait is not waited for, so that a card that hangs does
// not hold ipcam-hub's stop; what it finds later goes nowhere.
func TestCardSampler_RunJoinsItsGoroutines(t *testing.T) {
	jobs := []recorder.Job{recorder.NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}
	// start runs s until the returned cancel, and closes ran once run returns.
	start := func(t *testing.T, s *cardSampler) (cancel func(), ran chan struct{}) {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		ran = make(chan struct{})
		go func() {
			defer close(ran)
			s.run(ctx, jobs)
		}()
		return cancel, ran
	}
	stillRuns := func(t *testing.T, ran chan struct{}, what string) {
		t.Helper()
		select {
		case <-ran:
			t.Fatalf("run returned while %s", what)
		case <-time.After(200 * time.Millisecond):
		}
	}
	returns := func(t *testing.T, ran chan struct{}, what string) {
		t.Helper()
		select {
		case <-ran:
		case <-time.After(10 * time.Second):
			t.Fatalf("run did not return %s", what)
		}
	}
	// hungProbe makes s's probe hang until the returned release, which makes
	// it return err.
	hungProbe := func(t *testing.T, s *cardSampler, err error) (release func()) {
		hung := make(chan struct{})
		var once sync.Once
		release = func() { once.Do(func() { close(hung) }) }
		t.Cleanup(release)
		s.probeFile = func(string) (time.Duration, error) {
			<-hung
			return 0, err
		}
		return release
	}
	probeReturned := func(t *testing.T, s *cardSampler) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			s.mu.Lock()
			done := s.probing.IsZero()
			s.mu.Unlock()
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the probe did not return")
			}
			time.Sleep(time.Millisecond)
		}
	}

	t.Run("the stop at FAILED", func(t *testing.T) {
		s := newCardSampler(t.TempDir(), "", &events{out: io.Discard})
		s.probeEvery = time.Hour
		rec := &fakeCardRecorder{stops: make(chan string, 1), hold: make(chan struct{})}
		s.attach(rec)
		cancel, ran := start(t, s)
		for range 3 {
			s.childLog(eioLine)
		}
		select {
		case <-rec.stops:
		case <-time.After(10 * time.Second):
			t.Fatal("no Stop after FAILED")
		}
		cancel()
		stillRuns(t, ran, "the stop of the jobs, which the card holds, still ran")
		close(rec.hold)
		returns(t, ran, "once the stop of the jobs ended")
	})
	t.Run("a probe in flight", func(t *testing.T) {
		var out syncBuffer
		s := newCardSampler(t.TempDir(), "", &events{out: &out})
		s.probeEvery = time.Hour
		release := hungProbe(t, s, &fs.PathError{Op: "sync", Path: "probe", Err: syscall.EIO})
		s.probeTick()
		cancel, ran := start(t, s)
		cancel()
		stillRuns(t, ran, "its probe was in flight")
		release()
		returns(t, ran, "once its probe returned")
		if !strings.Contains(out.String(), `"state":"degraded"`) {
			t.Errorf("events %q, want the probe's I/O error observed before run returned", out.String())
		}
	})
	t.Run("a probe that the card holds", func(t *testing.T) {
		var out syncBuffer
		s := newCardSampler(t.TempDir(), "", &events{out: &out})
		s.probeEvery = time.Hour
		s.joinWait = 50 * time.Millisecond
		release := hungProbe(t, s, &fs.PathError{Op: "sync", Path: "probe", Err: syscall.EIO})
		s.probeTick()
		cancel, ran := start(t, s)
		cancel()
		returns(t, ran, "while its probe hung")
		release()
		probeReturned(t, s)
		if got := out.String(); got != "" {
			t.Errorf("events after run returned: %q, want none", got)
		}
	})
}

// syncBuffer is a bytes.Buffer that goroutines share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestCardSampler_HungProbeCountsAsSlowThenAsErrors (T40, §13.1; Task 8
// review M2): a probe that has not returned at the next interval counts
// its time so far as an fsync's, so a hung card is DEGRADED at once; from
// the second interval that finds it still in flight, each interval also
// counts as an I/O error. So a card whose fsync hangs for good is FAILED,
// and its jobs stop, at the 4th interval after the probe began: about 40 s
// after it, and at most about 50 s after the hang began, since a probe
// starts up to one interval after; inside §13.1's 60 s. The intervals are
// counted, not timed (Task 8b review): the tick that started the probe may
// have been handled late, behind pollMount on the same loop or by a real
// ticker's jitter, and the bound does not move.
func TestCardSampler_HungProbeCountsAsSlowThenAsErrors(t *testing.T) {
	const every = 10 * time.Second
	for _, tc := range []struct {
		name string
		late time.Duration // how late the tick that starts the probe is handled
	}{
		{"every tick on time", 0},
		{"the probe's tick handled 1 ms late", time.Millisecond},
		{"the probe's tick handled half an interval late", every / 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			s := newCardSampler(t.TempDir(), "", &events{out: &out})
			s.probeEvery = every
			var clock sync.Mutex
			base := time.Now()
			now := base.Add(tc.late)
			s.now = func() time.Time {
				clock.Lock()
				defer clock.Unlock()
				return now
			}
			hung := make(chan struct{})
			t.Cleanup(func() { close(hung) })
			s.probeFile = func(string) (time.Duration, error) {
				<-hung
				return 0, nil
			}

			s.probeTick() // the probe starts, and hangs
			var states []string
			var ioErrors []int
			for k := 1; k <= 4; k++ {
				clock.Lock()
				now = base.Add(time.Duration(k) * every)
				clock.Unlock()
				s.probeTick()
				s.mu.Lock()
				states = append(states, s.state.String())
				s.mu.Unlock()
				ioErrors = append(ioErrors, s.stats().IOErrors24h)
			}
			if want := []string{"degraded", "degraded", "degraded", "failed"}; !slices.Equal(states, want) {
				t.Errorf("after each interval of the hung probe, states %v, want %v", states, want)
			}
			if want := []int{0, 1, 2, 3}; !slices.Equal(ioErrors, want) {
				t.Errorf("after each interval of the hung probe, I/O errors %v, want %v", ioErrors, want)
			}
			if st, hungFor := s.stats(), 4*every-tc.late; st.IOErrors60s != 3 || st.FsyncP99_10m != hungFor {
				t.Errorf("stats %+v, want 3 I/O errors in 60 s and a p99 of %v", st, hungFor)
			}
			select {
			case <-s.failed:
			default:
				t.Error("FAILED did not ask for the jobs' stop")
			}
			if n := strings.Count(out.String(), `"type":"alert"`); n != 1 {
				t.Errorf("%d alerts, want 1:\n%s", n, out.String())
			}
		})
	}
}

// TestCardSampler_ReturnedProbeRestartsTheCount (T40; Task 8b review): the
// intervals that found a probe in flight are counted per probe. A probe
// that was slow for one interval and then returned leaves no count behind,
// so the next probe that hangs is slow only at its own first interval, and
// an I/O error from its second.
func TestCardSampler_ReturnedProbeRestartsTheCount(t *testing.T) {
	const every = 10 * time.Second
	s := newCardSampler(t.TempDir(), "", &events{out: io.Discard})
	s.probeEvery = every
	var clock sync.Mutex
	base := time.Now()
	now := base
	s.now = func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		return now
	}
	tick := func(k int) int {
		clock.Lock()
		now = base.Add(time.Duration(k) * every)
		clock.Unlock()
		s.probeTick()
		return s.stats().IOErrors24h
	}
	slow, hung := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(hung) })
	var calls sync.Mutex
	probes := 0
	s.probeFile = func(string) (time.Duration, error) {
		calls.Lock()
		probes++
		first := probes == 1
		calls.Unlock()
		if first {
			<-slow
		} else {
			<-hung
		}
		return 0, nil
	}

	tick(0) // the first probe starts
	if n := tick(1); n != 0 {
		t.Fatalf("one interval of the first probe: %d I/O errors, want 0", n)
	}
	close(slow) // the first probe returns
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		done := s.probing.IsZero()
		s.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first probe did not return")
		}
		time.Sleep(time.Millisecond)
	}
	tick(2) // the second probe starts, and hangs
	var ioErrors []int
	for k := 3; k <= 4; k++ {
		ioErrors = append(ioErrors, tick(k))
	}
	if want := []int{0, 1}; !slices.Equal(ioErrors, want) {
		t.Errorf("after each interval of the second probe, I/O errors %v, want %v", ioErrors, want)
	}
}
