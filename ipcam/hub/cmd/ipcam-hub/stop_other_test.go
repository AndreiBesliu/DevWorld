//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// inOwnGroup leaves cmd as it is: a signal reaches only the process it is sent to.
func inOwnGroup(*exec.Cmd) {}

// withoutConsole leaves cmd as it is: outside Windows, no stop of the hub
// goes through a console.
func withoutConsole(*exec.Cmd) {}

// stopHub sends SIGTERM to the hub, as systemd or recspike stop it.
func stopHub(h *hub) error { return h.cmd.Process.Signal(syscall.SIGTERM) }
