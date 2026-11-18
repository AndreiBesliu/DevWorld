// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !linux && !windows

package main

import "os/exec"

// startWithUs starts cmd, and returns a channel closed once it ended.
func startWithUs(cmd *exec.Cmd) (<-chan struct{}, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return done, nil
}
