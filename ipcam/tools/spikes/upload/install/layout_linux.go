// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package install

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Owners are the layout's owners: root's uid and gid, and the hub's
// identity's. Tests give their own (non-root uid seams).
type Owners struct{ RootUID, RootGID, SpikeUID, SpikeGID int }

// Bundle is what Layout copies into OptDir: the binaries found in the
// bundle's folder, and its media folder.
type Bundle struct {
	SrcDir   string
	Binaries []string
	Media    string // the media folder's name in SrcDir, "" for none
}

// Layout lays out the Pi's folders under prefix ("" on the Pi; tests give
// a temporary one): OptDir root-owned 0755 with the bundle's binaries and
// media, since Raspberry Pi OS's home folders are 0700 and the hub's
// identity could not reach them in the unpacked bundle; VarDir root-owned
// 0755, with hub/ and hub/spike/ owned by the hub's identity, 0700, and
// state/ and results/ root-owned 0700. It creates and re-owns each folder
// without following a link, as hubproc.Start does: an existing one that
// Lstat does not show as a folder, a symlink included, is refused, and
// its target keeps its owner and mode.
func Layout(prefix string, b Bundle, o Owners) error {
	opt, vardir := filepath.Join(prefix, OptDir), filepath.Join(prefix, VarDir)
	for _, d := range []string{filepath.Dir(opt), filepath.Dir(vardir)} {
		if err := os.MkdirAll(d, 0o755); err != nil { //nolint:gosec // G301: /opt and /var/lib, world-readable by design
			return err
		}
	}
	for _, d := range []struct {
		parent, name string
		mode         fs.FileMode
		uid, gid     int
	}{
		{filepath.Dir(opt), filepath.Base(opt), 0o755, o.RootUID, o.RootGID},
		{filepath.Dir(vardir), filepath.Base(vardir), 0o755, o.RootUID, o.RootGID},
		{vardir, "hub", 0o700, o.SpikeUID, o.SpikeGID},
		{filepath.Join(vardir, "hub"), "spike", 0o700, o.SpikeUID, o.SpikeGID},
		{vardir, "state", 0o700, o.RootUID, o.RootGID},
		{vardir, "results", 0o700, o.RootUID, o.RootGID},
	} {
		if err := ownedDir(d.parent, d.name, d.mode, d.uid, d.gid); err != nil {
			return err
		}
	}
	for _, name := range b.Binaries {
		src := filepath.Join(b.SrcDir, name)
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := copyFile(src, filepath.Join(opt, name), 0o755, o); err != nil {
			return err
		}
	}
	if b.Media == "" {
		return nil
	}
	srcMedia := filepath.Join(b.SrcDir, b.Media)
	if _, err := os.Stat(srcMedia); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(srcMedia, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcMedia, p)
		if err != nil {
			return err
		}
		to := filepath.Join(opt, b.Media, rel)
		if d.IsDir() {
			return ownedDir(filepath.Dir(to), filepath.Base(to), 0o755, o.RootUID, o.RootGID)
		}
		return copyFile(p, to, 0o644, o)
	})
}

// ownedDir makes parent/name a folder of mode and owner: created when
// missing, refused when Lstat shows anything but a folder, and changed
// through one descriptor opened without following a link.
func ownedDir(parent, name string, mode fs.FileMode, uid, gid int) error {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	path := filepath.Join(parent, name)
	if err := root.Mkdir(name, mode); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("install: %s: %w", path, err)
	}
	info, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("install: %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("install: %s is a link or no folder: refused", path)
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("install: %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || !fi.IsDir() || !os.SameFile(fi, info) {
		return fmt.Errorf("install: %s changed while it was checked: refused", path)
	}
	if err := f.Chown(uid, gid); err != nil {
		return fmt.Errorf("install: %s: %w", path, err)
	}
	return f.Chmod(mode)
}

// copyFile copies src to dst through a temporary file in dst's folder,
// with mode and root's owner, then renames it into place.
func copyFile(src, dst string, mode fs.FileMode, o Owners) error {
	in, err := os.Open(src) //nolint:gosec // the bundle's own file
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".install-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, in)
	err = errors.Join(err, tmp.Chmod(mode), tmp.Chown(o.RootUID, o.RootGID), tmp.Sync(), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
