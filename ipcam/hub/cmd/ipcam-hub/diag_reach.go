package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// reachSchema names the output of diag reach.
const reachSchema = "ipcam.diag.reach/1"

// reachTimeout bounds each probe's resolution and connection.
const reachTimeout = 5 * time.Second

// defaultReachPort is the port of a host given without one: HTTPS.
const defaultReachPort = 443

// The targets of diag reach's lines.
const (
	targetProvider = "provider"
	targetLAN      = "lan"
	targetLoopback = "loopback"
)

// The errors of a probe that did not connect. timeout, unreachable, denied
// and no-address are a blocked path; refused is a reset from the far end,
// which the path reached.
const (
	reachTimedOut    = "timeout"
	reachRefused     = "refused"
	reachUnreachable = "unreachable"
	reachDenied      = "denied" // EACCES or EPERM: a Windows Firewall block (WSAEACCES), a netfilter drop (EPERM)
	reachNoAddress   = "no-address"
)

// reachLine is one probe's line. A LAN probe names no host or port.
type reachLine struct {
	Schema  string `json:"schema"`
	Target  string `json:"target"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	Family  string `json:"family"`
	OK      bool   `json:"ok"`
	Blocked bool   `json:"blocked"`
	Ms      int64  `json:"ms"`
	Err     string `json:"err,omitempty"`
}

// reachProbe is one probe to run: a host and port over one family.
type reachProbe struct {
	target, host, family string
	port                 int
}

// diagReachCommand is "ipcam-hub diag reach --hosts <host[:port],…> [--lan
// <host:port>]" (P15): it checks what the WAN block lets this process reach,
// as the identity that the block names. For each host (port 443 by default)
// it resolves A and AAAA and connects over TCP to the first address of each
// family, 5 s at most; --lan's address is probed the same way, and its lines
// name no address; and it connects to listeners of its own on 127.0.0.1:0
// and, where the host has IPv6 loopback, [::1]:0. The probes run at once,
// and it prints one ipcam.diag.reach/1 line each, in that order. It exits 1
// on a usage error only.
func diagReachCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag reach", flag.ContinueOnError)
	flags.SetOutput(stderr)
	hosts := flags.String("hosts", "", "the provider hosts, host[:port] separated by commas")
	lan := flags.String("lan", "", "a LAN address, host:port, that must stay reachable")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *hosts == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	var probes []reachProbe
	for _, h := range strings.Split(*hosts, ",") {
		host, port, err := splitReachHost(strings.TrimSpace(h))
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag reach:", err)
			return exitFatal
		}
		probes = append(probes, reachProbe{targetProvider, host, "v4", port}, reachProbe{targetProvider, host, "v6", port})
	}
	if *lan != "" {
		host, port, err := splitReachHost(*lan)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag reach: --lan:", err)
			return exitFatal
		}
		probes = append(probes, reachProbe{targetLAN, host, "v4", port}, reachProbe{targetLAN, host, "v6", port})
	}
	for _, l := range []struct{ family, network, addr string }{{"v4", "tcp4", "127.0.0.1:0"}, {"v6", "tcp6", "[::1]:0"}} {
		ln, err := net.Listen(l.network, l.addr)
		if err != nil {
			continue // no IPv6 loopback here
		}
		defer func() { _ = ln.Close() }()
		ap := netip.MustParseAddrPort(ln.Addr().String())
		probes = append(probes, reachProbe{targetLoopback, ap.Addr().String(), l.family, int(ap.Port())})
	}
	lines := make([]reachLine, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Go(func() { lines[i] = p.run(context.Background()) })
	}
	wg.Wait()
	for _, line := range lines {
		if code := printJSONLine(stdout, stderr, line); code != exitOK {
			return code
		}
	}
	return exitOK
}

// splitReachHost splits host[:port], an IPv6 address in brackets with a port
// or bare without one; the port is 443 when absent.
func splitReachHost(s string) (string, int, error) {
	if host, port, err := net.SplitHostPort(s); err == nil {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || host == "" {
			return "", 0, fmt.Errorf("%q is not host:port", s)
		}
		return host, n, nil
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		return addr.String(), defaultReachPort, nil
	}
	if s == "" || strings.ContainsAny(s, ":[]/") {
		return "", 0, fmt.Errorf("%q is not host[:port]", s)
	}
	return s, defaultReachPort, nil
}

// run resolves the probe's host in its family and connects to the first
// address, within reachTimeout.
func (p reachProbe) run(ctx context.Context) reachLine {
	line := reachLine{Schema: reachSchema, Target: p.target, Host: p.host, Port: p.port, Family: p.family}
	if p.target == targetLAN {
		line.Host, line.Port = "", 0
	}
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	start := time.Now()
	finish := func(kind string) reachLine {
		line.Ms = time.Since(start).Milliseconds()
		line.OK, line.Err = kind == "", kind
		line.Blocked = kind != "" && kind != reachRefused
		return line
	}
	network := map[string]string{"v4": "ip4", "v6": "ip6"}[p.family]
	addr, ok := p.address(ctx, network)
	if !ok {
		return finish(reachNoAddress)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp"+p.family[1:], netip.AddrPortFrom(addr, uint16(p.port)).String()) //nolint:gosec // G115: a checked port
	if err != nil {
		return finish(reachErr(err))
	}
	_ = conn.Close()
	return finish("")
}

// address is the first address of the probe's host in network, ip4 or ip6:
// the host itself when it is an address.
func (p reachProbe) address(ctx context.Context, network string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(p.host); err == nil {
		addr = addr.Unmap()
		return addr, addr.Is4() == (network == "ip4")
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, network, p.host)
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, false
	}
	return addrs[0].Unmap(), true
}

// reachErr is the kind of a connection's error.
func reachErr(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return reachTimedOut
	case isRefused(err):
		return reachRefused
	case isDenied(err):
		return reachDenied
	}
	return reachUnreachable
}
