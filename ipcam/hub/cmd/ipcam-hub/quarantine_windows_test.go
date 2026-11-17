package main

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// checkQuarantined checks that only SYSTEM and the Administrators may read
// the quarantine's file or directory at p: its DACL is protected from
// inheritance and grants those two, nobody else.
func checkQuarantined(t *testing.T, p string, _ bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("%s: its DACL inherits (%s); want it protected", p, sd)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("%s: no DACL (%v): everyone may read it", p, err)
	}
	var system, admins bool
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) //nolint:gosec // G103: the ACE's SID, as GetAce lays it out
		switch {
		case ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE:
			t.Errorf("%s: an ACE of type %d (%s)", p, ace.Header.AceType, sd)
		case sid.IsWellKnown(windows.WinLocalSystemSid):
			system = true
		case sid.IsWellKnown(windows.WinBuiltinAdministratorsSid):
			admins = true
		default:
			t.Errorf("%s: %s may read it (%s); want SYSTEM and the Administrators only", p, sid, sd)
		}
	}
	if !system || !admins {
		t.Errorf("%s: SYSTEM %v, Administrators %v (%s); want both", p, system, admins, sd)
	}
}
