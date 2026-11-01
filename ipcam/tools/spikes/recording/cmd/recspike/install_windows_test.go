package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestPaths_WindowsOwnFolders (Final fix C review, M3): on the PC, with
// every environment variable that names one of Windows' folders, or the
// temporary one, pointing elsewhere, the protected folder, the boot task's
// definition and the configs that ipcam-hub starts with lie in
// C:\ProgramData\ipcam-recspike, and the kernel dump is C:\Windows\MEMORY.DMP.
func TestPaths_WindowsOwnFolders(t *testing.T) {
	elsewhere := elsewhereEnvironment(t)
	p, err := pathsFor("windows")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ got, want string }{
		{p.protected, `C:\ProgramData\ipcam-recspike`},
		{p.savedStart, `C:\ProgramData\ipcam-recspike\w32time-start.json`},
		{p.taskFile, `C:\ProgramData\ipcam-recspike\ipcam-recspike.xml`},
		{p.memoryDump, `C:\Windows\MEMORY.DMP`},
	} {
		if !strings.EqualFold(c.got, c.want) {
			t.Errorf("a path is %s, want %s (the environment points at %s)", c.got, c.want, elsewhere)
		}
	}
}

// The system's errors of a full disk and of a file that another program
// holds open, here.
var (
	diskFullErrno error = windows.ERROR_DISK_FULL
	inUseErrno    error = windows.ERROR_SHARING_VIOLATION
)
