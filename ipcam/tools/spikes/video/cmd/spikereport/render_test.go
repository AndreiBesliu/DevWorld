package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "rewrite testdata/golden.md")

const goldenDate = "2026-10-19"

func render(t *testing.T, results []Result) string {
	t.Helper()
	return BuildReport(results, nil, goldenDate).Markdown()
}

// An IPv4 or IPv6 literal, as a scanner for PH0-22 would look for it.
var (
	ipv4Literal = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	ipv6Literal = regexp.MustCompile(`(?i)(?:(?:[0-9a-f]{1,4}:)*[0-9a-f]{0,4}::(?:[0-9a-f]{0,4}(?::[0-9a-f]{1,4})*)|(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4})`)
	// What is left of an address when only a part of it was replaced: three dotted numbers of
	// up to three digits ("[ip].168.1.20" is what a scrub of the IPv6 part alone leaves of
	// ::ffff:192.168.1.20), not glued to a word or a slash, which is how an engine version
	// ("Chrome/141.0.0") looks.
	ipv4Fragment = regexp.MustCompile(`(?:^|[^\w/])\.?\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
)

func TestReport_FlagsHubJitter(t *testing.T) {
	rs := allRuns()
	s2 := find(t, rs, androidDevice, "S2")
	s2.HubJitterMs = []float64{3, 24.5, 4}
	find(t, rs, androidDevice, "S1").HubJitterMs = []float64{2, 20}     // 20 ms is the limit, not over it
	find(t, rs, androidDevice, "S3-4").HubJitterMs = []float64{19.9, 4} // under

	flags := HubJitterFlags(rs)
	if len(flags) != 1 || flags[0].File != s2.File || flags[0].MaxMs != 24.5 {
		t.Fatalf("flags = %+v, want only %s at 24.5 ms", flags, s2.File)
	}

	md := render(t, rs)
	warnings := section(t, md, "## Avertismente")
	for _, want := range []string{s2.File, "24,5 ms", "20 ms"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings lack %q:\n%s", want, warnings)
		}
	}
	if strings.Contains(warnings, "-s1.json") || strings.Contains(warnings, "-s3-4.json") {
		t.Errorf("warnings name a run that is under the limit:\n%s", warnings)
	}
	if !strings.Contains(md, "jitter") {
		t.Error("the report never says jitter")
	}

	quiet := render(t, allRuns())
	if strings.Contains(quiet, "24,5") || strings.Contains(quiet, "peste 20 ms") {
		t.Errorf("a quiet set is flagged:\n%s", quiet)
	}
}

func TestRender_NoIPOrSerialInOutput(t *testing.T) {
	rs := allRuns()
	dirt := []string{
		"connect failed: http://192.168.1.20:7080/spike/v1/offer",
		"retry 10.0.0.5:7080 then [fe80::1ff:fe23:4567:890a]:7080",
		"rtsp://admin:hunter2@10.0.0.9/stream1",
		"mac aa:bb:cc:dd:ee:ff seen",
		"owner@example.com asked",
		"serial: R58M12ABCDE",
		"SSID: Casa Mea 5G",
		"2001:db8:85a3:0:0:8a2e:370:7334 is the hub",
		"cand ::ffff:192.168.1.20 typ host",
		"relay [::ffff:10.0.0.1]:7080 refused",
		"link fe80::1%eth0 is up",
	}
	android := find(t, rs, androidDevice, "S3-9")
	android.Notes = append(android.Notes, dirt...)
	find(t, rs, androidDevice, "S1").Device.Model = "samsung SM-A245F serial=R58M12ABCDE"
	find(t, rs, androidDevice, "S2").Device.OS = "Android 14 via 192.168.1.20"
	// The version of an engine has four numbers, like an IPv4 address.
	find(t, rs, androidDevice, "S4").Device.WebView = "com.google.android.webview 120.0.6099.230"
	find(t, rs, "chrome", "S10").Device.WebView = "Chrome/141.0.0.0"

	odd := *find(t, rs, androidDevice, "S1")
	odd.Scenario = "S99 via 10.9.8.7"
	odd.File = "odd.json"
	rs = append(rs, odd)

	report := BuildReport(rs, []Skipped{{File: "bad.json", Reason: "invalid character 'x' at 10.1.2.3"}}, goldenDate)
	outputs := map[string]string{"markdown": report.Markdown()}
	for name, csv := range report.CSVs() {
		outputs[name] = csv
	}
	for name, out := range outputs {
		for _, re := range []*regexp.Regexp{ipv4Literal, ipv4Fragment, ipv6Literal} {
			if m := re.FindString(out); m != "" {
				t.Errorf("%s holds the address-like text %q", name, m)
			}
		}
		for _, secret := range []string{"192.168", "168.1.20", "10.0.0.1", "ffff:", "hunter2", "R58M12ABCDE", "Casa Mea", "aa:bb:cc", "owner@example", "http://", "rtsp://", "fe80", "eth0", "2001:db8"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s holds %q", name, secret)
			}
		}
	}
	md := outputs["markdown"]
	if !strings.Contains(md, "120.0.6099") || !strings.Contains(md, "141.0.0") {
		t.Errorf("the engine versions were dropped instead of shortened:\n%s", md)
	}
	if !strings.Contains(md, "samsung SM-A245F") {
		t.Error("the device model is gone")
	}
}

func TestScrubText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"hub at http://192.168.1.20:7080/x failed", "hub at [url] failed"},
		{"10.0.0.5:7080", "[ip]"},
		{"from fe80::1ff:fe23:4567:890a and 2001:db8:85a3:0:0:8a2e:370:7334", "from [ip] and [ip]"},
		// An IPv6 literal with a zone, and one that ends in an IPv4 address: all of it goes, not
		// the IPv6 part alone (which would leave ".168.1.20").
		{"fe80::1%eth0", "[ip]"},
		{"cand ::ffff:192.168.1.20 typ host", "cand [ip] typ host"},
		{"[::ffff:10.0.0.1]:7080", "[ip]"},
		{"2001:db8::1:192.168.1.20", "[ip]"},
		{"64:ff9b::10.1.2.3 and ::10.1.2.3", "[ip] and [ip]"},
		{"0:0:0:0:0:ffff:10.0.0.7", "[ip]"},
		{"aa:bb:cc:dd:ee:ff", "[mac]"},
		{"AA-BB-CC-DD-EE-FF", "[mac]"},
		{"write to owner@example.com now", "write to [email] now"},
		{"serial: R58M12ABCDE ok", "serial: [redacted] ok"},
		{"Serial Number=ABC123", "Serial Number=[redacted]"},
		{"SSID: Casa Mea 5G", "SSID: [redacted]"},
		{"WiFi ssid=Cafe", "WiFi ssid=[redacted]"},
		{"pair: host/host", "pair: host/host"},
		{"mx-h265/main: error CODEC_UNSUPPORTED", "mx-h265/main: error CODEC_UNSUPPORTED"},
		{"tile 3: NO_FIRST_FRAME (hint: ICE 12:30 timeout)", "tile 3: NO_FIRST_FRAME (hint: ICE 12:30 timeout)"},
	} {
		if got := scrubText(tc.in); got != tc.want {
			t.Errorf("scrubText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShortVersions(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Chrome/141.0.0.0", "Chrome/141.0.0"},
		{"com.google.android.webview 120.0.6099.230", "com.google.android.webview 120.0.6099"},
		{"WebView2 154.0.4258.62", "WebView2 154.0.4258"},
		{"Windows 10.0.26100.1", "Windows 10.0.26100"},
		{"Firefox/132.0", "Firefox/132.0"},
		{"Android 14 (API 34)", "Android 14 (API 34)"},
		{"Windows 11 Pro", "Windows 11 Pro"},
		// A real address is left to scrubText: shortening it would leak three quarters of it.
		{"via 192.168.1.20", "via 192.168.1.20"},
	} {
		if got := shortVersions(tc.in); got != tc.want {
			t.Errorf("shortVersions(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRender_GoldenMarkdown(t *testing.T) {
	results, skipped, err := loadResults(filepath.Join("testdata", "golden-set"))
	if err != nil {
		t.Fatal(err)
	}
	got := BuildReport(results, skipped, goldenDate).Markdown()
	golden := filepath.Join("testdata", "golden.md")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // a fixed path in testdata
	if err != nil {
		t.Fatalf("no golden file (go test -run Golden -update writes it): %v", err)
	}
	want = []byte(strings.ReplaceAll(string(want), "\r\n", "\n")) // a checkout with autocrlf
	if got != string(want) {
		t.Errorf("the report differs from testdata/golden.md (go test -run Golden -update rewrites it)\n%s", firstDifference(got, string(want)))
	}
	// Again, from the same files in another order: the report does not depend on it.
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	if again := BuildReport(results, skipped, goldenDate).Markdown(); again != got {
		t.Errorf("the report depends on the order of the results:\n%s", firstDifference(again, got))
	}
}

// firstDifference shows the first line where two texts differ.
func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return "line " + strconv.Itoa(i+1) + "\n got: " + gl + "\nwant: " + wl
		}
	}
	return "no difference"
}

// after is the text of the report from `heading` on.
func after(t *testing.T, md, heading string) string {
	t.Helper()
	i := strings.Index(md, heading)
	if i < 0 {
		t.Fatalf("the report has no %q", heading)
	}
	return md[i:]
}

// section is the text of the "## heading" section: up to the next "## " heading.
func section(t *testing.T, md, heading string) string {
	t.Helper()
	i := strings.Index(md, heading)
	if i < 0 {
		t.Fatalf("the report has no %q", heading)
	}
	rest := md[i+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestRender_RomanianDiacriticsAreCommaBelow(t *testing.T) {
	md := render(t, allRuns())
	if !utf8.ValidString(md) {
		t.Fatal("the report is not valid UTF-8")
	}
	for _, bad := range []string{"ş", "Ş", "ţ", "Ţ"} { // cedilla forms, which are wrong in Romanian
		if strings.Contains(md, bad) {
			t.Errorf("the report holds %q (cedilla) instead of the comma-below letter", bad)
		}
	}
	for _, good := range []string{"ș", "ț", "ă", "â", "î"} {
		if !strings.Contains(md, good) {
			t.Errorf("the report never uses %q", good)
		}
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "golden.md"))
	if err == nil {
		for _, bad := range []string{"ş", "Ş", "ţ", "Ţ"} {
			if strings.Contains(string(golden), bad) {
				t.Errorf("golden.md holds %q (cedilla)", bad)
			}
		}
	}
}

func TestRender_FollowsThePH030Layout(t *testing.T) {
	md := render(t, allRuns())
	order := []string{
		"# Rezultatele Fazei 0 (b): spike-ul video",
		"## Rezumat pentru proprietar",
		"## Rezultate pe dispozitiv",
		"### AND-1",
		"### WIN-1",
		"### Web: Chrome",
		"## Verdicte și decizii declanșate",
		"## Rămas pentru Faza 0-bis",
		"## Date brute",
	}
	at := -1
	for _, heading := range order {
		i := strings.Index(md, heading)
		if i < 0 {
			t.Errorf("the report lacks %q", heading)
			continue
		}
		if i < at {
			t.Errorf("%q comes before the heading that should precede it", heading)
		}
		at = i
	}
	if n := strings.Count(md, "| Cod | Valoare | Prag | Trece |"); n != 5 {
		t.Errorf("%d tables of Cod | Valoare | Prag | Trece, want one for each of the 5 devices", n)
	}
	for _, want := range []string{"| MV1 |", "| MV9 |", "| MV3/9 |", "| MV6 |", "| MV13 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report has no row %s", want)
		}
	}
	for _, verdict := range []Verdict{VerdictGOProvizoriu, VerdictGO} {
		if !strings.Contains(md, string(verdict)) {
			t.Errorf("the report never says %q", verdict)
		}
	}
	// The leftovers of Faza 0-bis (spec 16.9): iOS, MV11, the Chrome part of MV12, AND-2.
	tail := after(t, md, "## Rămas pentru Faza 0-bis")
	for _, want := range []string{"iOS-1", "iOS-2", "MV11", "MV12", "Chrome", "AND-2", "OWN-05", "OWN-11", "OWN-17", "Faza 4", "Faza 2"} {
		if !strings.Contains(tail, want) {
			t.Errorf("the Faza 0-bis list lacks %q", want)
		}
	}
	if !strings.Contains(md, "phase0-data/phase0b-android.csv") {
		t.Error("the report does not point to its CSV files")
	}
}

func TestRender_ShowsWhatIsMissing(t *testing.T) {
	f := newFixture(androidModel)
	f.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)})
	md := render(t, f.list())
	if !strings.Contains(md, "| MV2 | nemăsurat") || !strings.Contains(md, "| MV9 | nemăsurat") {
		t.Errorf("the rows of the scenarios that did not run are not shown as unmeasured:\n%s", md)
	}
	if !strings.Contains(md, string(VerdictIncomplete)) {
		t.Errorf("a phone with one scenario got a verdict:\n%s", md)
	}
	for _, verdict := range []Verdict{VerdictGOProvizoriu, VerdictGOConditionat, VerdictNOGOMobil} {
		if strings.Contains(md, string(verdict)) {
			t.Errorf("a phone with one scenario got %q", verdict)
		}
	}
}

func TestRender_ListsTheRunsThatDidNotCount(t *testing.T) {
	f := newFixture(androidModel)
	f.add("S3-9", androidGrid(9), "interrupted: the page was hidden 65 s into the run").Interrupted = true
	f.add("S3-9", androidGrid(9))
	f.add("S3-9", androidGrid(9))
	f.add("S4", stability(), "failed: Error: the hub closed the data channel")
	md := render(t, f.list())
	for _, want := range []string{"întreruptă", "înlocuită", "eșuată", "folosită", "the page was hidden 65 s"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	if n := strings.Count(md, "android-20261018-"); n < 4 {
		t.Errorf("the 4 runs are not all listed by file name (%d mentions)", n)
	}
}

func TestRender_WarnsOfSeveralModelsOfOnePlatform(t *testing.T) {
	rs := androidRuns()
	other := find(t, rs, androidDevice, "S1")
	other.Device.Model = "Google sdk_gphone64_x86_64"
	md := render(t, rs)
	warnings := section(t, md, "## Avertismente")
	for _, want := range []string{"Mai multe modele", "samsung SM-A245F", "Google sdk_gphone64_x86_64"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings lack %q:\n%s", want, warnings)
		}
	}
	if quiet := section(t, render(t, androidRuns()), "## Avertismente"); strings.Contains(quiet, "Mai multe modele") {
		t.Error("one model is warned about")
	}
}

// A browser result carries no model, so the machine shows only in the OS: Chrome on the phone
// and Chrome on the PC are two devices, and the report must not merge them without a word.
func TestRender_WarnsWhenOnePlatformRunsOnTwoMachines(t *testing.T) {
	chromeOn := func(os, webview, file string) Result {
		f := newFixture(Device{Platform: "chrome", OS: os, WebView: webview})
		r := f.add("S10", nil, "pair: host/host", "prompt: n/a")
		r.File = file
		return *r
	}
	warningsOf := func(rs ...Result) string { return section(t, render(t, rs), "## Avertismente") }

	// hub-4: the phone's Chrome is a device of its own, listed apart with a warning.
	t.Run("Chrome on the phone and on the PC", func(t *testing.T) {
		warnings := warningsOf(
			chromeOn("Android", "Chrome/141.0.0.0", "chrome-phone-s10.json"),
			chromeOn("Windows", "Chrome/141.0.0.0", "chrome-pc-s10.json"),
		)
		for _, want := range []string{"Chrome pe Android", "nu intră în verdicte", "PC"} {
			if !strings.Contains(warnings, want) {
				t.Errorf("warnings lack %q:\n%s", want, warnings)
			}
		}
		if strings.Contains(warnings, "Mai multe modele") {
			t.Errorf("the phone and the PC are still merged into one device:\n%s", warnings)
		}
	})
	t.Run("two versions of Windows under one browser", func(t *testing.T) {
		warnings := warningsOf(
			chromeOn("Windows 10", "Chrome/141.0.0.0", "chrome-a-s10.json"),
			chromeOn("Windows 11", "Chrome/141.0.0.0", "chrome-b-s10.json"),
		)
		for _, want := range []string{"Mai multe modele", "Windows 10", "Windows 11"} {
			if !strings.Contains(warnings, want) {
				t.Errorf("warnings lack %q:\n%s", want, warnings)
			}
		}
	})
	t.Run("one model on two versions of the OS", func(t *testing.T) {
		old := newFixture(androidModel)
		old.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)})
		updated := androidModel
		updated.OS = "Android 15 (API 35)"
		next := newFixture(updated)
		r := next.add("S2", nil)
		r.File = "android-after-update-s2.json"
		warnings := warningsOf(append(old.list(), *r)...)
		if !strings.Contains(warnings, "Android 14 (API 34)") || !strings.Contains(warnings, "Android 15 (API 35)") {
			t.Errorf("warnings do not name both OS versions:\n%s", warnings)
		}
	})
	t.Run("the same machine with another browser build is one device", func(t *testing.T) {
		warnings := warningsOf(
			chromeOn("Windows 11", "Chrome/141.0.0.0", "chrome-a-s10.json"),
			chromeOn("Windows 11", "Chrome/142.0.0.0", "chrome-b-s10.json"),
		)
		if strings.Contains(warnings, "Mai multe modele") {
			t.Errorf("a browser update is warned about:\n%s", warnings)
		}
	})
}

// R40: the newest valid run decides, but re-running until a pass must not hide a failure.
func TestRender_AnEarlierRunThatFailedStaysVisible(t *testing.T) {
	const warning = "o rulare anterioară a picat pragul"
	slow := map[string][]float64{"firstFrameMs": rep(2600, 20)} // p90 over 1.5 s: fails MV1
	fast := map[string][]float64{"firstFrameMs": rep(800, 20)}

	t.Run("the older run fails and the newer one passes", func(t *testing.T) {
		f := newFixture(androidModel)
		older := f.add("S1", slow)
		f.add("S1", fast)
		rs := f.list()
		if v := ComputeMV(rs)[mvKey(androidDevice, "MV1")]; !v.Pass || v.Value != 800 {
			t.Fatalf("MV1 = %+v, want the newer run's pass with p90 800 ms", v)
		}
		md := render(t, rs)
		row := rowOf(t, md, "| MV1 |")
		for _, want := range []string{"p90 800 ms", warning, "p90 2600 ms", older.File} {
			if !strings.Contains(row, want) {
				t.Errorf("the MV1 row lacks %q:\n%s", want, row)
			}
		}
		if !strings.HasSuffix(row, "| Da |") {
			t.Errorf("the newer run decides, so the row passes:\n%s", row)
		}
		summary := section(t, md, "## Rezumat pentru proprietar")
		if !strings.Contains(summary, "anterioară") || !strings.Contains(summary, "MV1") {
			t.Errorf("the summary does not point to the earlier failure:\n%s", summary)
		}
	})
	t.Run("every earlier run that failed is shown", func(t *testing.T) {
		f := newFixture(androidModel)
		first := f.add("S1", map[string][]float64{"firstFrameMs": rep(3100, 20)})
		second := f.add("S1", slow)
		f.add("S1", fast)
		row := rowOf(t, render(t, f.list()), "| MV1 |")
		for _, want := range []string{"p90 3100 ms", first.File, "p90 2600 ms", second.File} {
			if !strings.Contains(row, want) {
				t.Errorf("the MV1 row lacks %q:\n%s", want, row)
			}
		}
		if n := strings.Count(row, warning); n != 2 {
			t.Errorf("the MV1 row warns %d times, want once for each earlier failure:\n%s", n, row)
		}
	})
	t.Run("the newer run is unmeasured, the earlier failure still shows", func(t *testing.T) {
		f := newFixture(androidModel)
		older := f.add("S2", map[string][]float64{"subLatencyMs": rep(2000, 100), "mainLatencyMs": rep(300, 100)})
		f.add("S2", map[string][]float64{"subLatencyMs": rep(300, 60), "mainLatencyMs": rep(300, 60)})
		row := rowOf(t, render(t, f.list()), "| MV2 |")
		for _, want := range []string{"nemăsurat", warning, older.File} {
			if !strings.Contains(row, want) {
				t.Errorf("the MV2 row lacks %q:\n%s", want, row)
			}
		}
	})
	// The guide does not ask Chrome for S3-4, so MV3/4 has no row of its own when the newest run
	// gives no value: the earlier failure must bring the row, or it would not be shown at all.
	t.Run("an earlier failure of a threshold outside the guide still gets its row", func(t *testing.T) {
		f := newFixture(browserDevice("chrome"))
		bad := gridSamples(4)
		bad["tileFps"][1] = 3
		older := f.add("S3-4", bad)
		f.add("S3-4", map[string][]float64{"gridFps": rep(11.9, 300)}) // no per-tile numbers
		row := rowOf(t, render(t, f.list()), "| MV3/4 |")
		for _, want := range []string{"nemăsurat", warning, "fps minim 3", older.File} {
			if !strings.Contains(row, want) {
				t.Errorf("the MV3/4 row lacks %q:\n%s", want, row)
			}
		}
	})
	t.Run("an earlier run that passed is not a warning", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S1", fast)
		f.add("S1", slow) // the newer run fails and decides
		md := render(t, f.list())
		row := rowOf(t, md, "| MV1 |")
		if strings.Contains(row, "anterioară") || strings.Contains(section(t, md, "## Rezumat pentru proprietar"), "anterioară") {
			t.Errorf("an earlier pass is reported as a failure:\n%s", row)
		}
		if !strings.HasSuffix(row, "| Nu |") {
			t.Errorf("the newer run fails, so the row fails:\n%s", row)
		}
	})
	t.Run("an interrupted run is not an earlier valid run", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S1", slow, "interrupted: the page was hidden 12 s into the run").Interrupted = true
		f.add("S1", fast)
		md := render(t, f.list())
		if row := rowOf(t, md, "| MV1 |"); strings.Contains(row, "anterioară") || strings.Contains(section(t, md, "## Rezumat pentru proprietar"), "anterioară") {
			t.Errorf("an interrupted run is reported as an earlier failure:\n%s", row)
		}
	})
	// MV6 is judged on S3-16; its main-stream part takes the first grid run that has it, which
	// is S3-16's. A reading of an earlier S3-9 run is not what the row's value is made of.
	t.Run("a threshold that belongs to another scenario is not an earlier failure", func(t *testing.T) {
		f := newFixture(windowsModel)
		f.add("S3-16", windowsGrid(16))
		bad := windowsGrid(9)
		bad["mainCpuPctApp"] = rep(14, 12) // over 10 %
		f.add("S3-9", bad)
		f.add("S3-9", windowsGrid(9))
		if row := rowOf(t, render(t, f.list()), "| MV6 |"); strings.Contains(row, warning) || !strings.HasSuffix(row, "| Da |") {
			t.Errorf("the MV6 row blames a reading of another scenario's earlier run:\n%s", row)
		}
	})
	t.Run("a threshold that the earlier run passed is not a warning", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S3-9", androidGrid(9)) // passes
		bad := androidGrid(9)
		bad["tileFps"][3] = 4 // MV3/9 fails
		older := f.add("S3-9", bad)
		f.add("S3-9", androidGrid(9))
		md := render(t, f.list())
		if row := rowOf(t, md, "| MV3/9 |"); !strings.Contains(row, warning) || !strings.Contains(row, "fps minim 4") || !strings.Contains(row, older.File) {
			t.Errorf("the MV3/9 row does not show the earlier failure:\n%s", row)
		}
		for _, code := range []string{"MV4/9", "MV5/9"} {
			if row := rowOf(t, md, "| "+code+" |"); strings.Contains(row, warning) {
				t.Errorf("the %s row warns, but that run passed it:\n%s", code, row)
			}
		}
	})
}

// PH0-30 asks the owner to approve the numbers; so every choice of statistic where the spec
// names none is written down in the limitations.
func TestRender_LimitsNameEveryStatisticChoice(t *testing.T) {
	limits := section(t, render(t, allRuns()), "## Limitări ale măsurătorilor")
	for _, tc := range []struct {
		choice string
		words  []string
	}{
		{"MV10 and MV15 judge every reading", []string{"MV10 și MV15", "fiecare citire"}},
		{"MV8 uses the 2 s gap rule", []string{"MV8", "pauză", "2 s"}},
		{"MV6 uses the mean of cpuPctApp", []string{"MV6", "media", "cpuPctApp"}},
		{"MV5 fps uses the last 2 minutes", []string{"MV5", "ultimelor 2 minute", "gridFps"}},
		{"MV2 keeps unreliable-clock samples", []string{"MV2", "ceas nesigur", "le raportează"}},
		{"the resolution of the MV5 battery rate", []string{"MV5", "baterie", "6 %/h", "durata măsurată a grilei"}},
		{"WIN-1 memMb is private bytes", []string{"WIN-1", "memMb", "memorie privată", "R37", "memoria GPU"}},
		{"an earlier failing run stays visible (R40)", []string{"R40", "rulare anterioară"}},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			// Every word must come in one bullet, so that the choice is one statement.
			for _, line := range strings.Split(limits, "\n") {
				ok := true
				for _, w := range tc.words {
					ok = ok && strings.Contains(line, w)
				}
				if ok {
					return
				}
			}
			t.Errorf("no limitation says all of %q in one bullet:\n%s", tc.words, limits)
		})
	}
}

func TestRender_SaysWhatTheMemoryOfWindowsLeavesOut(t *testing.T) {
	md := render(t, windowsRuns())
	win := after(t, md, "### WIN-1")
	for _, want := range []string{"memorie privată", "GPU", "R37"} {
		if !strings.Contains(win, want) {
			t.Errorf("WIN-1 lacks %q next to MV7", want)
		}
	}
}

func TestRender_TheSummaryExplainsTheVerdicts(t *testing.T) {
	rs := allRuns()
	find(t, rs, androidDevice, "S3-9").Samples["tileFps"][5] = 6
	find(t, rs, windowsDevice, "S3-16").Samples["cpuPctApp"] = rep(40, 60)
	md := render(t, rs)
	summary := section(t, md, "## Rezumat pentru proprietar")
	for _, want := range []string{"GO condiționat", "MV3/9", "plan B: grilă desktop 9", "A6"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary lacks %q:\n%s", want, summary)
		}
	}
}

func TestRender_DecimalCommaInTheReportAndPointInTheCSV(t *testing.T) {
	rs := allRuns()
	find(t, rs, androidDevice, "S3-9").Samples["tileFps"] = rep(11.25, 9)
	report := BuildReport(rs, nil, goldenDate)
	if md := report.Markdown(); !strings.Contains(md, "11,25") || strings.Contains(md, "11.25") {
		t.Errorf("the report writes 11.25 with a point or not at all")
	}
	csv := report.CSVs()["phase0-data/phase0b-android.csv"]
	if !strings.Contains(csv, ",11.25\n") || strings.Contains(csv, "11,25") {
		t.Errorf("the CSV writes 11.25 with a comma or not at all")
	}
}

func TestCSVs_OneRowPerSample(t *testing.T) {
	f := newFixture(androidModel)
	f.add("S1", map[string][]float64{"firstFrameMs": {300, 450.5}})
	bad := f.add("S1", map[string][]float64{"firstFrameMs": {9000}}, "interrupted: the page was hidden 3 s into the run")
	bad.Interrupted = true
	bad.HubJitterMs = []float64{1.5}
	pc := newFixture(browserDevice("chrome"))
	pc.add("S8", map[string][]float64{"argon2idMs": {100}})
	report := BuildReport(append(f.list(), pc.list()...), nil, goldenDate)
	files := report.CSVs()
	if len(files) != 2 {
		t.Fatalf("CSV files = %v, want one for android and one for chrome", keys(files))
	}
	want := "device,scenario,run,status,key,index,value\n" +
		"android,S1,android-20261018-100100-s1.json,used,firstFrameMs,0,300\n" +
		"android,S1,android-20261018-100100-s1.json,used,firstFrameMs,1,450.5\n" +
		"android,S1,android-20261018-100200-s1.json,interrupted,firstFrameMs,0,9000\n" +
		"android,S1,android-20261018-100200-s1.json,interrupted,hubJitterMs,0,1.5\n"
	if got := files["phase0-data/phase0b-android.csv"]; got != want {
		t.Errorf("phase0b-android.csv =\n%s\nwant\n%s", got, want)
	}
	if got := files["phase0-data/phase0b-chrome.csv"]; !strings.Contains(got, "chrome,S8,") {
		t.Errorf("phase0b-chrome.csv = %q", got)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestReportFileName(t *testing.T) {
	if got := BuildReport(allRuns(), nil, "2026-10-19").FileName(); got != "2026-10-19-phase0b-results.md" {
		t.Errorf("file name = %q", got)
	}
}
