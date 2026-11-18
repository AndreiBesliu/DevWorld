// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package results

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// now is Write's clock; tests replace it.
var now = time.Now

// Write validates e, stamps its AtMs and writes it to
// <dir>/<code>/<set>/<runId>.json, 0600, as one line of JSON, as c1's
// results.Write: through a temporary file beside it, synced and renamed
// over it, so a reader never sees half an envelope, then the folder is
// synced, so the envelope keeps its name after a power cut; so is the
// parent of each folder that Write creates. A rewrite of the same run
// replaces the file. The known-secrets scan is ipcam-hub's diag
// scan-secrets, which uploadspike runs after the close.
func Write(dir string, e Envelope) error {
	e.AtMs = now().UnixMilli()
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
	final := filepath.Join(folder, e.RunID+".json")
	f, err := os.CreateTemp(folder, e.RunID+".*.tmp") // 0600
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
	if beforeRename != nil {
		beforeRename(tmp, final)
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	if err := syncDir(folder); err != nil {
		return errors.Join(err, os.Remove(final))
	}
	return nil
}

// beforeRename, when set, runs between the temporary file's close and its
// rename: a test seam.
var beforeRename func(tmp, final string)

// mkdirAllSynced creates folder and its missing parents, 0700, and syncs
// the parent of each folder that it created, top down.
func mkdirAllSynced(folder string) error {
	var missing []string
	for d := folder; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || !errors.Is(err, os.ErrNotExist) || filepath.Dir(d) == d {
			break
		}
		missing = append(missing, d)
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
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
