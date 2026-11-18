// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !linux

package main

import "os/exec"

// startWithUs starts cmd; on Windows the console's close ends it with
// uploadspike.
func startWithUs(cmd *exec.Cmd) error { return cmd.Start() }
