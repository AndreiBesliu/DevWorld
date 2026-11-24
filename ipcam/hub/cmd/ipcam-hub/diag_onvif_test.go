package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	onvif "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events/onviftest"
)

// onvifTestCamera is a simulated camera with sentinel credentials and
// serial, assembled at run time (P46), and its stdin line as id.
func onvifTestCamera(t *testing.T, id string, cfg onviftest.Config) (*onviftest.Sim, string, []string) {
	t.Helper()
	cfg.User, cfg.Pass = "SENT"+"INEL-user", "SENT"+"INEL-pass"
	cfg.Serial = "SENT" + "INEL-serial-0042"
	sim := onviftest.New(cfg)
	t.Cleanup(sim.Close)
	u, err := url.Parse(sim.XAddr())
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	return sim, cameraLine(t, id, sim.XAddr(), cfg.User, cfg.Pass), []string{host, port, cfg.User, cfg.Pass, cfg.Serial, "SENT" + "INEL", "http://", "onvif/"}
}

// cameraLine is a camera line of diag onvif's stdin.
func cameraLine(t *testing.T, id, xaddr, user, pass string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"id": id, "xaddr": xaddr, "user": user, "pass": pass})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// fastOnvif shortens the subscriptions' timing for the test.
func fastOnvif(t *testing.T) {
	t.Helper()
	saved := onvifOptions
	onvifOptions = onvif.Options{Term: 2 * time.Second, RenewAt: 1600 * time.Millisecond,
		UnhealthyAfter: 300 * time.Millisecond, LostAfter: 1500 * time.Millisecond, AuthPause: 300 * time.Millisecond}
	t.Cleanup(func() { onvifOptions = saved })
}

// runOnvif runs diag onvif with args and stdin, and returns its exit code,
// its output lines decoded, and its raw stdout and stderr.
func runOnvif(t *testing.T, stdin string, args ...string) (int, []map[string]any, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := diagCommand(append([]string{"onvif"}, args...), strings.NewReader(stdin), &stdout, &stderr)
	return code, decodeLines(t, stdout.String()), stdout.String(), stderr.String()
}

// decodeLines decodes JSON lines.
func decodeLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("diag onvif printed %q, not JSON", line)
		}
		lines = append(lines, m)
	}
	return lines
}

// byCam indexes lines by their cam and type.
func byCam(lines []map[string]any) map[string]map[string]any {
	m := map[string]map[string]any{}
	for _, l := range lines {
		m[l["cam"].(string)+"/"+l["type"].(string)] = l
	}
	return m
}

// assertNoSecrets fails when out holds any of secrets.
func assertNoSecrets(t *testing.T, what, out string, secrets []string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("%s holds %q:\n%s", what, s, out)
		}
	}
}

// TestDiagOnvif_ProbeLineHasNoAddressOrSerial (P41, P42, PH0-22): --probe
// prints brand, model, PullPoint support, clock offset and login method per
// camera, and onvif-port for a camera whose port does not answer; no line,
// and nothing on stderr, holds an address, port, user, password, URL or the
// serial number.
func TestDiagOnvif_ProbeLineHasNoAddressOrSerial(t *testing.T) {
	_, line1, secrets := onvifTestCamera(t, "real-1", onviftest.Config{Brand: "Hikvision", Model: "DS-2CD1043"})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	_, closedPort, _ := net.SplitHostPort(closed)
	line2 := cameraLine(t, "real-2", "http://"+closed+"/onvif/device_service", "SENT"+"INEL-user", "SENT"+"INEL-pass")
	code, lines, stdout, stderr := runOnvif(t, line1+line2, "--probe")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := byCam(lines)
	p := got["real-1/probe"]
	if p == nil || p["brand"] != "Hikvision" || p["model"] != "DS-2CD1043" || p["eventsService"] != true ||
		p["pullPoint"] != true || p["auth"] != onvif.AuthWSSEDigest || p["clockOffsetMs"] == nil {
		t.Errorf("real-1's probe = %v", p)
	}
	if e := got["real-2/probe"]; e == nil || e["error"] != onvif.ReasonOnvifPort || len(e) != 3 {
		t.Errorf("real-2's probe = %v; want {type, cam, error: onvif-port}", e)
	}
	if len(lines) != 2 {
		t.Errorf("%d lines; want 2", len(lines))
	}
	secrets = append(secrets, closedPort)
	assertNoSecrets(t, "stdout", stdout, secrets)
	assertNoSecrets(t, "stderr", stderr, secrets)
}

// TestDiagOnvif_ReadsCamerasFromStdinOnly (P42): cameras arrive only on
// stdin. A file or a camera on the command line is a usage error that reads
// nothing and prints no line, and a line that is not a camera is refused
// without quoting it.
func TestDiagOnvif_ReadsCamerasFromStdinOnly(t *testing.T) {
	_, line, secrets := onvifTestCamera(t, "real-1", onviftest.Config{Brand: "Reolink", Model: "RLC-510A"})
	file := filepath.Join(t.TempDir(), "cameras.jsonl")
	if err := os.WriteFile(file, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--probe", file},
		{"--probe", "--cameras", file},
		{"--probe", "--config", file},
		{"--uris", "--file", file},
		{file},
		{"--probe", "--uris"},
	} {
		code, lines, _, stderr := runOnvif(t, "", args...)
		if code != exitFatal || len(lines) != 0 {
			t.Errorf("diag onvif %v: exit %d, %d lines; want a usage error", args, code, len(lines))
		}
		assertNoSecrets(t, "stderr", stderr, secrets)
	}
	code, lines, _, stderr := runOnvif(t, line, "--probe")
	if code != exitOK || len(lines) != 1 || lines[0]["brand"] != "Reolink" {
		t.Errorf("stdin camera: exit %d, lines %v, %s", code, lines, stderr)
	}
	bad := strings.Replace(line, `"id"`, `"camera"`, 1)
	code, _, _, stderr = runOnvif(t, bad, "--probe")
	if code != exitFatal || !strings.Contains(stderr, "line 1") {
		t.Errorf("a bad line: exit %d, stderr %q; want it refused", code, stderr)
	}
	assertNoSecrets(t, "a bad line's stderr", stderr, secrets)
	notID := cameraLine(t, "192.0.2.10", "http://192.0.2.10/onvif/device_service", "u", "p")
	if code, lines, _, stderr := runOnvif(t, notID, "--probe"); code != exitFatal || len(lines) != 0 || strings.Contains(stderr, "192.0.2.10") {
		t.Errorf("an address as id: exit %d, lines %v, stderr %q; want it refused unquoted", code, lines, stderr)
	}
}

// TestDiagOnvif_URIs: --uris prints the sub profile's stream and snapshot
// URIs as the camera returns them.
func TestDiagOnvif_URIs(t *testing.T) {
	_, line, _ := onvifTestCamera(t, "real-1", onviftest.Config{
		StreamURIs:  map[string]string{"main": "rtsp://cam/main", "sub": "rtsp://cam/sub"},
		SnapshotURI: "http://cam/snap.jpg",
	})
	code, lines, _, stderr := runOnvif(t, line, "--uris", "--profile", "sub")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := byCam(lines)
	if got["real-1/stream_uri"]["uri"] != "rtsp://cam/sub" || got["real-1/snapshot_uri"]["uri"] != "http://cam/snap.jpg" {
		t.Errorf("lines = %v", lines)
	}
}

// TestDiagOnvif_SubscribesUntilStdinCloses: by default each camera is
// subscribed to as its line arrives, its events are printed with the hub's
// receipt time, a camera that refuses the login is subscribe_failed auth,
// and closing stdin unsubscribes and exits 0.
func TestDiagOnvif_SubscribesUntilStdinCloses(t *testing.T) {
	fastOnvif(t)
	sim, line, secrets := onvifTestCamera(t, "real-1", onviftest.Config{})
	_, wrongLine, _ := onvifTestCamera(t, "real-2", onviftest.Config{})
	wrongLine = strings.Replace(wrongLine, "INEL-pass", "INEL-wrong", 1)
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		code := diagCommand([]string{"onvif"}, stdinR, stdoutW, &stderr)
		_ = stdoutW.Close()
		done <- code
	}()
	if _, err := io.WriteString(stdinW, line+wrongLine); err != nil {
		t.Fatal(err)
	}
	lines := make(chan map[string]any, 16)
	var raw strings.Builder
	go func() {
		sc := bufio.NewScanner(stdoutR)
		for sc.Scan() {
			raw.WriteString(sc.Text() + "\n")
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				lines <- m
			}
		}
		close(lines)
	}()
	waitUntil(t, "the camera's subscription", func() bool { return sim.Subscriptions() == 1 })
	before := time.Now().UnixMilli()
	sim.Emit("tns1:RuleEngine/CellMotionDetector/Motion", true)
	var event, failed map[string]any
	timeout := time.After(5 * time.Second)
	for event == nil || failed == nil {
		select {
		case m := <-lines:
			switch m["type"] {
			case onvifTypeEvent:
				event = m
			case onvifTypeSubscribeFailed:
				failed = m
			}
		case <-timeout:
			t.Fatalf("event %v, failed %v; want both", event, failed)
		}
	}
	if event["cam"] != "real-1" || event["motion"] != true || event["topic"] != "tns1:RuleEngine/CellMotionDetector/Motion" ||
		event["atMs"].(float64) < float64(before) {
		t.Errorf("event = %v", event)
	}
	if failed["cam"] != "real-2" || failed["reason"] != onvif.ReasonAuth {
		t.Errorf("subscribe_failed = %v; want real-2 auth", failed)
	}
	if err := stdinW.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("exit %d: %s", code, stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("diag onvif did not end when stdin closed")
	}
	for range lines {
	}
	if n := sim.Subscriptions(); n != 0 {
		t.Errorf("the camera holds %d subscriptions after stdin closed; want 0", n)
	}
	assertNoSecrets(t, "stdout", raw.String(), secrets)
	assertNoSecrets(t, "stderr", stderr.String(), secrets)
}
