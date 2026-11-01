package mi3

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestCPU_ReadsProcInUserHZ: /proc/stat's cpu line gives the machine's busy
// time (all but idle and iowait) and total in USER_HZ ticks; a process's
// stat gives its parent and its user and system ticks after the command's
// name, which may hold spaces and parentheses.
func TestCPU_ReadsProcInUserHZ(t *testing.T) {
	dir := t.TempDir()
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", "cpu  100 20 30 400 50 6 4 2 7 0\ncpu0 1 2 3 4 5 6 7 8 0 0\n")
	write("10/stat", "10 (ipcam hub (x)) S 1 10 10 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 9 0 100 200000 300 0\n")
	write("10/statm", "5000 300 100 10 0 400 0\n")
	write("11/stat", "11 (mediamtx) S 10 10 10 0 -1 4194560 100 0 0 0 70 30 0 0 20 0 9 0 100 200000 300 0\n")
	write("12/stat", "12 (other) S 1 12 12 0 -1 4194560 100 0 0 0 1 1 0 0 20 0 9 0 100 200000 300 0\n")
	old := procDir
	procDir = dir
	t.Cleanup(func() { procDir = old })

	busy, total, err := machineTimes()
	if err != nil || busy != 162*tick || total != 612*tick {
		t.Errorf("machineTimes = %v, %v, %v; want 1.62s busy of 6.12s", busy, total, err)
	}
	if got, err := processTimes(10); err != nil || got != 3*time.Second {
		t.Errorf("processTimes(10) = %v, %v; want 3s", got, err)
	}
	if got, err := childrenOf(10); err != nil || !slices.Equal(got, []int{11}) {
		t.Errorf("childrenOf(10) = %v, %v; want [11]", got, err)
	}
	if got := tree(10); !slices.Equal(got, []int{10, 11}) {
		t.Errorf("tree(10) = %v, want [10 11]", got)
	}
	if got, err := processRSS(10); err != nil || got != 300*uint64(os.Getpagesize()) { //nolint:gosec // the page size is positive
		t.Errorf("processRSS(10) = %d, %v; want 300 pages", got, err)
	}
}
