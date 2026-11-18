// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package spiketest

import (
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procCreateRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// StandardToken is this elevated process's token as UAC filters it for a
// standard window: Administrators for deny only, no privilege beyond
// SeChangeNotifyPrivilege (DISABLE_MAX_PRIVILEGE, LUA_TOKEN), and medium
// integrity. The owner's own programs, and anything that runs as the
// owner without a UAC prompt, run with such a token.
func StandardToken(t *testing.T) windows.Token {
	t.Helper()
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &self); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = self.Close() }()
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	const disableMaxPrivilege, luaToken = 0x1, 0x4
	deny := windows.SIDAndAttributes{Sid: admins}
	var token windows.Token
	if r, _, err := procCreateRestrictedToken.Call(uintptr(self), disableMaxPrivilege|luaToken,
		1, uintptr(unsafe.Pointer(&deny)), 0, 0, 0, 0, uintptr(unsafe.Pointer(&token))); r == 0 { //nolint:gosec // CreateRestrictedToken's own arguments
		t.Fatalf("CreateRestrictedToken: %v", err)
	}
	t.Cleanup(func() { _ = token.Close() })
	medium, err := windows.CreateWellKnownSid(windows.WinMediumLabelSid)
	if err != nil {
		t.Fatal(err)
	}
	label := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: medium, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)), label.Size()); err != nil { //nolint:gosec // a TOKEN_MANDATORY_LABEL
		t.Fatal(err)
	}
	return token
}

// AsStandard runs f on this goroutine's thread while it impersonates
// token, a standard window's.
func AsStandard(t *testing.T, token windows.Token, f func()) {
	t.Helper()
	var imp windows.Token
	if err := windows.DuplicateTokenEx(token, windows.TOKEN_IMPERSONATE|windows.TOKEN_QUERY, nil,
		windows.SecurityImpersonation, windows.TokenImpersonation, &imp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = imp.Close() }()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.SetThreadToken(nil, imp); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.RevertToSelf(); err != nil {
			t.Fatal(err)
		}
	}()
	f()
}

// GrantFull tries to give sid Full control of path, as only a DACL's
// writer may: path's DACL becomes that one entry.
func GrantFull(path, sid string) error {
	return setFull(path, sid, false)
}

// AddFull adds to path's DACL an entry that gives sid Full control, of
// everything in it too when path is a folder: what a standard window may
// do to a folder or a file that it made, and owns.
func AddFull(path, sid string) error {
	return setFull(path, sid, true)
}

// setFull gives sid Full control of path, with the DACL's other entries
// when keep is set.
func setFull(path, sid string, keep bool) error {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return err
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	var merged *windows.ACL
	if keep {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
		}
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			return err
		}
		if merged, _, err = sd.DACL(); err != nil {
			return err
		}
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: inheritance,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(s)},
	}}, merged)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// SetOwner makes sid the owner of path, as when that user made it.
func SetOwner(t *testing.T, path, sid string) {
	t.Helper()
	s, err := windows.StringToSid(sid)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, s, nil, nil, nil); err != nil {
		t.Fatalf("owner of %s: %v", path, err)
	}
}
