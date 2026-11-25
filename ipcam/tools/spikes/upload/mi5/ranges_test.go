package mi5

import (
	"net/netip"
	"slices"
	"testing"
)

// TestBlockRanges_ComplementOfAllowList (P15, P56, Review Focus 2): the
// block ranges and the allow list (:: included) together cover all of
// IPv4 and IPv6 with no gap and no overlap, every range's endpoints
// swept; the providers' addresses are blocked, the LAN's and loopback's
// allowed.
func TestBlockRanges_ComplementOfAllowList(t *testing.T) {
	for _, fam := range []struct {
		name         string
		allow, block []string
		first, last  netip.Addr
	}{
		{"IPv4", AllowV4, BlockV4, netip.MustParseAddr("0.0.0.0"), netip.MustParseAddr("255.255.255.255")},
		{"IPv6", AllowV6, BlockV6, netip.MustParseAddr("::"), netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")},
	} {
		var all []addrRange
		for _, s := range slices.Concat(fam.allow, fam.block) {
			r, ok := parseRange(s)
			if !ok || r.lo.BitLen() != fam.first.BitLen() {
				t.Fatalf("%s: %q is not a range of the family", fam.name, s)
			}
			all = append(all, r)
		}
		slices.SortFunc(all, func(a, b addrRange) int { return a.lo.Compare(b.lo) })
		if all[0].lo != fam.first {
			t.Errorf("%s: the first range starts at %v, not %v", fam.name, all[0].lo, fam.first)
		}
		for i := 1; i < len(all); i++ {
			if want := all[i-1].hi.Next(); all[i].lo != want {
				t.Errorf("%s: %v-%v then %v-%v: a gap or an overlap (want %v next)", fam.name, all[i-1].lo, all[i-1].hi, all[i].lo, all[i].hi, want)
			}
		}
		if all[len(all)-1].hi != fam.last {
			t.Errorf("%s: the last range ends at %v, not %v", fam.name, all[len(all)-1].hi, fam.last)
		}
		// Each block range's endpoints are blocked, the addresses just
		// outside them are not, unless another block range holds them.
		for _, s := range fam.block {
			r, _ := parseRange(s)
			for _, a := range []netip.Addr{r.lo, r.hi} {
				if !Blocked(a) {
					t.Errorf("%s: %v, an endpoint of %s, is not blocked", fam.name, a, s)
				}
			}
			for _, a := range []netip.Addr{r.lo.Prev(), r.hi.Next()} {
				if a.IsValid() && Blocked(a) && !inAny(a, fam.block) {
					t.Errorf("%s: %v next to %s is blocked", fam.name, a, s)
				}
			}
		}
		for _, s := range fam.allow {
			r, _ := parseRange(s)
			for _, a := range []netip.Addr{r.lo, r.hi} {
				if Blocked(a) {
					t.Errorf("%s: %v, an endpoint of the allowed %s, is blocked", fam.name, a, s)
				}
			}
		}
	}
	for _, s := range []string{"8.8.8.8", "142.250.0.1", "2001:4860::8888", "2620:1ec::1"} {
		if !Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s is not blocked", s)
		}
	}
	for _, s := range []string{"192.168.1.20", "10.0.0.1", "127.0.0.1", "::1", "fe80::1", "fd00::1"} {
		if Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s is blocked", s)
		}
	}
}

// inAny reports whether a lies in one of the ranges.
func inAny(a netip.Addr, ranges []string) bool {
	for _, s := range ranges {
		if r, _ := parseRange(s); r.lo.Compare(a) <= 0 && a.Compare(r.hi) <= 0 {
			return true
		}
	}
	return false
}
