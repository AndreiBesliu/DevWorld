package recorder

import (
	"testing"

	"golang.org/x/sys/windows"
)

// processAlive reports whether pid runs.
func processAlive(t *testing.T, pid int) bool {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // a PID from the test
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	event, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		t.Fatalf("WaitForSingleObject: %v", err)
	}
	return event == uint32(windows.WAIT_TIMEOUT)
}
