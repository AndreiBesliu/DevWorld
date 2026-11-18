package catalog

import "os"

// fileUID: Windows files have no numeric owner.
func fileUID(os.FileInfo) (int, bool) { return 0, false }
