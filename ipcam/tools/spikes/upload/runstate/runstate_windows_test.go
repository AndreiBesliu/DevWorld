package runstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUpdate_WaitsForAReaderOnWindows: an Update while another process
// reads the run state (Go opens files without FILE_SHARE_DELETE, so
// MoveFileEx cannot replace it meanwhile) succeeds once the reader closes
// the file, instead of failing, which lost the update; a Load meanwhile
// reads the state.
func TestUpdate_WaitsForAReaderOnWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	if err := Update(path, func(s *State) error { s.Mode = "first"; return nil }); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = reader.Close()
	}()
	if err := Update(path, func(s *State) error { s.Mode = "second"; return nil }); err != nil {
		t.Fatalf("an Update while a reader had the file open: %v", err)
	}
	s, err := Load(path)
	if err != nil || s.Mode != "second" {
		t.Errorf("the run state after the update: %+v, %v; want mode second", s, err)
	}
}
