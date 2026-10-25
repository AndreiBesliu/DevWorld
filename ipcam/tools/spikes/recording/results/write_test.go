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
