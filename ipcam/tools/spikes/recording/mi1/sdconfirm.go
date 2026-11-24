// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// SdWarning is T40's wear warning, verbatim.
const SdWarning = "Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB."

// sdAsk asks for the owner's confirmation.
const sdAsk = "Scrieți DA și apăsați Enter: "

// ConfirmSD shows T40's warning and asks for «DA» until the owner types it
// (G16); any other answer asks again, and the end of the input refuses.
// The confirmation keeps the typed text and its time, the audit stand-in
// that every envelope carries, and lets ipcam-hub record on the card
// (sdCardConfirmed).
func ConfirmSD(in *bufio.Reader, out io.Writer, now func() time.Time) (*results.SdConfirmation, error) {
	_, _ = fmt.Fprintln(out, SdWarning)
	for {
		_, _ = fmt.Fprint(out, sdAsk)
		line, err := in.ReadString('\n')
		if answer := strings.TrimSpace(line); answer == "DA" {
			return &results.SdConfirmation{Text: answer, AtMs: now().UnixMilli()}, nil
		}
		if err != nil {
			return nil, errors.New("fără confirmarea DA, proba nu înregistrează pe cardul SD")
		}
	}
}
