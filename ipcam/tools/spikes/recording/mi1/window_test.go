package mi1

import (
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// at is a local time on the test's day.
func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-25 "+hhmm, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

// TestWindow_RefusesAutomatedStopOutsideWindow (G09, G14): the owner's
// window, local time, may run past midnight. An automated stop, kill9
// included, is allowed only inside it, and a run starts only when its
// latest stop still falls inside (MaxRunSpan); a malformed window is
// refused in Romanian.
func TestWindow_RefusesAutomatedStopOutsideWindow(t *testing.T) {
	w, err := ParseWindow("22:00-06:30")
	if err != nil {
		t.Fatal(err)
	}
	for hhmm, want := range map[string]bool{
		"21:59": false, "22:00": true, "23:59": true, "00:00": true, "06:29": true, "06:30": false, "12:00": false,
	} {
		for _, kind := range []controller.ShutdownKind{controller.KindKill9, controller.KindSysrq, controller.KindBugcheck} {
			if got := StopAllowed(ModeAutomated, &w, kind, at(hhmm)); got != want {
				t.Errorf("StopAllowed(%s at %s) = %v, want %v", kind, hhmm, got, want)
			}
		}
	}
	if StopAllowed(ModeAutomated, nil, controller.KindKill9, at("23:00")) {
		t.Error("an automated owner stop without a window is allowed")
	}
	for hhmm, want := range map[string]bool{"21:50": false, "22:00": true, "06:17": true, "06:18": false, "23:30": true} {
		if got := w.Fits(at(hhmm), MaxRunSpan); got != want {
			t.Errorf("Fits(%s, %v) = %v, want %v", hhmm, MaxRunSpan, got, want)
		}
	}
	day, err := ParseWindow("09:15-17:00")
	if err != nil {
		t.Fatal(err)
	}
	if !day.Contains(at("09:15")) || day.Contains(at("17:00")) || day.Contains(at("22:00")) {
		t.Error("a daytime window holds the wrong times")
	}
	for _, bad := range []string{"", "22:00", "22-06", "25:00-06:00", "22:00-06:60", "22:00-22:00", "22:00 - 06:00"} {
		if _, err := ParseWindow(bad); err == nil || !strings.Contains(err.Error(), "fereastr") {
			t.Errorf("ParseWindow(%q) = %v, want a Romanian error about the window", bad, err)
		}
	}
}

// TestWindow_RealCutsIgnoreWindow (G12): the owner's real cuts are not tied
// to the window, and neither are the lab's stops.
func TestWindow_RealCutsIgnoreWindow(t *testing.T) {
	w, err := ParseWindow("22:00-06:00")
	if err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{at("12:00"), at("23:00")} {
		for _, kind := range []controller.ShutdownKind{controller.KindPower, controller.KindForced} {
			if !StopAllowed(ModeReal, &w, kind, now) || !StopAllowed(ModeReal, nil, kind, now) {
				t.Errorf("a real %s cut at %s is refused", kind, now.Format("15:04"))
			}
		}
		if !StopAllowed(ModeLab, nil, controller.KindKill9, now) {
			t.Errorf("a lab kill at %s is refused", now.Format("15:04"))
		}
	}
}
