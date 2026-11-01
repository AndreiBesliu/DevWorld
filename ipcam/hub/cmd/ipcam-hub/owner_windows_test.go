package main

import "os"

// fileUID is the owner of the file that info describes: Windows has no
// uid, and the caller skips the check.
func fileUID(os.FileInfo) (int, bool) { return 0, false }
