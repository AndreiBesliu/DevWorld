// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// The modes of a set: automated on the owner's devices (inside the owner's
// window), real cuts by the owner, and the lab.
const (
	ModeAutomated = "automated"
	ModeReal      = "real"
	ModeLab       = "lab"
)

// MaxRunSpan is how long after its start an automated run may still stop:
// the controller draws the stop up to 12 min after the first beacon, which
// comes once ipcam-hub records, within a minute of the start.
const MaxRunSpan = 13 * time.Minute

// Window is the owner's daily window for automated stops (G09), in local
// time; it may run past midnight.
type Window struct {
	from, to int // minutes after midnight
	text     string
}

var windowSyntax = regexp.MustCompile(`^(\d\d):(\d\d)-(\d\d):(\d\d)$`)

// ParseWindow reads a window written HH:MM-HH:MM, such as 22:00-06:30.
func ParseWindow(s string) (Window, error) {
	m := windowSyntax.FindStringSubmatch(s)
	if m == nil {
		return Window{}, fmt.Errorf("fereastra %q nu este de forma HH:MM-HH:MM, de exemplu 22:00-06:30", s)
	}
	minute := func(h, mm string) (int, bool) {
		hh, _ := strconv.Atoi(h)
		mi, _ := strconv.Atoi(mm)
		return hh*60 + mi, hh < 24 && mi < 60
	}
	from, ok1 := minute(m[1], m[2])
	to, ok2 := minute(m[3], m[4])
	switch {
	case !ok1 || !ok2:
		return Window{}, fmt.Errorf("fereastra %q are o oră care nu există", s)
	case from == to:
		return Window{}, fmt.Errorf("fereastra %q începe și se termină la aceeași oră", s)
	}
	return Window{from: from, to: to, text: s}, nil
}

// String is the window as the owner wrote it.
func (w Window) String() string { return w.text }

// opening returns the opening of the window that holds t, if any: its start
// and end.
func (w Window) opening(t time.Time) (start, end time.Time, ok bool) {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	length := w.to - w.from
	if length < 0 {
		length += 24 * 60
	}
	for _, day := range []int{0, -1} {
		start = midnight.AddDate(0, 0, day).Add(time.Duration(w.from) * time.Minute)
		end = start.Add(time.Duration(length) * time.Minute)
		if !t.Before(start) && t.Before(end) {
			return start, end, true
		}
	}
	return time.Time{}, time.Time{}, false
}

// Contains reports whether t falls inside the window.
func (w Window) Contains(t time.Time) bool {
	_, _, ok := w.opening(t)
	return ok
}

// Fits reports whether start and start + d both fall inside the same
// opening of the window.
func (w Window) Fits(start time.Time, d time.Duration) bool {
	_, end, ok := w.opening(start)
	return ok && !start.Add(d).After(end)
}

// StopAllowed reports whether a stop of kind may happen at now in a set of
// the given mode (G09, G12): the lab's and the owner's real cuts always; an
// automated stop on the owner's device, kill9 included, only inside the
// owner's window.
func StopAllowed(mode string, w *Window, kind controller.ShutdownKind, now time.Time) bool {
	if mode != ModeAutomated || !kind.Automated() {
		return true
	}
	return w != nil && w.Contains(now)
}
