// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package hubproc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// fileOwner is the hub's identity, which owns its files on Linux when
// uploadspike runs as root; switchID is set when the hub runs as it.
type fileOwner struct {
	uid, gid int
	switchID bool
}

// betweenLstatAndOpen, when set, runs between reownFile's Lstat and its
// open: a test seam that swaps the file meanwhile.
var betweenLstatAndOpen func(rel string)

// The identity seams of the tests: whether uploadspike runs as root, and
// the hub identity's uid and gid.
var (
	asRoot          = func() bool { return os.Geteuid() == 0 }
	lookupSpikeUser = func(name string) (uid, gid int, err error) {
		u, err := user.Lookup(name)
		if err != nil {
			return 0, 0, err
		}
		if uid, err = strconv.Atoi(u.Uid); err != nil {
			return 0, 0, err
		}
		gid, err = strconv.Atoi(u.Gid)
		return uid, gid, err
	}
)

// spikeOwner is the hub's identity when uploadspike runs as root and
// RunAsUser names one: ErrNoSpikeUser when that user does not exist; nil
// otherwise.
func spikeOwner(runAsUser string) (*fileOwner, error) {
	if runAsUser == "" || !asRoot() {
		return nil, nil //nolint:nilnil // no owner: uploadspike's own identity
	}
	uid, gid, err := lookupSpikeUser(runAsUser)
	if err != nil {
		var unknown user.UnknownUserError
		if errors.As(err, &unknown) {
			return nil, ErrNoSpikeUser
		}
		return nil, err
	}
	return &fileOwner{uid: uid, gid: gid, switchID: uid != os.Geteuid()}, nil
}

// chmodOwn sets f's mode and, with an owner, its owner, on its descriptor.
func chmodOwn(f *os.File, mode fs.FileMode, owner *fileOwner) error {
	if err := f.Chmod(mode.Perm()); err != nil {
		return err
	}
	if owner == nil {
		return nil
	}
	return f.Chown(owner.uid, owner.gid)
}

// reownFile gives an existing file rel of root to owner through one
// descriptor, so that the check and the change cover the same inode:
// root.Lstat must show a regular file, the open is read-only and
// non-blocking (a FIFO swapped in cannot block the start), and the
// descriptor's own Stat must show a regular file with one link, the one
// that Lstat showed. A name swapped for a hard link after the open changes
// nothing: the chown never resolves the name again.
func reownFile(root *os.Root, dataDir, rel string, owner *fileOwner) error {
	path := filepath.Join(dataDir, rel)
	info, err := root.Lstat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("hubproc: %s: %w", path, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("hubproc: %s is a link or no regular file", path)
	}
	if betweenLstatAndOpen != nil {
		betweenLstatAndOpen(rel)
	}
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("hubproc: %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return fmt.Errorf("hubproc: %s: %w", path, err)
	}
	st, ok := opened.Sys().(*syscall.Stat_t)
	switch {
	case !ok || !opened.Mode().IsRegular():
		return fmt.Errorf("hubproc: %s is no regular file", path)
	case st.Nlink != 1:
		return fmt.Errorf("hubproc: %s has %d links", path, st.Nlink)
	case !os.SameFile(info, opened):
		return fmt.Errorf("hubproc: %s changed while it was opened", path)
	case int(st.Uid) == owner.uid && int(st.Gid) == owner.gid:
		return nil
	}
	if err := f.Chown(owner.uid, owner.gid); err != nil {
		return fmt.Errorf("hubproc: %s: %w", path, err)
	}
	return nil
}
