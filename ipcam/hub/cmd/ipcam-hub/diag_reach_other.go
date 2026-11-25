//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// isRefused reports a reset from the far end: the path is open.
func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }

// isDenied reports a connection that this host refused: a netfilter drop
// answers EPERM, a reject rule EACCES.
func isDenied(err error) bool { return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) }
