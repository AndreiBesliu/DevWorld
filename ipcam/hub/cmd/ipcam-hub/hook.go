package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

// hookTimeout bounds the hook command.
const hookTimeout = 15 * time.Second

// hookCommand is "ipcam-hub hook segment-complete", which MediaMTX runs
// when a segment is complete (G05, MTX-3): it posts the segment that its
// environment describes to the running ipcam-hub.
func hookCommand(args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "segment-complete" {
		_, _ = fmt.Fprintln(stderr, "usage: ipcam-hub hook segment-complete")
		return exitFatal
	}
	// When MediaMTX stops, it interrupts its hook commands and waits for
	// them: the post of the segment it has just closed must still land.
	signal.Ignore(os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	if err := recorder.PostSegmentComplete(ctx, os.Getenv); err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	return exitOK
}
