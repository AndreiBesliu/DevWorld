//go:build !windows

package recorder

import "os"

// renameReplace renames from to to, replacing to. The directory sync that
// follows it makes the new name durable.
func renameReplace(from, to string) error {
	return os.Rename(from, to)
}
