//go:build !windows

package main

import "syscall"

// The system's errors of a full disk and of a file that another program
// holds open, here: Linux has no lock that keeps a file from being removed.
var (
	diskFullErrno error = syscall.ENOSPC
	inUseErrno    error
)
