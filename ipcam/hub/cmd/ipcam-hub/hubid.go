package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// The files of the uploads under <dataDir>/state/.
const (
	// hubIDName is the hub's ID, a UUIDv7, which names its encrypted copies
	// (§9.10). The first run with an uploads section writes it (0600), and
	// every later run reads it.
	hubIDName = "hub_id"
	// hubLockName is the hub lock (C2-R11): a run with an uploads section
	// holds it for as long as it runs, and diag dest-probe, whose Drive
	// creates would add to the running hub's 2 a second, refuses while it
	// is held.
	hubLockName = "hub.lock"
)

// errNoHubID is the refusal of the diag commands on a data directory where
// no run with an uploads section wrote the hub's ID (g2-11).
var errNoHubID = errors.New("no hub id")

// errHubRunning is the refusal of a second holder of the hub lock.
var errHubRunning = errors.New("another ipcam-hub holds the hub lock of this data directory")

// readOrCreateHubID returns the hub's ID in stateDir, which it writes first
// when there is none.
func readOrCreateHubID(stateDir string) (string, error) {
	id, err := readHubID(stateDir)
	if !errors.Is(err, errNoHubID) {
		return id, err
	}
	uid, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	path := filepath.Join(stateDir, hubIDName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: under the data directory
	if errors.Is(err, fs.ErrExist) {
		return readHubID(stateDir) // another process wrote it first
	}
	if err != nil {
		return "", fmt.Errorf("the hub id: %w", err)
	}
	_, err = f.WriteString(uid.String() + "\n")
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("the hub id: %w", err)
	}
	return uid.String(), nil
}

// readHubID returns the hub's ID in stateDir, or errNoHubID when there is
// none; it creates nothing.
func readHubID(stateDir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, hubIDName)) //nolint:gosec // G304: under the data directory
	if errors.Is(err, fs.ErrNotExist) {
		return "", errNoHubID
	}
	if err != nil {
		return "", fmt.Errorf("the hub id: %w", err)
	}
	id, err := uuid.Parse(strings.TrimSpace(string(b)))
	if err != nil {
		return "", fmt.Errorf("the hub id in %s is not a UUID", hubIDName)
	}
	return id.String(), nil
}

// lockHub takes the hub lock in stateDir, exclusive and without waiting, and
// returns its release; errHubRunning when another process holds it. The
// system releases it when the process ends, a kill included.
func lockHub(stateDir string) (release func(), err error) {
	f, err := os.OpenFile(filepath.Join(stateDir, hubLockName), os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // G304: under the data directory
	if err != nil {
		return nil, fmt.Errorf("the hub lock: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
