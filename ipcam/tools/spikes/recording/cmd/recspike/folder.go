// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// protectFolder gives the PC's release folder dir a protected DACL
// (security-1): the boot task, the real cuts' shortcut and the bugcheck
// run its programs elevated, so that only an administrator may change
// them, while the owner's user, sid, reads them and writes the data
// folders, which it makes first. The owner reads why it fails, and then
// nothing is installed.
func (a autostart) protectFolder(dir, sid string) error {
	const cannot = "nu pot proteja folderul probei, așa că nu instalez pornirea automată"
	for _, name := range mi1.DataFolders {
		if err := a.mkdirAll(mi1.DataFolder(dir, name)); err != nil {
			return fmt.Errorf("%s: %w", cannot, err)
		}
	}
	for _, cmd := range mi1.ProtectFolderCommands(dir, sid) {
		if err := a.run(cmd[0], cmd[1:]...); err != nil {
			return fmt.Errorf("%s: %w", cannot, err)
		}
	}
	_, _ = fmt.Fprintln(a.out, "Folderul probei poate fi schimbat acum doar de un administrator; programele probei scriu doar în results și work.")
	return nil
}

// unprotectFolder gives the PC's release folder dir, and everything in it,
// back the permissions that they inherit (security-1): the owner can then
// delete it.
func (a autostart) unprotectFolder(dir string) error {
	for _, cmd := range mi1.UnprotectFolderCommands(dir) {
		if err := a.run(cmd[0], cmd[1:]...); err != nil {
			return fmt.Errorf("nu pot pune la loc permisiunile folderului probei: %w", err)
		}
	}
	_, _ = fmt.Fprintln(a.out, "Folderul probei are din nou permisiunile de dinainte; îl puteți șterge.")
	return nil
}

// piPrograms are the Pi's programs that run as root: recspike, from the
// boot service, and reccontroller, ipcam-hub and MediaMTX.
var piPrograms = []string{"recspike", "reccontroller", "ipcam-hub", "mediamtx"}

// ownRelease gives the Pi's release folder of exe, exe itself (the boot
// service's ExecStart) and the other programs in it to uid:gid, and, when
// mode is not 0, that mode (security-1). The data folders stay as they
// are. A program that is not there is left out; one that is not a regular
// file, a symbolic link for instance, is refused before anything changes,
// and is looked at again once the folder is root's, when nobody else can
// replace it.
func (a autostart) ownRelease(exe string, uid, gid int, mode os.FileMode) error {
	own := func(p string) error {
		if err := a.chown(p, uid, gid); err != nil || mode == 0 {
			return err
		}
		return a.chmod(p, mode)
	}
	dir := filepath.Dir(exe)
	if _, err := releasePrograms(dir, exe); err != nil {
		return err
	}
	if err := own(dir); err != nil {
		return err
	}
	programs, err := releasePrograms(dir, exe)
	if err != nil {
		return err
	}
	for _, p := range programs {
		if err := own(p); err != nil {
			return err
		}
	}
	return nil
}

// releasePrograms are exe and the other piPrograms in dir that are there;
// one that is not a regular file is an error.
func releasePrograms(dir, exe string) ([]string, error) {
	var paths []string
	for _, p := range append([]string{exe}, programPaths(dir)...) {
		info, err := os.Lstat(p)
		switch {
		case errors.Is(err, os.ErrNotExist) || slices.Contains(paths, p):
			continue
		case err != nil:
			return nil, err
		case !info.Mode().IsRegular():
			return nil, fmt.Errorf("%s nu este un fișier obișnuit", filepath.Base(p))
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// programPaths are the paths of piPrograms in dir.
func programPaths(dir string) []string {
	paths := make([]string, len(piPrograms))
	for i, name := range piPrograms {
		paths[i] = filepath.Join(dir, name)
	}
	return paths
}

// sudoUser is the account that ran sudo, from sudo's SUDO_UID and
// SUDO_GID; ok is false without them.
func sudoUser() (uid, gid int, ok bool) {
	uid, errUID := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, errGID := strconv.Atoi(os.Getenv("SUDO_GID"))
	return uid, gid, errUID == nil && errGID == nil
}
