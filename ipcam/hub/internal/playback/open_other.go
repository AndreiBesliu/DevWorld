//go:build !windows

package playback

import "os"

// openShared opens path to read: elsewhere than on Windows, a rename of an
// open file is never refused.
func openShared(path string) (*os.File, error) { return os.Open(path) } //nolint:gosec // G304: a segment under rec/
