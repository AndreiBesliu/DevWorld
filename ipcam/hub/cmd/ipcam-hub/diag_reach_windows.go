package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isRefused reports a reset from the far end: the path is open.
func isRefused(err error) bool { return errors.Is(err, windows.WSAECONNREFUSED) }

// isDenied reports a connection that this host refused: a Windows Firewall
// block answers WSAEACCES.
func isDenied(err error) bool { return errors.Is(err, windows.WSAEACCES) }
