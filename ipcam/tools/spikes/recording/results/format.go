// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

import (
	"strconv"
	"strings"
)

// Decimal renders f with prec decimals (-1: as few as it needs) and a
// decimal comma, as the report's Romanian writes figures.
func Decimal(f float64, prec int) string {
	return strings.Replace(strconv.FormatFloat(f, 'f', prec, 64), ".", ",", 1)
}

// RowPass is a row's Pass: false when ok fails, true when ok holds on a
// complete part, and none while the part is incomplete.
func RowPass(ok, complete bool) *bool {
	switch {
	case !ok:
		return new(false)
	case complete:
		return new(true)
	}
	return nil
}
