// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package results

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// TokenKind names a token pattern (P45).
type TokenKind string

// The token patterns that uploadreport applies after the video spike's
// redaction scanner (Task 11): what an OAuth provider hands out, by its
// shape.
const (
	GoogleAccessToken  TokenKind = "Google access token"
	GoogleRefreshToken TokenKind = "Google refresh token"
	GoogleClientSecret TokenKind = "Google client secret"
	MicrosoftToken     TokenKind = "Microsoft token"
	JWT                TokenKind = "JWT"
	DeviceUserCode     TokenKind = "device user code"
	// OpaqueID is a run of 25 or more base64url characters with upper-case
	// letters, lower-case letters and digits: a Drive file ID, a token's
	// body or a client ID. The report's own names are shorter, or carry no
	// upper-case letter (hex checksums, pseudonyms).
	OpaqueID TokenKind = "opaque identifier"
)

// TokenMatch is one token found: its kind and where, never its text.
type TokenMatch struct {
	Kind TokenKind
	// Start and End are byte offsets; Line and Column are 1-based, the
	// column counting characters.
	Start, End   int
	Line, Column int
}

// tokenPattern is a kind and its pattern; the patterns are tried in order,
// and a match inside an earlier one is left out.
type tokenPattern struct {
	kind TokenKind
	re   *regexp.Regexp
}

// b64 is a base64url character.
const b64 = `[A-Za-z0-9_-]`

var tokenPatterns = []tokenPattern{
	{GoogleAccessToken, regexp.MustCompile(`ya29\.` + b64 + `{10,}`)},
	{GoogleRefreshToken, regexp.MustCompile(`1//` + b64 + `{10,}`)},
	{GoogleClientSecret, regexp.MustCompile(`GOCSPX-` + b64 + `{10,}`)},
	{MicrosoftToken, regexp.MustCompile(`M\.C[0-9]{1,4}_[A-Z0-9]+\.[A-Za-z0-9!*$._-]{10,}`)},
	{MicrosoftToken, regexp.MustCompile(`EwB[A-Za-z0-9+/=_-]{20,}`)},
	{JWT, regexp.MustCompile(`eyJ` + b64 + `{5,}\.` + b64 + `{5,}\.` + b64 + `*`)},
	{DeviceUserCode, regexp.MustCompile(`[A-Z]{4}-[A-Z]{4}`)},
	{OpaqueID, regexp.MustCompile(b64 + `{25,}`)},
}

// ScanTokens returns the token-shaped text in text, ordered by position.
// Each match must stand alone: not glued to a letter or a digit (a device
// user code, an opaque ID: not to a hyphen or an underscore either).
func ScanTokens(text string) []TokenMatch {
	var found []TokenMatch
	inside := func(start, end int) bool {
		return slices.ContainsFunc(found, func(m TokenMatch) bool { return start < m.End && end > m.Start })
	}
	for _, p := range tokenPatterns {
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			start, end := loc[0], loc[1]
			if !standsAlone(text, start, end, p.kind) || inside(start, end) {
				continue
			}
			if p.kind == OpaqueID && !mixed(text[start:end]) {
				continue
			}
			found = append(found, TokenMatch{Kind: p.kind, Start: start, End: end})
		}
	}
	slices.SortFunc(found, func(a, b TokenMatch) int { return a.Start - b.Start })
	for i := range found {
		before := text[:found[i].Start]
		found[i].Line = strings.Count(before, "\n") + 1
		found[i].Column = utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:]) + 1
	}
	return found
}

// standsAlone reports whether text[start:end] is not glued to the text
// around it.
func standsAlone(text string, start, end int, kind TokenKind) bool {
	glued := func(c byte) bool {
		alnum := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if kind == DeviceUserCode || kind == OpaqueID {
			return alnum || c == '-' || c == '_'
		}
		return alnum
	}
	return (start == 0 || !glued(text[start-1])) && (end == len(text) || !glued(text[end]))
}

// mixed reports whether s has an upper-case letter, a lower-case letter
// and a digit.
func mixed(s string) bool {
	return strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") && strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz") &&
		strings.ContainsAny(s, "0123456789")
}
