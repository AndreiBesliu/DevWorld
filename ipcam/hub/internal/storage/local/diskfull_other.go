//go:build !windows

package local

import (
	"errors"
	"syscall"
)

// isDiskFull reports whether err says that the filesystem has no room left.
func isDiskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
