// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// isElevated reports whether uploadspike runs elevated: the scan's
// quarantine allows only SYSTEM and the Administrators (C2-R20). The
// tests' envNotElevated says no.
func isElevated() bool {
	if os.Getenv(envNotElevated) == "1" {
		return false
	}
	return windows.GetCurrentProcessToken().IsElevated()
}
