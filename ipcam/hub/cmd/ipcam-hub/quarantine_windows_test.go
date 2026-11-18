package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// checkQuarantined checks that only SYSTEM and the Administrators may read
// the quarantine's file or directory at p: its DACL is protected from
// inheritance and grants those two, nobody else; a directory belongs to
// one of them, since an owner may always rewrite its DACL (C2-R21).
func checkQuarantined(t *testing.T, p string, dir bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
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
	if dir {
		owner, _, err := sd.Owner()
		if err != nil || !trustedOwner(owner) {
			t.Errorf("%s: owner %v (%v); want SYSTEM or the Administrators", p, owner, err)
		}
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

// securityOf is the owner and the DACL of the file at p, as SDDL.
func securityOf(t *testing.T, p string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

// setOwner makes sid the owner of the file or directory at p.
func setOwner(t *testing.T, p string, sid *windows.SID) {
	t.Helper()
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION,
		sid, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

// TestDiagScanSecrets_JunctionSwappedBeforeTheMoveIsNotFollowed (C2-R21): a
// directory of a matched file's path that is swapped for a junction to a
// directory outside the results, after the scan read the file and before
// it moves, leads the quarantine's DACL to no file outside: the outside
// file keeps its bytes, its owner and its DACL, stays where it is, and the
// matched path is named in unmoved.
func TestDiagScanSecrets_JunctionSwappedBeforeTheMoveIsNotFollowed(t *testing.T) {
	value := "SENT" + "INEL-a-leaked-value"
	base := t.TempDir()
	dir := filepath.Join(base, "results")
	writeTree(t, dir, map[string]string{"MI6/pc/run1.json": `{"note":"` + value + `"}`})
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"pc/run1.json": "outside"})
	victim := filepath.Join(outside, "pc", "run1.json")
	before := securityOf(t, victim)
	beforeQuarantineMove = func(string) {
		if err := os.Rename(filepath.Join(dir, "MI6"), filepath.Join(dir, "MI6-old")); err != nil {
			t.Error(err)
			return
		}
		out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(dir, "MI6"), outside).CombinedOutput() //nolint:gosec // G204: the test's own paths
		if err != nil {
			t.Errorf("mklink /J: %v: %s", err, out)
		}
	}
	t.Cleanup(func() { beforeQuarantineMove = nil })
	code, lines, out, stderr := runScan(t, stdinLine(value), "--dir", dir)
	if len(lines) != 1 {
		t.Fatalf("scan-secrets = %d, %q (%s); want one line", code, out, stderr)
	}
	if after := securityOf(t, victim); after != before {
		t.Errorf("the outside file's security %s, before the scan %s; want it unchanged", after, before)
	}
	if b, err := os.ReadFile(victim); err != nil || string(b) != "outside" { //nolint:gosec // G304: the test's own file
		t.Errorf("the outside file: %q, %v; want it in place, unchanged", b, err)
	}
	moved, unmoved := stringsOf(lines[0], "moved"), stringsOf(lines[0], "unmoved")
	if code != exitFatal || len(moved) != 0 || !slices.Equal(unmoved, []string{"MI6/pc/run1.json"}) {
		t.Errorf("scan-secrets = %d, moved %v, unmoved %v (%s); want exit 1 with the path named in unmoved", code, moved, unmoved, stderr)
	}
}

// TestDiagScanSecrets_QuarantineOwnerChecked (C2-R21): an existing
// quarantine is used only when SYSTEM or the Administrators own it, as on
// Linux only when the scan's uid owns it. One that belongs to the account
// that runs the test, whose owner could rewrite its DACL, is refused: the
// matched file stays in the results, named in unmoved, and the scan exits
// 1. One that the Administrators own, with a DACL that lets others read
// it, is narrowed before a file moves in. A quarantine that the scan makes
// belongs to the Administrators.
func TestDiagScanSecrets_QuarantineOwnerChecked(t *testing.T) {
	value := "SENT" + "INEL-a-leaked-value"
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		owner *windows.SID // nil: no quarantine before the scan
		moved bool
	}{
		{"none before the scan", nil, true},
		{"the Administrators'", admins, true},
		{"the test account's", tokenUser.User.Sid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "results")
			writeTree(t, dir, map[string]string{"MI6/pc/run1.json": `{"note":"` + value + `"}`})
			q := filepath.Join(base, "results-quarantine")
			if tc.owner != nil {
				if err := os.Mkdir(q, 0o700); err != nil {
					t.Fatal(err)
				}
				setOwner(t, q, tc.owner)
			}
			code, lines, out, stderr := runScan(t, stdinLine(value), "--dir", dir)
			if len(lines) != 1 {
				t.Fatalf("scan-secrets = %d, %q (%s); want one line", code, out, stderr)
			}
			moved, unmoved := stringsOf(lines[0], "moved"), stringsOf(lines[0], "unmoved")
			_, statErr := os.Stat(filepath.Join(dir, "MI6", "pc", "run1.json"))
			if tc.moved {
				if code != exitOK || !slices.Equal(moved, []string{"MI6/pc/run1.json"}) || len(unmoved) != 0 || statErr == nil {
					t.Errorf("scan-secrets = %d, moved %v, unmoved %v (%s); want the file moved", code, moved, unmoved, stderr)
				}
				checkQuarantined(t, q, true)
				return
			}
			if code != exitFatal || len(moved) != 0 || !slices.Equal(unmoved, []string{"MI6/pc/run1.json"}) || statErr != nil {
				t.Errorf("scan-secrets = %d, moved %v, unmoved %v, the file %v (%s); want exit 1 with the file kept and named in unmoved",
					code, moved, unmoved, statErr, stderr)
			}
		})
	}
}
