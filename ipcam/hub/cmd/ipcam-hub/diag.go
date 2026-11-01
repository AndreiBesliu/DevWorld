package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
)

// diagLines are the diag subcommands' lines of a usage error.
const diagLines = "ipcam-hub diag host [--rec <dir>]\n       ipcam-hub diag pimodel --raw <model>\n" +
	"       ipcam-hub diag cryptobench [--seconds 60]\n" +
	"       ipcam-hub diag readbench --file <seg.mp4.age> --identity <file> [--reads 1000] [--size part|<bytes>]\n" +
	"       ipcam-hub diag seekbench --file <seg.mp4.age> --identity <file> [--seeks 20] [--whole-ram]\n" +
	"       ipcam-hub diag readopen --config <run.json> --cam <cam> [--profile main]\n" +
	"       ipcam-hub diag dest-probe --config <run.json> --dest <id> [--delete-segment] [--poll 30s] [--for 10m]\n" +
	"       ipcam-hub diag reach --hosts <host[:port],…> [--lan <host:port>]\n" +
	"       ipcam-hub diag uploads --config <run.json> [--list-remote]"

// diagUsage is the usage of the diag subcommands.
const diagUsage = "usage: " + diagLines

// diagCommand is "ipcam-hub diag": it prints what the hub reads about its
// host, or what its benches measure on it (ENC-08: the encryption
// throughput, and reads and seeks in an encrypted recording), as one JSON
// object on stdout; or, for the upload spike (Task 7b), a read of the open
// segment, a destination's probe, what the network reaches and the uploads'
// counts, as JSON lines.
func diagCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "host":
			return diagHostCommand(args[1:], stdout, stderr)
		case "pimodel":
			return diagPimodelCommand(args[1:], stdout, stderr)
		case "cryptobench":
			return diagCryptobenchCommand(args[1:], stdout, stderr)
		case "readbench":
			return diagReadbenchCommand(args[1:], stdout, stderr)
		case "seekbench":
			return diagSeekbenchCommand(args[1:], stdout, stderr)
		case "readopen":
			return diagReadopenCommand(args[1:], stdout, stderr)
		case "dest-probe":
			return diagDestProbeCommand(args[1:], stdout, stderr)
		case "reach":
			return diagReachCommand(args[1:], stdout, stderr)
		case "uploads":
			return diagUploadsCommand(args[1:], stdout, stderr)
		}
	}
	_, _ = fmt.Fprintln(stderr, diagUsage)
	return exitFatal
}

// printJSONLine prints v as one line of JSON.
func printJSONLine(stdout, stderr io.Writer, v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	if _, err := stdout.Write(append(b, '\n')); err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	return exitOK
}

// errNoCatalog is the refusal of the diag commands that read the catalog on
// a data directory where no run left one (g2-11).
var errNoCatalog = errors.New("no catalog")

// catalogPath is the catalog of the data directory dataDir.
func catalogPath(dataDir string) string { return filepath.Join(dataDir, "state", "catalog.db") }

// haveCatalog returns errNoCatalog unless a run left a catalog in dataDir.
func haveCatalog(dataDir string) error {
	_, err := os.Stat(catalogPath(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return errNoCatalog
	}
	return err
}

// openExistingCatalog opens the catalog that a run left in dataDir, and
// never creates one: errNoCatalog when there is none (g2-11).
func openExistingCatalog(ctx context.Context, dataDir string) (*catalog.DB, error) {
	if err := haveCatalog(dataDir); err != nil {
		return nil, err
	}
	return catalog.Open(ctx, catalogPath(dataDir))
}

// uploadsOf loads the run configuration at path for a diag command of the
// uploads: its data directory, its uploads section, and the hub's ID, which
// it reads and never creates (g2-11), after it checked that a run left a
// catalog.
func uploadsOf(path string) (dataDir string, uploads *uploadsConfig, hubID string, err error) {
	cfg, _, _, err := loadRunConfig(path)
	switch {
	case err != nil:
		return "", nil, "", err
	case cfg.Uploads == nil:
		return "", nil, "", fmt.Errorf("%s has no uploads section", path)
	}
	if err := haveCatalog(cfg.DataDir); err != nil {
		return "", nil, "", err
	}
	if hubID, err = readHubID(filepath.Join(cfg.DataDir, "state")); err != nil {
		return "", nil, "", err
	}
	return cfg.DataDir, cfg.Uploads, hubID, nil
}

// destByID is the uploads section's destination id.
func (u *uploadsConfig) destByID(id string) (destDef, bool) {
	for _, d := range u.Destinations {
		if d.ID == id {
			return d, true
		}
	}
	return destDef{}, false
}
