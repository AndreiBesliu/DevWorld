package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The firewall seam's lines of the block and of its removal (Windows
// Firewall, through PowerShell).
const (
	blockMarker   = "New-NetFirewallRule"
	unblockMarker = "Remove-NetFirewallRule -Group 'ipcam-spike-wanblock'"
)

// cutSignals are the ends of an mi5 that TestMI5_HolderEndsAnyWay tries
// on Windows: TerminateProcess.
func cutSignals() []os.Signal { return []os.Signal{os.Kill} }

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

// liveExes are the PIDs of the running processes whose image is the file
// at path (os.SameFile: a runner's temporary folder may be named in its
// short 8.3 form, the image in its long one).
func liveExes(path string) []int {
	want, err := os.Stat(path)
	if err != nil {
		return nil
	}
	var out []int
	for pid := range processes() {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf)) //nolint:gosec // MAX_LONG_PATH fits
		err = windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
		_ = windows.CloseHandle(h)
		if err != nil {
			continue
		}
		if got, err := os.Stat(windows.UTF16ToString(buf[:n])); err == nil && os.SameFile(got, want) {
			out = append(out, int(pid))
		}
	}
	return out
}

// parentOf is pid's parent, 0 when it ended.
func parentOf(pid int) int {
	return int(processes()[uint32(pid)]) //nolint:gosec // a PID
}

// hubToken: another process's environment is not readable on Windows.
func hubToken(int) (string, bool) { return "", false }
