package recorder

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/rtsptest"
)

// TestRecorder_NoSecretKeyInConfigOrChildEnv (C22): neither the MediaMTX
// configuration nor the environment of the child holds a secret key, even
// when the parent's environment does; the configuration is 0600 in the run
// directory; and the child gets the hook's address and token, but none of
// the MTX_ variables that MediaMTX would read as configuration.
func TestRecorder_NoSecretKeyInConfigOrChildEnv(t *testing.T) {
	scope, riG, _ := newTestScope(t)
	t.Setenv("IPCAM_TEST_PASTED_KEY", riG.String())
	t.Setenv("MTX_PATHS_CAM1_RECORD", "no")
	t.Setenv("TZ", testZone)
	dump := filepath.Join(t.TempDir(), "child.json")
	r, cfg := startFake(t, "dump:"+dump, Config{})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", &scope)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	d := readDump(t, dump)

	if len(d.Args) != 1 || filepath.Dir(d.Args[0]) != cfg.RunDir {
		t.Fatalf("the child got arguments %q, want the configuration in %s", d.Args, cfg.RunDir)
	}
	config := readFile(t, d.Args[0])
	if strings.Contains(string(config), "AGE-SECRET-KEY-") {
		t.Error("the configuration holds a secret key")
	}
	if !strings.Contains(string(config), scope.RecordingIdentity.String()) {
		t.Error("the configuration that Apply wrote lacks the job's recipients")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(d.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("the configuration's mode is %o, want 600", mode)
		}
	}

	env := map[string]string{}
	var zones []string
	for _, kv := range d.Env {
		if k, v, _ := strings.Cut(kv, "="); strings.EqualFold(k, "TZ") {
			zones = append(zones, v)
		}
		if strings.Contains(strings.ToUpper(kv), "AGE-SECRET-KEY-") {
			t.Errorf("the child's environment holds a secret key in %s", strings.SplitN(kv, "=", 2)[0])
		}
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
		if strings.HasPrefix(k, "MTX_") || strings.HasPrefix(k, "RTSP_") {
			t.Errorf("the child's environment has %s, which MediaMTX would read as configuration", k)
		}
	}
	if !strings.HasPrefix(env[HookAddrEnv], "127.0.0.1:") || len(env[HookTokenEnv]) < 32 {
		t.Errorf("the child's hook address %q or token (%d characters) is missing", env[HookAddrEnv], len(env[HookTokenEnv]))
	}
	// MediaMTX runs in UTC, whatever the hub's zone: its names are UTC (MTX-4).
	if !slices.Equal(zones, []string{"UTC"}) {
		t.Errorf("the child's TZ is %q, want only UTC", zones)
	}
}

// TestSupervisor_ForwardsChildStderr: every line that the child writes
// reaches Observer.ChildLog, in order, and none reaches the recorder's own
// output.
func TestSupervisor_ForwardsChildStderr(t *testing.T) {
	obs := &testObserver{}
	startFake(t, "stderr:5", Config{Observer: obs})
	want := []string{"line 1", "line 2", "line 3", "line 4", "line 5"}
	waitFor(t, 10*time.Second, "the child's lines did not reach ChildLog", func() bool {
		return len(obs.snapshot().logs) >= len(want)
	})
	if got := obs.snapshot().logs; !slices.Equal(got, want) {
		t.Errorf("ChildLog got %q, want %q", got, want)
	}
}

// TestSupervisor_RestartBackoff (C09, HUB-13): a child that exits is started
// again at once, then after 0.5 s, 1 s and 2 s, never more; a child that ran
// for 60 s or more starts the sequence again.
func TestSupervisor_RestartBackoff(t *testing.T) {
	var b backoff
	for i, tc := range []struct {
		uptime time.Duration
		want   time.Duration
	}{
		{time.Second, 0},
		{time.Second, 500 * time.Millisecond},
		{time.Second, time.Second},
		{time.Second, 2 * time.Second},
		{time.Second, 2 * time.Second},
		{59 * time.Second, 2 * time.Second},
		{60 * time.Second, 0},
		{time.Second, 500 * time.Millisecond},
	} {
		if got := b.next(tc.uptime); got != tc.want {
			t.Errorf("exit %d after %v: restart after %v, want %v", i+1, tc.uptime, got, tc.want)
		}
	}
}

// TestRecorder_ChildDiesWithParent (C07): when the process that runs the
// recorder is killed, its MediaMTX child is gone within 1 s.
func TestRecorder_ChildDiesWithParent(t *testing.T) {
	dir := t.TempDir()
	helper := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // the test binary itself
	helper.Env = append(withoutEnv(os.Environ(), fakeChildEnv), helperEnv+"="+dir)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	d := readDump(t, filepath.Join(dir, "child.json"))
	if !processAlive(t, d.PID) {
		t.Fatal("the child is not running")
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
	killed := time.Now()
	waitFor(t, time.Second, "the child outlived its parent", func() bool { return !processAlive(t, d.PID) })
	t.Logf("the child was gone %v after its parent was killed", time.Since(killed).Round(time.Millisecond))
}

func withoutEnv(env []string, key string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, key+"=") })
}

// TestStop_WritesOpenGapClosedOnReapply: Stop takes the stream out of the
// configuration and writes an open gap with its reason; Apply with the job
// again closes that gap.
func TestStop_WritesOpenGapClosedOnReapply(t *testing.T) {
	r, cfg := startFake(t, "sleep", Config{})
	ctx := t.Context()
	job := NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)
	if err := r.Apply(ctx, []Job{job}); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(ctx, "cam1", "main", catalog.GapCard); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	gaps, err := cfg.Catalog.Gaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0].Reason != catalog.GapCard || gaps[0].EndMs != 0 || gaps[0].Cam != "cam1" || gaps[0].Profile != "main" {
		t.Fatalf("after Stop: gaps %+v, want one open card gap of cam1/main", gaps)
	}
	if paths := configPaths(t, cfg); len(paths) != 0 {
		t.Errorf("after Stop the configuration still has %v", paths)
	}
	if r.Status(ctx, "cam1", "main").Running {
		t.Error("a stopped stream is still running")
	}
	if err := r.Stop(ctx, "cam9", "main", catalog.GapCard); err == nil {
		t.Error("Stop of a stream that is not recorded succeeded")
	}

	if err := r.Apply(ctx, []Job{job}); err != nil {
		t.Fatal(err)
	}
	gaps, _ = cfg.Catalog.Gaps(ctx)
	if len(gaps) != 1 || gaps[0].EndMs < gaps[0].StartMs || gaps[0].EndMs == 0 {
		t.Errorf("after Apply again: gaps %+v, want the card gap closed", gaps)
	}
	if paths := configPaths(t, cfg); !slices.Equal(paths, []string{"cam1/main"}) {
		t.Errorf("after Apply again the configuration has %v", paths)
	}
}

// TestStop_ReadOnlyCard_GapEventStillGoesOut (T40, Review Focus 4; Task 8
// review M1): on a card that a read-only remount left, Stop can write
// neither MediaMTX's configuration nor the gap's row, both on the card. It
// still stops the job, which no longer runs, still tries the gap's row, and
// still gives the gap out as an event, with its error: recording stops with
// a gap, never with a silent loss, for every job that the card failure
// stops.
func TestStop_ReadOnlyCard_GapEventStillGoesOut(t *testing.T) {
	var readOnly atomic.Bool
	t.Cleanup(setWriteConfigFile(func(path string, data []byte, perm os.FileMode) error {
		if readOnly.Load() {
			return &fs.PathError{Op: "open", Path: path + ".tmp", Err: errors.New("read-only file system")}
		}
		return writeFileSynced(path, data, perm)
	}))
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{Observer: obs})
	ctx := t.Context()
	jobs := []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil), NewJob("cam2", "main", "rtsp://127.0.0.1:1/cam2/main", nil)}
	if err := r.Apply(ctx, jobs); err != nil {
		t.Fatal(err)
	}

	readOnly.Store(true)
	if err := cfg.Catalog.Close(); err != nil { // its rows are on the card too
		t.Fatal(err)
	}
	for _, j := range jobs {
		if err := r.Stop(ctx, j.Cam, j.Profile, catalog.GapCard); err == nil {
			t.Errorf("Stop of %s/%s on a read-only card: no error", j.Cam, j.Profile)
		}
	}
	var stopped []string
	for _, g := range obs.snapshot().gaps {
		if g.Reason == catalog.GapCard && g.EndMs == 0 {
			stopped = append(stopped, g.Cam+"/"+g.Profile)
		}
	}
	if want := []string{"cam1/main", "cam2/main"}; !slices.Equal(stopped, want) {
		t.Errorf("open card gap events for %v, want one for each of %v", stopped, want)
	}
	for _, j := range jobs {
		if r.Status(ctx, j.Cam, j.Profile).Running {
			t.Errorf("%s/%s still runs after its Stop", j.Cam, j.Profile)
		}
	}
}

func configPaths(t *testing.T, cfg Config) []string {
	t.Helper()
	c := parseConfig(t, readFile(t, filepath.Join(cfg.RunDir, configName)))
	paths, _ := c["paths"].(map[string]any)
	var names []string
	for name := range paths {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TestRecorder_PrecreatesAndSyncsStreamDirs: MediaMTX creates a missing
// directory with MkdirAll and syncs only the segment's own directory, so
// the recorder creates each stream's directory and today's and tomorrow's
// date directories itself, syncing the parent of each directory it creates.
// The days are UTC days, as MediaMTX names them (MTX-4): at 01:30 in
// Romania on 21 October it is still 20 October in UTC.
func TestRecorder_PrecreatesAndSyncsStreamDirs(t *testing.T) {
	var synced []string
	defer setSyncDir(func(dir string) error {
		synced = append(synced, dir)
		return nil
	})()
	recDir := filepath.Join(t.TempDir(), "rec")
	now := time.Date(2026, 10, 21, 1, 30, 0, 0, time.Local)
	if err := ensureStreamDirs(recDir, "cam1", "main", now); err != nil {
		t.Fatalf("ensureStreamDirs: %v", err)
	}
	stream := filepath.Join(recDir, "cam1", "main")
	for _, day := range []string{"2026-10-20", "2026-10-21"} {
		if info, err := os.Stat(filepath.Join(stream, day)); err != nil || !info.IsDir() {
			t.Errorf("no directory %s", day)
		}
	}
	for _, parent := range []string{filepath.Dir(recDir), recDir, filepath.Join(recDir, "cam1"), stream} {
		if !slices.Contains(synced, parent) {
			t.Errorf("%s was not synced after a directory was created in it (synced: %q)", parent, synced)
		}
	}
	synced = nil
	if err := ensureStreamDirs(recDir, "cam1", "main", now); err != nil {
		t.Fatal(err)
	}
	if len(synced) != 0 {
		t.Errorf("directories that exist were synced again: %q", synced)
	}
}

// TestRecorder_ResumesAfterKill (C09, HUB-13): the real MediaMTX, killed
// while it records, is started again within 3 s, and a new part follows
// within 4 s of the kill. Real time, 1 s parts.
func TestRecorder_ResumesAfterKill(t *testing.T) {
	bin := rtsptest.MediaMTX(t)
	srv := rtsptest.Start(t, rtsptest.MainH264(t))
	obs := &testObserver{}
	r, _ := startReal(t, bin, Config{Observer: obs})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", srv.URL("cam1/main"), nil)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "MediaMTX wrote no part", func() bool { return len(obs.snapshot().firsts) >= 1 })

	pid := r.childPID()
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	killed := time.Now()
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "MediaMTX was not started again within 3 s", func() bool {
		return len(obs.snapshot().restarts) >= 1
	})
	waitFor(t, 4*time.Second-time.Since(killed), "no new part within 4 s of the kill", func() bool {
		return len(obs.snapshot().firsts) >= 2
	})
	s := obs.snapshot()
	restart, part := s.restarts[0].Sub(killed), s.firstAt[1].Sub(killed)
	t.Logf("restarted %v and a new part %v after the kill", restart.Round(time.Millisecond), part.Round(time.Millisecond))
	if restart > 3*time.Second || part > 4*time.Second {
		t.Errorf("restart after %v (limit 3 s), new part after %v (limit 4 s)", restart, part)
	}
	if r.childPID() == pid {
		t.Error("the child's PID did not change")
	}
}

// startReal runs New with the real MediaMTX binary.
func startReal(t *testing.T, bin string, cfg Config) (*rec, Config) {
	t.Helper()
	dir := t.TempDir()
	cfg.BinaryPath = bin
	cfg.RunDir = filepath.Join(dir, "run")
	if cfg.RecDir == "" {
		cfg.RecDir = filepath.Join(dir, "rec")
	}
	if cfg.Catalog == nil {
		cfg.Catalog = openCatalog(t)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg.HookCommand = []string{self, "hook", "segment-complete"}
	ctx, cancel := context.WithCancel(context.Background())
	r, report, err := New(ctx, cfg)
	if err != nil {
		cancel()
		t.Fatalf("New: %v (report %+v)", err, report)
	}
	t.Cleanup(func() {
		cancel()
		for range r.Closed(context.Background()) {
		}
	})
	return r.(*rec), cfg
}

// TestRecorder_GracefulStopWritesHubStoppedGaps (§4.5): when the hub stops,
// the real MediaMTX is asked to stop (SIGTERM on Linux, CTRL_BREAK_EVENT to
// its process group on Windows), closes its segments, which get their final
// names, their hooks and their rows, and exits. Each recorded stream then
// gets an open hub_stopped gap from the moment of the stop, and the next
// start finds no .part, so no restart gap. Each name encodes the start that
// its hook reports, in UTC (MTX-4), here with the hub in Romania's zone.
func TestRecorder_GracefulStopWritesHubStoppedGaps(t *testing.T) {
	bin := rtsptest.MediaMTX(t)
	srv := rtsptest.Start(t, rtsptest.MainH264(t))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	obs := &testObserver{}
	cfg := Config{BinaryPath: bin, RunDir: filepath.Join(dir, "run"), RecDir: filepath.Join(dir, "rec"), Catalog: openCatalog(t),
		Observer: obs, HookCommand: []string{self, "hook", "segment-complete"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for range r.Closed(context.Background()) {
		}
	}()
	if err := r.Apply(ctx, []Job{NewJob("cam1", "main", srv.URL("cam1/main"), nil)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "MediaMTX wrote no part", func() bool { return len(obs.snapshot().firsts) >= 1 })
	time.Sleep(1500 * time.Millisecond) // a part or two

	stopAt := time.Now().UnixMilli()
	cancel()
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("the recorder did not stop")
	}
	doneAt := time.Now().UnixMilli()

	l, err := scanRecDir(cfg.RecDir)
	if err != nil {
		t.Fatal(err)
	}
	segs := l.segs
	if len(segs) == 0 {
		t.Fatal("no segment")
	}
	rows := rowByPath(t, cfg.Catalog)
	for _, s := range segs {
		if s.part {
			t.Errorf("%s is still a .part: MediaMTX did not close it", s.rel)
			continue
		}
		row, ok := rows[s.rel]
		if !ok {
			t.Errorf("%s has no row: its hook did not come", s.rel)
		} else if row.StartMs != s.start.UnixMilli() {
			t.Errorf("%s: the hook's start %d ms, the name's %d ms, want the same instant", s.rel, row.StartMs, s.start.UnixMilli())
		}
	}
	gaps, err := cfg.Catalog.Gaps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0].Reason != catalog.GapHubStopped || gaps[0].EndMs != 0 || gaps[0].Cam != "cam1" || gaps[0].Profile != "main" ||
		gaps[0].StartMs < stopAt || gaps[0].StartMs > doneAt {
		t.Errorf("gaps %+v, want one open hub_stopped gap of cam1/main from the stop (%d–%d); errors %q", gaps, stopAt, doneAt, obs.snapshot().errs)
	}
	report, err := Reconcile(t.Context(), cfg.RecDir, nil, cfg.Catalog)
	if err != nil || report != (ReconcileReport{}) {
		t.Errorf("the next start's reconcile %+v, %v; want nothing to repair", report, err)
	}
}

// TestStop_GraceIsTenSecondsOnLinux (M2): a stop gives MediaMTX 10 s on
// Linux to close its segments, since a Pi that records its recommended
// cameras on an SD card fsyncs, renames, syncs a directory and runs a hook
// per segment. A MediaMTX that takes 7 s stops gracefully there: no error,
// and the stream gets its hub_stopped gap. Windows keeps 5 s: it is killed,
// with an error, and no gap is written.
func TestStop_GraceIsTenSecondsOnLinux(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv(fakeChildEnv, "slowstop:"+ready)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	obs := &testObserver{}
	cfg := Config{BinaryPath: self, RunDir: filepath.Join(dir, "run"), RecDir: filepath.Join(dir, "rec"), Catalog: openCatalog(t),
		Observer: obs, HookCommand: []string{self, "hook", "segment-complete"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for range r.Closed(context.Background()) {
		}
	}()
	if err := r.Apply(ctx, []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "the fake MediaMTX does not catch the stop signal", func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})

	asked := time.Now()
	cancel()
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("the recorder did not stop")
	}
	took := time.Since(asked)
	killed := slices.ContainsFunc(obs.snapshot().errs, func(e string) bool { return strings.Contains(e, "did not stop within") })
	gaps, err := cfg.Catalog.Gaps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if !killed || len(gaps) != 0 || took >= slowStop {
			t.Errorf("after %v: killed %v, gaps %+v; want MediaMTX killed after 5 s, without a gap", took, killed, gaps)
		}
		return
	}
	if killed || len(gaps) != 1 || gaps[0].Reason != catalog.GapHubStopped || took < slowStop {
		t.Errorf("after %v: killed %v, gaps %+v, errors %q; want a graceful stop after %v with one hub_stopped gap",
			took, killed, gaps, obs.snapshot().errs, slowStop)
	}
}

// TestRecorder_SegmentClosedUnseen_RecordingComesFirst: a segment that
// MediaMTX closed before a poll saw its .part grow, one shorter than
// pollInterval, is still its stream's first part since the child started:
// its hook reports FirstPart before the SegmentClosed goes out, and the
// stream is not reported again when its next .part grows.
func TestRecorder_SegmentClosedUnseen_RecordingComesFirst(t *testing.T) {
	obs := &testObserver{}
	r, cfg := startFake(t, "sleep", Config{Observer: obs})
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000}})
	start := time.Now().Add(-time.Second)
	final := segPath(cfg.RecDir, "cam1", "main", start, "")
	writeFile(t, final, f.data)

	code, err := r.segmentComplete(t.Context(), HookRequest{Path: final, StartMs: start.UnixMilli(), DurationMs: 1000, Cam: "cam1", Profile: "main"})
	if err != nil || code != 204 {
		t.Fatalf("segmentComplete: %d, %v", code, err)
	}
	if firsts := obs.snapshot().firsts; !slices.Equal(firsts, []string{"cam1/main"}) {
		t.Errorf("FirstPart before the SegmentClosed went out: %q, want cam1/main", firsts)
	}
	select {
	case s := <-r.Closed(t.Context()):
		if s.Path != final {
			t.Errorf("SegmentClosed %+v, want %s", s, final)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no SegmentClosed")
	}

	writeFile(t, segPath(cfg.RecDir, "cam1", "main", time.Now(), ".part"), f.data)
	time.Sleep(4 * pollInterval)
	if firsts := obs.snapshot().firsts; len(firsts) != 1 {
		t.Errorf("FirstPart %q, want it once since the child started", firsts)
	}
}

// gapBlocker is a testObserver whose Gap blocks, for a gap that is closed,
// until release is closed, as a catalog write that is slow would hold the
// recorder there.
type gapBlocker struct {
	testObserver
	blocked chan struct{} // closed when the first Gap of a closed gap blocks
	release chan struct{}
	once    sync.Once
}

func (o *gapBlocker) Gap(g catalog.Gap) {
	if g.EndMs != 0 {
		o.once.Do(func() { close(o.blocked) })
		<-o.release
	}
	o.testObserver.Gap(g)
}

// TestRecorder_FirstPartsOfOnePollComeBeforeGapCloses: when one poll finds
// the first part of two streams, both FirstParts go out before the gaps
// that recording resumed are closed in the catalog: a slow close of one
// stream's gap does not hold back the other stream's FirstPart, which must
// come before its first SegmentClosed.
func TestRecorder_FirstPartsOfOnePollComeBeforeGapCloses(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	f := makeFMP4(t, fmp4Spec{samples: 20, sizes: []int{1000, 1000}})
	for _, p := range []string{"main", "sub"} { // each stream gets an open restart gap at startup
		writeFile(t, segPath(recDir, "cam1", p, t0, ".part"), f.data[:f.partStart[1]+10])
	}
	obs := &gapBlocker{blocked: make(chan struct{}), release: make(chan struct{})}
	r, _ := startFake(t, "sleep", Config{RecDir: recDir, Observer: obs})
	defer close(obs.release)
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil),
		NewJob("cam1", "sub", "rtsp://127.0.0.1:1/cam1/sub", nil)}); err != nil {
		t.Fatal(err)
	}
	// Both .part files appear between two polls.
	r.watch.mu.Lock()
	now := time.Now()
	for _, p := range []string{"main", "sub"} {
		writeFile(t, segPath(recDir, "cam1", p, now, ".part"), f.data)
	}
	r.watch.mu.Unlock()

	select {
	case <-obs.blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("no gap was closed")
	}
	if firsts := obs.snapshot().firsts; len(firsts) != 2 {
		t.Errorf("FirstPart %q while the first gap closes, want both streams", firsts)
	}
}
