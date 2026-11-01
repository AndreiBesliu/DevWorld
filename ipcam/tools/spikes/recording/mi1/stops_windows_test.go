package mi1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// TestOSStop_RefusesAnUnsignedNotMyFault (security-1): the bugcheck checks
// notmyfault64.exe's signature again right before it runs it, so a file
// replaced after the preflight, here the unsigned test binary, is never
// started.
func TestOSStop_RefusesAnUnsignedNotMyFault(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.ReadFile(exe) //nolint:gosec // the test binary itself
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, NotMyFault), self, 0o700); err != nil { //nolint:gosec // an executable stand-in
		t.Fatal(err)
	}
	err = OSStop(t.Context(), controller.KindBugcheck, dir)
	if err == nil || !strings.Contains(err.Error(), "nu mai e semnat de Microsoft") {
		t.Errorf("OSStop with an unsigned notmyfault64.exe = %v, want the signature's refusal", err)
	}
}
