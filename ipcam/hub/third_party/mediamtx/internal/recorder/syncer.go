package recorder

import (
	"io"
	"os"
)

// segmentFile is the open file of an fMP4 segment. *os.File is one. A clear
// segment's close seeks back to write the mvhd duration; an encrypted one
// only ever appends (MTX-2).
type segmentFile interface {
	io.ReadWriteSeeker
	Name() string
	Sync() error
	Close() error
}

// MTX-1 and MTX-2 (hub/third_party/PATCHES.md): the seams through which the
// fMP4 segment writer creates a segment file and makes it durable. Each one is
// a single function, so that the tests can replace it to count and order the
// calls or to wrap the file; every call site is in format_fmp4_segment.go.
var (
	// createFile creates a segment file, truncating a file of that name.
	createFile = func(name string) (segmentFile, error) {
		f, err := os.Create(name)
		if err != nil {
			return nil, err
		}
		return f, nil
	}

	// syncFile flushes a segment file's data and size to stable storage.
	syncFile = func(f segmentFile) error { return f.Sync() }

	// syncDir makes the entries of a directory durable: on Windows a no-op
	// (dirsync_windows.go), on other systems an fsync of the directory
	// (dirsync_other.go).
	syncDir = func(dir string) error { return syncDirectory(dir) }

	// renameFinal gives a closed segment its final name, replacing a file of
	// that name (rename_other.go, rename_windows.go).
	renameFinal = func(from, to string) error { return renameReplace(from, to) }
)

// partSuffix ends the name of a segment that is still being written. The
// segment is renamed without it once its last bytes are durable.
const partSuffix = ".part"
