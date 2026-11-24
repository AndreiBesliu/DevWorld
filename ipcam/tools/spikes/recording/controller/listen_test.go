package controller

import (
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// ipv6Loopback reports whether this machine can listen on ::1, so that a
// dual-stack listener would accept a connection there.
func ipv6Loopback(t *testing.T) bool {
	t.Helper()
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// TestListeners_BindIPv4Only (R08): every listener of reccontroller binds an
// IPv4 socket, on all interfaces or on 127.0.0.1 with --local, never a
// dual-stack one.
func TestListeners_BindIPv4Only(t *testing.T) {
	if got := DefaultListenConfig(false); got.Host != "" || got.RTSPPort != 8554 || got.LinkPort != 7081 ||
		got.BeaconPort != 7082 || got.AppPort != 7080 || got.UploadPort != 7083 {
		t.Errorf("DefaultListenConfig(false) = %+v, want all interfaces on 8554, 7081, 7082, 7080 and 7083", got)
	}
	if got := DefaultListenConfig(true); got.Host != "127.0.0.1" || got.RTSPPort != 8554 {
		t.Errorf("DefaultListenConfig(true) = %+v, want 127.0.0.1 (--local)", got)
	}

	for _, tc := range []struct {
		host string
		want net.IP
	}{
		{"", net.IPv4zero},
		{"127.0.0.1", net.IPv4(127, 0, 0, 1)},
	} {
		t.Run("host "+tc.host, func(t *testing.T) {
			lns, err := ListenAll(t.Context(), ListenConfig{Host: tc.host}) // port 0: any free port
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = lns.Close() })
			for name, addr := range map[string]net.Addr{
				"rtsp":   lns.RTSP.Addr(),
				"link":   lns.Link.Addr(),
				"app":    lns.App.Addr(),
				"upload": lns.Upload.Addr(),
				"beacon": lns.Beacon.LocalAddr(),
			} {
				var ip net.IP
				switch a := addr.(type) {
				case *net.TCPAddr:
					ip = a.IP
				case *net.UDPAddr:
					ip = a.IP
				}
				if ip.To4() == nil || !ip.Equal(tc.want) {
					t.Errorf("%s listens on %v, want %v", name, addr, tc.want)
				}
			}
			for name, c := range map[string]syscall.Conn{
				"rtsp":   lns.RTSP.(syscall.Conn),
				"link":   lns.Link.(syscall.Conn),
				"app":    lns.App.(syscall.Conn),
				"upload": lns.Upload.(syscall.Conn),
				"beacon": lns.Beacon.(syscall.Conn),
			} {
				if domain, ok := socketDomain(t, c); ok && domain != syscall.AF_INET {
					t.Errorf("%s: socket domain %d, want AF_INET", name, domain)
				}
			}
			if tc.host == "" && ipv6Loopback(t) {
				for name, ln := range map[string]net.Listener{"rtsp": lns.RTSP, "link": lns.Link, "app": lns.App, "upload": lns.Upload} {
					port := ln.Addr().(*net.TCPAddr).Port
					if c, err := net.DialTimeout("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)), time.Second); err == nil {
						_ = c.Close()
						t.Errorf("%s accepted a connection on ::1", name)
					}
				}
			}
		})
	}
}
