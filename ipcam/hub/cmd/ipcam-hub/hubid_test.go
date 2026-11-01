package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestHubID_CutWriteLeavesNoHubID (Task 7b review M1): state/hub_id is
// written through a temporary file that is fsynced and renamed into place,
// so a write that a cut stops leaves no hub_id at all, never an empty or a
// partial one, and the next start writes a whole one; a temporary file that
// a cut left is ignored and replaced.
func TestHubID_CutWriteLeavesNoHubID(t *testing.T) {
	stateDir := t.TempDir()
	tmp := filepath.Join(stateDir, hubIDName+".tmp")
	if err := os.WriteFile(tmp, []byte("0198"), 0o600); err != nil { // a cut write's leftover
		t.Fatal(err)
	}
	cut := errors.New("cut")
	hubIDWritten = func() error { return cut }
	t.Cleanup(func() { hubIDWritten = nil })
	if _, err := readOrCreateHubID(stateDir); !errors.Is(err, cut) {
		t.Fatalf("readOrCreateHubID with a cut write = %v, want the cut", err)
	}
	if _, err := readHubID(stateDir); !errors.Is(err, errNoHubID) {
		t.Fatalf("after a cut write, readHubID = %v, want %v: no hub_id, never a partial one", err, errNoHubID)
	}
	hubIDWritten = nil
	id, err := readOrCreateHubID(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if u, err := uuid.Parse(id); err != nil || u.Version() != 7 {
		t.Errorf("hub id %q (%v), want a UUIDv7", id, err)
	}
	if again, err := readHubID(stateDir); err != nil || again != id {
		t.Errorf("readHubID = %q, %v; want %q", again, err, id)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is still there (%v): the write renames it into place", tmp, err)
	}
}
