package main

import "os/exec"

// execCmd is an exec.Cmd of a test.
type execCmd = exec.Cmd

// newExecCmd is exec.Command.
func newExecCmd(name string, args ...string) *execCmd {
	return exec.Command(name, args...) //nolint:gosec // the test's own binaries
}
