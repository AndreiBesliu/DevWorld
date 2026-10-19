package main

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestBanner_ListsPrivateURLsAndFirewallHint(t *testing.T) {
	var out bytes.Buffer
	writeBanner(&out, bannerInfo{
		port: 7080,
		lan: []lanAddr{
			{Name: "Wi-Fi", IP: net.IPv4(192, 168, 1, 12)},
			{Name: "Ethernet", IP: net.IPv4(10, 0, 0, 5)},
		},
		sims:       16,
		resultsDir: "results",
	})
	text := out.String()

	for _, want := range []string{
		"http://192.168.1.12:7080",
		"http://10.0.0.5:7080",
		"Wi-Fi",
		"Ethernet",
		"Rețele private",
		// The hint of the plan's Review Focus 1, word for word.
		"același Wi-Fi, nu rețeaua pentru oaspeți; permiteți spikehub în firewall la «Rețele private»",
		"16",
		"results",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("banner lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "mDNS") {
		t.Errorf("banner warns about mDNS although it is available:\n%s", text)
	}
	if strings.Contains(text, "localhost") {
		t.Errorf("banner lists a loopback URL, which a phone cannot use:\n%s", text)
	}
}

func TestBanner_UsesCommaBelowDiacritics(t *testing.T) {
	var out bytes.Buffer
	writeBanner(&out, bannerInfo{port: 7080, mdnsUnavailable: true, lan: []lanAddr{{Name: "Wi-Fi", IP: net.IPv4(192, 168, 1, 12)}}})
	// ş and ţ (with cedilla) are the Turkish letters; Romanian has ș and ț.
	if strings.ContainsAny(out.String(), "şţŞŢ") {
		t.Errorf("banner uses cedilla letters:\n%s", out.String())
	}
	if !strings.ContainsAny(out.String(), "șț") {
		t.Errorf("banner has no ș or ț; is it still Romanian?\n%s", out.String())
	}
}

func TestBanner_ReportsUnavailableMDNS(t *testing.T) {
	var out bytes.Buffer
	writeBanner(&out, bannerInfo{port: 7080, mdnsUnavailable: true})

	text := out.String()
	for _, want := range []string{"mDNS", "5353", "mdnsUnavailable"} {
		if !strings.Contains(text, want) {
			t.Errorf("banner lacks %q:\n%s", want, text)
		}
	}
}

func TestBanner_WithoutAPrivateNetworkSaysSo(t *testing.T) {
	var out bytes.Buffer
	writeBanner(&out, bannerInfo{port: 7080})

	text := out.String()
	if !strings.Contains(text, "Nu am găsit nicio rețea privată") {
		t.Errorf("banner does not say that no private network was found:\n%s", text)
	}
	if strings.Contains(text, "http://") {
		t.Errorf("banner lists a URL although there is no private network:\n%s", text)
	}
	// The hint is still useful: the owner may connect the PC to Wi-Fi and look again.
	if !strings.Contains(text, "Rețele private") {
		t.Errorf("banner lacks the firewall hint:\n%s", text)
	}
}

// When the interfaces cannot be listed, the banner says so with the error,
// instead of blaming a network that may well be there.
func TestBanner_ReportsAnInterfaceListingError(t *testing.T) {
	var out bytes.Buffer
	writeBanner(&out, bannerInfo{port: 7080, lanErr: errors.New("route ip+net: netlinkrib: permission denied")})

	text := out.String()
	for _, want := range []string{"interfețele de rețea", "route ip+net: netlinkrib: permission denied", "Rețele private"} {
		if !strings.Contains(text, want) {
			t.Errorf("banner lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Nu am găsit nicio rețea privată") {
		t.Errorf("banner says that no private network was found, but the listing failed:\n%s", text)
	}
}

func TestIsPrivateIPv4(t *testing.T) {
	for ip, want := range map[string]bool{
		"192.168.1.12":   true,
		"10.0.0.5":       true,
		"172.16.0.1":     true,
		"172.31.255.255": true,
		"172.32.0.1":     false,
		"8.8.8.8":        false,
		"127.0.0.1":      false,
		"169.254.10.20":  false, // link-local: the PC has no DHCP lease on that network
		"100.64.0.1":     false, // carrier-grade NAT
		"fd00::1":        false, // IPv6: the spike's ICE is IPv4 only (R25)
		"::1":            false,
	} {
		if got := isPrivateIPv4(net.ParseIP(ip)); got != want {
			t.Errorf("isPrivateIPv4(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestLANAddrs_AreAllPrivateIPv4(t *testing.T) {
	// The host that runs the test may have no private network, so this checks only
	// what the function returns.
	addrs, err := lanAddrs()
	if err != nil {
		t.Skipf("the interfaces of this host cannot be listed: %v", err)
	}
	for _, a := range addrs {
		if !isPrivateIPv4(a.IP) || a.Name == "" {
			t.Errorf("lanAddrs returned %+v", a)
		}
	}
}
