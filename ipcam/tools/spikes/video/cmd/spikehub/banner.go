package main

import (
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
)

// lanAddr is a private IPv4 address of this PC, with the name of its network
// interface, so that the owner can tell the Wi-Fi from a virtual adapter.
type lanAddr struct {
	Name string
	IP   net.IP
}

// isPrivateIPv4 reports whether ip is an IPv4 address of a private network
// (10/8, 172.16/12 or 192.168/16). The spike's ICE is IPv4 only (R25), and a
// link-local address (169.254/16) means that the PC has no lease on that network.
func isPrivateIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4.IsPrivate()
}

// lanAddrs returns the private IPv4 addresses of the running network
// interfaces of this PC, by interface name and address, or the error of the
// listing. An interface whose addresses cannot be read is skipped.
func lanAddrs() ([]lanAddr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []lanAddr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && isPrivateIPv4(ipnet.IP) {
				out = append(out, lanAddr{Name: iface.Name, IP: ipnet.IP})
			}
		}
	}
	slices.SortFunc(out, func(a, b lanAddr) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.IP.String(), b.IP.String())
	})
	return out, nil
}

// bannerInfo is what the start banner reports.
type bannerInfo struct {
	port int
	lan  []lanAddr
	// lanErr is why the interfaces could not be listed; lan is empty then.
	lanErr error
	// sims is the number of simulated cameras.
	sims            int
	resultsDir      string
	mdnsUnavailable bool
	// keepsAwake is set when Windows keeps the PC awake for spikehub (R49), and
	// awakeErr is why it could not; both are empty on the other systems.
	keepsAwake bool
	awakeErr   error
}

// writeBanner prints the start banner, in Romanian: where the phone connects,
// how, and what to check when it cannot. The addresses are the owner's own
// screen only; they never go into a log or a results file (PH0-22).
func writeBanner(w io.Writer, b bannerInfo) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }

	p("")
	p("spikehub: serverul de test video pentru telefon și PC (Faza 0 b)")
	p("")
	switch {
	case b.lanErr != nil:
		p("Nu pot citi interfețele de rețea ale acestui calculator: %v", b.lanErr)
		p("Aflați adresa lui în rețeaua locală din setările de rețea (de exemplu cu ipconfig)")
		p("și introduceți pe telefon http://<adresa>:%d.", b.port)
	case len(b.lan) == 0:
		p("Nu am găsit nicio rețea privată activă pe acest calculator.")
		p("Conectați-l la Wi-Fi sau la cablu și porniți din nou spikehub.")
	default:
		p("Adresele acestui calculator în rețeaua locală:")
		for _, a := range b.lan {
			p("  http://%s:%d   (%s)", a.IP, b.port, a.Name)
		}
		p("")
		p("Pe telefon: conectați-l la același Wi-Fi ca acest calculator, deschideți aplicația")
		p("de test și introduceți una dintre adresele de mai sus (sau deschideți-o în browser).")
	}
	p("")
	p("Dacă telefonul nu se poate conecta:")
	p("  - același Wi-Fi, nu rețeaua pentru oaspeți; permiteți spikehub în firewall la «Rețele private»;")
	p("  - la prima pornire Windows întreabă dacă permite accesul: bifați «Rețele private»;")
	p("  - telefonul și calculatorul trebuie să fie în aceeași subrețea.")
	p("")
	p("Camere simulate: %d, plus 2 pentru matricea de coduri (mx-h265, mx-high51).", b.sims)
	if b.mdnsUnavailable {
		p("")
		p("Atenție: portul mDNS (UDP 5353) nu a putut fi deschis, deci spikehub nu poate rezolva")
		p("adresele .local ale browserelor. Conectarea poate merge totuși, dar perechea ICE din")
		p("rezultate (MV13) poate fi alta. Fiecare fișier de rezultate va purta mdnsUnavailable: true.")
	}
	p("")
	switch {
	case b.keepsAwake:
		p("Cât timp această fereastră e deschisă, PC-ul nu intră în repaus.")
	case b.awakeErr != nil:
		p("Atenție: spikehub nu poate împiedica intrarea PC-ului în repaus. Pe durata probei,")
		p("opriți repausul din setările de alimentare ale Windows.")
	}
	if b.resultsDir != "" {
		p("Rezultatele se salvează în: %s", b.resultsDir)
	}
	p("Opriți cu Ctrl+C.")
	p("")
}
