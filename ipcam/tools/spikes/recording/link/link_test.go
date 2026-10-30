package link

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// pair returns the two ends of a link over loopback TCP: the controller's,
// which accepted, and the host's, which dialled out.
func pair(t *testing.T) (controller, host *Conn) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	host, err = Dial(t.Context(), ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	c, ok := <-accepted
	if !ok {
		t.Fatal("accept failed")
	}
	controller = NewConn(c)
	t.Cleanup(func() { _ = controller.Close() })
	return controller, host
}

func TestLink_MessagesRoundTrip(t *testing.T) {
	controllerEnd, hostEnd := pair(t)
	resume, coldBoot := int64(812), int64(41250)
	messages := []Message{
		Hello{Role: "host", Host: "pi", Version: "c1"},
		Session{RTSPBase: "rtsp://192.0.2.10:8554", User: "u", Pass: "p", Streams: []string{"cam01_main", "cam99_main"},
			BeaconPort: 7082, Needles: [][]byte{[]byte("ftypmp42"), {0x67, 0x64, 0x00, 0x28}}, SinkPort: 7083, SinkCert: []byte{0x30, 0x82, 0x01}},
		RunStart{RunID: "pi-sysrq-7", Set: "pi-sysrq", Kind: "sysrq", Encrypted: true, PadLowBitrate: true, Streams: []string{"cam01_main"},
			Host: "pi", OS: "linux", Storage: "sd", Attempt: 2,
			PiModel:        &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4, Source: "cpuinfo"},
			SdConfirmation: &results.SdConfirmation{Text: "DA", AtMs: 1792584000000}},
		RunStart{RunID: "real-pc-03-4", Set: "real-pc", Kind: "power", Encrypted: true, PadLowBitrate: true, Streams: []string{"cam01_main"},
			Host: "pc", OS: "windows", Storage: "ssd", Slot: 3, Attempt: 4},
		RunStart{RunID: "lab-linux-kill9-01-1", Set: "lab-linux-kill9", Kind: "kill9", Encrypted: true, PadLowBitrate: true, Streams: []string{"cam01_main"},
			Host: "lab-linux", OS: "linux", Storage: "unknown", Attempt: 1, Smoke: true},
		Ready{RunID: "pi-sysrq-7"},
		Cut{RunID: "pi-sysrq-7", Kind: "sysrq", PromptAt: time.Date(2026, 10, 21, 12, 7, 3, 250e6, time.UTC)},
		Report{
			RunID:      "pi-sysrq-7",
			Streams:    []StreamIndex{{Stream: "cam01_main", LastRecordedIndex: 1 << 40}},
			Reconcile:  json.RawMessage(`{"type":"reconcile","atMs":1792584423000,"seq":2,"final":true,"aborted":false}`),
			Verify:     json.RawMessage(`{"type":"verify","catalogMismatches":0}`),
			Hub04Clear: true, ResumeMs: &resume, ColdBootMs: &coldBoot, NtpWaitMs: 4100, NtpSynced: true, WriteCache: "on",
		},
		Verdict{Run: results.Run{RunID: "pi-sysrq-7", Valid: true, Attempt: 2, ShutdownKind: "sysrq", Encrypted: true, PadLowBitrate: true,
			Streams: []results.StreamLoss{{Stream: "cam01_main", BitrateKbps: 4000, Encrypted: true, PadLowBitrate: true, LossMs: 812, LimitMs: 2000}},
			LossMs:  812, ErrBoundMs: 53, Reconciled: true, Hub04Clear: true, PacingP99Ms: 4, Cameras: 1, ColdBootMs: &coldBoot, NtpWaitMs: 4100, NtpSynced: true}},
		Refused{RunID: "pc-kill9-01-1", Reason: "tipul de oprire „restart” nu este cunoscut"},
		BeaconCheck{ID: "verificare-1"},
		BeaconAck{ID: "verificare-1", Received: true},
		Results{Name: "MI2/pi/mi2.json", Data: []byte("{\"code\":\"MI2\"}\n")},
		PullResults{},
		ResultsTar{Data: []byte{0, 1, 2, 0xff}},
		Mi3Start{RunID: "pc-mi3-1-8x60-1", Sessions: 3, Tiles: []string{"cam01_sub", "cam02_sub", "cam01_main"}},
		Mi3Offers{RunID: "pc-mi3-1-8x60-1", Offers: []string{"v=0 a", "v=0 b", "v=0 c"}},
		Mi3Answers{RunID: "pc-mi3-1-8x60-1", Answers: []string{"v=0 x", "v=0 y", "v=0 z"}},
		Mi3Live{RunID: "pc-mi3-1-8x60-1"},
		Mi3End{RunID: "pc-mi3-1-8x60-1"},
		Mi3Result{RunID: "pc-mi3-1-8x60-1", PacingP99Ms: 9, SkippedAUs: 1, LatencyP95Ms: 61, LatencySamples: 702_000},
		Bye{},
	}
	wantTypes := []string{"hello", "session", "run_start", "run_start", "run_start", "ready", "cut", "report",
		"verdict", "refused", "beacon_check", "beacon_ack", "results", "pull_results", "results_tar",
		"mi3_start", "mi3_offers", "mi3_answers", "mi3_live", "mi3_end", "mi3_result", "bye"}
	go func() {
		for _, m := range messages {
			if err := hostEnd.Send(m); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i, want := range messages {
		got, err := controllerEnd.Receive()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got.Type() != wantTypes[i] {
			t.Errorf("message %d: type %q, want %q", i, got.Type(), wantTypes[i])
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("message %d: got %#v, want %#v", i, got, want)
		}
	}
}

// TestLink_WireFormat: one JSON object per line, with "type" and the field
// names of the protocol.
func TestLink_WireFormat(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	sender := NewConn(a)
	go func() {
		_ = sender.Send(Report{RunID: "r", Streams: []StreamIndex{{Stream: "cam01_main", LastRecordedIndex: 719}}, NtpWaitMs: 1})
		_ = sender.Send(PullResults{})
	}()
	r := bufio.NewReader(b)
	for _, want := range []string{
		`{"type":"report","runId":"r","streams":[{"stream":"cam01_main","lastRecordedIndex":719}],"reconcile":null,"verify":null,"hub04Clear":false,"resumeMs":null,"coldBootMs":null,"ntpWaitMs":1,"ntpSynced":false,"writeCache":""}`,
		`{"type":"pull_results"}`,
	} {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSuffix(line, "\n") != want {
			t.Errorf("wire line\n%s\nwant\n%s", line, want)
		}
	}

	t.Run("an unknown type or a bad line is an error", func(t *testing.T) {
		for _, line := range []string{`{"type":"shutdown"}`, `{"runId":"r"}`, `not json`, `{"type":"ready","runId":7}`} {
			x, y := net.Pipe()
			go func() { _, _ = x.Write([]byte(line + "\n")) }()
			if m, err := NewConn(y).Receive(); err == nil {
				t.Errorf("Receive(%s) = %#v, want an error", line, m)
			}
			_ = x.Close()
			_ = y.Close()
		}
	})
}

// writeTree writes files (relative path → content) under dir.
func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// readTree returns every regular file under dir by its slash path.
func readTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // under the test's directory
		files[filepath.ToSlash(rel)] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestLink_PullResultsTar (G15): the far side's results folder arrives
// byte-equal, nested folders and binary files included, without its lock
// files.
func TestLink_PullResultsTar(t *testing.T) {
	controllerEnd, hostEnd := pair(t)
	results := t.TempDir()
	big := bytes.Repeat([]byte{0, 0xff, 0x0a, 0x0d}, 600_000) // 2.4 MB, line breaks included
	files := map[string][]byte{
		"MI1/pi-sysrq/pi-sysrq-7.json": []byte("{\"code\":\"MI1\"}\n"),
		"MI2/pi/mi2.json":              []byte("{}"),
		"MI4/pi/readbench.bin":         big,
		"state/run-state.json":         []byte("{\"set\":\"pi-sysrq\"}"),
		"empty.txt":                    {},
	}
	writeTree(t, results, files)
	// The run state's lock is no result, and Windows keeps a locked file
	// unreadable: it stays behind.
	writeTree(t, results, map[string][]byte{"state/run-state.lock": {}})

	served := make(chan error, 1)
	go func() {
		m, err := hostEnd.Receive()
		if err != nil {
			served <- err
			return
		}
		if _, ok := m.(PullResults); !ok {
			served <- errors.New("the host got " + m.Type() + ", want pull_results")
			return
		}
		served <- hostEnd.SendResultsTar(results)
	}()

	into := filepath.Join(t.TempDir(), "results", "pi")
	if err := controllerEnd.PullResults(into); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if got := readTree(t, into); !reflect.DeepEqual(got, files) {
		t.Errorf("pulled %d files, want %d byte-equal ones", len(got), len(files))
		for rel := range files {
			if !bytes.Equal(got[rel], files[rel]) {
				t.Errorf("%s differs", rel)
			}
		}
	}
}

// TestLink_UntarStaysInside: a tar from the far side cannot write outside the
// folder it is unpacked into, nor create links.
func TestLink_UntarStaysInside(t *testing.T) {
	for name, hdr := range map[string]tar.Header{
		"parent":   {Name: "../escape.txt", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1},
		"absolute": {Name: "/tmp/escape.txt", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1},
		"nested":   {Name: "a/../../escape.txt", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1},
		"symlink":  {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			w := tar.NewWriter(&buf)
			if err := w.WriteHeader(&hdr); err != nil {
				t.Fatal(err)
			}
			if hdr.Size > 0 {
				if _, err := w.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			parent := t.TempDir()
			into := filepath.Join(parent, "pi")
			if err := Untar(into, buf.Bytes()); err == nil {
				t.Error("Untar accepted the entry")
			}
			if _, err := os.Lstat(filepath.Join(parent, "escape.txt")); err == nil {
				t.Error("a file was written outside the folder")
			}
		})
	}
}

// TestBeaconPacket: 16 bytes, FNV-64a of the run id then the sequence
// number, both big-endian.
func TestBeaconPacket(t *testing.T) {
	h := fnv.New64a()
	_, _ = h.Write([]byte("pc-kill9-3"))
	p := BeaconPacket("pc-kill9-3", 0x0102030405060708)
	if len(p) != 16 || binary.BigEndian.Uint64(p[:8]) != h.Sum64() || binary.BigEndian.Uint64(p[8:]) != 0x0102030405060708 {
		t.Errorf("BeaconPacket = % x", p)
	}
	if RunHash("pc-kill9-3") != h.Sum64() {
		t.Error("RunHash is not FNV-64a of the run id")
	}
	run, seq, ok := ParseBeacon(p)
	if !ok || run != h.Sum64() || seq != 0x0102030405060708 {
		t.Errorf("ParseBeacon = %x, %x, %v", run, seq, ok)
	}
	for _, bad := range [][]byte{nil, p[:15], append(bytes.Clone(p), 0)} {
		if _, _, ok := ParseBeacon(bad); ok {
			t.Errorf("ParseBeacon(% x) ok", bad)
		}
	}
}
