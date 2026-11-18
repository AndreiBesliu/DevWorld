//go:build !windows

package local_test

import "syscall"

// diskFullErrors are the errors with which this system refuses a write to a
// full disk.
func diskFullErrors() []error { return []error{syscall.ENOSPC} }
