package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/playback"
)

// readopenSchema names the output of diag readopen.
const readopenSchema = "ipcam.diag.readopen/1"

// readopenRead is diag readopen's line for a read of the open segment.
type readopenRead struct {
	Schema       string  `json:"schema"`
	OK           bool    `json:"ok"`
	Encrypted    bool    `json:"encrypted"`
	Parts        int     `json:"parts"`
	DurableBytes int64   `json:"durableBytes"`
	ReadMs       float64 `json:"readMs"`
}

// diagFailure is a diag line that says why there is no result.
type diagFailure struct {
	Schema string `json:"schema"`
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
}

// diagReadopenCommand is "ipcam-hub diag readopen --config <run.json> --cam
// <cam> [--profile main]" (P14): it reads the stream's open segment, as
// playback.ReadOpen does, and prints one ipcam.diag.readopen/1 line, exit 0,
// with ok false and no-open-segment or unreadable when there is no read. It
// never prints a path. It exits 1 on a usage error, and with no catalog: it
// opens none and creates none (g2-11).
func diagReadopenCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag readopen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "the run configuration (run.json)")
	cam := flags.String("cam", "", "the camera")
	profile := flags.String("profile", "main", "the stream's profile")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *config == "" || *cam == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	cfg, _, id, err := loadRunConfig(*config)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	if err := haveCatalog(cfg.DataDir); err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag readopen:", err)
		return exitFatal
	}
	var tail crypto.TailSealer
	if id != nil {
		tail = crypto.NewTailSealer(id)
	}
	read, err := playback.ReadOpen(context.Background(), filepath.Join(cfg.DataDir, "rec"), *cam, *profile, tail)
	switch {
	case errors.Is(err, playback.ErrNoOpenSegment):
		return printJSONLine(stdout, stderr, diagFailure{Schema: readopenSchema, Error: "no-open-segment"})
	case err != nil:
		return printJSONLine(stdout, stderr, diagFailure{Schema: readopenSchema, Error: "unreadable"})
	}
	return printJSONLine(stdout, stderr, readopenRead{Schema: readopenSchema, OK: true, Encrypted: read.Encrypted,
		Parts: read.Parts, DurableBytes: read.DurableBytes, ReadMs: read.ReadMs})
}
