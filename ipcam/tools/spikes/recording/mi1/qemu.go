// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The lab guest's virtio serial ports: on LabPort the guest's recspike asks
// for its reset at each cut, and the guest reports recspike's exit; OutPort
// carries the guest recspike's output to the runner's screen.
const (
	LabPort = "ipcam.lab"
	OutPort = "ipcam.out"
)

// The lines of the lab port: a reset request, and recspike's end with its
// exit status, "exit 0".
const (
	resetRequest = "reset"
	exitReport   = "exit"
)

// The guest's image (R03): the Ubuntu 24.04 amd64 cloud image of the
// newest release folder at least 14 days old when Task 7.3 pinned it
// (2026-10-26), pinned by its URL and by the SHA-256 that the folder's
// SHA256SUMS lists (signed by the Ubuntu Cloud Image Builder's key
// D2EB44626FDDC30B513D5BB71A5D6C4C7DB87C81).
const (
	guestImageURL    = "https://cloud-images.ubuntu.com/releases/noble/release-20260926/ubuntu-24.04-server-cloudimg-amd64.img"
	guestImageSHA256 = "6a81c37564db9b1ee84e141922625e1d7c5b389b99bb3c572e0243607d5bb4d2"
)

// guestController is the runner's reccontroller as the guest reaches it:
// QEMU's user network maps 10.0.2.2 to the runner's loopback, where
// reccontroller -local listens.
const guestController = "10.0.2.2"

// binLabel labels the guest's read-only disk of binaries.
const binLabel = "IPCAMBIN"

// userAgent names the project, never a person, where a request names its
// client.
const userAgent = "ipcam-spike (+https://github.com/AndreiBesliu/DevWorld)"

// The guest's resources, and the driver's waits.
const (
	guestMemory   = "2048"
	guestCPUs     = "2"
	guestDiskSize = "20G"
	binDiskSize   = 512 << 20
	// qemuStartWait bounds the wait for QEMU's sockets after its start.
	qemuStartWait = 30 * time.Second
	// qmpTimeout bounds one QMP command.
	qmpTimeout = 10 * time.Second
	// powerOffWait bounds the wait for QEMU's end once the guest reported
	// recspike's.
	powerOffWait = 2 * time.Minute
	// copyPoll is how often copyLines reads the guest's output file.
	copyPoll = 100 * time.Millisecond
)

// guestBinaries are the files that the guest's recspike runs: itself,
// ipcam-hub and the MediaMTX fork next to it.
var guestBinaries = []string{"recspike", "ipcam-hub", "mediamtx"}

// QEMU runs a shard of lab-linux-qemu-reset (R03, TST-09) on a Linux runner
// with KVM. It boots the pinned Ubuntu 24.04 cloud image with a NoCloud
// seed, whose first boot installs recspike, ipcam-hub and MediaMTX from a
// second, read-only disk, runs `recspike install` and starts the shard
// against the runner's reccontroller at 10.0.2.2. At each cut the guest's
// recspike asks for its reset on the lab port, and the driver resets the
// virtual machine with QMP's system_reset: a power cut of the guest, whose
// disk loses what the guest had not flushed (cache=none). After each boot
// the installed service runs `recspike resume`, which finishes the run and
// goes on with the shard. The guest recspike's lines reach Out as they
// come; the driver ends when the guest reports recspike's end and powers
// off, with an error unless recspike ended well.
type QEMU struct {
	// Dir holds the driver's files: the image, the disks, the seed, QEMU's
	// sockets, the guest's console and output.
	Dir string
	// Bins holds recspike, ipcam-hub and mediamtx for linux/amd64.
	Bins  string
	Shard int
	Smoke bool
	// Out is the runner's screen.
	Out io.Writer
}

func (q *QEMU) path(name string) string { return filepath.Join(q.Dir, name) }

func (q *QEMU) say(format string, args ...any) {
	if q.Out != nil {
		_, _ = fmt.Fprintf(q.Out, format+"\n", args...)
	}
}

// Run boots the guest and runs its shard until the guest's recspike ends.
func (q *QEMU) Run(ctx context.Context) error {
	dir, err := filepath.Abs(q.Dir)
	if err != nil {
		return err
	}
	q.Dir = dir
	if err := checkKVM(); err != nil {
		return err
	}
	if err := os.MkdirAll(q.Dir, 0o700); err != nil {
		return err
	}
	q.say("Aduc imaginea Ubuntu a mașinii virtuale și îi verific SHA-256…")
	if err := fetchImage(ctx, guestImageURL, guestImageSHA256, q.path("base.img")); err != nil {
		return err
	}
	if err := q.disks(ctx); err != nil {
		return err
	}
	log, err := os.Create(q.path("qemu.log"))
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	cmd := exec.CommandContext(ctx, "qemu-system-x86_64", q.args()...) //nolint:gosec // QEMU with the driver's own arguments
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("pornirea QEMU: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	defer func() {
		_ = cmd.Process.Kill()
		<-exited
	}()
	q.say("Mașina virtuală pornește (shard %d).", q.Shard)

	qmpConn, err := dialUnix(ctx, q.path("qmp.sock"), exited)
	if err != nil {
		return err
	}
	defer func() { _ = qmpConn.Close() }()
	qmp, err := dialQMP(qmpConn)
	if err != nil {
		return err
	}
	lab, err := dialUnix(ctx, q.path("lab.sock"), exited)
	if err != nil {
		return err
	}
	defer func() { _ = lab.Close() }()

	copyCtx, stopCopy := context.WithCancel(ctx)
	copied := make(chan error, 1)
	go func() { copied <- copyLines(copyCtx, q.path("guest.log"), q.Out) }()
	err = q.serve(ctx, qmp, lab, exited)
	stopCopy()
	return errors.Join(err, <-copied)
}

// serve answers the guest on the lab port: each reset request with QMP's
// system_reset, until the guest reports recspike's end and QEMU exits.
func (q *QEMU) serve(ctx context.Context, qmp *qmpClient, lab io.Reader, exited <-chan struct{}) error {
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(lab)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	resets := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return fmt.Errorf("mașina virtuală s-a oprit fără ca recspike din ea să-și raporteze sfârșitul, după %d resetări", resets)
		case line, ok := <-lines:
			if !ok {
				lines = nil // the port closed with QEMU: its exit follows
				continue
			}
			switch {
			case line == resetRequest:
				resets++
				q.say("Mașina virtuală cere oprirea: o resetez (system_reset %d).", resets)
				if err := qmp.execute("system_reset"); err != nil {
					return err
				}
			case strings.HasPrefix(line, exitReport+" "):
				code, err := strconv.Atoi(strings.TrimPrefix(line, exitReport+" "))
				if err != nil {
					return fmt.Errorf("mașina virtuală a raportat %q", line)
				}
				select {
				case <-exited:
				case <-time.After(powerOffWait):
					q.say("Mașina virtuală nu s-a oprit în %v după sfârșitul lui recspike.", powerOffWait)
				case <-ctx.Done():
				}
				if code != 0 {
					return fmt.Errorf("recspike din mașina virtuală s-a încheiat cu codul %d", code)
				}
				q.say("recspike din mașina virtuală a încheiat shardul, după %d resetări.", resets)
				return nil
			default:
				q.say("Mașina virtuală a trimis o linie necunoscută: %q", line)
			}
		}
	}
}

// guestCommand is the shard that the guest runs, against the runner's
// reccontroller.
func (q *QEMU) guestCommand() []string {
	args := []string{"mi1", "--lab", "--kind", "qemu-reset", "--shard", strconv.Itoa(q.Shard), "--controller", guestController}
	if q.Smoke {
		args = append(args, "--smoke")
	}
	return args
}

// args are QEMU's: KVM; the system disk, an overlay on the image, virtio
// with cache=none, so that the guest's flushes reach the runner's disk and
// a reset loses only what the guest had not flushed; the binaries' disk and
// the seed read-only; the user network, where the runner is 10.0.2.2; the
// lab's two serial ports; the QMP socket; the console in a file.
func (q *QEMU) args() []string {
	return []string{
		"-enable-kvm", "-cpu", "host", "-machine", "q35", "-smp", guestCPUs, "-m", guestMemory,
		"-display", "none", "-monitor", "none", "-serial", "file:" + q.path("console.log"),
		"-drive", "file=" + q.path("disk.qcow2") + ",if=virtio,format=qcow2,cache=none",
		"-drive", "file=" + q.path("bin.img") + ",if=virtio,format=raw,readonly=on",
		"-drive", "file=" + q.path("seed.img") + ",if=virtio,format=raw,readonly=on",
		"-netdev", "user,id=net0", "-device", "virtio-net-pci,netdev=net0",
		"-device", "virtio-serial-pci",
		"-chardev", "socket,id=lab,path=" + q.path("lab.sock") + ",server=on,wait=off",
		"-device", "virtserialport,chardev=lab,name=" + LabPort,
		"-chardev", "file,id=out,path=" + q.path("guest.log"),
		"-device", "virtserialport,chardev=out,name=" + OutPort,
		"-qmp", "unix:" + q.path("qmp.sock") + ",server=on,wait=off",
	}
}

// userData is the NoCloud seed's cloud-config. The first boot runs
// start.sh: it copies the binaries from their read-only disk, runs
// `recspike install`, syncs, so that the binaries and the service outlive
// the first reset, and runs the shard through lab.sh. lab.sh sends
// recspike's output to the runner (the out port), then its exit status
// (the lab port), and powers the guest off. The drop-in makes the installed
// service's `recspike resume` run through lab.sh too after each reset.
// systemd-timesyncd's drop-in tries the NTP servers again after 1 s, not
// its default 30 s, when its first try at boot fails: each run's NTP wait
// after its reset used to last 30 to 40 s. Only an NTP answer syncs the
// clock, so an unsynced clock still shows (ntpSynced).
func (q *QEMU) userData() string {
	shard := "['/opt/recspike-lab/start.sh'"
	for _, a := range q.guestCommand() {
		shard += ", '" + a + "'"
	}
	shard += "]"
	return `#cloud-config
# The lab guest of mi1.QEMU (R03, TST-09).
write_files:
  - path: /opt/recspike-lab/lab.sh
    permissions: '0755'
    content: |
      #!/bin/sh
      # recspike with these arguments, its output on the runner's screen;
      # then its exit status to the runner, and the guest powers off.
      /opt/recspike/recspike "$@" > /dev/virtio-ports/` + OutPort + ` 2>&1
      status=$?
      echo "exit $status" > /dev/virtio-ports/` + LabPort + `
      systemctl poweroff
  - path: /opt/recspike-lab/start.sh
    permissions: '0755'
    content: |
      #!/bin/sh
      # The first boot: recspike from the read-only disk, its autostart,
      # then the shard. A step that fails ends the shard at once.
      fail() {
        echo "exit 1" > /dev/virtio-ports/` + LabPort + `
        systemctl poweroff
        exit 1
      }
      # The image's daily package jobs stay out of the measurement.
      systemctl disable --now apt-daily.timer apt-daily-upgrade.timer unattended-upgrades.service
      mkdir -p /mnt/ipcam-bin /opt/recspike || fail
      mount -o ro LABEL=` + binLabel + ` /mnt/ipcam-bin || fail
      cp /mnt/ipcam-bin/recspike /mnt/ipcam-bin/ipcam-hub /mnt/ipcam-bin/mediamtx /opt/recspike/ || fail
      umount /mnt/ipcam-bin || fail
      /opt/recspike/recspike install > /dev/virtio-ports/` + OutPort + ` 2>&1 || fail
      sync || fail
      exec /opt/recspike-lab/lab.sh "$@"
  - path: /etc/systemd/system/ipcam-recspike.service.d/lab.conf
    content: |
      [Service]
      ExecStart=
      ExecStart=/opt/recspike-lab/lab.sh resume
  - path: /etc/systemd/timesyncd.conf.d/ipcam-lab.conf
    content: |
      [Time]
      ConnectionRetrySec=1
runcmd:
  - ` + shard + `
`
}

// disks makes the guest's disks: the overlay on the image, the binaries'
// disk and the seed.
func (q *QEMU) disks(ctx context.Context) error {
	disk := q.path("disk.qcow2")
	if err := removeIfThere(disk); err != nil {
		return err
	}
	if _, err := runTool(ctx, "qemu-img", "create", "-q", "-f", "qcow2", "-F", "qcow2", "-b", q.path("base.img"), disk, guestDiskSize); err != nil {
		return err
	}
	root := q.path("bin")
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil { //nolint:gosec // the guest's binaries, read by root in the guest
		return err
	}
	for _, name := range guestBinaries {
		data, err := os.ReadFile(filepath.Join(q.Bins, name)) //nolint:gosec // the binaries next to recspike
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o755); err != nil { //nolint:gosec // executables
			return err
		}
	}
	bin := q.path("bin.img")
	if err := removeIfThere(bin); err != nil {
		return err
	}
	if err := os.WriteFile(bin, nil, 0o600); err != nil {
		return err
	}
	if err := os.Truncate(bin, binDiskSize); err != nil {
		return err
	}
	if _, err := runTool(ctx, "mkfs.ext4", "-q", "-F", "-L", binLabel, "-d", root, bin); err != nil {
		return err
	}
	if err := os.WriteFile(q.path("user-data"), []byte(q.userData()), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(q.path("meta-data"), []byte("instance-id: ipcam-lab\nlocal-hostname: ipcam-lab\n"), 0o600); err != nil {
		return err
	}
	_, err := runTool(ctx, "cloud-localds", q.path("seed.img"), q.path("user-data"), q.path("meta-data"))
	return err
}

// checkKVM checks that this runner can run the guest under KVM.
func checkKVM() error {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("KVM nu este disponibil pe acest runner (/dev/kvm): %w", err)
	}
	return f.Close()
}

// imageHeaderTimeout bounds the wait for the image server's answer, and
// imageDeadline the whole download: a stalled download would otherwise
// hold the shard until its step's timeout. Tests shorten them.
var (
	imageHeaderTimeout = time.Minute
	imageDeadline      = 15 * time.Minute
)

// fetchImage downloads the image at url into path and keeps it only when
// its SHA-256 is sum; an image already there with that SHA-256 is kept.
func fetchImage(ctx context.Context, url, sum, path string) error {
	if got, err := fileSHA256(path); err == nil && got == sum {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, imageDeadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	// Something may have replaced the default transport with a RoundTripper
	// of its own: the download then uses a transport of its own (8a M7).
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = t.Clone()
	}
	transport.ResponseHeaderTimeout = imageHeaderTimeout
	defer transport.CloseIdleConnections()
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return fmt.Errorf("descărcarea imaginii: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descărcarea imaginii %s: %s", url, resp.Status)
	}
	part := path + ".download"
	f, err := os.Create(part) //nolint:gosec // the driver's own folder
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if err = errors.Join(err, f.Close()); err == nil {
		if got := hex.EncodeToString(h.Sum(nil)); got != sum {
			err = fmt.Errorf("imaginea descărcată are SHA-256 %s, nu %s, cel fixat", got, sum)
		}
	}
	if err != nil {
		return errors.Join(err, os.Remove(part))
	}
	return os.Rename(part, path)
}

// fileSHA256 is the SHA-256 of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // the driver's own image
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dialUnix connects to QEMU's socket at path, once QEMU has made it; an
// exit of QEMU ends the wait.
func dialUnix(ctx context.Context, path string, exited <-chan struct{}) (net.Conn, error) {
	deadline := time.Now().Add(qemuStartWait)
	for {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", path)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("QEMU nu a deschis %s în %v: %w", filepath.Base(path), qemuStartWait, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			return nil, errors.New("QEMU s-a oprit la pornire (vedeți qemu.log)")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// qmpClient sends QEMU's monitor commands over QMP.
type qmpClient struct {
	conn net.Conn
	dec  *json.Decoder
	enc  *json.Encoder
}

// dialQMP reads QMP's greeting on conn and enters command mode.
func dialQMP(conn net.Conn) (*qmpClient, error) {
	c := &qmpClient{conn: conn, dec: json.NewDecoder(conn), enc: json.NewEncoder(conn)}
	_ = conn.SetDeadline(time.Now().Add(qmpTimeout))
	var greeting struct {
		QMP json.RawMessage `json:"QMP"`
	}
	if err := c.dec.Decode(&greeting); err != nil || greeting.QMP == nil {
		return nil, fmt.Errorf("mi1: QMP gave no greeting: %v", err)
	}
	if err := c.execute("qmp_capabilities"); err != nil {
		return nil, err
	}
	return c, nil
}

// execute runs a QMP command and waits for its answer, past the events
// that come before it.
func (c *qmpClient) execute(command string) error {
	_ = c.conn.SetDeadline(time.Now().Add(qmpTimeout))
	if err := c.enc.Encode(map[string]string{"execute": command}); err != nil {
		return fmt.Errorf("mi1: QMP %s: %w", command, err)
	}
	for {
		var reply struct {
			Return json.RawMessage `json:"return"`
			Error  *struct {
				Desc string `json:"desc"`
			} `json:"error"`
		}
		if err := c.dec.Decode(&reply); err != nil {
			return fmt.Errorf("mi1: QMP %s: %w", command, err)
		}
		switch {
		case reply.Error != nil:
			return fmt.Errorf("mi1: QMP %s: %s", command, reply.Error.Desc)
		case reply.Return != nil:
			return nil
		}
	}
}

// copyLines copies the lines that QEMU writes to the file at path, the
// guest recspike's output, to out as they come, without the carriage
// returns of a serial line, until ctx ends; then it copies the rest, with a
// last line that has no line break ended.
func copyLines(ctx context.Context, path string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	var f *os.File
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	var pending []byte
	buf := make([]byte, 64<<10)
	for {
		done := ctx.Err() != nil
		if f == nil {
			if opened, err := os.Open(path); err == nil { //nolint:gosec // QEMU's output file in the driver's folder
				f = opened
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		for f != nil {
			n, err := f.Read(buf)
			pending = append(pending, bytes.ReplaceAll(buf[:n], []byte("\r"), nil)...)
			if i := bytes.LastIndexByte(pending, '\n'); i >= 0 {
				if _, werr := out.Write(pending[:i+1]); werr != nil {
					return werr
				}
				pending = append(pending[:0], pending[i+1:]...)
			}
			if err != nil || n == 0 {
				break
			}
		}
		if done {
			if len(pending) > 0 {
				_, err := out.Write(append(pending, '\n'))
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(copyPoll):
		}
	}
}

// removeIfThere removes the file at path, if there is one.
func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// runTool runs a system command and returns its output; its error carries
// what the command printed.
func runTool(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the lab's tools, with recspike's own arguments
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
