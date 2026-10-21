package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

// statusInterval is how often each job's status goes out as an event and
// into the status file.
const statusInterval = time.Second

// mountInfoPath is the mount table that tells whether rec/ is on an SD card
// (T40); tests replace it, and Windows has none.
var mountInfoPath = health.SystemRoots.MountInfo

// runConfig is run.json.
type runConfig struct {
	DataDir string `json:"dataDir"`
	// MediaMTX is the fork's binary; empty is the file mediamtx (mediamtx.exe
	// on Windows) next to ipcam-hub.
	MediaMTX string `json:"mediamtx"`
	// IdentityFile is RI_g's identity, a 0600 file, for reconcile and the
	// verification of encrypted segments; empty when every job is clear.
	IdentityFile string `json:"identityFile"`
	// SDCardConfirmed is the owner's answer to the SD-card warning (T40):
	// without it, ipcam-hub writes nothing while rec/ is on an SD card.
	SDCardConfirmed bool        `json:"sdCardConfirmed"`
	Jobs            []runJobDef `json:"jobs"`
}

type runJobDef struct {
	Cam         string `json:"cam"`
	Profile     string `json:"profile"`
	RestreamURL string `json:"restreamUrl"`
	SegmentSecs int    `json:"segmentSecs"` // 0: 300 s
	// Recipients are RI_g then RK; null or absent records in clear. Any other
	// count, an empty list included, is an error: a job meant to be
	// encrypted never records in clear.
	Recipients    *[]string `json:"recipients"`
	PadLowBitrate *bool     `json:"padLowBitrate"` // absent: true (O17)
}

// loadRunConfig reads run.json, its jobs and the identity that it names, if
// any. An encrypted job needs the identity of its RI_g: without it,
// reconcile and the verification could read none of the job's files. Its
// errors quote no key.
func loadRunConfig(path string) (runConfig, []recorder.Job, *age.X25519Identity, error) {
	var cfg runConfig
	b, err := os.ReadFile(path) //nolint:gosec // the configuration that the caller named
	if err != nil {
		return cfg, nil, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.DataDir == "" {
		return cfg, nil, nil, fmt.Errorf("%s: no dataDir", path)
	}
	var id *age.X25519Identity
	if cfg.IdentityFile != "" {
		if id, err = loadIdentity(cfg.IdentityFile); err != nil {
			return cfg, nil, nil, err
		}
	}
	jobs := make([]recorder.Job, 0, len(cfg.Jobs))
	for i, d := range cfg.Jobs {
		var scope *crypto.KeyScope
		if d.Recipients != nil {
			if len(*d.Recipients) != 2 {
				return cfg, nil, nil, fmt.Errorf("%s: job %d: recipients must be RI_g and RK, not %d entries", path, i+1, len(*d.Recipients))
			}
			s, err := crypto.ParseKeyScope((*d.Recipients)[0], (*d.Recipients)[1])
			if err != nil {
				return cfg, nil, nil, fmt.Errorf("%s: job %d: %w", path, i+1, err)
			}
			switch {
			case id == nil:
				return cfg, nil, nil, fmt.Errorf("%s: job %d records encrypted, and no identityFile holds the identity of its RI_g", path, i+1)
			case id.Recipient().String() != s.RecordingIdentity.String():
				return cfg, nil, nil, fmt.Errorf("%s: job %d: the identityFile's identity is not the one of the job's RI_g", path, i+1)
			}
			scope = &s
		}
		j := recorder.NewJob(recorder.CameraID(d.Cam), recorder.Profile(d.Profile), d.RestreamURL, scope)
		if d.SegmentSecs != 0 {
			j.SegmentSecs = d.SegmentSecs
		}
		if d.PadLowBitrate != nil {
			j.PadLowBitrate = *d.PadLowBitrate
		}
		jobs = append(jobs, j)
	}
	return cfg, jobs, id, nil
}

// loadIdentity reads RI_g from a file that only its owner may read.
func loadIdentity(path string) (*age.X25519Identity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("the identity file %s is readable by others (mode %o); it must be 0600", path, info.Mode().Perm())
	}
	f, err := os.Open(path) //nolint:gosec // the identity file that the configuration names
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ids, err := age.ParseIdentities(f)
	if err != nil || len(ids) != 1 {
		// age's errors may quote the file's lines.
		return nil, fmt.Errorf("the identity file %s does not hold exactly one age identity", path)
	}
	id, ok := ids[0].(*age.X25519Identity)
	if !ok {
		return nil, fmt.Errorf("the identity file %s does not hold an X25519 identity", path)
	}
	return id, nil
}

// runCommand is "ipcam-hub run": it reconciles, records the jobs of
// run.json and reports, until SIGTERM, Ctrl-C or Ctrl-Break.
func runCommand(args []string, stdout, stderr io.Writer) int {
	ev := &events{out: stdout}
	fail := func(err error) int {
		ev.emit("error", map[string]any{"message": err.Error()})
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "the run configuration (run.json)")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *configPath == "" || flags.NArg() != 0 {
		return fail(errors.New("usage: ipcam-hub run --config <run.json>"))
	}
	cfg, jobs, id, err := loadRunConfig(*configPath)
	if err != nil {
		return fail(err)
	}
	// HUB-17, T40: no segment goes to an SD card before the owner confirmed
	// the wear warning, so nothing under the data directory changes first:
	// no reconcile, no catalog, no MediaMTX.
	if !cfg.SDCardConfirmed && health.IsSdCard(mountInfoPath, filepath.Join(cfg.DataDir, "rec")) {
		ev.emit("sd_card_unconfirmed", nil)
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: rec/ is on an SD card, and run.json does not confirm the SD-card warning (sdCardConfirmed)")
		return exitSdCardUnconfirmed
	}

	// <data>/state/catalog.db, <data>/state/run/, <data>/rec/, and <data>/tmp/,
	// emptied at every start (§4.9).
	stateDir := filepath.Join(cfg.DataDir, "state")
	runDir := filepath.Join(stateDir, "run")
	tmpDir := filepath.Join(cfg.DataDir, "tmp")
	if err := os.RemoveAll(tmpDir); err != nil {
		return fail(err)
	}
	for _, dir := range []string{runDir, tmpDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fail(err)
		}
	}
	mediamtx := cfg.MediaMTX
	if mediamtx == "" {
		self, err := os.Executable()
		if err != nil {
			return fail(err)
		}
		mediamtx = filepath.Join(filepath.Dir(self), "mediamtx")
		if runtime.GOOS == "windows" {
			mediamtx += ".exe"
		}
	}
	rc := recorder.Config{BinaryPath: mediamtx, RunDir: runDir, RecDir: filepath.Join(cfg.DataDir, "rec")}
	if id != nil {
		rc.Tail, rc.Recording = crypto.NewTailSealer(id), crypto.NewRecording(id)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cat, err := catalog.Open(ctx, filepath.Join(stateDir, "catalog.db"))
	if err != nil {
		return fail(err)
	}
	defer func() { _ = cat.Close() }()
	rc.Catalog = cat
	rc.Observer = &observer{ev: ev, stderr: stderr}

	// New gives the reconcile event out (observer.Reconciled), as later updates.
	rec, _, err := recorder.New(ctx, rc)
	if errors.Is(err, recorder.ErrReconcileAborted) {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitAborted
	}
	if err != nil {
		return fail(err)
	}
	if err := rec.Apply(ctx, jobs); err != nil {
		stop()
		for range rec.Closed(context.Background()) {
		}
		return fail(err)
	}
	ready := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		ready = append(ready, map[string]any{"cam": j.Cam, "profile": j.Profile, "encrypted": j.Scope != nil, "padLowBitrate": j.PadLowBitrate})
	}
	ev.emit("ready", map[string]any{"jobs": ready})

	tick := time.NewTicker(statusInterval)
	defer tick.Stop()
	closed := rec.Closed(ctx)
	for {
		select {
		case s, ok := <-closed:
			if !ok {
				return exitOK // the recorder stopped after SIGTERM or Ctrl-C
			}
			ev.emit("segment_closed", map[string]any{"cam": s.Cam, "profile": s.Profile, "path": s.Path,
				"startMs": s.StartMs, "endMs": s.EndMs, "encrypted": s.Encrypted})
		case <-tick.C:
			if ctx.Err() == nil {
				writeStatus(ctx, rec, jobs, ev, runDir)
			}
		}
	}
}

// writeStatus emits each job's status and writes them to status.json.
func writeStatus(ctx context.Context, rec recorder.Recorder, jobs []recorder.Job, ev *events, runDir string) {
	all := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		fields := statusFields(j.Cam, j.Profile, rec.Status(ctx, j.Cam, j.Profile))
		ev.emit("status", fields)
		all = append(all, fields)
	}
	b, err := json.Marshal(map[string]any{"atMs": time.Now().UnixMilli(), "jobs": all})
	if err != nil {
		return
	}
	path := filepath.Join(runDir, "status.json")
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		ev.emit("error", map[string]any{"message": err.Error()})
		return
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		ev.emit("error", map[string]any{"message": err.Error()})
	}
}
