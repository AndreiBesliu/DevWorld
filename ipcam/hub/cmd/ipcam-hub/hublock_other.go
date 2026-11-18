//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile locks f, exclusive and without waiting (flock).
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // G115: a file descriptor
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errHubRunning
	}
	if err != nil {
		return fmt.Errorf("the hub lock: %w", err)
	}
	return nil
}
