package recorder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// Config is what a recorder needs.
type Config struct {
	BinaryPath string // the MediaMTX fork
	RunDir     string // <data>/state/run: MediaMTX's configuration (0600) and working directory
	RecDir     string // <data>/rec
	Tail       crypto.TailSealer
	Catalog    *catalog.DB
	Observer   Observer
	// Recording decrypts closed .age segments for the background
	// verification; without it they count as unreadable.
	Recording crypto.Recording
	// HookCommand is the command that MediaMTX runs when a segment is
	// complete; nil is this executable with "hook segment-complete".
	HookCommand []string
}

// Recorder records the applied jobs through MediaMTX.
type Recorder interface {
	// Apply records exactly jobs. It rewrites MediaMTX's configuration, which
	// MediaMTX reloads; a job that Stop took out gets its gap closed.
	Apply(ctx context.Context, jobs []Job) error
	// Closed delivers each segment that MediaMTX completed. It is closed
	// once the recorder has stopped, after the context given to New is done.
	Closed(ctx context.Context) <-chan SegmentClosed
	Status(ctx context.Context, cam CameraID, p Profile) RecStatus
	// Stop takes the stream out of the configuration and writes an open gap
	// with reason.
	Stop(ctx context.Context, cam CameraID, p Profile, reason catalog.GapReason) error
}

// ErrorObserver is implemented by an Observer that wants the errors of the
// background work: verification, rewrites, the child's start.
type ErrorObserver interface{ Error(err error) }

// ErrReconcileAborted is New's error when reconcile found more than half of
// the segments lost and changed nothing (HUB-09).
var ErrReconcileAborted = errors.New("recorder: reconcile aborted: more than half of the segments would be lost")

const (
	// minConfigInterval spaces the configuration writes: MediaMTX ignores a
	// change of its file within 1 s of the last one it reloaded
	// (internal/confwatcher), which would leave it on an old configuration.
	minConfigInterval = 1100 * time.Millisecond
	// configSettle is how long a starting MediaMTX may take before it
	// watches its file; a write in that time is made again after it.
	configSettle = 1500 * time.Millisecond
	// stopGrace is how long MediaMTX gets to close its segments when the hub
	// stops, before it is killed.
	stopGrace = 10 * time.Second
	// logDrain is how long the child's output may stay open after it exited:
	// a hook command that MediaMTX started holds it too.
	logDrain = 2 * time.Second
	// dirsInterval is how often the date directories are made ahead.
	dirsInterval = time.Minute
	// maxLogLine bounds a line of the child's output.
	maxLogLine = 64 << 10
)

// rewriteHook, when a test sets it, runs as each background rewrite starts.
var rewriteHook = func() {}

type rec struct {
	cfg        Config
	obs        Observer
	configPath string
	hook       *hookServer
	watch      *partWatcher
	verify     *verifier
	closed     chan SegmentClosed
	verifyKick chan struct{}
	jobsKick   chan struct{}
	wg         sync.WaitGroup // the background loops and the child's log readers

	mu        sync.Mutex
	jobs      map[streamKey]Job
	stopped   map[streamKey]bool
	report    ReconcileReport
	pending   []*pendingRewrite
	rewriting map[string]bool // the rel paths that a rewrite is changing
	childPid  int             // 0 while no child runs

	// fileMu is held while a verification has a segment open and while a
	// rewrite renames over one: Windows renames over a file only when
	// nobody has it open.
	fileMu sync.Mutex

	writeMu   sync.Mutex
	lastWrite time.Time
}

// New reconciles the recordings under cfg.RecDir (Reconcile), then starts
// MediaMTX and supervises it until ctx is done: it binds the child to the
// hub's lifetime (C07), starts it again at once when it exits, then after
// 0.5 s, 1 s and 2 s (HUB-13, C09), serves the hook, polls the open parts,
// verifies closed segments and runs the rewrites that reconcile left, all in
// the background. When reconcile aborts, it starts nothing and returns
// ErrReconcileAborted with the report.
func New(ctx context.Context, cfg Config) (Recorder, ReconcileReport, error) {
	if cfg.BinaryPath == "" || cfg.RunDir == "" || cfg.RecDir == "" || cfg.Catalog == nil {
		return nil, ReconcileReport{}, errors.New("recorder: the configuration needs a binary, a run and a recordings directory, and a catalog")
	}
	for _, dir := range []*string{&cfg.RunDir, &cfg.RecDir} {
		abs, err := filepath.Abs(*dir)
		if err != nil {
			return nil, ReconcileReport{}, err
		}
		*dir = abs
	}
	if cfg.HookCommand == nil {
		self, err := os.Executable()
		if err != nil {
			return nil, ReconcileReport{}, err
		}
		cfg.HookCommand = []string{self, "hook", "segment-complete"}
	}
	if cfg.Observer == nil {
		cfg.Observer = nopObserver{}
	}
	if err := os.MkdirAll(cfg.RunDir, 0o700); err != nil {
		return nil, ReconcileReport{}, err
	}
	if err := mkdirSynced(cfg.RecDir); err != nil {
		return nil, ReconcileReport{}, err
	}

	report, pending, gaps, err := reconcile(ctx, cfg.RecDir, cfg.Tail, cfg.Catalog)
	if err != nil {
		return nil, report, err
	}
	if report.Aborted {
		return nil, report, ErrReconcileAborted
	}
	r := &rec{
		cfg: cfg, obs: cfg.Observer, configPath: filepath.Join(cfg.RunDir, configName),
		watch: newPartWatcher(cfg.RecDir), closed: make(chan SegmentClosed, 1024),
		verifyKick: make(chan struct{}, 1), jobsKick: make(chan struct{}, 1),
		jobs: map[streamKey]Job{}, stopped: map[streamKey]bool{}, report: report, pending: pending, rewriting: map[string]bool{},
	}
	r.verify = &verifier{recDir: cfg.RecDir, cat: cfg.Catalog, recording: cfg.Recording, obs: cfg.Observer, busy: r.isRewriting, open: &r.fileMu}
	for _, g := range gaps {
		r.notifyGap(g)
	}
	if r.hook, err = startHookServer(r.segmentComplete); err != nil {
		return nil, report, err
	}
	if err := r.writeConfig(ctx); err != nil {
		r.hook.shutdown(context.Background())
		return nil, report, err
	}

	started := make(chan error, 1)
	childDone := make(chan struct{})
	go func() {
		defer close(childDone)
		r.supervise(ctx, started)
	}()
	if err := <-started; err != nil {
		<-childDone
		r.hook.shutdown(context.Background())
		return nil, report, fmt.Errorf("recorder: start MediaMTX: %w", err)
	}
	r.wg.Add(3)
	go r.pollLoop(ctx)
	go r.verifyLoop(ctx)
	go r.rewriteLoop(ctx)
	r.kickVerify()
	go func() { //nolint:gosec // ctx is done once the child has stopped, so the shutdown has its own deadline
		<-childDone // the child has stopped, and its last hooks are in
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		r.hook.shutdown(shutdown)
		cancel()
		r.wg.Wait()
		close(r.closed)
	}()
	return r, report, nil
}

// supervise runs the MediaMTX child until ctx is done and starts it again
// when it exits. It reports the result of the first start on started.
//
// Its goroutine stays on one OS thread: on Linux the child's parent-death
// signal fires when the thread that started it exits.
func (r *rec) supervise(ctx context.Context, started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var b backoff
	for restart := false; ; restart = true {
		child, logs, err := r.startChild()
		if !restart {
			started <- err
			if err != nil {
				return
			}
		}
		var uptime time.Duration
		if err != nil {
			r.notifyError(fmt.Errorf("recorder: start MediaMTX again: %w", err))
		} else {
			at := time.Now()
			if restart {
				r.obs.ChildRestarted(at)
			}
			uptime = r.runChild(ctx, child, logs, at)
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-time.After(b.next(uptime)):
		case <-ctx.Done():
			return
		}
	}
}

// startChild starts MediaMTX with the configuration in the run directory,
// its output going to Observer.ChildLog. logs is closed when the output
// has been read to its end.
func (r *rec) startChild() (*childProc, <-chan struct{}, error) {
	cmd := exec.Command(r.cfg.BinaryPath, r.configPath) //nolint:gosec // the configured MediaMTX binary
	cmd.Dir = r.cfg.RunDir
	cmd.Env = childEnv(os.Environ(), r.hook.env())
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	child, err := startChild(cmd)
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return nil, nil, err
	}
	logs := make(chan struct{})
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(logs)
		r.forwardLines(pr)
	}()
	return child, logs, nil
}

// runChild waits until the child exits, or stops it when ctx is done, and
// returns how long it ran.
func (r *rec) runChild(ctx context.Context, child *childProc, logs <-chan struct{}, at time.Time) time.Duration {
	r.setChild(child.cmd.Process.Pid)
	r.watch.childStarted(at)
	go r.settleConfig(ctx, at)
	exited := make(chan struct{})
	go func() {
		_ = child.cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-ctx.Done():
		_ = child.interrupt()
		select {
		case <-exited:
		case <-time.After(stopGrace):
			_ = child.cmd.Process.Kill()
			<-exited
		}
	}
	child.release()
	r.setChild(0)
	select {
	case <-logs:
	case <-time.After(logDrain):
	}
	return time.Since(at)
}

// forwardLines hands each line of the child's output to ChildLog, cutting a
// line longer than maxLogLine, until the output ends.
func (r *rec) forwardLines(out io.ReadCloser) {
	defer func() { _ = out.Close() }()
	br := bufio.NewReaderSize(out, maxLogLine)
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			r.obs.ChildLog(strings.TrimRight(string(line), "\r\n"))
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n') // the rest of the line is dropped
			}
		}
		if err != nil {
			return
		}
	}
}

// childEnv is the child's environment: the hub's, without any secret key
// (C22) and without the MTX_ and RTSP_ variables that MediaMTX reads as
// configuration, so the generated configuration is all there is, plus the
// hook's address and token.
func childEnv(parent, hook []string) []string {
	env := make([]string, 0, len(parent)+len(hook))
	for _, kv := range parent {
		k, v, _ := strings.Cut(kv, "=")
		k = strings.ToUpper(k)
		if strings.HasPrefix(k, "MTX_") || strings.HasPrefix(k, "RTSP_") || k == HookAddrEnv || k == HookTokenEnv ||
			strings.Contains(strings.ToUpper(v), "AGE-SECRET-KEY-") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, hook...)
}

// backoff is the restart policy: at once, then after 0.5 s, 1 s and 2 s,
// never longer, so every restart comes within 3 s (HUB-13); a child that ran
// for 60 s starts the sequence again.
type backoff struct{ restarts int }

var restartDelays = []time.Duration{0, 500 * time.Millisecond, time.Second, 2 * time.Second}

func (b *backoff) next(uptime time.Duration) time.Duration {
	if uptime >= time.Minute {
		b.restarts = 0
	}
	d := restartDelays[min(b.restarts, len(restartDelays)-1)]
	b.restarts++
	return d
}

func (r *rec) setChild(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.childPid = pid
}

// childPID is the running child's PID, or 0.
func (r *rec) childPID() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.childPid
}

// appliedJobs returns the jobs, sorted by stream.
func (r *rec) appliedJobs() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		jobs = append(jobs, j)
	}
	slices.SortFunc(jobs, func(a, b Job) int { return strings.Compare(a.pathName(), b.pathName()) })
	return jobs
}

func (r *rec) Apply(ctx context.Context, jobs []Job) error {
	if _, err := GenerateConfig(jobs, r.cfg.HookCommand, r.cfg.RecDir); err != nil {
		return err
	}
	now := time.Now()
	for _, j := range jobs {
		if err := ensureStreamDirs(r.cfg.RecDir, j.Cam, j.Profile, now); err != nil {
			return err
		}
	}
	r.mu.Lock()
	var readded []streamKey
	r.jobs = map[streamKey]Job{}
	keys := make([]streamKey, 0, len(jobs))
	for _, j := range jobs {
		r.jobs[j.key()] = j
		keys = append(keys, j.key())
		if r.stopped[j.key()] {
			delete(r.stopped, j.key())
			readded = append(readded, j.key())
		}
	}
	r.mu.Unlock()
	r.watch.setStreams(keys)
	if err := r.writeConfig(ctx); err != nil {
		return err
	}
	for _, k := range readded {
		if err := r.closeGaps(ctx, k, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	select {
	case r.jobsKick <- struct{}{}:
	default:
	}
	return nil
}

func (r *rec) Stop(ctx context.Context, cam CameraID, p Profile, reason catalog.GapReason) error {
	k := streamKey{cam, p}
	r.mu.Lock()
	if _, ok := r.jobs[k]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("recorder: %s is not recorded", k)
	}
	delete(r.jobs, k)
	r.stopped[k] = true
	keys := make([]streamKey, 0, len(r.jobs))
	for key := range r.jobs {
		keys = append(keys, key)
	}
	r.mu.Unlock()
	r.watch.setStreams(keys)
	if err := r.writeConfig(ctx); err != nil {
		return err
	}
	g := catalog.Gap{Cam: string(cam), Profile: string(p), StartMs: time.Now().UnixMilli(), Reason: reason}
	if err := r.cfg.Catalog.PutGap(ctx, g); err != nil {
		return err
	}
	r.notifyGap(g)
	return nil
}

func (r *rec) Status(_ context.Context, cam CameraID, p Profile) RecStatus {
	k := streamKey{cam, p}
	s := r.watch.status(k, time.Now())
	r.mu.Lock()
	_, applied := r.jobs[k]
	s.Running = applied && r.childPid != 0
	r.mu.Unlock()
	return s
}

func (r *rec) Closed(context.Context) <-chan SegmentClosed { return r.closed }

// writeConfig writes MediaMTX's configuration for the applied jobs, no
// sooner than minConfigInterval after the last write.
func (r *rec) writeConfig(ctx context.Context) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	config, err := GenerateConfig(r.appliedJobs(), r.cfg.HookCommand, r.cfg.RecDir)
	if err != nil {
		return err
	}
	if wait := minConfigInterval - time.Since(r.lastWrite); !r.lastWrite.IsZero() && wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := writeFileSynced(r.configPath, config, 0o600); err != nil {
		return fmt.Errorf("recorder: write the MediaMTX configuration: %w", err)
	}
	r.lastWrite = time.Now()
	return nil
}

// settleConfig writes the configuration again once a child that started at
// at watches its file, if it was written meanwhile: MediaMTX reads its file
// before it starts to watch it, and would miss that change.
func (r *rec) settleConfig(ctx context.Context, at time.Time) {
	select {
	case <-time.After(configSettle):
	case <-ctx.Done():
		return
	}
	r.writeMu.Lock()
	raced := r.lastWrite.After(at.Add(-200 * time.Millisecond))
	r.writeMu.Unlock()
	if raced {
		if err := r.writeConfig(ctx); err != nil && ctx.Err() == nil {
			r.notifyError(err)
		}
	}
}

// writeFileSynced replaces path with data: a sibling written and synced,
// renamed over it, and the directory synced.
func writeFileSynced(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // in the run directory
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	// On Windows a reader that has the file open makes the rename fail for a moment.
	for attempt := 0; err == nil; attempt++ {
		if err = renameReplace(tmp, path); err == nil || attempt == 20 {
			break
		}
		err = nil
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(path))
}

// segmentComplete handles a hook post: the segment gets its catalog row and
// one SegmentClosed, and a verification pass follows.
func (r *rec) segmentComplete(ctx context.Context, req HookRequest) (int, error) {
	path := filepath.Clean(req.Path)
	rel, err := filepath.Rel(r.cfg.RecDir, path)
	if err != nil || !filepath.IsLocal(rel) {
		return 400, fmt.Errorf("%s is not under the recordings directory", req.Path)
	}
	seg, ok := parseSegment(r.cfg.RecDir, rel)
	if !ok || seg.part {
		return 400, fmt.Errorf("%s is not the final name of a segment", req.Path)
	}
	if string(seg.key.cam) != req.Cam || string(seg.key.profile) != req.Profile || seg.encrypted != req.Encrypted {
		return 400, fmt.Errorf("the camera, profile or encryption does not match %s", req.Path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 400, err
	}
	closed := SegmentClosed{Cam: seg.key.cam, Profile: seg.key.profile, Path: path, StartMs: req.StartMs, EndMs: req.StartMs + req.DurationMs, Encrypted: seg.encrypted}
	if err := r.cfg.Catalog.PutSegment(ctx, catalog.Segment{Cam: req.Cam, Profile: req.Profile, Path: seg.rel,
		StartMs: closed.StartMs, EndMs: closed.EndMs, Bytes: info.Size(), Encrypted: seg.encrypted, State: catalog.StateComplete}); err != nil {
		return 500, err
	}
	r.watch.segmentClosed(seg.key, time.Now())
	select {
	case r.closed <- closed:
	default:
		r.notifyError(fmt.Errorf("recorder: SegmentClosed of %s dropped: nobody reads Closed", seg.rel))
	}
	r.kickVerify()
	return 204, nil
}

func (r *rec) kickVerify() {
	select {
	case r.verifyKick <- struct{}{}:
	default:
	}
}

func (r *rec) notifyGap(g catalog.Gap) {
	if o, ok := r.obs.(GapObserver); ok {
		o.Gap(g)
	}
}

func (r *rec) notifyError(err error) {
	if o, ok := r.obs.(ErrorObserver); ok {
		o.Error(err)
	}
}

// closeGaps closes the stream's open gaps at endMs.
func (r *rec) closeGaps(ctx context.Context, k streamKey, endMs int64) error {
	gaps, err := r.cfg.Catalog.Gaps(ctx)
	if err != nil {
		return err
	}
	var open []catalog.Gap
	for _, g := range gaps {
		if g.Cam == string(k.cam) && g.Profile == string(k.profile) && g.EndMs == 0 {
			open = append(open, g)
		}
	}
	if len(open) == 0 {
		return nil
	}
	if err := r.cfg.Catalog.CloseGap(ctx, string(k.cam), string(k.profile), endMs); err != nil {
		return err
	}
	for _, g := range open {
		g.EndMs = max(endMs, g.StartMs)
		r.notifyGap(g)
	}
	return nil
}

// pollLoop polls the open parts and makes the date directories ahead.
func (r *rec) pollLoop(ctx context.Context) {
	defer r.wg.Done()
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	dirs := time.NewTicker(dirsInterval)
	defer dirs.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-poll.C:
			for _, f := range r.watch.poll(now) {
				r.obs.FirstPart(f.key.cam, f.key.profile, f.at)
				// Recording resumed: the stream's gap ends where its new segment starts.
				if err := r.closeGaps(ctx, f.key, f.segmentStart.UnixMilli()); err != nil && ctx.Err() == nil {
					r.notifyError(err)
				}
			}
		case now := <-dirs.C:
			for _, j := range r.appliedJobs() {
				if err := ensureStreamDirs(r.cfg.RecDir, j.Cam, j.Profile, now); err != nil {
					r.notifyError(err)
				}
			}
		}
	}
}

// verifyLoop runs a verification pass each time it is kicked: after the
// start, after each SegmentClosed and after each rewrite.
func (r *rec) verifyLoop(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.verifyKick:
			if _, err := r.verify.pass(ctx); err != nil && ctx.Err() == nil {
				r.notifyError(fmt.Errorf("recorder: verification: %w", err))
			}
		}
	}
}

// rewriteLoop runs the rewrites that reconcile left, one at a time, each
// once its stream's job gives it the recipients: recording has resumed by
// then, since New returns only after MediaMTX started.
func (r *rec) rewriteLoop(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.jobsKick:
		}
		for {
			p, scope := r.nextRewrite()
			if p == nil {
				break
			}
			r.runRewrite(ctx, p, scope)
			if ctx.Err() != nil {
				return
			}
		}
	}
}

// nextRewrite takes a pending rewrite whose stream has an encrypted job.
func (r *rec) nextRewrite() (*pendingRewrite, crypto.KeyScope) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.pending {
		if j, ok := r.jobs[p.seg.key]; ok && j.Scope != nil {
			r.pending = slices.Delete(r.pending, i, i+1)
			r.rewriting[p.seg.rel] = true
			return p, *j.Scope
		}
	}
	return nil, crypto.KeyScope{}
}

func (r *rec) isRewriting(rel string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rewriting[rel]
}

// runRewrite rewrites a salvage over its interim file (C1-R9), then updates
// its catalog row, its gap and the reconcile report.
func (r *rec) runRewrite(ctx context.Context, p *pendingRewrite, scope crypto.KeyScope) {
	defer func() {
		r.mu.Lock()
		delete(r.rewriting, p.seg.rel)
		r.mu.Unlock()
		r.kickVerify()
	}()
	rewriteHook()
	size, err := rewriteSegment(ctx, p, scope, &r.fileMu)
	if err != nil {
		if ctx.Err() == nil {
			r.notifyError(fmt.Errorf("recorder: rewrite %s: %w", p.seg.rel, err))
		}
		return
	}
	cat, k := r.cfg.Catalog, p.seg.key
	if err := cat.PutSegment(ctx, catalog.Segment{Cam: string(k.cam), Profile: string(k.profile), Path: p.seg.rel,
		StartMs: p.seg.start.UnixMilli(), EndMs: p.salvagedEndMs, Bytes: size, Encrypted: true, State: catalog.StateComplete}); err != nil {
		r.notifyError(err)
		return
	}
	if err := cat.MoveGapStart(ctx, string(k.cam), string(k.profile), p.interimEndMs, p.salvagedEndMs); err != nil {
		r.notifyError(err)
		return
	}
	if gaps, err := cat.Gaps(ctx); err == nil {
		for _, g := range gaps {
			if g.Cam == string(k.cam) && g.Profile == string(k.profile) && g.StartMs == p.salvagedEndMs {
				r.notifyGap(g)
			}
		}
	}
	r.mu.Lock()
	r.report.Rewritten++
	report := r.report
	r.mu.Unlock()
	if o, ok := r.obs.(ReconcileObserver); ok {
		o.Reconciled(report)
	}
}

// rewriteSegment writes the salvage into <name>.mp4.age.rewrite, ciphertext
// only, syncs it, renames it over the interim file, holding renameMu, and
// syncs the directory. A cut leaves the interim file as it was, and the
// sibling, which the next reconcile removes. It returns the new file's size.
func rewriteSegment(ctx context.Context, p *pendingRewrite, scope crypto.KeyScope, renameMu sync.Locker) (int64, error) {
	interim, err := os.Open(p.seg.abs)
	if err != nil {
		return 0, err
	}
	defer func() { _ = interim.Close() }()
	src, err := p.kept.Source(interim)
	if err != nil {
		return 0, err
	}
	tmp := p.seg.abs + suffixRewrite
	w, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // a segment's sibling
	if err != nil {
		return 0, err
	}
	fail := func(err error) (int64, error) {
		_ = w.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	bw := bufio.NewWriterSize(w, 1<<20)
	if err := crypto.Rewrite(ctx, src, p.keep, p.patches, scope, bw); err != nil {
		return fail(err)
	}
	if err := bw.Flush(); err != nil {
		return fail(err)
	}
	if err := w.Sync(); err != nil {
		return fail(err)
	}
	info, err := w.Stat()
	if err != nil {
		return fail(err)
	}
	if err := w.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	_ = interim.Close() // Windows renames over a file only when nobody has it open
	renameMu.Lock()
	err = renameReplace(tmp, p.seg.abs)
	renameMu.Unlock()
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return info.Size(), syncDir(filepath.Dir(p.seg.abs))
}
