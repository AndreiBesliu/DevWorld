// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"encoding/json"
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

// saveModes keeps in savedModes the modes of the Pi's release folder of
// exe and of the programs in it, before install gives them mode 0755, so
// that uninstall puts them back (giveRelease). A path whose mode is kept
// already keeps it: an install again does not take the probe's modes for
// those of before.
func (a autostart) saveModes(exe string) error {
	modes, err := a.loadModes()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	programs, err := releasePrograms(dir, exe)
	if err != nil {
		return err
	}
	for _, p := range append([]string{dir}, programs...) {
		if _, ok := modes[p]; ok {
			continue
		}
		info, err := os.Lstat(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return err
		}
		modes[p] = info.Mode().Perm()
	}
	data, err := json.Marshal(modes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.savedModes), 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.savedModes, append(data, '\n'), 0o600)
}

// loadModes reads the modes that install kept, by path; none without a
// file.
func (a autostart) loadModes() (map[string]os.FileMode, error) {
	modes := map[string]os.FileMode{}
	data, err := os.ReadFile(a.savedModes)
	if errors.Is(err, os.ErrNotExist) {
		return modes, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &modes)
	}
	return modes, err
}

// giveRelease gives the Pi's release folder of exe and the programs in it
// back to uid:gid, with the modes that install kept, and forgets them. The
// modes go back first, while root still owns the folder and nobody else
// can put a link in place of a program.
func (a autostart) giveRelease(exe string, uid, gid int) error {
	modes, err := a.loadModes()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	programs, err := releasePrograms(dir, exe)
	if err != nil {
		return err
	}
	for _, p := range append([]string{dir}, programs...) {
		if mode, ok := modes[p]; ok {
			if err := a.chmod(p, mode); err != nil {
				return err
			}
		}
	}
	if err := a.ownRelease(exe, uid, gid, 0); err != nil {
		return err
	}
	if err := os.Remove(a.savedModes); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
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
