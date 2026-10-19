package redact

import (
	"fmt"
	"strings"
	"testing"
)

// found is "<kind>: <text>" for each match, in order.
func found(text string) []string {
	var out []string
	for _, m := range Scan(text) {
		out = append(out, fmt.Sprintf("%s: %s", m.Kind, text[m.Start:m.End]))
	}
	return out
}

// expectOne checks that text holds exactly one match, of kind, whose text is want.
func expectOne(t *testing.T, text string, kind Kind, want string) {
	t.Helper()
	got := found(text)
	if len(got) != 1 || got[0] != fmt.Sprintf("%s: %s", kind, want) {
		t.Errorf("Scan(%q) = %q, want exactly [%s: %s]", text, got, kind, want)
	}
}

// expectNone checks that text holds no match.
func expectNone(t *testing.T, text string) {
	t.Helper()
	if got := found(text); len(got) != 0 {
		t.Errorf("Scan(%q) = %q, want no match", text, got)
	}
}

func TestScan_IPv4(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"the hub at 192.168.1.20 answered", "192.168.1.20"},
		{"http://10.0.0.5:7080/spike", "10.0.0.5"},
		{"(172.16.4.1)", "172.16.4.1"},
		{"public 8.8.8.8.", "8.8.8.8"},
		{"rtt to 0.0.0.0", "0.0.0.0"},
		{"addr=255.255.255.255;", "255.255.255.255"},
	} {
		expectOne(t, tc.text, IPv4, tc.want)
	}
	for _, text := range []string{
		"open http://127.0.0.1:7080 on the PC", // the documented loopback address (owner guide, R38)
		"an address of the form 192.168.x.x",   // the documented placeholder
		"192.168.…",                            // an ellipsis form
		"Chrome/141.0.0.0 and Edg/141.0.0.0",   // a browser version after its product token
		"com.google.android.webview 120.0.6099.230",
		"Windows 10.0.26100.1",
		"spec §16.2.3.1",         // a section number
		"version 1.2.3.4.5",      // five numbers: a version, not an address
		"999.1.1.1 and 1.2.3",    // an octet over 255; three numbers
		"v10.0.0.1 and a1.2.3.4", // glued to a word
	} {
		expectNone(t, text)
	}
}

func TestScan_IPv6(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"from fe80::1ff:fe23:4567:890a on wlan0", "fe80::1ff:fe23:4567:890a"},
		{"2001:db8:85a3:0:0:8a2e:370:7334 is the hub", "2001:db8:85a3:0:0:8a2e:370:7334"},
		{"cand ::ffff:192.168.1.20 typ host", "::ffff:192.168.1.20"},
		{"relay [2001:db8::1]:7080 refused", "2001:db8::1"},
		{"link fe80::1%eth0 is up", "fe80::1%eth0"},
		{"0:0:0:0:0:ffff:10.0.0.7", "0:0:0:0:0:ffff:10.0.0.7"},
		{"remote ip:fe80::1 up", "fe80::1"}, // after a label and a colon
	} {
		expectOne(t, tc.text, IPv6, tc.want)
	}
	for _, text := range []string{
		"std::string and Vec::new and Self::default", // Task 13b: a scope operator is not an address
		"a::b::c in Rust paths like std::fs::read",
		"at 12:30 and 10:20:30",
		"the loopback ::1 is documented", // loopback, like 127.0.0.1
		"just :: punctuation",
	} {
		expectNone(t, text)
	}
}

func TestScan_MAC(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"mac aa:bb:cc:dd:ee:ff seen", "aa:bb:cc:dd:ee:ff"},
		{"AA-BB-CC-DD-EE-01", "AA-BB-CC-DD-EE-01"},
		{"cisco aabb.ccdd.ee01 form", "aabb.ccdd.ee01"},
	} {
		expectOne(t, tc.text, MAC, tc.want)
	}
	for _, text := range []string{
		"aa:bb:cc:… (an OUI with the rest left out)",
		"aa:bb:cc:dd:ee", // five groups
		"10:20:30 is a time",
		"aa:bb:cc:dd:ee:ff:00", // seven groups is not a MAC
	} {
		expectNone(t, text)
	}
}

func TestScan_Email(t *testing.T) {
	expectOne(t, "write to owner.name+ipcam@gmail.com now", Email, "owner.name+ipcam@gmail.com")
	expectOne(t, "<andrei@firma.ro>", Email, "andrei@firma.ro")
	for _, text := range []string{
		"the package @ipcam/player",
		"pnpm@10.4.1 and node@24",
		"someone@example.com writes the docs", // RFC 2606 documentation domain
		"a placeholder …@… form",
	} {
		expectNone(t, text)
	}
}

func TestScan_RTSPURL(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"camera rtsp://admin:hunter2@cam/stream1 ok", "rtsp://admin:hunter2@cam/stream1"},
		{"RTSPS://cam.example/live", "RTSPS://cam.example/live"},
		{"`rtsp://cam:554/h264`", "rtsp://cam:554/h264"},
	} {
		expectOne(t, tc.text, RTSPURL, tc.want)
	}
	// The address inside a URL is part of the URL's match, not a second one.
	expectOne(t, "rtsp://u:p@10.0.0.9/s", RTSPURL, "rtsp://u:p@10.0.0.9/s")
	for _, text := range []string{
		"it starts with `rtsp://` usually",
		"rtsp://… is the form",
		"rtsp://<user>:<password>@<address>/<path>",
		"http://cam/stream is not rtsp",
	} {
		expectNone(t, text)
	}
}

func TestScan_LocalHost(t *testing.T) {
	expectOne(t, "the PC is andrei-pc.local now", LocalHost, "andrei-pc.local")
	expectOne(t, "cand 1f4712db-ea17-4bcf-a596-105139dfd8bf.local 54321 typ host", LocalHost, "1f4712db-ea17-4bcf-a596-105139dfd8bf.local")
	expectOne(t, "Living.Room.LOCAL:8080", LocalHost, "Living.Room.LOCAL")
	for _, text := range []string{
		"the browsers' .local candidates",
		"mDNS `.local` names",
		"settings.local.json",
		"<name>.local",
		"localhost and local",
	} {
		expectNone(t, text)
	}
}

func TestScan_UserPath(t *testing.T) {
	expectOne(t, `log in C:\Users\Andrei\AppData\Local\ipcam`, UserPath, `C:\Users\Andrei`)
	expectOne(t, `"d:\\Users\\maria.pop\\Desktop"`, UserPath, `d:\\Users\\maria.pop`)
	expectOne(t, "C:/Users/ion/Downloads", UserPath, "C:/Users/ion")
	for _, text := range []string{
		`C:\Users\Public\Documents`,
		`C:\Users\Default\NTUSER.DAT`,
		`C:\Users\<nume>\Downloads`,
		`C:\Users\%USERNAME%\Downloads`,
		`C:\Users\…\Downloads`,
		`C:\Program Files\ipcam`,
	} {
		expectNone(t, text)
	}
}

// The serial-like rule: a token of at least 10 characters, only upper-case letters and digits,
// with at least 2 of each, cut at anything else (a hyphen, an underscore, a dot, a space); an
// all-hex token of 32 or more characters is a checksum and is left alone.
func TestScan_Serial(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"phone R58M12ABCDE ready", "R58M12ABCDE"},
		{"serialno=26071FDH2000JG", "26071FDH2000JG"},
		{"DS-2CD2143G2-I20170101AAWR123456789", "I20170101AAWR123456789"},
	} {
		expectOne(t, tc.text, Serial, tc.want)
	}
	for _, text := range []string{
		"samsung SM-A245F, AMD Ryzen 5 5600G with Radeon Graphics",
		"CODEC_UNSUPPORTED NO_FIRST_FRAME ES_SYSTEM_REQUIRED",
		"android-samsung-sm-a245f-20261018-094000-s3-9.json",
		"2026-10-18T10:00:00.000Z and 20261018",
		"MV3/16 PH0-22 OWN-05 APP-32 H264 4D4028 c2.mtk.avc.decoder OMX.google.h264.decoder",
		"RTX4070TI and NVIDIA GeForce RTX 4070",
		"E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855", // a SHA-256 checksum
		"ABCDEFGHIJKLMN and 12345678901234",                                // letters only; digits only
		"R58m12abcde",                                                      // lower case: not the rule
	} {
		expectNone(t, text)
	}
}

func TestScan_LineAndColumn(t *testing.T) {
	text := "first line\nsecond: ăîș 10.0.0.5 and\n\nfe80::1 last"
	got := Scan(text)
	if len(got) != 2 {
		t.Fatalf("Scan = %+v, want 2 matches", got)
	}
	if m := got[0]; m.Kind != IPv4 || m.Line != 2 || m.Column != 13 || text[m.Start:m.End] != "10.0.0.5" {
		t.Errorf("first match = %+v (%q), want IPv4 at line 2, column 13 (columns count characters)", m, text[m.Start:m.End])
	}
	if m := got[1]; m.Kind != IPv6 || m.Line != 4 || m.Column != 1 {
		t.Errorf("second match = %+v, want IPv6 at line 4, column 1", m)
	}
}

func TestScan_OrderedAndWithoutOverlaps(t *testing.T) {
	text := "x rtsp://ion@andrei-pc.local:554/s y owner@firma.ro z 10.1.2.3"
	got := found(text)
	want := []string{"rtsp URL: rtsp://ion@andrei-pc.local:554/s", "e-mail address: owner@firma.ro", "IPv4 address: 10.1.2.3"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Scan = %q, want %q", got, want)
	}
}

func TestScan_CleanReportText(t *testing.T) {
	// What spikereport writes after its scrub: placeholders and versions, nothing to find.
	text := "| MV13 | pereche host/prflx | ambele capete host sau prflx |\n" +
		"- connect failed: [url]; retry [ip] then [ip]; mac [mac]; [email]; serial: [redacted]; SSID: [redacted]\n" +
		"**Dispozitiv:** samsung SM-A245F · Android 14 (API 34) · com.google.android.webview 120.0.6099\n" +
		"Deschideți `http://127.0.0.1:7080`; pe telefon, adresa de forma `http://…:7080`.\n" +
		"chrome,S1,chrome-20261019-042329-s1.json,used,firstFrameMs,0,12.5\n"
	expectNone(t, text)
}
