// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !windows

package main

import "os"

// isElevated is true off Windows: on the Pi the guide runs uploadspike
// with sudo, and the scan's quarantine is its own identity's. The tests'
// envNotElevated says no.
func isElevated() bool { return os.Getenv(envNotElevated) != "1" }
