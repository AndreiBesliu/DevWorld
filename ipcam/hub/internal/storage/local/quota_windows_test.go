package local_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// independentQuota reads dir's volume through .NET's DriveInfo in PowerShell:
// the bytes available to this user, and the volume's size.
func independentQuota(t *testing.T, dir string) (free, total int64) {
	t.Helper()
	script := fmt.Sprintf("$d = [System.IO.DriveInfo]::new('%s'); '{0} {1}' -f $d.TotalSize, $d.AvailableFreeSpace",
		strings.ReplaceAll(dir, "'", "''"))
	out, err := exec.CommandContext(t.Context(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output() //nolint:gosec // G204: the test's temporary root
	if err != nil {
		t.Fatalf("DriveInfo of %s: %v", dir, err)
	}
	if _, err := fmt.Sscan(string(out), &total, &free); err != nil {
		t.Fatalf("DriveInfo of %s printed %q: %v", dir, out, err)
	}
	return free, total
}
