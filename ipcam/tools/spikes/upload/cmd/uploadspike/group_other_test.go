//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// ownGroupForTest starts an uploadspike in a process group of its own, as a
// terminal's job: a hangup to the group then reaches it, and the test.
func ownGroupForTest(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// childrenOf finds nothing here: Linux removes a folder that a process
// still uses as its current one (see the Windows version).
func childrenOf(map[int]int64) []int { return nil }
