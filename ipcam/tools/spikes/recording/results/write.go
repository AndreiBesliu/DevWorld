// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Write validates e and writes it to <dir>/<code>/<set>/<runId>.json, as one
// line of JSON. The file is written beside its final name, synced and
// renamed over it, so a reader never sees half an envelope.
func Write(dir string, e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	folder := filepath.Join(dir, e.Code, e.Set)
	if err := os.MkdirAll(folder, 0o750); err != nil {
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
	return os.Rename(tmp, filepath.Join(folder, e.RunID+".json"))
}
