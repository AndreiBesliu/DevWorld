package results

import "testing"

// TestDecimal: a figure in the report's Romanian, with a decimal comma and
// prec decimals, or as few as it needs with -1.
func TestDecimal(t *testing.T) {
	for _, c := range []struct {
		f    float64
		prec int
		want string
	}{{1.5, -1, "1,5"}, {10, -1, "10"}, {10, 1, "10,0"}, {455.4, 0, "455"}, {1.0123, 3, "1,012"}} {
		if got := Decimal(c.f, c.prec); got != c.want {
			t.Errorf("Decimal(%v, %d) = %q, want %q", c.f, c.prec, got, c.want)
		}
	}
}

// TestRowPass: a row's Pass is false when its check fails, true when it
// holds on a complete part, and none while the part is incomplete.
func TestRowPass(t *testing.T) {
	text := func(p *bool) string {
		if p == nil {
			return "none"
		}
		if *p {
			return "true"
		}
		return "false"
	}
	for _, c := range []struct {
		ok, complete bool
		want         string
	}{{false, true, "false"}, {false, false, "false"}, {true, true, "true"}, {true, false, "none"}} {
		if got := text(RowPass(c.ok, c.complete)); got != c.want {
			t.Errorf("RowPass(%v, %v) = %s, want %s", c.ok, c.complete, got, c.want)
		}
	}
}
