// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package sdconfirm is the upload spike's copy of the recorder spike's SD
// card confirmation (c1's mi1.SdWarning and its prompt, c1 G16; P01:
// copied, never imported). On a Pi whose rec/ is on an SD card, the first
// hub start is MI6's, so `sudo ./uploadspike setup` asks it once per
// device and the run state keeps the answer (P02, P17).
package sdconfirm

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// Warning is T40's wear warning, verbatim.
const Warning = "Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB."

// Prompt asks for the owner's confirmation; Ask prints it followed by ": ",
// as c1's prompt ends.
const Prompt = "Scrieți DA și apăsați Enter"

// Refused is what the owner reads when the input ends without DA.
const Refused = "Fără confirmarea DA, hub-ul nu pornește pe cardul SD."

// Answer is the one answer that confirms.
const Answer = "DA"

// Ask shows Warning and asks Prompt until the owner types DA; any other
// answer asks again, and the end of the input refuses (false). It reads
// in a byte at a time, never past its answer's line, so that a caller may
// share in with its later prompts. The confirmation keeps the typed text
// and its time.
func Ask(in io.Reader, out io.Writer, now func() time.Time) (results.SdConfirmation, bool) {
	_, _ = fmt.Fprintln(out, Warning)
	for {
		_, _ = fmt.Fprint(out, Prompt+": ")
		line, err := readLine(in)
		if strings.TrimSpace(line) == Answer {
			return results.SdConfirmation{Text: Answer, AtMs: now().UnixMilli()}, true
		}
		if err != nil {
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, Refused)
			return results.SdConfirmation{}, false
		}
	}
}

// readLine reads in up to and without the next newline, a byte at a time;
// err is set at the end of the input.
func readLine(in io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return b.String(), io.EOF
			}
			return b.String(), err
		}
	}
}
