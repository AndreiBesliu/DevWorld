package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// The quarantine's DACLs on Windows (C2-R20 (a)): full control for SYSTEM
// and the Administrators, nobody else, and nothing inherited from the
// parent; a directory's ACEs are inherited by what it holds.
const (
	quarantineDirSDDL  = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	quarantineFileSDDL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)"
)

// restrictDir makes the quarantine's directory rel of root (at rootDir)
// readable by SYSTEM and the Administrators alone: it must be a directory,
// not a link or a junction, and its DACL is replaced, protected from
// inheritance, through a handle on that very directory (openChecked), so
// a name swapped meanwhile changes nothing (C2-R21). A directory that this
// scan made (created) gets the Administrators as its owner; any other must
// already belong to SYSTEM or the Administrators, as on Linux it must
// belong to the scan's uid: an owner may always rewrite its DACL.
func restrictDir(root *os.Root, rootDir, rel string, created bool) error {
	path := filepath.Join(rootDir, rel)
	h, err := openChecked(root, rel, true, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER)
	if err != nil {
		return &fs.PathError{Op: "restrict", Path: path, Err: err}
	}
	defer func() { _ = windows.CloseHandle(h) }()
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	var owner *windows.SID
	if created {
		info |= windows.OWNER_SECURITY_INFORMATION
		owner = admins
	} else if err := checkOwner(h); err != nil {
		return &fs.PathError{Op: "restrict", Path: path, Err: err}
	}
	return setProtectedDACL(h, path, info, owner, quarantineDirSDDL)
}

// restrictFile gives the results' file rel of root, before it moves into
// the quarantine, the DACL of a quarantined file: a move keeps a file's
// own DACL, which grants the owner's account what the results folder
// granted. The DACL goes on through a handle on the file that root's walk
// reaches, which follows no link or junction out of root (openChecked), so
// a directory of its path swapped for a junction meanwhile leaves every
// file outside as it was (C2-R21). A file that then fails to move stays in
// the results, and the scan names it in unmoved.
func restrictFile(root *os.Root, rel string) error {
	h, err := openChecked(root, rel, false, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return &fs.PathError{Op: "restrict", Path: rel, Err: err}
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return setProtectedDACL(h, rel,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, quarantineFileSDDL)
}

// openChecked opens rel in root, as the walk does, and opens that very
// object again with access through its handle (ReOpenFile), resolving no
// name. root.Lstat must show it as a directory (dir) or a regular file,
// never a link or a junction, the object opened must be the one it showed,
// and the new handle must hold no reparse point, a directory when dir is
// set, else a file of one link.
func openChecked(root *os.Root, rel string, dir bool, access uint32) (windows.Handle, error) {
	want := fs.FileMode(0)
	if dir {
		want = fs.ModeDir
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return windows.InvalidHandle, err
	}
	if info.Mode().Type() != want {
		return windows.InvalidHandle, fmt.Errorf("a link, a junction or another type (%v)", info.Mode().Type())
	}
	f, err := root.Open(rel)
	if err != nil {
		return windows.InvalidHandle, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return windows.InvalidHandle, err
	}
	if !os.SameFile(info, opened) {
		return windows.InvalidHandle, fmt.Errorf("replaced while it was opened")
	}
	h, err := reOpenFile(windows.Handle(f.Fd()), access)
	if err != nil {
		return windows.InvalidHandle, err
	}
	var bhfi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &bhfi); err != nil {
		_ = windows.CloseHandle(h)
		return windows.InvalidHandle, err
	}
	isDir := bhfi.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if bhfi.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || isDir != dir || (!dir && bhfi.NumberOfLinks != 1) {
		_ = windows.CloseHandle(h)
		return windows.InvalidHandle, fmt.Errorf("a reparse point, another type or a file of %d links", bhfi.NumberOfLinks)
	}
	return h, nil
}

// procReOpenFile is kernel32's ReOpenFile, which x/sys/windows lacks.
var procReOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

// reOpenFile opens the object of h again with access, following no name.
func reOpenFile(h windows.Handle, access uint32) (windows.Handle, error) {
	r, _, err := procReOpenFile.Call(uintptr(h), uintptr(access),
		uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE),
		uintptr(windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT))
	if nh := windows.Handle(r); nh != windows.InvalidHandle {
		return nh, nil
	}
	return windows.InvalidHandle, err
}

// checkOwner refuses an object of h that belongs to neither SYSTEM nor the
// Administrators.
func checkOwner(h windows.Handle) error {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !trustedOwner(owner) {
		return fmt.Errorf("it belongs to %v, not to SYSTEM or the Administrators", owner)
	}
	return nil
}

// trustedOwner reports whether owner is SYSTEM or the Administrators.
func trustedOwner(owner *windows.SID) bool {
	return owner != nil && (owner.IsWellKnown(windows.WinLocalSystemSid) || owner.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}

// setProtectedDACL replaces, through h, the DACL of the file or directory
// at path (named in the error only) with sddl's, protected from
// inheritance, and its owner with owner when info asks for it.
func setProtectedDACL(h windows.Handle, path string, info windows.SECURITY_INFORMATION, owner *windows.SID, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, info, owner, nil, dacl, nil); err != nil {
		return &fs.PathError{Op: "restrict", Path: path, Err: err}
	}
	return nil
}
