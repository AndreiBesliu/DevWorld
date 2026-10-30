// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The security descriptors, in SDDL, of recspike's folder under
// %ProgramData% and of the files that it writes there: Administrators (BA)
// own them, and a protected DACL, which inherits nothing from
// %ProgramData%, gives Full control (FA) to Administrators and SYSTEM (SY)
// alone, passed on to what the folder holds (OICI).
const (
	protectedFolderSDDL = "O:BAD:P(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)"
	protectedFileSDDL   = "O:BAD:P(A;;FA;;;BA)(A;;FA;;;SY)"
)

// ProtectConfigDir makes dir, recspike's folder under %ProgramData%, and
// gives it to Administrators with the protected DACL of
// protectedFolderSDDL. Any user may make that folder first, give itself an
// entry in its DACL or deny Administrators one, and leave files in it
// (Final fix C review, I2). So the owner and the whole DACL are replaced
// in one change, which leaves no other entry, through a handle that keeps
// the folder from being renamed or deleted meanwhile and that the restore
// privilege opens whatever the planted DACL says. A folder that is a link
// or a junction to another place is refused. WriteProtected writes the
// files in it anew.
func ProtectConfigDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(protectedFolderSDDL)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return withPrivileges([]string{"SeRestorePrivilege", "SeTakeOwnershipPrivilege"}, func() error {
		// Without FILE_SHARE_DELETE, nothing renames or deletes the folder,
		// or puts a junction at its name, while it is open.
		h, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return &os.PathError{Op: "open", Path: dir, Err: err}
		}
		defer func() { _ = windows.CloseHandle(h) }()
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err != nil {
			return err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("%s nu este un folder obișnuit, ci o legătură spre alt loc, pe care poate a pus-o alt cont", dir)
		}
		return windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
			windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			owner, nil, dacl, nil)
	})
}

// withPrivileges runs f on this goroutine's thread with the named
// privileges enabled, in a copy of the process's token that only that
// thread uses: an elevated token holds them, disabled. A privilege that the
// token lacks stays off, and f's access checks decide as without it.
func withPrivileges(names []string, f func() error) error {
	runtime.LockOSThread()
	if err := windows.ImpersonateSelf(windows.SecurityImpersonation); err != nil {
		runtime.UnlockOSThread()
		return err
	}
	defer func() {
		// A thread that cannot stop impersonating stays locked to this
		// goroutine, and ends with it.
		if windows.RevertToSelf() == nil {
			runtime.UnlockOSThread()
		}
	}()
	var token windows.Token
	if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, true, &token); err != nil {
		return err
	}
	defer func() { _ = token.Close() }()
	for _, n := range names {
		p, err := windows.UTF16PtrFromString(n)
		if err != nil {
			return err
		}
		var on windows.Tokenprivileges
		if err := windows.LookupPrivilegeValue(nil, p, &on.Privileges[0].Luid); err != nil {
			return err
		}
		on.PrivilegeCount, on.Privileges[0].Attributes = 1, windows.SE_PRIVILEGE_ENABLED
		if err := windows.AdjustTokenPrivileges(token, false, &on, 0, nil, nil); err != nil {
			return err
		}
	}
	return f()
}

// createProtected creates the file at path, which must not exist, with the
// security descriptor of protectedFileSDDL from its first moment: no
// account but Administrators and SYSTEM ever has a right to it, its
// owner's included, and nothing else may open it until it is closed.
func createProtected(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString(protectedFileSDDL)
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
