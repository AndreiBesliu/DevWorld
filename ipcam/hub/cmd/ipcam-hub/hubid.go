package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// hubIDWritten, when a test sets it, runs once the temporary file holds the
// ID, before its fsync and rename: its error stops the write there, as a
// cut would.
var hubIDWritten func() error

// readOrCreateHubID returns the hub's ID in stateDir, which it writes first
// when there is none. The caller holds the hub lock, so no other process
// writes it meanwhile.
func readOrCreateHubID(stateDir string) (string, error) {
	id, err := readHubID(stateDir)
	if !errors.Is(err, errNoHubID) {
		return id, err
	}
	uid, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	if err := writeHubID(stateDir, uid.String()); err != nil {
		return "", fmt.Errorf("the hub id: %w", err)
	}
	return uid.String(), nil
}

// writeHubID writes id to stateDir's hub_id through hub_id.tmp, which it
// truncates first, fsyncs and renames into place, then fsyncs stateDir
// (Task 7b review M1): a cut leaves the whole file or none, never an empty
// one, and a temporary file that a cut left is replaced.
func writeHubID(stateDir, id string) error {
	path := filepath.Join(stateDir, hubIDName)
	f, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: under the data directory
	if err != nil {
		return err
	}
	_, err = f.WriteString(id + "\n")
	if err == nil && hubIDWritten != nil {
		err = hubIDWritten()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	return syncDir(stateDir)
}

// syncDir fsyncs dir, so that a file renamed into it keeps its name after a
// power cut. Windows has no directory fsync: NTFS journals the directory
// entries.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // G304: under the data directory
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
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
