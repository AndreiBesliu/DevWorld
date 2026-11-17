// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package hubproc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// spikeDirName is the folder of the hub's data directory that holds
// uploadspike's files for the hub: run.json and any identity file.
const spikeDirName = "spike"

// The files that ipcam-hub keeps under <dataDir>/state/ and that its
// identity must own before each start (C2-R15 (a)): the catalog and
// SQLite's files beside it, the inner hub lock and the hub's id.
var stateFiles = []string{"catalog.db", "catalog.db-wal", "catalog.db-shm", "catalog.db-journal", "hub.lock", "hub_id"}

// prepareFiles writes cfg.Files into the hub's spike folder and, with an
// owner, gives the hub's identity its data directory, the spike folder,
// those files and the files of state/ (Files for the hub). Every write and
// change goes through an os.Root opened on the data directory, which no
// path leaves through a link or "..": a file is created as <name>.tmp with
// O_EXCL after the removal of any old .tmp (O_EXCL fails on a link),
// written, chowned on its descriptor and renamed over <name>, which
// replaces a link instead of following it. Anything else stops the start
// with an error naming the path.
func prepareFiles(cfg Config, owner *fileOwner) error {
	spikeDir := filepath.Dir(cfg.RunJSON)
	dataDir := filepath.Dir(spikeDir)
	if filepath.Base(spikeDir) != spikeDirName {
		return fmt.Errorf("hubproc: %s is not in the data directory's %s folder", cfg.RunJSON, spikeDirName)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dataDir); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("hubproc: %s is a link or no directory", dataDir)
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if owner != nil {
		if err := ownDir(root, dataDir, ".", owner); err != nil {
			return err
		}
	}
	if err := root.Mkdir(spikeDirName, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := ownDir(root, dataDir, spikeDirName, owner); err != nil {
		return err
	}
	for name, data := range cfg.Files {
		if name != filepath.Base(name) {
			return fmt.Errorf("hubproc: %q is not a file name", name)
		}
		if err := writeFile(root, dataDir, filepath.Join(spikeDirName, name), data, owner); err != nil {
			return err
		}
	}
	if owner == nil {
		return nil
	}
	if _, err := root.Lstat("state"); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := ownDir(root, dataDir, "state", owner); err != nil {
		return err
	}
	for _, name := range stateFiles {
		if err := reownFile(root, dataDir, filepath.Join("state", name), owner); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes data at rel of root, 0600, owned by owner when set,
// through rel.tmp, made with O_EXCL after the removal of any old one, and
// renamed over rel.
func writeFile(root *os.Root, dataDir, rel string, data []byte, owner *fileOwner) error {
	tmp := rel + ".tmp"
	if err := root.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("hubproc: %s: %w", filepath.Join(dataDir, tmp), err)
	}
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("hubproc: %s: %w", filepath.Join(dataDir, tmp), err)
	}
	_, err = f.Write(data)
	if err == nil {
		err = chmodOwn(f, 0o600, owner)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(tmp, rel)
	}
	if err != nil {
		_ = root.Remove(tmp)
		return fmt.Errorf("hubproc: %s: %w", filepath.Join(dataDir, rel), err)
	}
	return nil
}

// ownDir checks that rel of root is a directory, not a link, and with an
// owner gives it to the owner, 0700, through its own descriptor, which
// must be the directory that root.Lstat showed.
func ownDir(root *os.Root, dataDir, rel string, owner *fileOwner) error {
	path := filepath.Join(dataDir, rel)
	info, err := root.Lstat(rel)
	if err != nil {
		return fmt.Errorf("hubproc: %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("hubproc: %s is a link or no directory", path)
	}
	if owner == nil {
		return nil
	}
	f, err := root.Open(rel)
	if err != nil {
		return fmt.Errorf("hubproc: %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if opened, err := f.Stat(); err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("hubproc: %s changed while it was opened", path)
	}
	if err := chmodOwn(f, 0o700|fs.ModeDir, owner); err != nil {
		return fmt.Errorf("hubproc: %s: %w", path, err)
	}
	return nil
}
