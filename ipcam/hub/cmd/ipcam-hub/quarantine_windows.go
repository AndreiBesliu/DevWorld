package main

import (
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// The quarantine's DACLs on Windows (C2-R20 (a)): full control for SYSTEM
// and the Administrators, nobody else, and nothing inherited from the
// parent; a directory's ACEs are inherited by what it holds.
const (
	quarantineDirSDDL  = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	quarantineFileSDDL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)"
)

// restrictDir makes the quarantine's directory at path readable by SYSTEM
// and the Administrators alone: it must be a directory, not a link or a
// junction, and its DACL is replaced, protected from inheritance.
func restrictDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is a link or no directory", path)
	}
	return setProtectedDACL(path, quarantineDirSDDL)
}

// restrictFile gives the file at path, before it moves into the
// quarantine, the DACL of a quarantined file: a move keeps a file's own
// DACL, which grants the owner's account what the results folder granted.
// A file that then fails to move stays in the results with that DACL, and
// the scan names it in unmoved.
func restrictFile(path string) error { return setProtectedDACL(path, quarantineFileSDDL) }

// setProtectedDACL replaces the DACL of the file or directory at path with
// sddl's, protected from inheritance.
func setProtectedDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return &fs.PathError{Op: "restrict", Path: path, Err: err}
	}
	return nil
}
