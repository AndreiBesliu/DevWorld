package recorder

import "os"

// MTX-1 (hub/third_party/PATCHES.md): the seams through which the fMP4 segment
// writer makes a segment durable. Each one is a single function, so that the
// tests can replace it to count and order the calls; every call site is in
// format_fmp4_segment.go.
var (
	// syncFile flushes a segment file's data and size to stable storage.
	syncFile = func(f *os.File) error { return f.Sync() }

	// syncDir makes the entries of a directory durable: on Linux an fsync of
	// the directory (dirsync_other.go), on Windows a no-op (dirsync_windows.go).
	syncDir = func(dir string) error { return syncDirectory(dir) }

	// renameFinal gives a closed segment its final name, replacing a file of
	// that name (rename_other.go, rename_windows.go).
	renameFinal = func(from, to string) error { return renameReplace(from, to) }
)

// partSuffix ends the name of a segment that is still being written. The
// segment is renamed without it once its last bytes are durable.
const partSuffix = ".part"
