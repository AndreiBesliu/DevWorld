// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"net/netip"
	"strings"
)

// The block's allow list (P15, P56): destination addresses, never an
// interface. Loopback, the private and link-local ranges, multicast and
// the IPv4 broadcast stay reachable for the blocked hub; everything else
// is the WAN. "::", the unspecified address, is never a destination: it
// is listed so that the IPv6 block ranges start at ::2 and the two sets
// cover IPv6 with no gap.
var (
	AllowV4 = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255"}
	AllowV6 = []string{"::1", "::", "fe80::/10", "fc00::/7", "ff00::/8"}
)

// The allow list's complement, as Windows Firewall's -RemoteAddress takes
// it: explicit ranges, since a Windows Block rule wins over any Allow
// rule and so no Allow rule can carve the LAN out of a broad block.
var (
	BlockV4 = []string{
		"0.0.0.0-9.255.255.255",
		"11.0.0.0-126.255.255.255",
		"128.0.0.0-169.253.255.255",
		"169.255.0.0-172.15.255.255",
		"172.32.0.0-192.167.255.255",
		"192.169.0.0-223.255.255.255",
		"240.0.0.0-255.255.255.254",
	}
	BlockV6 = []string{
		"::2-fbff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"fe00::-fe7f:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"fec0::-feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	}
)

// addrRange is an inclusive range of addresses of one family.
type addrRange struct{ lo, hi netip.Addr }

// parseRange reads "a-b", a prefix or a single address.
func parseRange(s string) (addrRange, bool) {
	if lo, hi, ok := strings.Cut(s, "-"); ok {
		a, err1 := netip.ParseAddr(lo)
		b, err2 := netip.ParseAddr(hi)
		return addrRange{a, b}, err1 == nil && err2 == nil && a.BitLen() == b.BitLen() && a.Compare(b) <= 0
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		p = p.Masked()
		return addrRange{p.Addr(), lastAddr(p)}, true
	}
	a, err := netip.ParseAddr(s)
	return addrRange{a, a}, err == nil
}

// lastAddr is a prefix's last address.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// Blocked reports whether the block cuts a destination address: it falls
// in one of the block ranges of its family.
func Blocked(a netip.Addr) bool {
	a = a.Unmap()
	list := BlockV4
	if a.Is6() {
		list = BlockV6
	}
	for _, s := range list {
		if r, ok := parseRange(s); ok && r.lo.Compare(a) <= 0 && a.Compare(r.hi) <= 0 {
			return true
		}
	}
	return false
}
