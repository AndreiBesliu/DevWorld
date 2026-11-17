// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package spiketest

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// A test binary stands in for ipcam-hub when FakeHubEnv is "1" and its
// TestMain hands its arguments to FakeHub.
const (
	FakeHubEnv = "UPLOADSPIKE_FAKE_HUB"
	// FakeHubLogEnv names the file to which the stand-in appends a record
	// (FakeRecord) of each start, signal, stop and request.
	FakeHubLogEnv = "UPLOADSPIKE_FAKE_LOG"
	// FakeStorageEnv is the storage that diag host reports: "ssd" when
	// unset. With "sd", run refuses a run.json without sdCardConfirmed,
	// as ipcam-hub's SD gate does (exit 4).
	FakeStorageEnv = "UPLOADSPIKE_FAKE_STORAGE"
	// FakeFailStartEnv makes run record its start and exit 1 before its
	// control and ready events, a hub that fails early: always with "1",
	// else while the file that it names exists.
	FakeFailStartEnv = "UPLOADSPIKE_FAKE_FAIL_START"
)

// The stand-in's records.
const (
	RecordStart   = "start"
	RecordSignal  = "signal"
	RecordEOFStop = "eof-stop"
	RecordRequest = "request"
	RecordSdGate  = "sd-gate"
)

// FakeRecord is one record of the stand-in hub: its process and parent,
// what happened, and a detail (the signal's name, the request's path, or
// the start's arguments joined by spaces).
type FakeRecord struct {
	PID             int      `json:"pid"`
	PPID            int      `json:"ppid"`
	Event           string   `json:"event"`
	Detail          string   `json:"detail,omitempty"`
	SdCardConfirmed bool     `json:"sdCardConfirmed,omitempty"`
	Destinations    []string `json:"destinations,omitempty"`
	Token           bool     `json:"token,omitempty"`
	// Body is a POST /control/v1/clients request's body: the test's own
	// clients.
	Body string `json:"body,omitempty"`
	AtMs int64  `json:"atMs"`
}

var fakeLogMu sync.Mutex

// record appends r to FakeHubLogEnv's file, when it names one.
func record(r FakeRecord) {
	path := os.Getenv(FakeHubLogEnv)
	if path == "" {
		return
	}
	r.PID, r.PPID, r.AtMs = os.Getpid(), os.Getppid(), time.Now().UnixMilli()
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	fakeLogMu.Lock()
	defer fakeLogMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own log
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

// FakeHub is the stand-in ipcam-hub's main; it returns the exit code.
//   - "diag host" prints a host without a Pi model, on FakeStorageEnv's
//     storage;
//   - "diag visibility --creator <dest> --reader <dest>" prints a cell
//     that was listed and read;
//   - "diag scan-secrets --dir <dir>" searches the files under dir for the
//     values on stdin, raw, and reports those that hold one as moved;
//   - "run --config <run.json> --stop-on-stdin-eof" records its start,
//     exits 1 with FakeFailStartEnv, refuses an unconfirmed SD card as
//     ipcam-hub's gate does, serves a control endpoint on 127.0.0.1 that
//     checks the token and records each request, prints control and
//     ready, records every SIGINT, SIGTERM and SIGHUP without stopping,
//     and stops, exit 0, at the end of its stdin, which it records.
func FakeHub(args []string) int {
	fail := func(err error) int {
		_, _ = fmt.Fprintln(os.Stderr, "stand-in ipcam-hub:", err)
		return 1
	}
	switch {
	case len(args) >= 2 && args[0] == "diag" && args[1] == "host":
		storage := os.Getenv(FakeStorageEnv)
		if storage == "" {
			storage = "ssd"
		}
		fmt.Printf(`{"schema":"ipcam.diag.host/1","os":%q,"arch":%q,"physicalCores":4,"logicalCpus":8,"ramMb":8000,"piModel":null,"storage":%q,"writeCache":"on","recommendedCameras":4,"cores6Plus":false}`+"\n",
			runtime.GOOS, runtime.GOARCH, storage)
		return 0
	case len(args) >= 2 && args[0] == "diag" && args[1] == "visibility":
		return fakeVisibility(args[2:])
	case len(args) >= 2 && args[0] == "diag" && args[1] == "scan-secrets":
		return fakeScan(args[2:])
	case len(args) == 4 && args[0] == "run" && args[1] == "--config" && args[3] == "--stop-on-stdin-eof":
	default:
		return fail(errors.New("usage: run --config <run.json> --stop-on-stdin-eof"))
	}
	b, err := os.ReadFile(args[2])
	if err != nil {
		return fail(err)
	}
	var cfg struct {
		SDCardConfirmed bool `json:"sdCardConfirmed"`
		Uploads         *struct {
			Destinations []struct {
				ID string `json:"id"`
			} `json:"destinations"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fail(err)
	}
	token := os.Getenv("IPCAM_CONTROL_TOKEN")
	_ = os.Unsetenv("IPCAM_CONTROL_TOKEN")
	start := FakeRecord{Event: RecordStart, Detail: strings.Join(args, " "), SdCardConfirmed: cfg.SDCardConfirmed, Token: token != ""}
	if cfg.Uploads != nil {
		for _, d := range cfg.Uploads.Destinations {
			start.Destinations = append(start.Destinations, d.ID)
		}
	}
	record(start)
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range signals {
			record(FakeRecord{Event: RecordSignal, Detail: s.String()})
		}
	}()
	emit := func(fields map[string]any) {
		fields["atMs"] = time.Now().UnixMilli()
		b, _ := json.Marshal(fields)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	if failStart() {
		return fail(errors.New("failing its start, as asked"))
	}
	if os.Getenv(FakeStorageEnv) == "sd" && !cfg.SDCardConfirmed {
		record(FakeRecord{Event: RecordSdGate})
		emit(map[string]any{"type": "sd_card_unconfirmed"})
		return 4
	}
	if token != "" {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return fail(err)
		}
		srv := &http.Server{Handler: fakeControl(token), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		emit(map[string]any{"type": "control", "addr": ln.Addr().String()})
	}
	emit(map[string]any{"type": "reconcile", "seq": 1, "final": true, "aborted": false})
	emit(map[string]any{"type": "ready", "jobs": []any{}})
	_, _ = io.Copy(io.Discard, os.Stdin)
	record(FakeRecord{Event: RecordEOFStop})
	return 0
}

// failStart reports whether FakeFailStartEnv asks run to fail.
func failStart() bool {
	v := os.Getenv(FakeFailStartEnv)
	if v == "" || v == "1" {
		return v == "1"
	}
	_, err := os.Stat(v) //nolint:gosec // the test's own file
	return err == nil
}

// fakeControl is the stand-in's control endpoint: every request needs the
// token; begin answers a pending login (B1 on the web, A elsewhere, or the
// plan asked for), status keeps it pending, complete refuses its code.
func fakeControl(token string) http.Handler {
	var n atomic.Int64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/control/v1/clients":
			body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			record(FakeRecord{Event: RecordRequest, Detail: r.Method + " " + r.URL.Path, Body: string(body)})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		record(FakeRecord{Event: RecordRequest, Detail: r.Method + " " + r.URL.Path})
		switch r.URL.Path {
		case "/control/v1/oauth/begin":
			var req struct {
				Platform string `json:"platform"`
				Plan     string `json:"plan"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			plan := req.Plan
			if plan == "" {
				plan = "A"
				if req.Platform == "web" {
					plan = "B1"
				}
			}
			res := map[string]any{"state": fmt.Sprintf("stand-in-state-%d", n.Add(1)), "plan": plan,
				"expiresAt": time.Now().Add(10 * time.Minute)}
			if plan == "B1" {
				res["verificationUrl"], res["userCode"] = "https://fake.invalid/device", "STAND-IN"
			} else {
				res["authUrl"], res["redirectUri"] = "https://fake.invalid/auth", "http://127.0.0.1:1/"
			}
			_ = json.NewEncoder(w).Encode(res)
		case "/control/v1/oauth/status":
			_, _ = io.WriteString(w, `{"status":"pending"}`)
		case "/control/v1/oauth/complete":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"refused","reason":"invalid_grant"}`)
		case "/control/v1/visibility/targets":
			_, _ = io.WriteString(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// fakeClientTypes are the client types of the MI6 destinations' logins.
var fakeClientTypes = map[string]string{"g-android": "android", "g-windows": "desktop", "g-web": "tv"}

// fakeVisibility prints a visibility cell that was listed and read.
func fakeVisibility(args []string) int {
	flags := flag.NewFlagSet("visibility", flag.ContinueOnError)
	_ = flags.String("config", "", "")
	creator := flags.String("creator", "", "")
	reader := flags.String("reader", "", "")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	c, r := fakeClientTypes[*creator], fakeClientTypes[*reader]
	b, _ := json.Marshal(map[string]any{"schema": "ipcam.diag.visibility/1", "provider": "GOOGLE_DRIVE", "creator": c, "reader": r,
		"self": c == r, "listed": true, "read": true})
	fmt.Println(string(b))
	return 0
}

// fakeScan reports the files under --dir that hold a value of stdin, raw.
func fakeScan(args []string) int {
	flags := flag.NewFlagSet("scan-secrets", flag.ContinueOnError)
	_ = flags.String("config", "", "")
	dir := flags.String("dir", "", "")
	if err := flags.Parse(args); err != nil || *dir == "" {
		return 2
	}
	var values [][]byte
	dec := json.NewDecoder(os.Stdin)
	for {
		var v struct {
			Value string `json:"value"`
		}
		if err := dec.Decode(&v); err != nil {
			break
		}
		values = append(values, []byte(v.Value))
	}
	moved := []string{}
	_ = filepath.WalkDir(*dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // the stand-in's scan
		if err != nil {
			return nil
		}
		for _, v := range values {
			if len(v) > 0 && bytes.Contains(b, v) {
				rel, _ := filepath.Rel(*dir, path)
				moved = append(moved, filepath.ToSlash(rel))
				break
			}
		}
		return nil
	})
	b, _ := json.Marshal(map[string]any{"schema": "ipcam.diag.scansecrets/1", "values": len(values), "shortValues": 0,
		"moved": moved, "unreadable": []string{}, "unmoved": []string{}, "quarantine": ""})
	fmt.Println(string(b))
	return 0
}

// InstallFakeHub copies the test binary into dir as ipcam-hub (with .exe on
// Windows), sets the environment that makes it the stand-in for the rest
// of the test, and returns its path and the file of its records.
func InstallFakeHub(t *testing.T, dir string) (hub, log string) {
	t.Helper()
	hub = filepath.Join(dir, "ipcam-hub")
	if runtime.GOOS == "windows" {
		hub += ".exe"
	}
	self, err := os.ReadFile(os.Args[0]) //nolint:gosec // the test binary, the stand-in ipcam-hub
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hub, self, 0o700); err != nil { //nolint:gosec // the test binary as the stand-in
		t.Fatal(err)
	}
	log = filepath.Join(dir, "fakehub.log")
	t.Setenv(FakeHubEnv, "1")
	t.Setenv(FakeHubLogEnv, log)
	return hub, log
}

// FakeRecords reads the stand-in's records from log: none when it never
// ran.
func FakeRecords(t *testing.T, log string) []FakeRecord {
	t.Helper()
	fakeLogMu.Lock()
	b, err := os.ReadFile(log) //nolint:gosec // the test's own log
	fakeLogMu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []FakeRecord
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r FakeRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // a line cut by a process's death
		}
		out = append(out, r)
	}
	return out
}
