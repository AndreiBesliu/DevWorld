package recorder

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"testing"
)

// processAlive reports whether pid runs. A zombie has exited: it only waits
// for its parent, here init, to reap it.
func processAlive(t *testing.T, pid int) bool {
	t.Helper()
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 || i+2 >= len(stat) {
		t.Fatalf("cannot read the state in /proc/%d/stat", pid)
	}
	state := stat[i+2]
	return state != 'Z' && state != 'X'
}
