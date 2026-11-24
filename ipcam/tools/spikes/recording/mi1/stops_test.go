package mi1

import (
	"errors"
	"strings"
	"testing"
)

// TestBugcheck_ChecksNotMyFaultAgainBeforeTheCrash (security-1): the
// preflight checked notmyfault64.exe's signature minutes or hours before
// the crash, and the boot task runs it elevated: the bugcheck checks it
// again right before it starts it, and starts nothing that fails.
func TestBugcheck_ChecksNotMyFaultAgainBeforeTheCrash(t *testing.T) {
	const path = `C:\proba-inregistrare\notmyfault64.exe`
	var started []string
	start := func(p string) error {
		started = append(started, p)
		return nil
	}
	err := bugcheck(path, func(string) error { return errors.New("TRUST_E_NOSIGNATURE") }, start)
	if err == nil || !strings.Contains(err.Error(), "notmyfault64.exe nu mai e semnat de Microsoft") || len(started) != 0 {
		t.Errorf("an unsigned NotMyFault: %v, started %q; want a Romanian refusal and nothing started", err, started)
	}
	var verified []string
	if err := bugcheck(path, func(p string) error { verified = append(verified, p); return nil }, start); err != nil {
		t.Fatalf("a signed NotMyFault: %v", err)
	}
	if len(verified) != 1 || verified[0] != path || len(started) != 1 || started[0] != path {
		t.Errorf("verified %q, started %q; want %s checked, then started", verified, started, path)
	}
}
