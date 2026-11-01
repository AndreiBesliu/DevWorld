package mi1

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestSystemNTP_RealW32tmAnswersTheSource (mi1-2): on the Windows runner,
// the real `w32tm /query /source` answers with the clock's source alone,
// one line without a label, which w32tmSynced reads in any language; or it
// fails with one of the failures that the preflight names a remedy for.
func TestSystemNTP_RealW32tmAnswersTheSource(t *testing.T) {
	out, err := runW32tm(t.Context(), "/query", "/source")
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		failure := w32tmError(string(out)+string(exit.Stderr), uint32(exit.ExitCode())) //nolint:gosec // a Windows exit code is a DWORD
		t.Logf("w32tm /query /source failed: %v", failure)
		if !errors.Is(failure, ErrNTPNeedsAdmin) && !errors.Is(failure, ErrTimeServiceStopped) {
			t.Errorf("w32tm /query /source failed with %v, not a failure that the preflight names", failure)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.Lines(string(out)) {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	t.Logf("w32tm /query /source: %q, synced %v", out, w32tmSynced(string(out)))
	if len(lines) != 1 {
		t.Errorf("w32tm /query /source printed %q, want the source alone, on one line", out)
	}
}
