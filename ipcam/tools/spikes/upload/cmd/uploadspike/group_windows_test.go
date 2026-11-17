package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ownGroupForTest starts an uploadspike in a process group of its own, so
// that a console control event can reach it alone.
func ownGroupForTest(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}
