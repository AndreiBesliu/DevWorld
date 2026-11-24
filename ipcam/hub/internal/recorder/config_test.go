package recorder

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

var testHookCmd = []string{"/opt/ipcam/ipcam-hub", "hook", "segment-complete"}

func parseConfig(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var c map[string]any
	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatalf("the generated configuration is not the JSON form of YAML: %v\n%s", err, out)
	}
	return c
}

func configPath(t *testing.T, c map[string]any, name string) map[string]any {
	t.Helper()
	paths, _ := c["paths"].(map[string]any)
	p, ok := paths[name].(map[string]any)
	if !ok {
		t.Fatalf("no path %q in %v", name, paths)
	}
	return p
}

func TestGenerateConfig_ServersOffAndPullOverTCP(t *testing.T) {
	recDir := filepath.Join(t.TempDir(), "rec")
	main := NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", nil)
	sub := NewJob("cam2", "sub", "rtsp://127.0.0.1:8554/cam2/sub", nil)
	sub.SegmentSecs = 10
	out, err := GenerateConfig([]Job{main, sub}, testHookCmd, recDir)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	c := parseConfig(t, out)
	for _, server := range []string{"rtsp", "rtmp", "hls", "webrtc", "srt", "moq", "playback", "api", "metrics", "pprof"} {
		if c[server] != false {
			t.Errorf("%s = %v, want false", server, c[server])
		}
	}
	if defaults, ok := c["pathDefaults"]; ok && !reflect.DeepEqual(defaults, map[string]any{}) {
		t.Errorf("pathDefaults = %v; the generator sets every key per path", defaults)
	}
	if paths, _ := c["paths"].(map[string]any); len(paths) != 2 {
		t.Errorf("paths = %v, want cam1/main and cam2/sub", paths)
	}
	recordPath := filepath.ToSlash(recDir) + "/%path/%Y-%m-%d/%H%M%S_%f"
	want := map[string]any{
		"source":                     "rtsp://127.0.0.1:8554/cam1/main",
		"rtspTransport":              "tcp",
		"record":                     true,
		"recordPath":                 recordPath,
		"recordFormat":               "fmp4",
		"recordPartDuration":         "1s",
		"recordSegmentDuration":      "300s",
		"recordDeleteAfter":          "0s",
		"runOnRecordSegmentComplete": "'/opt/ipcam/ipcam-hub' 'hook' 'segment-complete'",
	}
	if got := configPath(t, c, "cam1/main"); !reflect.DeepEqual(got, want) {
		t.Errorf("path cam1/main = %v\nwant %v", got, want)
	}
	if !strings.Contains(recordPath, "%path") {
		t.Errorf("recordPath lacks %s, which MediaMTX requires", "%path")
	}
	if got := configPath(t, c, "cam2/sub")["recordSegmentDuration"]; got != "10s" {
		t.Errorf("cam2/sub recordSegmentDuration = %v, want 10s", got)
	}
}

// TestGenerateConfig_ScopedJobHasTwoRecipientsNoSecret (C11): a scoped job
// lists exactly RI_g and RK, and nothing in the file is a secret key.
func TestGenerateConfig_ScopedJobHasTwoRecipientsNoSecret(t *testing.T) {
	scope, riG, rk := newTestScope(t)
	out, err := GenerateConfig([]Job{NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", &scope)}, testHookCmd, filepath.Join(t.TempDir(), "rec"))
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if strings.Contains(string(out), "AGE-SECRET-KEY-") {
		t.Fatal("the configuration holds a secret key")
	}
	if n := len(regexp.MustCompile(`age1[0-9a-z]+`).FindAll(out, -1)); n != 2 {
		t.Errorf("%d age1 strings in the configuration, want 2", n)
	}
	got := configPath(t, parseConfig(t, out), "cam1/main")["recordAgeRecipients"]
	want := []any{riG.Recipient().String(), rk.Recipient().String()}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recordAgeRecipients = %v, want RI_g then RK", got)
	}

	if _, err := GenerateConfig([]Job{NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", &crypto.KeyScope{})}, testHookCmd, t.TempDir()); err == nil {
		t.Error("GenerateConfig accepted a scope without both keys")
	}
}

func TestGenerateConfig_PadLowBitrateFollowsJob(t *testing.T) {
	scope, _, _ := newTestScope(t)
	for _, pad := range []bool{true, false} {
		j := NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", &scope)
		j.PadLowBitrate = pad
		out, err := GenerateConfig([]Job{j}, testHookCmd, t.TempDir())
		if err != nil {
			t.Fatalf("GenerateConfig: %v", err)
		}
		if got := configPath(t, parseConfig(t, out), "cam1/main")["recordAgePadLowBitrate"]; got != pad {
			t.Errorf("PadLowBitrate %v: recordAgePadLowBitrate = %v", pad, got)
		}
	}
	// A clear job has neither key: MediaMTX refuses recipients that are
	// empty or null, and the padding only concerns encrypted parts.
	out, err := GenerateConfig([]Job{NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", nil)}, testHookCmd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := configPath(t, parseConfig(t, out), "cam1/main")
	for _, key := range []string{"recordAgeRecipients", "recordAgePadLowBitrate"} {
		if _, ok := p[key]; ok {
			t.Errorf("a clear job has %s", key)
		}
	}
}

func TestGenerateConfig_RejectsWhatMediaMTXWouldMisread(t *testing.T) {
	recDir := t.TempDir()
	good := NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", nil)
	bad := func(edit func(*Job)) []Job {
		j := good
		edit(&j)
		return []Job{j}
	}
	for _, tc := range []struct {
		name   string
		jobs   []Job
		hook   []string
		recDir string
	}{
		{"a relative recDir", []Job{good}, testHookCmd, "rec"},
		{"a % in recDir", []Job{good}, testHookCmd, filepath.Join(recDir, "100%")},
		{"no hook command", []Job{good}, nil, recDir},
		{"a $ in the hook command", []Job{good}, []string{"/opt/$HOME/ipcam-hub", "hook", "segment-complete"}, recDir},
		{"the same stream twice", []Job{good, good}, testHookCmd, recDir},
		{"an empty camera", bad(func(j *Job) { j.Cam = "" }), testHookCmd, recDir},
		{"a camera with a slash", bad(func(j *Job) { j.Cam = "a/b" }), testHookCmd, recDir},
		{"a camera ..", bad(func(j *Job) { j.Cam = ".." }), testHookCmd, recDir},
		{"another profile", bad(func(j *Job) { j.Profile = "third" }), testHookCmd, recDir},
		{"no restream URL", bad(func(j *Job) { j.RestreamURL = "" }), testHookCmd, recDir},
		{"no segment length", bad(func(j *Job) { j.SegmentSecs = 0 }), testHookCmd, recDir},
		{"parts longer than segments", bad(func(j *Job) { j.PartSecs = 400 }), testHookCmd, recDir},
	} {
		if _, err := GenerateConfig(tc.jobs, tc.hook, tc.recDir); err == nil {
			t.Errorf("%s: GenerateConfig accepted it", tc.name)
		}
	}
}
