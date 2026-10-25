// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Write validates e and writes it to <dir>/<code>/<set>/<runId>.json, as one
// line of JSON. The file is written beside its final name, synced and
// renamed over it, so a reader never sees half an envelope, and the folder
// is synced after the rename, so the envelope keeps its name after a power
// cut; so is the parent of each folder that Write creates.
func Write(dir string, e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	folder := filepath.Join(dir, e.Code, e.Set)
	if err := mkdirAllSynced(folder); err != nil {
		return err
	}
	f, err := os.CreateTemp(folder, e.RunID+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // gone after the rename
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(folder, e.RunID+".json")); err != nil {
		return err
	}
	return syncDir(folder)
}

// mkdirAllSynced creates folder and its missing parents, as os.MkdirAll
// does, and syncs the parent of each folder that it created, top down, so
// that the new folders survive a power cut.
func mkdirAllSynced(folder string) error {
	var missing []string
	for d := folder; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || !errors.Is(err, os.ErrNotExist) || filepath.Dir(d) == d {
			break
		}
		missing = append(missing, d)
	}
	if err := os.MkdirAll(folder, 0o750); err != nil {
		return err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := syncDir(filepath.Dir(missing[i])); err != nil {
			return err
		}
	}
	return nil
}

// syncDir makes the entries of the folder dir durable: an fsync of the
// directory, except on Windows, which has none (NTFS journals its entries).
// Tests replace it to see when Write calls it.
var syncDir = func(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // the results folder that Write made
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
