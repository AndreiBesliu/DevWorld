package recorder

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
)

// hookEnv is the environment that MediaMTX gives its hook command for a
// segment of cam/profile at path, plus the hook's address and token.
func hookEnv(child fakeChildDump, cam, profile, path string, start time.Time, seconds string) map[string]string {
	env := map[string]string{}
	for _, kv := range child.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && (k == HookAddrEnv || k == HookTokenEnv) {
			env[k] = v
		}
	}
	env["MTX_PATH"] = cam + "/" + profile
	env["MTX_SEGMENT_PATH"] = path
	env["MTX_SEGMENT_START"] = strconv.FormatInt(start.UnixMilli(), 10)
	env["MTX_SEGMENT_DURATION"] = seconds
	return env
}

func receive(t *testing.T, r *rec) SegmentClosed {
	t.Helper()
	select {
	case s := <-r.Closed(t.Context()):
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("no SegmentClosed")
	}
	return SegmentClosed{}
}

// TestHook_ForwardsSegmentClosed (G05): the hook command that MediaMTX runs
// when a segment is complete posts it to the recorder with the token from
// its environment, which gives one SegmentClosed and one catalog row; a
// wrong token gets HTTP 401 and changes nothing.
func TestHook_ForwardsSegmentClosed(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "child.json")
	r, cfg := startFake(t, "dump:"+dump, Config{})
	child := readDump(t, dump)
	ctx := t.Context()

	start := time.Date(2026, 10, 20, 10, 0, 0, 123_000_000, time.Local)
	path := filepath.Join(cfg.RecDir, "cam1", "main", segmentName(start)) + ".age"
	writeFile(t, path, []byte("ciphertext"))
	env := hookEnv(child, "cam1", "main", path, start, "10.5")

	// The hook command itself, as MediaMTX runs it: this test binary with
	// "hook segment-complete" plays ipcam-hub.
	cmd := exec.CommandContext(ctx, os.Args[0], "hook", "segment-complete") //nolint:gosec // the test binary itself
	cmd.Env = withoutEnv(os.Environ(), fakeChildEnv)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the hook command: %v: %s", err, out)
	}
	want := SegmentClosed{Cam: "cam1", Profile: "main", Path: path, StartMs: start.UnixMilli(), EndMs: start.UnixMilli() + 10500, Encrypted: true}
	if got := receive(t, r); got != want {
		t.Errorf("SegmentClosed = %+v, want %+v", got, want)
	}
	segs, err := cfg.Catalog.Segments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantRow := catalog.Segment{Cam: "cam1", Profile: "main", Path: "cam1/main/" + segmentName(start) + ".age",
		StartMs: want.StartMs, EndMs: want.EndMs, Bytes: int64(len("ciphertext")), Encrypted: true, State: catalog.StateComplete}
	if len(segs) != 1 {
		t.Fatalf("%d catalog rows, want 1", len(segs))
	}
	wantRow.ID = segs[0].ID
	if segs[0].ID == "" || segs[0] != wantRow {
		t.Errorf("catalog row %+v, want %+v", segs[0], wantRow)
	}

	// A wrong token: 401, from the client and on the wire.
	env[HookTokenEnv] = strings.Repeat("0", len(env[HookTokenEnv]))
	err = PostSegmentComplete(ctx, func(k string) string { return env[k] })
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("PostSegmentComplete with a wrong token = %v, want an HTTP 401 error", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+env[HookAddrEnv]+HookPath, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer wrong")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong token got HTTP %d, want 401", res.StatusCode)
	}
	select {
	case s := <-r.Closed(ctx):
		t.Errorf("a refused hook gave %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
	if segs, _ := cfg.Catalog.Segments(ctx); len(segs) != 1 {
		t.Errorf("after the refused hooks: %d rows, want 1", len(segs))
	}

	// A path outside the recordings is refused too, with the right token.
	child2 := hookEnv(child, "cam1", "main", filepath.Join(t.TempDir(), "x.mp4"), start, "1")
	if err := PostSegmentComplete(ctx, func(k string) string { return child2[k] }); err == nil {
		t.Error("a segment outside the recordings directory was accepted")
	}
}

// TestHook_TokenIsNewForEachChild (G05): every MediaMTX child that the
// recorder starts gets a hook token of its own. Once a child is started
// again, a hook with the previous child's token gets HTTP 401, and one with
// the new child's token is accepted.
func TestHook_TokenIsNewForEachChild(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "child.json")
	obs := &testObserver{}
	r, cfg := startFake(t, "dump:"+dump, Config{Observer: obs})
	first := readDump(t, dump)
	if err := os.Remove(dump); err != nil {
		t.Fatal(err)
	}
	killChild(t, r)
	waitFor(t, 10*time.Second, "the child was not started again", func() bool { return len(obs.snapshot().restarts) >= 1 })
	second := readDump(t, dump)
	if second.PID == first.PID {
		t.Fatal("the dump is the first child's")
	}

	start := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(cfg.RecDir, "cam1", "main", segmentName(start))
	writeFile(t, path, []byte("a segment"))
	previous, current := hookEnv(first, "cam1", "main", path, start, "1"), hookEnv(second, "cam1", "main", path, start, "1")
	if previous[HookTokenEnv] == current[HookTokenEnv] {
		t.Fatal("the new child got the previous child's token")
	}
	err := PostSegmentComplete(t.Context(), func(k string) string { return previous[k] })
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("a hook with the previous child's token = %v, want an HTTP 401 error", err)
	}
	if err := PostSegmentComplete(t.Context(), func(k string) string { return current[k] }); err != nil {
		t.Fatalf("a hook with the new child's token: %v", err)
	}
	if got := receive(t, r); got.Path != path {
		t.Errorf("SegmentClosed %+v, want %s", got, path)
	}
}
