//go:build !windows

package recorder

import "os"

// syncDirectory fsyncs a directory, so that a file created or renamed in it
// keeps that name after a power cut.
func syncDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	err2 := d.Close()
	if err == nil {
		err = err2
	}
	return err
}
