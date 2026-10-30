package mi3

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// loadStep is a step of 2 cameras on H1b whose window is minutes seconds,
// an hour scaled down: its recordings hold each camera's parts of 1 s, and
// its log is empty.
func loadStep(t *testing.T, parts map[string]int) *stepRun {
	t.Helper()
	dir := t.TempDir()
	for cam, n := range parts {
		writeTestFile(t, filepath.Join(dir, "data", "rec", cam, "main", "2026-10-30", "120000_000000.mp4"), segment(t, n, 20))
	}
	writeTestFile(t, filepath.Join(dir, "ipcam-hub.log"), nil)
	r := &stepRunner{o: Options{Window: func(minutes int) time.Duration { return time.Duration(minutes) * time.Second }}, tier: TierH1b, dir: dir}
	return &stepRun{r: r, dir: dir, cameras: 2, minutes: FullMinutes}
}

// fullLive is what the controller says of the step's live tiles when they
// received frames over the whole window of 60 s (Coverage): its least
// covered tile covered all of it, and its frames gave latency samples.
var fullLive = link.Mi3Result{LiveLeastS: 60.1, LiveLeastTile: "cam01_sub", LiveLeastSession: 1, LatencySamples: 44 * 60, PacingP99Ms: 8}

// TestMI3_StepCountsOnlyAFullLoad (mis-2): a step counts only when its
// load ran for its whole window: every live tile of every session received
// frames, without a pause longer than 2 s, for at least 95 % of the window
// (the controller's Coverage), each recorded camera's parts cover at least
// 95 % of it, and MediaMTX did not restart. A tile that stops at minute 10,
// a camera that stops recording, a camera that recorded nothing, or a
// restart of MediaMTX makes the step invalid with a Romanian reason, so
// that it is redone and never judged. The live frames' count does not
// decide (Final fix C): the frames that a healthy load's tiles lose, at
// the start or on the LAN, leave it valid.
func TestMI3_StepCountsOnlyAFullLoad(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	upload := LoadReport{Seconds: 60, Bytes: 1}
	stopped := fullLive
	stopped.LiveLeastS, stopped.LiveLeastTile, stopped.LiveLeastSession = 10, "cam02_sub", 2
	lossy := fullLive
	lossy.LiveLeastS, lossy.LatencySamples = 57.5, 44*60*80/100
	for _, tc := range []struct {
		name     string
		parts    map[string]int
		live     link.Mi3Result
		restarts int32
		why      string
	}{
		{"the whole load", map[string]int{"cam01": 60, "cam02": 58}, fullLive, 0, ""},
		{"a whole load that lost frames", map[string]int{"cam01": 60, "cam02": 60}, lossy, 0, ""},
		{"a tile that stops at minute 10", map[string]int{"cam01": 60, "cam02": 60}, stopped, 0,
			"placa live cam02_sub din sesiunea live 2 a primit cadre timp de 10 s din cele 60 s ale ferestrei pasului (sub 95 %)"},
		{"a camera that stops recording", map[string]int{"cam01": 60, "cam02": 30}, fullLive, 0,
			"camera cam02_main a înregistrat 30 s din cele 60 s ale ferestrei pasului"},
		{"a camera that recorded nothing", map[string]int{"cam01": 60}, fullLive, 0,
			"camera cam02_main a înregistrat 0 s din cele 60 s ale ferestrei pasului"},
		{"a restart of MediaMTX", map[string]int{"cam01": 60, "cam02": 60}, fullLive, 1,
			"MediaMTX a repornit în timpul pasului"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := loadStep(t, tc.parts)
			s.childRestarts.Store(tc.restarts)
			f, err := s.figures(usage{}, usage{}, 0, tc.live, upload, id)
			if err != nil {
				t.Fatal(err)
			}
			if tc.why == "" {
				if !Valid(f) || f.Invalid != "" {
					t.Errorf("a full load: valid %v, invalid %q; want it to count", Valid(f), f.Invalid)
				}
				return
			}
			if Valid(f) || !strings.Contains(f.Invalid, tc.why) {
				t.Errorf("valid %v, invalid %q; want a step that does not count, because %q", Valid(f), f.Invalid, tc.why)
			}
		})
	}
}

// TestMI3_ShortLoadIsRedone (mis-2): a step whose load did not run for
// its whole window is redone, as a step paced unevenly is (R07), at most
// 3 attempts, and never judged: its step and the judgement say why, and
// after 3 such attempts the host's MI3 has no verdict, with the reason.
func TestMI3_ShortLoadIsRedone(t *testing.T) {
	const why = "camera cam02_main a înregistrat 30 s din cele 3600 s ale ferestrei pasului (sub 95 %)"
	short := func() Figures { f := passIf(true); f.Invalid = why; return f }
	m := &fakeMeasure{figures: func(c call) Figures {
		if c.attempt == 1 {
			return short()
		}
		return passIf(true)
	}}
	d, _, err := runSteps(t.Context(), "H3", 8, false, m.measure)
	if err != nil {
		t.Fatal(err)
	}
	if got := stepsText(d.Steps); got != "8x60#1 invalid, 8x60#2 pass" {
		t.Errorf("steps %s, want the short attempt redone", got)
	}
	if s := d.Steps[0]; s.Pass || s.InvalidReason != why || stepText(s) != "8 camere 1 h: nevalidă ("+why+")" {
		t.Errorf("the short attempt %+v reads %q; want it invalid with its reason", s, stepText(s))
	}
	if j := judgePC(d); j.Verdict != results.Pass {
		t.Errorf("Judge = %q, reasons %q; want trece from the second attempt", j.Verdict, j.Reasons)
	}

	m = &fakeMeasure{figures: func(call) Figures { return short() }}
	d, _, err = runSteps(t.Context(), "H3", 8, true, m.measure)
	if err != nil {
		t.Fatal(err)
	}
	if got := stepsText(d.Steps); got != "8x60#1 invalid, 8x60#2 invalid, 8x60#3 invalid" || m.sixteenCalled() {
		t.Errorf("steps %s, calls %v; want three attempts and no more", got, m.calls)
	}
	j := judgePC(d)
	if j.Verdict != results.Incomplete || !slices.ContainsFunc(j.Reasons, func(r string) bool { return strings.Contains(r, why) }) {
		t.Errorf("Judge = %q, reasons %q; want fără verdict, with the reason", j.Verdict, j.Reasons)
	}
	out := &strings.Builder{}
	o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Results: t.TempDir(), Work: t.TempDir(), Out: out}
	if err := o.finish(*d, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), why) || strings.Contains(out.String(), "neuniform de 3 ori") {
		t.Errorf("the screen says:\n%s\nwant the load's reason, not the controller's pacing", out)
	}
}

// TestMI3_FinishedStepKeepsOnlyItsLogs (mis-5): when a step ends, its
// folder keeps only the step logs that keepStepLogs moves: its
// recordings, its RI_g identity and its run.json, which holds the
// controller's RTSP login, go with it.
func TestMI3_FinishedStepKeepsOnlyItsLogs(t *testing.T) {
	work := t.TempDir()
	o := Options{Host: mi1.HostState{Pseudonym: "pc"}, Work: work, Out: &strings.Builder{}, FreeBytes: func() (uint64, error) { return 1e12, nil }}
	r := &stepRunner{o: o, l: deadLink(t), dir: filepath.Join(work, "mi3"), tier: TierH3}
	if _, err := r.measure(t.Context(), 2, FullMinutes, 1); err == nil {
		t.Fatal("a step on a dead link succeeded")
	}
	entries, err := os.ReadDir(filepath.Join(work, "mi3", "01-2cam-60min-1"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"ipcam-hub.log", "loads.log", "recspike.log"}; !slices.Equal(names, want) {
		t.Errorf("the finished step's folder holds %q, want only %q", names, want)
	}
}
