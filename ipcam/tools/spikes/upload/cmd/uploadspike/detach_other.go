// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !linux

package main

import (
	"context"
	"errors"
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi8"
)

// canDetach: on Windows the console window is MI8's process, which holds
// its lock and runs the 72 h (P49).
const canDetach = false

// detachMI8 is never called where canDetach is false.
func detachMI8(context.Context, *env, *os.File, []mi8.CameraRef, string) error {
	return errors.New("mi8: no detached copy on this platform")
}

// ignoreHangup does nothing: there is no detached copy here.
func ignoreHangup() {}

// closeStdin closes the copy's stdin.
func closeStdin() { _ = os.Stdin.Close() }
