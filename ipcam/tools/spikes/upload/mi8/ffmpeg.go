// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The pinned FFmpeg builds (P41): BtbN's LGPL builds of FFmpeg 9.0, from the
// month-end release autobuild-2026-10-31-12-59 (published 2026-10-31), as
// the ffmpeg-pin job of ipcam-spike-upload.yml printed them. LGPL, no
// libx264; a separate process, fetched at run time, never shipped.
const (
	PinTag           = "autobuild-2026-10-31-12-59"
	pinWin64URL      = "https://github.com/FFmpeg-Builds/FFmpeg-Builds/releases/download/autobuild-2026-10-31-12-59/ffmpeg-n9.0.2-39-g7adfbe40dc-win64-lgpl-9.0.zip"
	pinWin64SHA256   = "9d37c564e0d01c18cde184a079fa6ef04d7d2783ada41da81d04165cc8f1a650"
	pinLinuxARMURL   = "https://github.com/FFmpeg-Builds/FFmpeg-Builds/releases/download/autobuild-2026-10-31-12-59/ffmpeg-n9.0.2-39-g7adfbe40dc-linuxarm64-lgpl-9.0.tar.xz"
	pinLinuxARMSHA25 = "3d1f66827a73ce5a446312bc4924234dc1515726d3743e2df35c0177e1f82128"
)

// UserAgent is the User-Agent of every request that uploadspike makes on its
// own (C2-R5): it names no person and no account.
const UserAgent = "ipcam-spike/0.1"

// Pin is an archive to fetch: its exact URL and SHA-256 (hex).
type Pin struct{ URL, SHA256 string }

// PinFor is the pinned build of a platform: win64 on windows/amd64,
// linuxarm64 on linux/arm64. Other platforms (armv7, linux/amd64) have none:
// MI8 falls back to the snapshot diff there.
func PinFor(goos, goarch string) (Pin, bool) {
	switch {
	case goos == "windows" && goarch == "amd64":
		return Pin{URL: pinWin64URL, SHA256: pinWin64SHA256}, true
	case goos == "linux" && goarch == "arm64":
		return Pin{URL: pinLinuxARMURL, SHA256: pinLinuxARMSHA25}, true
	}
	return Pin{}, false
}

// The fetch's errors, each a reason to fall back to the snapshot diff.
var (
	ErrNoPin     = errors.New("mi8: no FFmpeg build is pinned for this platform")
	ErrHash      = errors.New("mi8: the FFmpeg archive's SHA-256 is not the pinned one")
	ErrNoTar     = errors.New("mi8: tar or xz is missing: the .tar.xz cannot be unpacked")
	ErrNoFFmpeg  = errors.New("mi8: the archive holds no ffmpeg binary")
	errDownload  = errors.New("mi8: the FFmpeg download failed")
	maxArchive   = int64(400 << 20)
	fetchTimeout = 30 * time.Minute
)

// Fetcher fetches the pinned FFmpeg build and unpacks ffmpeg[.exe] into Dir,
// next to uploadspike.
type Fetcher struct {
	// Client makes the download; nil: a client without proxy settings of
	// its own beyond the environment's.
	Client *http.Client
	// Dir is where ffmpeg[.exe] lands.
	Dir string
	// Pin is the archive; Windows is the binary's platform (ffmpeg.exe).
	Pin     Pin
	Windows bool
	// Untar unpacks a .tar.xz archive into dir: the system's tar -xJf
	// (Go's standard library has no xz reader); a seam of the tests.
	Untar func(ctx context.Context, archive, dir string) error
}

// NewFetcher is the fetcher of this platform, into dir; ErrNoPin without a
// pinned build.
func NewFetcher(dir string) (Fetcher, error) {
	pin, ok := PinFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return Fetcher{}, ErrNoPin
	}
	return Fetcher{Dir: dir, Pin: pin, Windows: runtime.GOOS == "windows"}, nil
}

// binName is ffmpeg's file name.
func (f Fetcher) binName() string {
	if f.Windows {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

// Path is where the unpacked ffmpeg is.
func (f Fetcher) Path() string { return filepath.Join(f.Dir, f.binName()) }

// Fetch returns ffmpeg's path: the one unpacked by an earlier fetch of the
// same pin, else the pinned archive downloaded by its exact URL with the
// User-Agent ipcam-spike/0.1, its SHA-256 checked before anything is
// unpacked, then ffmpeg[.exe] unpacked next to uploadspike: archive/zip for
// a .zip, tar -xJf for a .tar.xz.
func (f Fetcher) Fetch(ctx context.Context) (string, error) {
	if f.Pin.URL == "" {
		return "", ErrNoPin
	}
	marker := f.Path() + ".sha256"
	if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == f.Pin.SHA256 { //nolint:gosec // uploadspike's own folder
		if _, err := os.Stat(f.Path()); err == nil {
			return f.Path(), nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	if err := os.MkdirAll(f.Dir, 0o755); err != nil { //nolint:gosec // G301: the program's folder
		return "", err
	}
	archive, err := os.CreateTemp(f.Dir, "ffmpeg-archive-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = archive.Close(); _ = os.Remove(archive.Name()) }()
	if err := f.download(ctx, archive); err != nil {
		return "", err
	}
	switch {
	case strings.HasSuffix(f.Pin.URL, ".zip"):
		err = f.unzip(archive)
	case strings.HasSuffix(f.Pin.URL, ".tar.xz"):
		err = f.untar(ctx, archive.Name())
	default:
		err = ErrNoFFmpeg
	}
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(marker, []byte(f.Pin.SHA256+"\n"), 0o600); err != nil {
		return "", err
	}
	return f.Path(), nil
}

// download writes the archive to w and checks its SHA-256.
func (f Fetcher) download(ctx context.Context, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Pin.URL, nil)
	if err != nil {
		return errDownload
	}
	req.Header.Set("User-Agent", UserAgent)
	client := f.Client
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return errDownload
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", errDownload, resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, maxArchive+1))
	if err != nil || n > maxArchive {
		return errDownload
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(f.Pin.SHA256) {
		return ErrHash
	}
	return nil
}

// unzip takes ffmpeg.exe out of the zip archive.
func (f Fetcher) unzip(archive *os.File) error {
	st, err := archive.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(archive, st.Size())
	if err != nil {
		return ErrNoFFmpeg
	}
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != f.binName() || !strings.Contains(zf.Name, "bin/") || zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = f.install(rc)
		return errors.Join(err, rc.Close())
	}
	return ErrNoFFmpeg
}

// untar unpacks the .tar.xz archive into a folder beside ffmpeg and takes
// bin/ffmpeg out of it.
func (f Fetcher) untar(ctx context.Context, archive string) error {
	untar := f.Untar
	if untar == nil {
		untar = SystemUntar
	}
	tmp, err := os.MkdirTemp(f.Dir, "ffmpeg-unpack-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := untar(ctx, archive, tmp); err != nil {
		return err
	}
	var found string
	_ = filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == f.binName() && filepath.Base(filepath.Dir(p)) == "bin" {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if found == "" {
		return ErrNoFFmpeg
	}
	src, err := os.Open(found) //nolint:gosec // the archive's own file
	if err != nil {
		return err
	}
	err = f.install(src)
	return errors.Join(err, src.Close())
}

// install writes r as ffmpeg, executable, through a temporary file.
func (f Fetcher) install(r io.Reader) error {
	tmp, err := os.CreateTemp(f.Dir, "ffmpeg-bin-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, io.LimitReader(r, maxArchive)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil && !f.Windows { //nolint:gosec // G302: an executable
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path())
}

// SystemUntar runs `tar -xJf <archive> -C <dir>`; ErrNoTar when tar or xz is
// missing.
func SystemUntar(ctx context.Context, archive, dir string) error {
	if _, err := exec.LookPath("tar"); err != nil {
		return ErrNoTar
	}
	if _, err := exec.LookPath("xz"); err != nil {
		return ErrNoTar
	}
	cmd := exec.CommandContext(ctx, "tar", "-xJf", archive, "-C", dir) //nolint:gosec // the fetched archive, in uploadspike's folder
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mi8: tar -xJf: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Codecs that FFmpeg reads from its stdin: the raw elementary stream's
// demuxer.
const (
	CodecH264 = "h264"
	CodecH265 = "hevc"
)

// FFmpegArgs is FFmpeg's command line for a codec: the elementary stream on
// stdin, stamped with FFmpeg's wall clock as it reads it, decoded to 2 fps
// of 160 × 90 gray raw frames on stdout. No URL, address or credential is
// ever in it.
func FFmpegArgs(codec string) []string {
	return []string{"-hide_banner", "-loglevel", "error", "-use_wallclock_as_timestamps", "1", "-f", codec, "-i", "pipe:0",
		"-vf", "fps=2,scale=160:90,format=gray", "-f", "rawvideo", "pipe:1"}
}

// ffmpegEnv is FFmpeg's environment: empty, but for the variables that
// Windows needs to load a program.
func ffmpegEnv() []string {
	if runtime.GOOS != "windows" {
		return []string{}
	}
	var env []string
	for _, k := range []string{"SYSTEMROOT", "WINDIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Starter starts a command whose process must end with uploadspike's, and
// returns a channel closed once it ended (uploadspike's startWithUs).
type Starter func(cmd *exec.Cmd) (<-chan struct{}, error)

// plainStart starts cmd and waits for it.
func plainStart(cmd *exec.Cmd) (<-chan struct{}, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return done, nil
}

// decoder is a running FFmpeg: the stream goes into In, and Out gives its
// raw frames.
type decoder struct {
	in   io.WriteCloser
	out  *bufio.Reader
	done <-chan struct{}
	kill func()
}

// startDecoder starts bin with FFmpegArgs(codec), its stderr discarded.
func startDecoder(bin, codec string, start Starter) (*decoder, error) {
	if start == nil {
		start = plainStart
	}
	cmd := exec.Command(bin, FFmpegArgs(codec)...) //nolint:gosec // the pinned FFmpeg
	cmd.Env = ffmpegEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	done, err := start(cmd)
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return nil, errors.New("mi8: FFmpeg did not start")
	}
	d := &decoder{in: stdin, out: bufio.NewReaderSize(pr, FrameSize*2), done: done}
	d.kill = func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = pr.Close()
	}
	return d, nil
}
