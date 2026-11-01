package controller

import (
	"syscall"
	"testing"
)

// socketDomain returns the address family of a socket (SO_DOMAIN): a
// dual-stack listener is AF_INET6 even where this machine has no IPv6.
func socketDomain(t *testing.T, c syscall.Conn) (int, bool) {
	t.Helper()
	raw, err := c.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var domain int
	var serr error
	if err := raw.Control(func(fd uintptr) {
		domain, serr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_DOMAIN) //nolint:gosec // a socket descriptor fits in an int
	}); err != nil {
		t.Fatal(err)
	}
	if serr != nil {
		t.Fatal(serr)
	}
	return domain, true
}
