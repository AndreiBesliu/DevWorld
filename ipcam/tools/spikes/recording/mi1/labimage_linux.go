// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The files of an encrypted lab run's image, in the run's folder: the
// image, the loop device it is attached to, and the folder it is mounted
// on, whose data folder is the run's.
const (
	imageFile  = "data.img"
	deviceFile = "data.img.device"
	mountDir   = "mnt"
	// imageSize is an image's size, sparse: more than a run of 12 min at
	// 4 Mbps and the restarted ipcam-hub's recording after it.
	imageSize = 1 << 30
	// releaseTimeout bounds the unmount and detach of an image.
	releaseTimeout = time.Minute
)

// labVolumes is where a Linux lab shard's runs record: a kill9 shard's
// encrypted runs on images of their own (Images), every other run in a
// plain folder.
func labVolumes(kind, root string) Volumes {
	if kind == "kill9" {
		return &Images{Root: root}
	}
	return Dirs{Root: root}
}

// Images gives each encrypted run of a Linux lab kill9 shard a fresh ext4
// image of its own (G08): a new zero-filled file (truncate -s 1G),
// formatted with mkfs.ext4, attached with sudo losetup and mounted as the
// parent of the run's data folder; Release unmounts, detaches and deletes
// it once the run is reported. The run's raw scans read that image only.
// Clear runs record in a plain folder (Dirs): an image that a clear run
// had used would still hold its ftyp, moof and NAL bytes, in live files and
// in freed blocks, and fail HUB-04 by itself.
type Images struct {
	// Root holds a folder per run, <Root>/<runId>, as Dirs does.
	Root string
	// tools are the system's when nil, and size is imageSize when 0;
	// tests replace them.
	tools imageTools
	size  int64
}

// imageTools are the commands that make, attach and mount an image.
type imageTools interface {
	format(ctx context.Context, image string) error
	attach(ctx context.Context, image string) (device string, err error)
	mount(ctx context.Context, device, dir string) error
	unmount(ctx context.Context, dir string) error
	detach(ctx context.Context, device string) error
	// flush writes to its device what the file system mounted on dir
	// holds in memory, so that the image file holds every byte written.
	flush(dir string) error
}

func (m *Images) sys() imageTools {
	if m.tools != nil {
		return m.tools
	}
	return systemImages{}
}

// Prepare gives an encrypted run a fresh image, mounted, and a clear run a
// plain folder.
func (m *Images) Prepare(ctx context.Context, runID string, encrypted bool) (Volume, error) {
	if !encrypted {
		return Dirs{Root: m.Root}.Prepare(ctx, runID, false)
	}
	dir := filepath.Join(m.Root, runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	v := &imageVolume{image: filepath.Join(dir, imageFile), mount: filepath.Join(dir, mountDir), tools: m.sys()}
	f, err := os.OpenFile(v.image, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // never an image of an earlier run
	if err != nil {
		return nil, err
	}
	size := m.size
	if size == 0 {
		size = imageSize
	}
	err = errors.Join(f.Truncate(size), f.Close())
	if err == nil {
		err = v.tools.format(ctx, v.image)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("mi1: the image of run %s: %w", runID, err), os.Remove(v.image))
	}
	if err := v.attachAndMount(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("mi1: the image of run %s: %w", runID, err), v.Release())
	}
	return v, nil
}

// Reopen finds the run's volume again: an encrypted run's image as it is
// mounted, or attached and mounted again when it is not (after a reboot).
func (m *Images) Reopen(ctx context.Context, runID string, encrypted bool) (Volume, error) {
	if !encrypted {
		return Dirs{Root: m.Root}.Reopen(ctx, runID, false)
	}
	dir := filepath.Join(m.Root, runID)
	v := &imageVolume{image: filepath.Join(dir, imageFile), mount: filepath.Join(dir, mountDir), tools: m.sys()}
	if _, err := os.Stat(v.image); err != nil {
		return nil, err
	}
	if device, err := os.ReadFile(filepath.Join(dir, deviceFile)); err == nil && mounted(v.mount) { //nolint:gosec // the run's own folder
		v.device, v.mounted = strings.TrimSpace(string(device)), true
		return v, nil
	}
	if err := v.attachAndMount(ctx); err != nil {
		return nil, fmt.Errorf("mi1: the image of run %s: %w", runID, err)
	}
	return v, nil
}

// mounted reports whether a file system is mounted on dir: dir is on
// another device than its parent.
func mounted(dir string) bool {
	in, err1 := os.Stat(dir)
	parent, err2 := os.Stat(filepath.Dir(dir))
	if err1 != nil || err2 != nil {
		return false
	}
	a, ok1 := in.Sys().(*syscall.Stat_t)
	b, ok2 := parent.Sys().(*syscall.Stat_t)
	return ok1 && ok2 && a.Dev != b.Dev
}

// imageVolume is an encrypted run's image, attached to device (once
// attached) and mounted on mount (once mounted).
type imageVolume struct {
	image, device, mount string
	mounted              bool
	tools                imageTools
}

// attachAndMount attaches the image, notes its device beside it, and mounts
// it, with the run's data folder on it.
func (v *imageVolume) attachAndMount(ctx context.Context) error {
	device, err := v.tools.attach(ctx, v.image)
	if err != nil {
		return err
	}
	v.device = device
	if err := os.WriteFile(filepath.Join(filepath.Dir(v.image), deviceFile), []byte(device+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.MkdirAll(v.mount, 0o700); err != nil {
		return err
	}
	if err := v.tools.mount(ctx, device, v.mount); err != nil {
		return err
	}
	v.mounted = true
	return os.MkdirAll(v.DataDir(), 0o700)
}

// DataDir is the run's data folder, on the image.
func (v *imageVolume) DataDir() string { return filepath.Join(v.mount, "data") }

// ScanRaw scans the whole image, once the file system has written what it
// held in memory to it.
func (v *imageVolume) ScanRaw(needles [][]byte) ([]Finding, error) {
	if err := v.tools.flush(v.mount); err != nil {
		return nil, fmt.Errorf("mi1: writing the image's file system out: %w", err)
	}
	return ScanRaw(v.image, imageFile, needles)
}

// Release unmounts the image, detaches it and deletes it. It goes as far as
// it can: an image that will not unmount stays attached, and is kept.
func (v *imageVolume) Release() error {
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	if v.mounted {
		if err := v.tools.unmount(ctx, v.mount); err != nil {
			return err
		}
		v.mounted = false
	}
	if v.device != "" {
		if err := v.tools.detach(ctx, v.device); err != nil {
			return err
		}
		v.device = ""
	}
	return errors.Join(removeIfThere(filepath.Join(filepath.Dir(v.image), deviceFile)), removeIfThere(v.image), os.RemoveAll(v.mount))
}

// systemImages are the system's mkfs.ext4, and losetup and mount through
// sudo, as the lab runner's user has them without a password.
type systemImages struct{}

func (systemImages) format(ctx context.Context, image string) error {
	_, err := runTool(ctx, "mkfs.ext4", "-q", "-F", "-E", fmt.Sprintf("root_owner=%d:%d", os.Getuid(), os.Getgid()), image)
	return err
}

func (systemImages) attach(ctx context.Context, image string) (string, error) {
	out, err := runTool(ctx, "sudo", "-n", "losetup", "--find", "--show", image)
	return strings.TrimSpace(out), err
}

func (systemImages) mount(ctx context.Context, device, dir string) error {
	_, err := runTool(ctx, "sudo", "-n", "mount", "-t", "ext4", device, dir)
	return err
}

func (systemImages) unmount(ctx context.Context, dir string) error {
	_, err := runTool(ctx, "sudo", "-n", "umount", dir)
	return err
}

func (systemImages) detach(ctx context.Context, device string) error {
	_, err := runTool(ctx, "sudo", "-n", "losetup", "--detach", device)
	return err
}

func (systemImages) flush(dir string) error {
	f, err := os.Open(dir) //nolint:gosec // the run's own mount point
	if err != nil {
		return err
	}
	return errors.Join(unix.Syncfs(int(f.Fd())), f.Close()) //nolint:gosec // a file descriptor fits an int
}
