package local

import "os"

// SetSync replaces the driver's fsync of a file for a test, and returns the
// function that restores it.
func SetSync(sync func(*os.File) error) (restore func()) {
	was := syncFile
	syncFile = sync
	return func() { syncFile = was }
}

// SetOpen replaces OpenRange's open of an object's file for a test, and
// returns the function that restores it.
func SetOpen(open func(string) (*os.File, error)) (restore func()) {
	was := openFile
	openFile = open
	return func() { openFile = was }
}
