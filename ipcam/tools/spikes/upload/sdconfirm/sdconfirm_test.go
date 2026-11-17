package sdconfirm

import (
	"io"
	"strings"
	"testing"
	"time"
)

// TestSdConfirm_RequiresDA (P02, P17, c1 G16): only DA confirms; any other
// answer asks again, and the end of the input refuses. The warning is T40's
// verbatim, and the prompt ends with ": " as c1's. Ask reads no further
// than its answer's line.
func TestSdConfirm_RequiresDA(t *testing.T) {
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return at }
	for _, tc := range []struct {
		name, input string
		ok          bool
		prompts     int
		left        string
	}{
		{"DA", "DA\nrest\n", true, 1, "rest\n"},
		{"DA with spaces and CRLF", "  DA \r\nrest\n", true, 1, "rest\n"},
		{"da, then DA", "da\nnu\nDA\nrest\n", true, 3, "rest\n"},
		{"DA without a newline at the end", "DA", true, 1, ""},
		{"no answer", "", false, 1, ""},
		{"other answers, then the end", "da\nyes\n", false, 3, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			in := strings.NewReader(tc.input)
			conf, ok := Ask(in, &out, clock)
			if ok != tc.ok {
				t.Fatalf("Ask = %+v, %v; want ok %v (printed %q)", conf, ok, tc.ok, out.String())
			}
			if ok && (conf.Text != "DA" || conf.AtMs != at.UnixMilli()) {
				t.Errorf("confirmation %+v; want DA at %d", conf, at.UnixMilli())
			}
			if !ok && (conf.Text != "" || !strings.Contains(out.String(), Refused)) {
				t.Errorf("refusal: %+v, printed %q; want no confirmation and %q", conf, out.String(), Refused)
			}
			if !strings.HasPrefix(out.String(), Warning+"\n") {
				t.Errorf("printed %q; want the warning first", out.String())
			}
			if n := strings.Count(out.String(), "Scrieți DA și apăsați Enter: "); n != tc.prompts {
				t.Errorf("asked %d times, want %d (printed %q)", n, tc.prompts, out.String())
			}
			if rest, _ := io.ReadAll(in); string(rest) != tc.left {
				t.Errorf("left %q of the input, want %q", rest, tc.left)
			}
		})
	}
	if Warning != "Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB." {
		t.Error("the warning is not T40's")
	}
}
