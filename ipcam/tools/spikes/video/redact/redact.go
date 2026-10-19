// Package redact finds text that must not reach the public repository: the automatic
// check of PH0-22 (spec §16.2.3 and §16.2.4, ruling R51). spikereport runs Scan on
// everything it writes, cmd/redactscan runs it in CI over the published Phase 0 reports
// and their raw data, and the detector's `verify` can reuse it in Phase 0 (a).
//
// Scan finds:
//
//   - IPv4 literals: four dotted numbers of at most 255, not part of a longer dotted
//     number and not glued to a word; a version after its product token (Chrome/141.0.0.0)
//     or a section number (§16.2.3.1) is not an address;
//   - IPv6 literals: full or compressed with ::, with an IPv4 tail or a zone, standing
//     alone (std::string is a scope operator, not an address);
//   - MAC addresses: six pairs of hex digits joined by ":" or "-", or three groups of four
//     joined by "." (aabb.ccdd.eeff);
//   - e-mail addresses;
//   - rtsp:// and rtsps:// URLs, with or without credentials;
//   - .local hostnames (mDNS names: a PC, a phone, a browser's candidate);
//   - Windows user paths, C:\Users\<name> (also with "/" or doubled "\\");
//   - serial-like tokens: see Serial.
//
// The documented placeholders pass: 127.0.0.1 and ::1 (loopback, the address the owner
// guide gives for the PC), forms with letters or an ellipsis (192.168.x.x, http://…:7080,
// rtsp://…, aa:bb:cc:…, C:\Users\<nume>), the RFC 2606 documentation domains
// (example.com, example.net, example.org) and the shared user folders Public and Default.
//
// A Match carries where and what kind, never the matched text, so that a CI log or an
// error message cannot repeat what it found.
package redact

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind names what a match is.
type Kind string

// The kinds of text that Scan finds.
const (
	IPv4      Kind = "IPv4 address"
	IPv6      Kind = "IPv6 address"
	MAC       Kind = "MAC address"
	Email     Kind = "e-mail address"
	RTSPURL   Kind = "rtsp URL"
	LocalHost Kind = ".local hostname"
	UserPath  Kind = "Windows user path"
	// Serial is a long serial-like token: at least 10 characters, only upper-case ASCII
	// letters and digits, with at least 2 of each, cut at anything else (a hyphen, an
	// underscore, a dot, a space, a lower-case letter does not belong to it). Phone and camera
	// serials look like this (R58M12ABCDE, 26071FDH2000JG); model codes are shorter or broken
	// by hyphens (SM-A245F, 2CD2143G2), and words have no digits. An all-hex token of 32 or more
	// characters is a checksum (an MD5, SHA-1 or SHA-256 written in upper case) and passes.
	Serial Kind = "serial-like token"
)

// Match is one finding: its kind and where it is. The text itself is text[Start:End]; a
// caller that reports matches prints the position and the kind only.
type Match struct {
	Kind Kind
	// Start and End are byte offsets in the scanned text.
	Start, End int
	// Line and Column are 1-based; the column counts characters, not bytes.
	Line, Column int
}

// The parts of the address patterns.
const (
	hextet     = `[0-9a-fA-F]{1,4}`
	dottedQuad = `\d{1,3}(?:\.\d{1,3}){3}`
	// A DNS label; its 63-character limit is not checked, which keeps the automaton small.
	hostLabel = `[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?`
)

var (
	ipv4Pattern = regexp.MustCompile(dottedQuad)
	ipv6Pattern = regexp.MustCompile(`(?:` +
		`(?:` + hextet + `:)*(?:` + hextet + `)?::` +
		`(?:(?:` + hextet + `(?::` + hextet + `)*:)?` + dottedQuad + `|(?:` + hextet + `)?(?::` + hextet + `)*)` +
		`|(?:` + hextet + `:){7}` + hextet +
		`|(?:` + hextet + `:){6}` + dottedQuad +
		`)(?:%[A-Za-z0-9_.]+)?`)
	macPattern   = regexp.MustCompile(`(?i)[0-9a-f]{2}(?::[0-9a-f]{2}){5}|[0-9a-f]{2}(?:-[0-9a-f]{2}){5}|[0-9a-f]{4}\.[0-9a-f]{4}\.[0-9a-f]{4}`)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@(` + hostLabel + `(?:\.` + hostLabel + `)*\.[A-Za-z]{2,})`)
	// The scheme; the URL runs to the first space, quote or bracket after it.
	rtspPattern      = regexp.MustCompile(`(?i)rtsps?://`)
	rtspEnd          = regexp.MustCompile("[\\s\"'`<>()\\[\\]{}]|$")
	localPattern     = regexp.MustCompile(`(?i)` + hostLabel + `(?:\.` + hostLabel + `)*\.local`)
	userPathPattern  = regexp.MustCompile(`(?i)[a-z]:(?:\\\\|\\|/)users(?:\\\\|\\|/)([^\\/\s"'<>|:*?%…]+)`)
	serialCandidates = regexp.MustCompile(`[A-Z0-9]{10,}`)
)

// documentationDomains are the e-mail domains of RFC 2606, which name nobody.
var documentationDomains = []string{"example.com", "example.net", "example.org"}

// sharedUserFolders are the folders under C:\Users that belong to no person.
var sharedUserFolders = []string{"public", "default", "all users"}

// Scan returns what text holds that PH0-22 forbids, ordered by position. A match inside
// another one (the address in an rtsp URL) is left out: the outer match covers it.
func Scan(text string) []Match {
	var found []Match
	add := func(kind Kind, start, end int) { found = append(found, Match{Kind: kind, Start: start, End: end}) }

	for _, loc := range ipv4Pattern.FindAllStringIndex(text, -1) {
		if isIPv4(text, loc[0], loc[1]) {
			add(IPv4, loc[0], loc[1])
		}
	}
	for _, loc := range ipv6Pattern.FindAllStringIndex(text, -1) {
		if isIPv6(text, loc[0], loc[1]) {
			add(IPv6, loc[0], loc[1])
		}
	}
	for _, loc := range macPattern.FindAllStringIndex(text, -1) {
		if standsAlone(text, loc[0], loc[1], "0123456789abcdefABCDEF:-.", "0123456789abcdefABCDEF:-") {
			add(MAC, loc[0], loc[1])
		}
	}
	for _, loc := range emailPattern.FindAllStringSubmatchIndex(text, -1) {
		if !isDocumentationDomain(text[loc[2]:loc[3]]) {
			add(Email, loc[0], loc[1])
		}
	}
	for _, loc := range rtspPattern.FindAllStringIndex(text, -1) {
		if end, ok := rtspURL(text, loc[0], loc[1]); ok {
			add(RTSPURL, loc[0], end)
		}
	}
	for _, loc := range localPattern.FindAllStringIndex(text, -1) {
		if isLocalName(text, loc[0], loc[1]) {
			add(LocalHost, loc[0], loc[1])
		}
	}
	for _, loc := range userPathPattern.FindAllStringSubmatchIndex(text, -1) {
		if isPersonalFolder(text, loc) {
			add(UserPath, loc[0], loc[1])
		}
	}
	for _, loc := range serialCandidates.FindAllStringIndex(text, -1) {
		if isSerial(text, loc[0], loc[1]) {
			add(Serial, loc[0], loc[1])
		}
	}
	return positioned(text, outermost(found))
}

// isIPv4 says whether the dotted number at text[start:end] is an address to report.
func isIPv4(text string, start, end int) bool {
	for _, octet := range strings.Split(text[start:end], ".") {
		if n, err := strconv.Atoi(octet); err != nil || n > 255 {
			return false
		}
	}
	if text[start:end] == "127.0.0.1" {
		return false
	}
	before, after := charBefore(text, start), charAfter(text, end)
	if isWordChar(before) || before == '.' || before == '§' {
		return false
	}
	// A version after its product token: Chrome/141.0.0.0, Edg/141.0.0.0.
	if before == '/' && start >= 2 && isWordChar(rune(text[start-2])) {
		return false
	}
	if isWordChar(after) {
		return false
	}
	// A longer dotted number (1.2.3.4.5) is a version.
	if after == '.' && end+1 < len(text) && isDigit(rune(text[end+1])) {
		return false
	}
	return true
}

// isIPv6 says whether the literal at text[start:end] is an address to report.
func isIPv6(text string, start, end int) bool {
	literal := text[start:end]
	if !strings.ContainsAny(literal, "0123456789abcdefABCDEF") || literal == "::1" {
		return false
	}
	// A colon before the literal is a label's ("ip:fe80::1"); the literal itself never starts
	// right after a group of its own, since the pattern would have taken that group in.
	before, after := charBefore(text, start), charAfter(text, end)
	return !isWordChar(before) && before != '.' && !isWordChar(after) && after != ':'
}

// standsAlone: the characters right before and after text[start:end] are not in the given sets.
func standsAlone(text string, start, end int, notBefore, notAfter string) bool {
	before, after := charBefore(text, start), charAfter(text, end)
	if before != 0 && (strings.ContainsRune(notBefore, before) || isWordChar(before)) {
		return false
	}
	return after == 0 || !strings.ContainsRune(notAfter, after) && !isWordChar(after)
}

func isDocumentationDomain(domain string) bool {
	domain = strings.ToLower(domain)
	for _, d := range documentationDomains {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// rtspURL returns the end of the rtsp URL whose scheme is text[start:schemeEnd], unless what
// follows the scheme is no host (a bare "rtsp://") or a placeholder ("rtsp://…", "rtsp://<user>…").
func rtspURL(text string, start, schemeEnd int) (int, bool) {
	if isWordChar(charBefore(text, start)) {
		return 0, false
	}
	rest := text[schemeEnd:]
	if rest == "" || strings.HasPrefix(rest, "…") || strings.HasPrefix(rest, "<") || strings.HasPrefix(rest, "{") {
		return 0, false
	}
	end := rtspEnd.FindStringIndex(rest)[0]
	if end == 0 {
		return 0, false
	}
	return schemeEnd + end, true
}

// isLocalName says whether text[start:end] is a whole .local name: not part of a longer
// name or file name (settings.local.json), not glued to a word.
func isLocalName(text string, start, end int) bool {
	before, after := charBefore(text, start), charAfter(text, end)
	if isWordChar(before) || before == '.' || before == '-' {
		return false
	}
	if isWordChar(after) || after == '-' {
		return false
	}
	return after != '.' || end+1 >= len(text) || !isWordChar(rune(text[end+1]))
}

// isPersonalFolder says whether the folder of a C:\Users path match is a person's.
func isPersonalFolder(text string, loc []int) bool {
	if isWordChar(charBefore(text, loc[0])) {
		return false
	}
	name := strings.ToLower(text[loc[2]:loc[3]])
	for _, shared := range sharedUserFolders {
		if name == shared {
			return false
		}
	}
	return true
}

// isSerial applies the rule of Serial to the run of upper-case letters and digits at text[start:end].
func isSerial(text string, start, end int) bool {
	if isASCIIAlnum(charBefore(text, start)) || isASCIIAlnum(charAfter(text, end)) {
		return false
	}
	token := text[start:end]
	letters, digits, hex := 0, 0, true
	for _, c := range token {
		switch {
		case c >= '0' && c <= '9':
			digits++
		default:
			letters++
			hex = hex && c <= 'F'
		}
	}
	if hex && len(token) >= 32 {
		return false
	}
	return letters >= 2 && digits >= 2
}

// outermost drops every match that lies inside another one, and orders the rest by position.
func outermost(found []Match) []Match {
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Start != found[j].Start {
			return found[i].Start < found[j].Start
		}
		return found[i].End > found[j].End
	})
	var kept []Match
	for _, m := range found {
		if n := len(kept); n > 0 && m.Start < kept[n-1].End {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// positioned fills in the line and column of each match.
func positioned(text string, matches []Match) []Match {
	line, lineStart, at := 1, 0, 0
	for i := range matches {
		for ; at < matches[i].Start; at++ {
			if text[at] == '\n' {
				line++
				lineStart = at + 1
			}
		}
		matches[i].Line = line
		matches[i].Column = utf8.RuneCountInString(text[lineStart:matches[i].Start]) + 1
	}
	return matches
}

// charBefore and charAfter are the characters next to a span; 0 at the edges of the text.
func charBefore(text string, start int) rune {
	if start == 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return r
}

func charAfter(text string, end int) rune {
	if end >= len(text) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(text[end:])
	return r
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

func isASCIIAlnum(c rune) bool {
	return isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isWordChar: a letter (of any script), a digit or an underscore.
func isWordChar(c rune) bool { return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) }
