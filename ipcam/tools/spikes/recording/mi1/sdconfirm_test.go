package mi1

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

// TestSdConfirm_RequiresDA (G16, T40): on an SD card, recspike shows T40's
// warning verbatim and asks «Scrieți DA și apăsați Enter»; only «DA» is a
// confirmation, which it keeps with its time; anything else asks again,
// and the end of the input refuses.
func TestSdConfirm_RequiresDA(t *testing.T) {
	now := time.Date(2026, 10, 25, 21, 30, 0, 0, time.UTC)
	var out strings.Builder
	c, err := ConfirmSD(bufio.NewReader(strings.NewReader("da\nyes\n\n DA \n")), &out, func() time.Time { return now })
	if err != nil {
		t.Fatalf("ConfirmSD: %v", err)
	}
	if c.Text != "DA" || c.AtMs != now.UnixMilli() {
		t.Errorf("confirmation = %+v, want DA at %d", c, now.UnixMilli())
	}
	const warning = "Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB."
	if !strings.Contains(out.String(), warning) {
		t.Errorf("output lacks T40's warning verbatim:\n%s", out.String())
	}
	if n := strings.Count(out.String(), "Scrieți DA și apăsați Enter"); n != 4 {
		t.Errorf("asked %d times, want 4 (da, yes and an empty line are no confirmation)", n)
	}

	out.Reset()
	if c, err := ConfirmSD(bufio.NewReader(strings.NewReader("nu\n")), &out, time.Now); err == nil || c != nil {
		t.Errorf("ConfirmSD without DA = %+v, %v; want an error", c, err)
	} else if !strings.Contains(err.Error(), "DA") {
		t.Errorf("the refusal %q does not say what to type", err)
	}
}
