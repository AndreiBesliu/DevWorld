package fakegdrive

import (
	"context"
	"sync"
)

// FolderStore is the gdrive driver's FolderStore in memory, for the tests
// that run the driver without a catalog: as the catalog's remote_folders, it
// keeps the IDs of folders and small files by destination and path, and
// keeps the first ID stored for a path (g2-11). Its methods are safe for
// concurrent use.
type FolderStore struct {
	mu  sync.Mutex
	ids map[string]string // by destination ID and path
}

// NewFolderStore returns an empty store.
func NewFolderStore() *FolderStore { return &FolderStore{ids: map[string]string{}} }

// Folder returns the ID stored for path on destID, and whether there is one.
func (s *FolderStore) Folder(_ context.Context, destID, path string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.ids[destID+"|"+path]
	return id, ok, nil
}

// PutFolder stores remoteID for path on destID, unless an ID is stored for
// it already: the first one stays.
func (s *FolderStore) PutFolder(_ context.Context, destID, path, remoteID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[destID+"|"+path]; !ok {
		s.ids[destID+"|"+path] = remoteID
	}
	return nil
}
