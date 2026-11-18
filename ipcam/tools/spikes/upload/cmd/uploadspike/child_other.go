// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !linux && !windows

package main

import "os/exec"

// startWithUs starts cmd.
func startWithUs(cmd *exec.Cmd) error { return cmd.Start() }
