package main

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// ownGroupForTest starts an uploadspike in a process group of its own, so
// that a console control event can reach it alone.
func ownGroupForTest(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// processes are the running processes' PIDs and parents.
func processes() map[uint32]uint32 {
	out := map[uint32]uint32{}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		out[entry.ProcessID] = entry.ParentProcessID
	}
	return out
}

// childrenOf are the running processes that one of parents (PID → its
// start) started: Windows keeps a dead parent's PID in its children's
// entries, and a child started after its parent, so a later process that
// reuses the parent's PID has none of them. A test's command that its
// cleanup killed can leave such a child, a PowerShell of the firewall's
// check, in the command's current folder, which Windows then cannot
// remove.
func childrenOf(parents map[int]int64) []int {
	var out []int
	for pid, ppid := range processes() {
		parentStart, ok := parents[int(ppid)]
		if !ok {
			continue
		}
		if start, err := runstate.ProcStartMs(int(pid)); err == nil && start >= parentStart {
			out = append(out, int(pid))
		}
	}
	return out
}
