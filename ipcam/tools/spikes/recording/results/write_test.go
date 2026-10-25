package results

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestWrite_SyncsTheFolderAfterTheRename: Write syncs the set's folder once
// the envelope has its final name, so that the name survives a power cut on
// the writer (an fsync of the directory; nothing on Windows), and a failed
// sync is Write's error. The folders that Write creates are made durable
// too: the parent of each is synced after it is created, top down, before
// the envelope is written.
func TestWrite_SyncsTheFolderAfterTheRename(t *testing.T) {
	dir := t.TempDir()
	code := filepath.Join(dir, "MI1")
	folder := filepath.Join(code, "pi-kill9")
	final := filepath.Join(folder, "pi-kill9-07.json")
	realSync := syncDir
	t.Cleanup(func() { syncDir = realSync })
	var synced []string
	syncDir = func(d string) error {
		_, err := os.Stat(final)
		if d == folder && err != nil {
			t.Errorf("the folder is synced before the rename: %v", err)
		}
		if d != folder && err == nil {
			t.Errorf("%s is synced after the envelope was written", d)
		}
		synced = append(synced, d)
		return realSync(d)
	}
	if err := Write(dir, sample()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if want := []string{dir, code, folder}; !slices.Equal(synced, want) {
		t.Errorf("synced %q, want the parents of the new folders, then the set's folder: %q", synced, want)
	}
	synced = nil
	if err := Write(dir, sample()); err != nil {
		t.Fatalf("Write again: %v", err)
	}
	if !slices.Equal(synced, []string{folder}) {
		t.Errorf("synced %q on the second Write, want the set's folder %q once", synced, folder)
	}

	failed := errors.New("sync failed")
	syncDir = func(string) error { return failed }
	if err := Write(dir, sample()); !errors.Is(err, failed) {
		t.Errorf("Write with a failing folder sync = %v, want %v", err, failed)
	}

	if err := realSync(folder); err != nil {
		t.Errorf("syncDir(%q) = %v", folder, err)
	}
}

// TestWrite_RemovesTheEnvelopeWhenTheFolderSyncFails: when the folder's
// sync fails after the rename, Write removes the renamed envelope before it
// returns the error: the writer (reccontroller) then answers that the run
// does not count, and the host redoes it, so no envelope that would count
// may stay on disk for it.
func TestWrite_RemovesTheEnvelopeWhenTheFolderSyncFails(t *testing.T) {
	dir := t.TempDir()
	e := sample()
	folder := filepath.Join(dir, e.Code, e.Set)
	final := filepath.Join(folder, e.RunID+".json")
	realSync := syncDir
	t.Cleanup(func() { syncDir = realSync })
	failed := errors.New("sync failed")
	syncDir = func(d string) error {
		if _, err := os.Stat(final); err == nil {
			return failed // the folder's sync after the rename
		}
		return realSync(d)
	}
	if err := Write(dir, e); !errors.Is(err, failed) {
		t.Fatalf("Write with a failing folder sync = %v, want %v", err, failed)
	}
	if _, err := os.Stat(final); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the envelope is still on disk after the failed sync (%v): it would count for a run that the host redoes", err)
	}
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 0 {
		t.Errorf("the set's folder holds %v (%v), want nothing", entries, err)
	}
}
