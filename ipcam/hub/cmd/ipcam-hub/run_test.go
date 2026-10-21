package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/rtsptest"
)

// The test binary is ipcam-hub too: with mainEnv set, it runs main. The hook
// command that MediaMTX runs inherits the variable, so it is this binary
// with "hook segment-complete". As ipcam-hub, it reads the mount table that
// mountInfoEnv names, none when it is unset, and never the host's.
const (
	mainEnv      = "IPCAM_HUB_TEST_MAIN"
	mountInfoEnv = "IPCAM_HUB_TEST_MOUNTINFO"
)

func TestMain(m *testing.M) {
	if os.Getenv(mainEnv) == "1" {
		mountInfoPath = os.Getenv(mountInfoEnv)
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// hub is an ipcam-hub process that a test started.
type hub struct {
	cmd    *exec.Cmd
	events chan map[string]any
	stderr *bytes.Buffer
	done   chan struct{}
}

func startHub(t *testing.T, args ...string) *hub {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...) //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), mainEnv+"=1")
	inOwnGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	h := &hub{cmd: cmd, events: make(chan map[string]any, 1024), stderr: &bytes.Buffer{}, done: make(chan struct{})}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(h.done)
		defer close(h.events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			var e map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				e = map[string]any{"type": "not-json", "line": scanner.Text()}
			}
			h.events <- e
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-h.done
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("ipcam-hub stderr:\n%s", h.stderr.String())
		}
	})
	return h
}

// next returns the next event that match accepts, failing after d.
func (h *hub) next(t *testing.T, d time.Duration, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case e, ok := <-h.events:
			if !ok {
				t.Fatalf("ipcam-hub ended before %s", what)
			}
			if e["type"] == "not-json" {
				t.Fatalf("ipcam-hub printed a line that is not JSON: %q", e["line"])
			}
			if _, ok := e["atMs"].(float64); !ok {
				t.Fatalf("event without atMs: %v", e)
			}
			if match(e) {
				return e
			}
		case <-timeout:
			t.Fatalf("after %v: no %s", d, what)
		}
	}
}

func ofType(typ string) func(map[string]any) bool {
	return func(e map[string]any) bool { return e["type"] == typ }
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRun_EmitsReadyThenRecordingThenSegmentClosed: ipcam-hub run with the
// real MediaMTX reconciles (one reconcile event, seq 1 and final), is
// ready, reports the first part of each stream, then each 10 s segment as it
// closes, and then each closed segment that the background verification
// passed. Stopped, by SIGTERM on Linux and Ctrl+Break on Windows, it stops
// MediaMTX gracefully, reports a hub_stopped gap for each stream, and
// exits 0.
func TestRun_EmitsReadyThenRecordingThenSegmentClosed(t *testing.T) {
	bin := rtsptest.MediaMTX(t)
	srv := rtsptest.Start(t, rtsptest.MainH264(t))
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	identity := filepath.Join(dir, "ri_g.key")
	if err := os.WriteFile(identity, []byte(riG.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	config := filepath.Join(dir, "run.json")
	writeJSON(t, config, map[string]any{
		"dataDir":      dataDir,
		"mediamtx":     bin,
		"identityFile": identity,
		"jobs": []map[string]any{
			{"cam": "cam1", "profile": "main", "restreamUrl": srv.URL("cam1/main"), "segmentSecs": 10,
				"recipients": []string{riG.Recipient().String(), rk.Recipient().String()}},
			{"cam": "cam1", "profile": "sub", "restreamUrl": srv.URL("cam1/sub"), "segmentSecs": 10, "recipients": nil},
		},
	})
	h := startHub(t, "run", "--config", config)

	reconcile := h.next(t, 30*time.Second, "reconcile event", ofType("reconcile"))
	if reconcile["aborted"] != false || reconcile["seq"] != float64(1) || reconcile["final"] != true {
		t.Errorf("reconcile %v, want seq 1, final, not aborted", reconcile)
	}
	h.next(t, 30*time.Second, "ready event", ofType("ready"))
	recording := map[string]bool{}
	closed := map[string]string{} // path → stream
	// The background verification reports from its own goroutine: a
	// segment_verified can come before the other stream's first
	// segment_closed, or before its own, so it is noted whenever it comes.
	// A verification that finds an unreadable file or a torn part fails the
	// test at once.
	verified := map[string]bool{}
	verification := func(e map[string]any) bool {
		switch e["type"] {
		case "segment_verified":
			verified[e["path"].(string)] = true
		case "verify":
			if e["unreadable"] != float64(0) || e["tornParts"] != float64(0) {
				t.Fatalf("the background verification reported %v", e)
			}
		default:
			return false
		}
		return true
	}
	for len(closed) < 2 {
		e := h.next(t, 40*time.Second, "recording and segment_closed events", func(e map[string]any) bool {
			return e["type"] == "recording" || e["type"] == "segment_closed" || e["type"] == "segment_verified" || e["type"] == "verify"
		})
		if verification(e) {
			continue
		}
		stream := e["cam"].(string) + "/" + e["profile"].(string)
		switch e["type"] {
		case "recording":
			recording[stream] = true
		case "segment_closed":
			if !recording[stream] {
				t.Errorf("segment_closed of %s before its recording event", stream)
			}
			closed[e["path"].(string)] = stream
			if e["encrypted"] != (stream == "cam1/main") {
				t.Errorf("segment_closed %v: encrypted %v", e, e["encrypted"])
			}
		}
	}
	// Later segments keep closing and passing, so the wait has one deadline.
	deadline := time.Now().Add(30 * time.Second)
	for path := range closed {
		for !verified[path] {
			verification(h.next(t, time.Until(deadline), "segment_verified event of "+path, func(e map[string]any) bool {
				return e["type"] == "segment_verified" || e["type"] == "verify"
			}))
		}
	}
	for path, stream := range closed {
		if !strings.HasPrefix(path, filepath.Join(dataDir, "rec", filepath.FromSlash(stream))) {
			t.Errorf("segment %s of %s is not under the stream's directory", path, stream)
		}
		if stream == "cam1/main" {
			f, err := os.Open(path) //nolint:gosec // a segment of the test
			if err != nil {
				t.Fatal(err)
			}
			r, err := age.Decrypt(f, rk)
			if err != nil {
				t.Errorf("%s does not decrypt with RK: %v", path, err)
			} else if _, err := io.Copy(io.Discard, r); err != nil {
				t.Errorf("%s: %v", path, err)
			}
			_ = f.Close()
		}
	}
	h.next(t, 5*time.Second, "status event", ofType("status"))
	if _, err := os.Stat(filepath.Join(dataDir, "state", "run", "status.json")); err != nil {
		t.Errorf("no status file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state", "catalog.db")); err != nil {
		t.Errorf("no catalog: %v", err)
	}

	if err := stopHub(h); err != nil {
		t.Fatalf("stop ipcam-hub: %v", err)
	}
	stoppedGaps := map[string]bool{}
	for e := range h.events {
		if e["type"] == "gap" && e["reason"] == "hub_stopped" && e["endMs"] == float64(0) {
			stoppedGaps[e["cam"].(string)+"/"+e["profile"].(string)] = true
		}
		if e["type"] == "gap" && e["reason"] == "restart" {
			t.Errorf("a restart gap after a graceful stop: %v", e)
		}
		if e["type"] == "error" {
			t.Errorf("an error after the stop: %v", e["message"])
		}
	}
	<-h.done
	if err := h.cmd.Wait(); err != nil {
		t.Errorf("ipcam-hub after the stop: %v, want exit 0", err)
	}
	if !stoppedGaps["cam1/main"] || !stoppedGaps["cam1/sub"] {
		t.Errorf("hub_stopped gaps for %v, want both streams", stoppedGaps)
	}
}

// minimalFMP4 is an fMP4 recording with one part of one sample, cut short
// by cut bytes.
func minimalFMP4(t *testing.T, cut int) []byte {
	t.Helper()
	var buf seekablebuffer.Buffer
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{
		SPS: []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20},
		PPS: []byte{0x08, 0x06, 0x07, 0x08},
	}}}}
	if err := init.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	part := fmp4.Part{Tracks: []*fmp4.PartTrack{{ID: 1, Samples: []*fmp4.Sample{{Duration: 4500, Payload: bytes.Repeat([]byte{1}, 5000)}}}}}
	if err := part.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()[:buf.Len()-cut]
}

// TestRun_ExitCode3WhenReconcileAborts (HUB-09): when more than half of the
// segments would be lost, ipcam-hub reports the aborted reconcile, changes
// nothing, starts no MediaMTX and exits 3.
func TestRun_ExitCode3WhenReconcileAborts(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	files := map[string][]byte{}
	for i, cam := range []string{"cam1", "cam2", "cam3", "cam4", "cam5"} {
		data := bytes.Repeat([]byte("not an fMP4 file "), 300)
		if i < 2 {
			data = minimalFMP4(t, 100)
		}
		path := filepath.Join(dataDir, "rec", cam, "main", "2026-10-20", "100000_000000.mp4.part")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		files[path] = data
	}
	config := filepath.Join(dir, "run.json")
	writeJSON(t, config, map[string]any{"dataDir": dataDir, "mediamtx": filepath.Join(dir, "no-mediamtx"), "jobs": []any{}})
	h := startHub(t, "run", "--config", config)

	e := h.next(t, 30*time.Second, "reconcile event", ofType("reconcile"))
	if e["aborted"] != true || e["seq"] != float64(1) || e["final"] != true {
		t.Errorf("reconcile %v, want aborted, seq 1 and final", e)
	}
	<-h.done
	err := h.cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Errorf("ipcam-hub ended with %v, want exit code 3", err)
	}
	for path, data := range files {
		got, err := os.ReadFile(path) //nolint:gosec // a file of the test
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("%s changed (%v)", path, err)
		}
	}
}

// TestRun_ExitCode1OnBadConfig: a configuration that cannot run is an error
// event and exit code 1.
func TestRun_ExitCode1OnBadConfig(t *testing.T) {
	dir := t.TempDir()
	for name, cfg := range map[string]any{
		"no data directory": map[string]any{"jobs": []any{}},
		"an unknown key":    map[string]any{"dataDir": filepath.Join(dir, "d1"), "jobz": []any{}},
		"one recipient":     map[string]any{"dataDir": filepath.Join(dir, "d2"), "jobs": []map[string]any{{"cam": "c", "profile": "main", "restreamUrl": "rtsp://127.0.0.1:1/c", "recipients": []string{"age1x"}}}},
	} {
		config := filepath.Join(dir, "run.json")
		writeJSON(t, config, cfg)
		h := startHub(t, "run", "--config", config)
		e := h.next(t, 10*time.Second, "error event", ofType("error"))
		if strings.Contains(e["message"].(string), "AGE-SECRET-KEY-") {
			t.Errorf("%s: the error quotes a key", name)
		}
		<-h.done
		err := h.cmd.Wait()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Errorf("%s: ipcam-hub ended with %v, want exit code 1", name, err)
		}
	}
}

// mountInfoLine is a line of /proc/self/mountinfo that mounts the device
// devnum ("major:minor") from source at mountPoint, with the mount point
// escaped as the kernel escapes it.
func mountInfoLine(id int, devnum, mountPoint, source string) string {
	escaped := strings.NewReplacer("\\", `\134`, " ", `\040`, "\t", `\011`, "\n", `\012`).Replace(mountPoint)
	return fmt.Sprintf("%d 1 %s / %s rw,noatime shared:%d - ext4 %s rw\n", id, devnum, escaped, id, source)
}

// countFiles counts the regular files under dir, 0 when dir is absent.
func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return n
}

// TestRun_RefusesSdCardUntilConfirmed (HUB-17, T40, G16): while rec/ is on
// an SD card and run.json has no sdCardConfirmed, ipcam-hub reports
// sd_card_unconfirmed and exits 4 before it changes anything under its data
// directory: rec/ holds no file, and no reconcile or MediaMTX ran. With
// sdCardConfirmed true, it records.
func TestRun_RefusesSdCardUntilConfirmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the SD-card check reads Linux's mount table (T40); Windows has none, and its gate never closes")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "hub data")
	mountinfo := filepath.Join(dir, "mountinfo")
	table := mountInfoLine(21, "259:2", "/", "/dev/nvme0n1p2") + mountInfoLine(22, "179:2", dataDir, "/dev/mmcblk0p2")
	if err := os.WriteFile(mountinfo, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mountInfoEnv, mountinfo)
	job := map[string]any{"cam": "cam1", "profile": "main", "segmentSecs": 10, "recipients": nil}

	t.Run("unconfirmed", func(t *testing.T) {
		config := filepath.Join(dir, "run-unconfirmed.json")
		job["restreamUrl"] = "rtsp://127.0.0.1:1/cam1/main"
		writeJSON(t, config, map[string]any{"dataDir": dataDir, "mediamtx": filepath.Join(dir, "no-mediamtx"), "jobs": []any{job}})
		h := startHub(t, "run", "--config", config)
		var types []any
		for e := range h.events {
			types = append(types, e["type"])
		}
		<-h.done
		err := h.cmd.Wait()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 4 {
			t.Errorf("ipcam-hub ended with %v, want exit code 4", err)
		}
		if !slices.Equal(types, []any{"sd_card_unconfirmed"}) {
			t.Errorf("events %v, want sd_card_unconfirmed alone", types)
		}
		if n := countFiles(t, filepath.Join(dataDir, "rec")); n != 0 {
			t.Errorf("rec/ holds %d files", n)
		}
		if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the data directory is there (%v): ipcam-hub changed it before the check", err)
		}
	})

	t.Run("confirmed", func(t *testing.T) {
		bin := rtsptest.MediaMTX(t)
		srv := rtsptest.Start(t, rtsptest.MainH264(t))
		config := filepath.Join(dir, "run-confirmed.json")
		job["restreamUrl"] = srv.URL("cam1/main")
		writeJSON(t, config, map[string]any{"dataDir": dataDir, "mediamtx": bin, "sdCardConfirmed": true, "jobs": []any{job}})
		h := startHub(t, "run", "--config", config)
		h.next(t, 40*time.Second, "recording event", ofType("recording"))
		if n := countFiles(t, filepath.Join(dataDir, "rec")); n == 0 {
			t.Error("rec/ holds no file after the recording event")
		}
		if err := stopHub(h); err != nil {
			t.Fatalf("stop ipcam-hub: %v", err)
		}
		for e := range h.events {
			if e["type"] == "sd_card_unconfirmed" || e["type"] == "error" {
				t.Errorf("event %v", e)
			}
		}
		<-h.done
		if err := h.cmd.Wait(); err != nil {
			t.Errorf("ipcam-hub after the stop: %v, want exit 0", err)
		}
	})
}

// TestLoadRunConfig_EncryptedJobsNeedTheirRIg: a run.json with an encrypted
// job is refused without an identityFile, and with one whose recipient is
// not the job's RI_g: reconcile and the verification could read none of its
// files, and every cut would count toward HUB-09's abort. The errors quote
// no key, public or secret.
func TestLoadRunConfig_EncryptedJobsNeedTheirRIg(t *testing.T) {
	dir := t.TempDir()
	ids := make([]*age.X25519Identity, 3)
	for i := range ids {
		id, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	riG, rk, other := ids[0], ids[1], ids[2]
	identity := filepath.Join(dir, "ri_g.key")
	if err := os.WriteFile(identity, []byte(riG.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherIdentity := filepath.Join(dir, "other.key")
	if err := os.WriteFile(otherIdentity, []byte(other.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := func(recipients any) map[string]any {
		return map[string]any{"cam": "cam1", "profile": "main", "restreamUrl": "rtsp://127.0.0.1:1/cam1/main", "recipients": recipients}
	}
	encrypted := job([]string{riG.Recipient().String(), rk.Recipient().String()})
	for _, tc := range []struct {
		name     string
		identity string
		jobs     []map[string]any
		ok       bool
	}{
		{"encrypted, no identity", "", []map[string]any{job(nil), encrypted}, false},
		{"encrypted, another identity", otherIdentity, []map[string]any{encrypted}, false},
		{"encrypted, its RI_g", identity, []map[string]any{encrypted}, true},
		{"clear, no identity", "", []map[string]any{job(nil)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "run.json")
			writeJSON(t, config, map[string]any{"dataDir": filepath.Join(dir, "data"), "identityFile": tc.identity, "jobs": tc.jobs})
			_, _, id, err := loadRunConfig(config)
			if tc.ok {
				if err != nil {
					t.Fatalf("loadRunConfig: %v", err)
				}
				if (tc.identity != "") != (id != nil) {
					t.Errorf("identity %v, want one exactly when the configuration names it", id)
				}
				return
			}
			if err == nil {
				t.Fatal("loadRunConfig accepted it")
			}
			for _, key := range []string{"AGE-SECRET-KEY-", "age1"} {
				if strings.Contains(err.Error(), key) {
					t.Errorf("the error %q quotes a key", err)
				}
			}
		})
	}
}
