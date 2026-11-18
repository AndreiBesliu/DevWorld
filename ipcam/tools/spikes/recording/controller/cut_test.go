package controller

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

var (
	automatedKinds = []ShutdownKind{KindKill9, KindSysrq, KindBugcheck, KindQEMUReset}
	realKinds      = []ShutdownKind{KindPower, KindForced}
)

// TestCut_OutsideWindow_MarkedInvalid (Review Focus 1): a real cut counts
// only when its boot-to-stop time lies in [3 min, 12 min] and the stop
// falls within [prompt, prompt + 2 min].
func TestCut_OutsideWindow_MarkedInvalid(t *testing.T) {
	boot := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cut := func(kind ShutdownKind, promptAfterBoot, stopAfterPrompt time.Duration) Cut {
		prompt := boot.Add(promptAfterBoot)
		return Cut{RunID: "real-pc-3", Kind: kind, BootAt: boot, PromptAt: prompt, StopAt: prompt.Add(stopAfterPrompt)}
	}
	for _, kind := range realKinds {
		for _, tc := range []struct {
			name                             string
			promptAfterBoot, stopAfterPrompt time.Duration
			valid                            bool
		}{
			{"stop 2 min after boot", 90 * time.Second, 30 * time.Second, false},
			{"stop 13 min after boot", 12*time.Minute + 30*time.Second, 30 * time.Second, false},
			{"stop 2 min 1 s after the prompt", 5 * time.Minute, 2*time.Minute + time.Second, false},
			{"stop 40 ms before the prompt", 7 * time.Minute, -40 * time.Millisecond, false},
			{"stop 1 min before the prompt", 7 * time.Minute, -time.Minute, false},
			{"stop 7 min after boot, 30 s after the prompt", 6*time.Minute + 30*time.Second, 30 * time.Second, true},
			{"stop at the prompt, 3 min after boot", 3 * time.Minute, 0, true},
			{"stop 2 min after the prompt, 12 min after boot", 10 * time.Minute, 2 * time.Minute, true},
		} {
			c := cut(kind, tc.promptAfterBoot, tc.stopAfterPrompt)
			if got := Valid(c); got != tc.valid {
				t.Errorf("%s, %s: Valid = %v, want %v", kind, tc.name, got, tc.valid)
			}
		}
	}

	t.Run("missing timestamps or an unknown kind", func(t *testing.T) {
		good := cut(KindPower, 6*time.Minute+30*time.Second, 30*time.Second)
		for name, c := range map[string]Cut{
			"no boot":      {RunID: good.RunID, Kind: good.Kind, PromptAt: good.PromptAt, StopAt: good.StopAt},
			"no prompt":    {RunID: good.RunID, Kind: good.Kind, BootAt: good.BootAt, StopAt: good.StopAt},
			"no stop":      {RunID: good.RunID, Kind: good.Kind, BootAt: good.BootAt, PromptAt: good.PromptAt},
			"unknown kind": {RunID: good.RunID, Kind: "unplug", BootAt: good.BootAt, PromptAt: good.PromptAt, StopAt: good.StopAt},
		} {
			if Valid(c) {
				t.Errorf("%s: Valid = true", name)
			}
		}
	})
}

// TestCut_AutomatedStop_LastBeaconJustBeforePrompt: recspike stops the host
// a few ms after the cut, and its last beacon left up to one interval before
// the stop, so on a LAN the last beacon of an automated stop arrives just
// before PromptAt. The run stays valid; a stop well before the prompt, or
// late, does not.
func TestCut_AutomatedStop_LastBeaconJustBeforePrompt(t *testing.T) {
	boot := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, window := range []struct {
		name             string
		minBoot, maxBoot time.Duration
		validate         func(Cut) bool
	}{
		{"owner window", 3 * time.Minute, 12 * time.Minute, Valid},
		{"smoke window", 10 * time.Second, 60 * time.Second, func(c Cut) bool { return ValidWithin(c, 10*time.Second, 60*time.Second) }},
	} {
		inWindow := (window.minBoot + window.maxBoot) / 2
		for _, kind := range automatedKinds {
			for _, tc := range []struct {
				name                             string
				promptAfterBoot, stopAfterPrompt time.Duration
				valid                            bool
			}{
				{"last beacon 40 ms before the prompt", inWindow, -40 * time.Millisecond, true},
				{"last beacon 150 ms before the prompt", inWindow, -AutoStopSlack, true},
				{"last beacon 2 s before the prompt", inWindow, -2 * time.Second, false},
				{"last beacon 2 min 1 s after the prompt", inWindow, 2*time.Minute + time.Second, false},
				{"prompt 1 s before the boot window", window.minBoot - time.Second, 10 * time.Millisecond, false},
				{"prompt 1 s after the boot window", window.maxBoot + time.Second, 10 * time.Millisecond, false},
				{"prompt at the window's start", window.minBoot, 5 * time.Millisecond, true},
				{"prompt at the window's end, stop 20 ms later", window.maxBoot, 20 * time.Millisecond, true},
			} {
				prompt := boot.Add(tc.promptAfterBoot)
				c := Cut{RunID: "lab-linux-kill9-4", Kind: kind, BootAt: boot, PromptAt: prompt, StopAt: prompt.Add(tc.stopAfterPrompt)}
				if got := window.validate(c); got != tc.valid {
					t.Errorf("%s, %s, %s: valid = %v, want %v", window.name, kind, tc.name, got, tc.valid)
				}
			}
		}
	}

	t.Run("2 min 59 s from boot to prompt", func(t *testing.T) {
		for _, kind := range automatedKinds {
			prompt := boot.Add(2*time.Minute + 59*time.Second)
			c := Cut{RunID: "pc-kill9-1", Kind: kind, BootAt: boot, PromptAt: prompt, StopAt: prompt.Add(-40 * time.Millisecond)}
			if Valid(c) {
				t.Errorf("%s: Valid = true", kind)
			}
		}
	})

	if AutoStopSlack != 150*time.Millisecond {
		t.Errorf("AutoStopSlack = %v, want 150 ms: one beacon interval plus StopMoment's 100 ms", AutoStopSlack)
	}
}

// TestLoss_UsesControllerSendLog_NotHostClock (Review Focus 2): the loss is
// StopAt − SendTime(lastRecordedIndex), both on the controller's clock. The
// host's timestamps in its report, here 5 s apart as after an NTP step or an
// unsynced Pi, do not change it.
func TestLoss_UsesControllerSendLog_NotHostClock(t *testing.T) {
	sendLog := NewSendLog()
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for i := range uint64(2000) {
		sendLog.Record("cam01_main", i, start.Add(time.Duration(i)*50*time.Millisecond)) //nolint:gosec // i < 2000
	}
	stopAt := start.Add(1500*50*time.Millisecond + 700*time.Millisecond)
	report := func(atMs int64) link.Report {
		event := func(typ string) json.RawMessage {
			raw, err := json.Marshal(map[string]any{"type": typ, "atMs": atMs, "seq": 3, "final": true})
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
		return link.Report{
			RunID:     "pi-sysrq-7",
			Streams:   []link.StreamIndex{{Stream: "cam01_main", LastRecordedIndex: 1480}},
			Reconcile: event("reconcile"),
			Verify:    event("verify"),
		}
	}
	host := time.Date(2026, 10, 21, 12, 1, 15, 0, time.UTC).UnixMilli()
	var losses []time.Duration
	for _, r := range []link.Report{report(host), report(host + 5000)} {
		for _, s := range r.Streams {
			loss, ok := Loss(stopAt, s.Stream, s.LastRecordedIndex, sendLog)
			if !ok {
				t.Fatalf("Loss(%s, %d): not ok", s.Stream, s.LastRecordedIndex)
			}
			losses = append(losses, loss)
		}
	}
	sent, _ := sendLog.SendTime("cam01_main", 1480)
	want := stopAt.Sub(sent)
	if want != 1700*time.Millisecond {
		t.Fatalf("test setup: StopAt − SendTime = %v, want 1.7 s", want)
	}
	for i, loss := range losses {
		if loss != want {
			t.Errorf("report %d: Loss = %v, want %v", i, loss, want)
		}
	}

	if _, ok := Loss(stopAt, "cam01_main", 5000, sendLog); ok {
		t.Error("Loss of an index that was never sent is ok")
	}
	if _, ok := Loss(stopAt, "cam02_main", 1480, sendLog); ok {
		t.Error("Loss of a stream that was never sent is ok")
	}
	if _, ok := Loss(time.Time{}, "cam01_main", 1480, sendLog); ok {
		t.Error("Loss without a stop moment is ok")
	}
}
