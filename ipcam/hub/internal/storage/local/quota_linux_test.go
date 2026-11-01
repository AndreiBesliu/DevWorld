package local_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// independentQuota reads dir's filesystem with GNU df: the bytes available to
// an unprivileged writer, and the filesystem's size.
func independentQuota(t *testing.T, dir string) (free, total int64) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "df", "-B1", "--output=size,avail", dir).Output() //nolint:gosec // G204: the test's temporary root
	if err != nil {
		t.Fatalf("df %s: %v", dir, err)
	}
	// A header line, then "<size> <avail>".
	fields := strings.Fields(string(out))
	if len(fields) != 4 {
		t.Fatalf("df %s printed %q; want a header and one line of two numbers", dir, out)
	}
	total, terr := strconv.ParseInt(fields[2], 10, 64)
	free, ferr := strconv.ParseInt(fields[3], 10, 64)
	if terr != nil || ferr != nil {
		t.Fatalf("df %s printed %q: %v, %v", dir, out, terr, ferr)
	}
	return free, total
}
