package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The firewall seam's lines of the block and of its removal (nft).
const (
	blockMarker   = "nft -f -"
	unblockMarker = "nft delete table inet ipcam_spike"
)

// cutSignals are the ends of an mi5 that TestMI5_HolderEndsAnyWay tries:
// a closed window or SSH session (SIGHUP), then a kill.
func cutSignals() []os.Signal { return []os.Signal{syscall.SIGHUP, os.Kill} }

// liveExes are the PIDs of the processes that run the program at path
// (Linux's /proc).
func liveExes(path string) []int {
	var out []int
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe")); err == nil && exe == path {
			out = append(out, pid)
		}
	}
	return out
}

// parentOf is pid's parent (Linux's /proc), 0 when it has none or ended.
func parentOf(pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}

// hubToken is the hub's control token, from its environment at its start
// (Linux's /proc/<pid>/environ: the hub unsets the variable in its own
// copy only).
func hubToken(pid int) (string, bool) {
	env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return "", false
	}
	for kv := range strings.SplitSeq(string(env), "\x00") {
		if v, ok := strings.CutPrefix(kv, "IPCAM_CONTROL_TOKEN="); ok {
			return v, true
		}
	}
	return "", false
}
