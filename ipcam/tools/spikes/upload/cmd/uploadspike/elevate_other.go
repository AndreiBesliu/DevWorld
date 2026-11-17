// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !windows

package main

import "errors"

// relaunchElevated is Windows' UAC relaunch; elsewhere the guide runs
// uploadspike with sudo.
func relaunchElevated([]string) (int, error) { return 0, errors.New("relaunchElevated: Windows only") }
