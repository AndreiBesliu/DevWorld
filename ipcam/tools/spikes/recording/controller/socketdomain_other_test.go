//go:build !linux

package controller

import (
	"syscall"
	"testing"
)

// socketDomain is only read on Linux (SO_DOMAIN); elsewhere the test relies
// on the listening address and on a dial to ::1.
func socketDomain(*testing.T, syscall.Conn) (int, bool) { return 0, false }
