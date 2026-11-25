package results

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// body is a token body of n characters of base64url, built at run time.
func body(n int) string {
	const alphabet = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_"
	var b strings.Builder
	for i := range n {
		b.WriteByte(alphabet[(i*7)%len(alphabet)])
	}
	return b.String()
}

// TestTokenPatterns_Match: each provider's token shape (P45), assembled
// from parts at run time, is found alone and inside a sentence, with its
// kind and position.
func TestTokenPatterns_Match(t *testing.T) {
	cases := []struct {
		name, token string
		kind        TokenKind
	}{
		{"google access", "ya" + "29." + body(60), GoogleAccessToken},
		{"google refresh", "1/" + "/0" + body(40), GoogleRefreshToken},
		{"google client secret", "GOC" + "SPX-" + body(28), GoogleClientSecret},
		{"microsoft refresh", "M." + "C5" + "07_BAY.0.U.-" + body(40), MicrosoftToken},
		{"microsoft access", "Ew" + "B" + body(80), MicrosoftToken},
		{"jwt", "ey" + "J" + body(20) + "." + "ey" + "J" + body(30) + "." + body(30), JWT},
		{"device user code", "WD" + "JB-MJ" + "HT", DeviceUserCode},
		{"opaque id", "1" + "Bx" + "9" + body(30), OpaqueID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if m := ScanTokens(c.token); len(m) != 1 || m[0].Kind != c.kind {
				t.Fatalf("alone: got %+v, want one %s", m, c.kind)
			}
			text := "prima linie\nvaloarea «" + c.token + "» aici"
			m := ScanTokens(text)
			if len(m) != 1 || m[0].Kind != c.kind || m[0].Line != 2 || m[0].Column != 11 {
				t.Fatalf("in text: got %+v, want one %s at 2:11", m, c.kind)
			}
		})
	}
}

// TestTokenPatterns_NoFalsePositiveOnGolden: the report's golden file, the
// plan's codes and the spike's own names match no pattern.
func TestTokenPatterns_NoFalsePositiveOnGolden(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("..", "cmd", "uploadreport", "testdata", "golden.md"))
	if err != nil {
		t.Fatal(err)
	}
	if m := ScanTokens(string(golden)); len(m) > 0 {
		t.Fatalf("golden.md: %+v", m)
	}
	for _, s := range []string{
		"TST-07", "OWN-09", "PH0-30", "SPIKE-ONLY", "G14 și G15", "mi5-20261124t101010z", "lab-linux-kill9",
		"files.create.resumable", "M.C. Escher", "1/2", "ya29", "dpapi-machine", "MI5/pc/mi5-20261124t101010z",
		"DS-2CD2143G2-I", "ABCD-1234", "0123456789abcdef0123456789abcdef", "ipcam.phase0c2.result/1",
	} {
		if m := ScanTokens(s); len(m) > 0 {
			t.Errorf("%q: %+v", s, m)
		}
	}
}
