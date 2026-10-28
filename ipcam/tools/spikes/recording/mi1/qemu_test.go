package mi1

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestQEMU_ImageIsPinned (R03): the guest boots the Ubuntu 24.04 amd64
// cloud image of a dated release folder of cloud-images.ubuntu.com, the
// newest at least 14 days old when Task 7.3 pinned it on 2026-10-26
// (release-20260926; release-20261017 was 9 days old), pinned by its URL
// and by the SHA-256 that the folder's SHA256SUMS lists.
func TestQEMU_ImageIsPinned(t *testing.T) {
	m := regexp.MustCompile(`^https://cloud-images\.ubuntu\.com/releases/noble/release-(\d{8})/ubuntu-24\.04-server-cloudimg-amd64\.img$`).
		FindStringSubmatch(guestImageURL)
	if m == nil {
		t.Fatalf("guestImageURL = %q, want a noble release folder's amd64 .img", guestImageURL)
	}
	released, err := time.Parse("20060102", m[1])
	if err != nil {
		t.Fatal(err)
	}
	if pinned := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC); pinned.Sub(released) < 14*24*time.Hour {
		t.Errorf("release-%s was under 14 days old on %s", m[1], pinned.Format(time.DateOnly))
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(guestImageSHA256) {
		t.Errorf("guestImageSHA256 = %q, want 64 lowercase hex digits", guestImageSHA256)
	}
}

// TestQEMU_FetchImageChecksTheSHA256: the image is kept only when its
// SHA-256 is the pinned one, and the request names the project, not a
// person; a wrong download or an HTTP error leaves no file.
func TestQEMU_FetchImageChecksTheSHA256(t *testing.T) {
	image := []byte("a cloud image, as far as this test is concerned")
	sum := sha256.Sum256(image)
	var agents []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.UserAgent())
		mu.Unlock()
		if r.URL.Path != "/release/image.img" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(image)
	}))
	defer srv.Close()
	dir := t.TempDir()

	path := filepath.Join(dir, "base.img")
	if err := fetchImage(t.Context(), srv.URL+"/release/image.img", hex.EncodeToString(sum[:]), path); err != nil {
		t.Fatalf("fetchImage: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(image) { //nolint:gosec // the test's own file
		t.Errorf("the image = %q, %v", data, err)
	}
	for name, url := range map[string]string{"a wrong SHA-256": srv.URL + "/release/image.img", "not found": srv.URL + "/missing.img"} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		wrong := strings.Repeat("0", 64)
		if name == "not found" {
			wrong = hex.EncodeToString(sum[:])
		}
		if err := fetchImage(t.Context(), url, wrong, path); err == nil {
			t.Errorf("%s: fetchImage = nil, want an error", name)
		}
		if leftovers, _ := filepath.Glob(path + "*"); len(leftovers) != 0 {
			t.Errorf("%s: files left: %v", name, leftovers)
		}
	}
	for _, a := range agents {
		if a != userAgent {
			t.Errorf("User-Agent %q, want %q", a, userAgent)
		}
	}
}

// TestQEMU_FetchImageGivesUpOnAStalledServer: a server that never answers,
// or that stops sending the image halfway, fails the download within its
// limits (an answer within imageHeaderTimeout, the whole image within
// imageDeadline), leaving no file, instead of holding the shard until its
// step's timeout.
func TestQEMU_FetchImageGivesUpOnAStalledServer(t *testing.T) {
	header, deadline := imageHeaderTimeout, imageDeadline
	t.Cleanup(func() { imageHeaderTimeout, imageDeadline = header, deadline })
	imageHeaderTimeout, imageDeadline = 200*time.Millisecond, 600*time.Millisecond
	for _, tc := range []struct {
		name  string
		serve func(w http.ResponseWriter)
	}{
		{"no answer", func(http.ResponseWriter) {}},
		{"the image stops halfway", func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "1048576")
			_, _ = w.Write(make([]byte, 4096))
			w.(http.Flusher).Flush()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stall := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.serve(w)
				select { // stalled until the client gives up, or the test ends
				case <-r.Context().Done():
				case <-stall:
				}
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(func() { close(stall) }) // before srv.Close, which waits for the handler
			path := filepath.Join(t.TempDir(), "base.img")
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- fetchImage(t.Context(), srv.URL+"/image.img", strings.Repeat("0", 64), path) }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("fetchImage = nil, want an error")
				}
				t.Logf("fetchImage gave up after %v: %v", time.Since(start).Round(time.Millisecond), err)
			case <-time.After(10 * time.Second):
				t.Fatal("fetchImage still waits after 10 s")
			}
			if leftovers, _ := filepath.Glob(path + "*"); len(leftovers) != 0 {
				t.Errorf("files left: %v", leftovers)
			}
		})
	}
}

// labQEMU is a QEMU driver of shard 2's smoke run in dir.
func labQEMU(dir string) *QEMU {
	return &QEMU{Dir: dir, Bins: filepath.Join(dir, "bin"), Shard: 2, Smoke: true}
}

// TestQEMU_BootsUnderKVMWithTheLabsDisksAndPorts (R03, TST-09): the guest
// runs under KVM, its system disk virtio with cache=none (the guest's
// flushes reach the runner's disk, and a reset loses only what the guest
// had not flushed), the binaries' disk and the seed read-only, QEMU's user
// network (the runner is 10.0.2.2), a QMP socket, and the lab's two virtio
// serial ports.
func TestQEMU_BootsUnderKVMWithTheLabsDisksAndPorts(t *testing.T) {
	dir := t.TempDir()
	q := labQEMU(dir)
	args := q.args()
	joined := strings.Join(args, " ")
	value := func(flag string) []string {
		var out []string
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				out = append(out, args[i+1])
			}
		}
		return out
	}
	if !slices.Contains(args, "-enable-kvm") {
		t.Errorf("no -enable-kvm in %s", joined)
	}
	drives := value("-drive")
	want := []string{
		"file=" + filepath.Join(dir, "disk.qcow2") + ",if=virtio,format=qcow2,cache=none",
		"file=" + filepath.Join(dir, "bin.img") + ",if=virtio,format=raw,readonly=on",
		"file=" + filepath.Join(dir, "seed.img") + ",if=virtio,format=raw,readonly=on",
	}
	if !slices.Equal(drives, want) {
		t.Errorf("drives %q\nwant %q", drives, want)
	}
	if qmp := value("-qmp"); !slices.Equal(qmp, []string{"unix:" + filepath.Join(dir, "qmp.sock") + ",server=on,wait=off"}) {
		t.Errorf("-qmp %q, want the QMP socket", qmp)
	}
	if netdev := value("-netdev"); !slices.Equal(netdev, []string{"user,id=net0"}) {
		t.Errorf("-netdev %q, want QEMU's user network", netdev)
	}
	for _, port := range []string{"name=" + LabPort, "name=" + OutPort} {
		if !strings.Contains(joined, port) {
			t.Errorf("no virtio serial port %s in %s", port, joined)
		}
	}
	if chardevs := value("-chardev"); !slices.Equal(chardevs, []string{
		"socket,id=lab,path=" + filepath.Join(dir, "lab.sock") + ",server=on,wait=off",
		"file,id=out,path=" + filepath.Join(dir, "guest.log"),
	}) {
		t.Errorf("-chardev %q", chardevs)
	}
}

// TestQEMU_SeedInstallsAndRunsTheShard (G14): the NoCloud seed's
// cloud-config, on the guest's first boot, mounts the read-only binaries'
// disk by its label, copies recspike, ipcam-hub and mediamtx into
// /opt/recspike, runs `recspike install`, and runs the shard against the
// runner's reccontroller at 10.0.2.2. Its drop-in runs the installed
// service's `recspike resume` the same way after each reset: the output
// on the out port, then "exit <status>" on the lab port and a power-off,
// which ends the driver. After each reset, systemd-timesyncd tries its NTP
// servers again within a second when its first try at boot fails, not
// after its default 30 s, which every run's NTP wait used to show.
func TestQEMU_SeedInstallsAndRunsTheShard(t *testing.T) {
	q := labQEMU(t.TempDir())
	ud := q.userData()
	if !strings.HasPrefix(ud, "#cloud-config\n") {
		t.Fatalf("user-data does not start with #cloud-config:\n%s", ud)
	}
	for _, want := range []string{
		"mount -o ro LABEL=" + binLabel + " /mnt/ipcam-bin",
		"cp /mnt/ipcam-bin/recspike /mnt/ipcam-bin/ipcam-hub /mnt/ipcam-bin/mediamtx /opt/recspike/",
		"/opt/recspike/recspike install > /dev/virtio-ports/" + OutPort + " 2>&1 || fail\n      sync || fail\n",
		`"$@" > /dev/virtio-ports/` + OutPort + ` 2>&1`,
		`echo "exit $status" > /dev/virtio-ports/` + LabPort,
		"systemctl poweroff",
		"ExecStart=\n      ExecStart=/opt/recspike-lab/lab.sh resume\n",
		"  - path: /etc/systemd/timesyncd.conf.d/ipcam-lab.conf\n",
		"      [Time]\n      ConnectionRetrySec=1\n",
		"['/opt/recspike-lab/start.sh', 'mi1', '--lab', '--kind', 'qemu-reset', '--shard', '2', '--controller', '" + guestController + "', '--smoke']",
	} {
		if !strings.Contains(ud, want) {
			t.Errorf("user-data lacks %q:\n%s", want, ud)
		}
	}
	if got := q.guestCommand(); !slices.Equal(got, []string{"mi1", "--lab", "--kind", "qemu-reset", "--shard", "2", "--controller", "10.0.2.2", "--smoke"}) {
		t.Errorf("guestCommand = %q", got)
	}
	q.Smoke = false
	if got := q.guestCommand(); slices.Contains(got, "--smoke") {
		t.Errorf("a full shard's guestCommand = %q, want no --smoke", got)
	}
}

// fakeQMP is QEMU's QMP socket, as far as the driver uses it: the greeting,
// qmp_capabilities, and system_reset, which it counts; it sends an event
// before each answer, as QEMU may.
type fakeQMP struct {
	mu     sync.Mutex
	resets int
	errs   []error
}

func (f *fakeQMP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	enc := json.NewEncoder(conn)
	_ = enc.Encode(map[string]any{"QMP": map[string]any{"version": map[string]any{}, "capabilities": []any{}}})
	dec := json.NewDecoder(conn)
	for first := true; ; first = false {
		var cmd struct {
			Execute string `json:"execute"`
		}
		if err := dec.Decode(&cmd); err != nil {
			return
		}
		f.mu.Lock()
		switch {
		case first && cmd.Execute != "qmp_capabilities":
			f.errs = append(f.errs, errors.New("the first command is "+cmd.Execute+", not qmp_capabilities"))
		case !first && cmd.Execute != "system_reset":
			f.errs = append(f.errs, errors.New("an unexpected command: "+cmd.Execute))
		case cmd.Execute == "system_reset":
			f.resets++
		}
		f.mu.Unlock()
		_ = enc.Encode(map[string]any{"event": "RESET", "data": map[string]any{"guest": false}})
		_ = enc.Encode(map[string]any{"return": map[string]any{}})
	}
}

// TestQEMU_ResetsOnRequestAndEndsWithTheGuest (R03): each "reset" line
// that the guest's recspike sends at a cut becomes one QMP system_reset;
// the driver ends when the guest reports recspike's exit and powers off,
// with an error unless the exit status is 0. A guest that goes away without
// reporting an exit is an error too.
func TestQEMU_ResetsOnRequestAndEndsWithTheGuest(t *testing.T) {
	for _, tc := range []struct {
		name, lab string
		wantErr   string
		resets    int
	}{
		{"the shard completes", "reset\nreset\nexit 0\n", "", 2},
		{"recspike fails in the guest", "reset\nexit 1\n", "codul 1", 1},
		{"the guest goes away", "reset\n", "fără", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			f := &fakeQMP{}
			go f.serve(server)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			qmp, err := dialQMP(client)
			if err != nil {
				t.Fatalf("dialQMP: %v", err)
			}
			exited := make(chan struct{})
			lab := &guestLab{lines: tc.lab, done: func() { close(exited) }}
			var out strings.Builder
			q := labQEMU(t.TempDir())
			q.Out = &out
			err = q.serve(ctx, qmp, lab, exited)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("serve = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("serve = %v, want an error with %q", err, tc.wantErr)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.resets != tc.resets || len(f.errs) != 0 {
				t.Errorf("%d system_resets (errors %v), want %d", f.resets, f.errs, tc.resets)
			}
		})
	}
}

// guestLab is the guest's side of the lab port: its lines, then the end of
// the guest (done), as QEMU's exit after the guest's power-off.
type guestLab struct {
	lines string
	r     *bufio.Reader
	done  func()
	once  sync.Once
}

func (g *guestLab) Read(p []byte) (int, error) {
	if g.r == nil {
		g.r = bufio.NewReader(strings.NewReader(g.lines))
	}
	n, err := g.r.Read(p)
	if errors.Is(err, io.EOF) {
		g.once.Do(g.done)
	}
	return n, err
}

// TestQEMU_CopiesTheGuestsLines: the guest recspike's output reaches the
// runner's screen line by line as QEMU writes it to its file, without the
// carriage returns of a serial line, so that a line that starts with
// "mi1-diag: " in the guest starts so on the runner too.
func TestQEMU_CopiesTheGuestsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guest.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- copyLines(ctx, path, out) }()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []string{"Rularea lab-linux-qemu-reset-01-1 (criptată) începe.\r\n", "mi1-diag: {\"run\":", "\"x\"}\n", "half"} {
		if _, err := f.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * copyPoll)
	}
	_ = f.Close()
	waitFor(t, "the guest's lines", func() bool { return strings.Count(out.String(), "\n") == 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("copyLines = %v", err)
	}
	want := "Rularea lab-linux-qemu-reset-01-1 (criptată) începe.\nmi1-diag: {\"run\":\"x\"}\nhalf\n"
	if out.String() != want {
		t.Errorf("copied %q, want %q", out.String(), want)
	}
}
