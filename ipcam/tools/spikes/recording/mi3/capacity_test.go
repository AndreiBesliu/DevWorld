package mi3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// call is one step that runSteps asked for.
type call struct{ cameras, minutes, attempt int }

// fakeMeasure measures a step as figures says, or fails it with fail's
// error when fail is set and gives one, and records the calls.
type fakeMeasure struct {
	calls   []call
	figures func(c call) Figures
	fail    func(c call) error
}

func (m *fakeMeasure) measure(_ context.Context, cameras, minutes, attempt int) (Figures, error) {
	c := call{cameras, minutes, attempt}
	m.calls = append(m.calls, c)
	if m.fail != nil {
		if err := m.fail(c); err != nil {
			return Figures{}, err
		}
	}
	return m.figures(c), nil
}

// sixteenCalled reports whether runSteps asked for an hour at 16 cameras.
func (m *fakeMeasure) sixteenCalled() bool {
	return slices.ContainsFunc(m.calls, func(c call) bool { return c.cameras == 16 })
}

// judgePC judges d as the PC's MI3, with the Pi passing and the PC's cold
// boot at 15 s.
func judgePC(d *results.Mi3Data) results.Judgement {
	envs := owners()
	envs[0] = results.Envelope{Schema: results.Schema, Code: "MI3", RunID: "pc-mi3-1", Set: SetFor("pc"), Host: "pc", OS: "windows", Storage: "ssd", MI3: d}
	return Judge(envs)
}

// passIf is a step's figures: the budgets met when pass holds, else 70 %
// total CPU.
func passIf(pass bool) Figures {
	f := meeting()
	if !pass {
		f.CPUPctTotal = 70
	}
	return f
}

func stepsText(steps []results.Mi3Step) string {
	var parts []string
	for _, s := range steps {
		verdict := "fail"
		switch {
		case !s.Valid:
			verdict = "invalid"
		case s.Pass:
			verdict = "pass"
		}
		parts = append(parts, fmt.Sprintf("%dx%d#%d %s", s.Cameras, s.Minutes, s.Attempt, verdict))
	}
	return strings.Join(parts, ", ")
}

// TestMI3_OnFail_StepsDownByHalving (G20, O16): a failed hour at the
// recommended count halves it in 15-min runs until a count passes, which a
// full hour confirms; a failed confirmation halves again. The minimum is 1:
// when 1 fails too, the number stays 1. Every step is recorded, and the
// envelope's figures are the first hour's.
func TestMI3_OnFail_StepsDownByHalving(t *testing.T) {
	cases := []struct {
		name  string
		pass  func(c call) bool
		steps string
		want  string
	}{
		{"8 fails, 2 passes", func(c call) bool { return c.cameras <= 2 },
			"8x60#1 fail, 4x15#1 fail, 2x15#1 pass, 2x60#1 pass", "pc: numărul recomandat devine 2"},
		{"the confirmation of 4 fails", func(c call) bool { return c.cameras <= 2 || (c.cameras <= 4 && c.minutes == 15) },
			"8x60#1 fail, 4x15#1 pass, 4x60#1 fail, 2x15#1 pass, 2x60#1 pass", "pc: numărul recomandat devine 2"},
		{"1 fails too", func(call) bool { return false },
			"8x60#1 fail, 4x15#1 fail, 2x15#1 fail, 1x15#1 fail", "pc: numărul recomandat devine 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeMeasure{figures: func(cl call) Figures { return passIf(c.pass(cl)) }}
			d, sixteenErr, err := runSteps(t.Context(), "H3", 8, false, m.measure)
			if err != nil || sixteenErr != nil {
				t.Fatal(err, sixteenErr)
			}
			if got := stepsText(d.Steps); got != c.steps {
				t.Errorf("steps %s\nwant  %s", got, c.steps)
			}
			if d.Cameras != 8 || d.CPUPctTotal != 70 || d.Tier != "H3" {
				t.Errorf("the envelope's figures: %d cameras on %s, %v %% CPU; want the first hour's (8 on H3, 70 %%)", d.Cameras, d.Tier, d.CPUPctTotal)
			}
			j := judgePC(d)
			if j.Verdict != results.Fail || j.PlanB != c.want {
				t.Errorf("Judge = %q → %q, want pică → %q", j.Verdict, j.PlanB, c.want)
			}
		})
	}

	t.Run("a pass needs no step", func(t *testing.T) {
		m := &fakeMeasure{figures: func(call) Figures { return passIf(true) }}
		d, _, err := runSteps(t.Context(), "H1b", 4, false, m.measure)
		if err != nil || d == nil || stepsText(d.Steps) != "4x60#1 pass" {
			t.Errorf("steps %s, %v; want the hour at 4 only", stepsText(d.Steps), err)
		}
	})
}

// TestMI3_InvalidPacing_RunRedone (R07): a step whose controller sent
// unevenly (p99 above 100 ms, or one skipped access unit) is redone under
// the next attempt and never judged, whatever it measured; after three
// invalid attempts the steps stop, and the host's MI3 has no verdict.
func TestMI3_InvalidPacing_RunRedone(t *testing.T) {
	for _, bad := range []struct {
		name    string
		p99     int64
		skipped int
	}{{"p99 110 ms", 110, 0}, {"one skipped AU", 8, 1}} {
		t.Run(bad.name, func(t *testing.T) {
			m := &fakeMeasure{figures: func(c call) Figures {
				if c.attempt == 1 {
					f := passIf(false)
					f.PacingP99Ms, f.SkippedAUs = bad.p99, bad.skipped
					return f
				}
				return passIf(true)
			}}
			d, _, err := runSteps(t.Context(), "H3", 8, false, m.measure)
			if err != nil {
				t.Fatal(err)
			}
			if got := stepsText(d.Steps); got != "8x60#1 invalid, 8x60#2 pass" {
				t.Errorf("steps %s, want the invalid attempt redone", got)
			}
			if d.CPUPctTotal != 55 || d.PacingP99Ms != 8 || d.SkippedAUs != 0 {
				t.Errorf("the envelope has the invalid attempt's figures: %v %% CPU, p99 %d, %d skipped", d.CPUPctTotal, d.PacingP99Ms, d.SkippedAUs)
			}
			if s := d.Steps[0]; s.PacingP99Ms != bad.p99 || s.SkippedAUs != bad.skipped || s.Pass {
				t.Errorf("the invalid attempt %+v does not show its pacing, or is judged", s)
			}
		})
	}

	t.Run("three invalid attempts", func(t *testing.T) {
		m := &fakeMeasure{figures: func(call) Figures {
			f := passIf(true)
			f.PacingP99Ms = 110
			return f
		}}
		d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
		if err != nil || sixteenErr != nil {
			t.Fatal(err, sixteenErr)
		}
		if got := stepsText(d.Steps); got != "8x60#1 invalid, 8x60#2 invalid, 8x60#3 invalid" {
			t.Errorf("steps %s, want three attempts and no more", got)
		}
		if d.Sixteen != nil || m.sixteenCalled() || d.PacingP99Ms != 110 {
			t.Errorf("sixteen %+v, calls %v, p99 %d: want no 16-camera hour and the last attempt's pacing", d.Sixteen, m.calls, d.PacingP99Ms)
		}
		if j := judgePC(d); j.Verdict != results.Incomplete {
			t.Errorf("Judge = %q, want %q", j.Verdict, results.Incomplete)
		}
	})
}

// TestMI3_SixteenOnlyWithSixCores (R07): with 6 cores or more, a PC runs
// one hour at 16 cameras after its steps, as information, whether its
// recommended count passed or the steps lowered it; without them it never
// does. The hour is no step. Steps that stopped on the controller's pacing
// are run again whole, so no hour at 16 follows them
// (TestMI3_InvalidPacing_RunRedone).
func TestMI3_SixteenOnlyWithSixCores(t *testing.T) {
	m := &fakeMeasure{figures: func(c call) Figures { return passIf(c.cameras <= 8) }}
	d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
	if err != nil || sixteenErr != nil {
		t.Fatal(err, sixteenErr)
	}
	if !slices.Contains(m.calls, call{16, 60, 1}) || d.Sixteen == nil || d.Sixteen.Cameras != 16 || d.Sixteen.Minutes != 60 || d.Sixteen.Pass || !d.Sixteen.Valid {
		t.Errorf("with 6 cores: calls %v, sixteen %+v; want one failing hour at 16", m.calls, d.Sixteen)
	}
	if got := stepsText(d.Steps); got != "8x60#1 pass" {
		t.Errorf("steps %s: the 16-camera hour is no step", got)
	}
	if d.SixteenStopped != "" || d.StepsStopped != "" {
		t.Errorf("stopped %q, %q; want none", d.SixteenStopped, d.StepsStopped)
	}

	t.Run("after a failed hour", func(t *testing.T) {
		m := &fakeMeasure{figures: func(c call) Figures { return passIf(c.cameras <= 4) }}
		d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
		if err != nil || sixteenErr != nil {
			t.Fatal(err, sixteenErr)
		}
		if got := stepsText(d.Steps); got != "8x60#1 fail, 4x15#1 pass, 4x60#1 pass" {
			t.Errorf("steps %s, want the steps down to 4 first", got)
		}
		if m.calls[len(m.calls)-1] != (call{16, 60, 1}) || d.Sixteen == nil || d.Sixteen.Cameras != 16 || d.Sixteen.Pass || !d.Sixteen.Valid {
			t.Errorf("calls %v, sixteen %+v; want one failing hour at 16 after the steps", m.calls, d.Sixteen)
		}
		if j := judgePC(d); j.Verdict != results.Fail || j.PlanB != "pc: numărul recomandat devine 4" {
			t.Errorf("Judge = %q → %q; the 16-camera hour changed the verdict", j.Verdict, j.PlanB)
		}
	})

	t.Run("without 6 cores", func(t *testing.T) {
		m := &fakeMeasure{figures: func(call) Figures { return passIf(true) }}
		d, sixteenErr, err := runSteps(t.Context(), "H3", 8, false, m.measure)
		if err != nil || sixteenErr != nil || d.Sixteen != nil || m.sixteenCalled() {
			t.Errorf("calls %v, sixteen %+v, %v, %v; want no hour at 16", m.calls, d.Sixteen, err, sixteenErr)
		}
	})
}

// TestMI3_MeasureErrorEndsTheSteps: a first hour that cannot be measured
// before one of its attempts was valid ends the run with its error, and no
// envelope is made of it: there is no measured hour to judge.
func TestMI3_MeasureErrorEndsTheSteps(t *testing.T) {
	boom := errors.New("boom")
	for _, at := range []int{1, 2} {
		t.Run(fmt.Sprintf("attempt %d", at), func(t *testing.T) {
			m := &fakeMeasure{
				figures: func(call) Figures { f := passIf(true); f.PacingP99Ms = 110; return f },
				fail: func(c call) error {
					if c.attempt == at {
						return boom
					}
					return nil
				},
			}
			d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
			if !errors.Is(err, boom) || d != nil || sixteenErr != nil {
				t.Errorf("runSteps = %+v, %v, %v; want no data and %v", d, sixteenErr, err, boom)
			}
		})
	}
}

// hostPath and hostAddr stand for what a step's error may name on the
// owner's screen and never in an envelope (PH0-22).
const (
	hostPath = "/home/owner/recspike/work/mi3/02-16cam-60min-1"
	hostAddr = "192.0.2.7:7081"
)

// TestMI3_SixteenErrorIsInformation (R07, G27): an hour at 16 cameras that
// cannot be measured leaves Sixteen empty and says why, while the first
// hour's result stands and G12 is judged on it; runSteps gives its error
// apart, for the owner's screen. The envelope names ipcam-hub's exit or the
// owner's stop, never an error's own text, which may hold a path or an
// address (PH0-22).
func TestMI3_SixteenErrorIsInformation(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"ipcam-hub exits", fmt.Errorf("pasul 2 (16 camere): %w; jurnalele sunt în %s", hubExited(2), hostPath),
			"16 camere 1 h: nu s-a putut măsura; ipcam-hub s-a oprit în timpul pasului (codul 2)"},
		{"the link fails", fmt.Errorf("pasul 2 (16 camere): dial tcp %s: i/o timeout; jurnalele sunt în %s", hostAddr, hostPath),
			"16 camere 1 h: nu s-a putut măsura; eroarea este în jurnalele pasului (work/mi3; după o nouă pornire, work/mi3-anterior)"},
		{"the owner stops it", fmt.Errorf("pasul 2 (16 camere): %w; jurnalele sunt în %s", context.Canceled, hostPath),
			"16 camere 1 h: nu s-a putut măsura; " + ownersStop},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeMeasure{figures: func(call) Figures { return passIf(true) }, fail: func(cl call) error {
				if cl.cameras == 16 {
					return c.err
				}
				return nil
			}}
			d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
			if err != nil || !errors.Is(sixteenErr, c.err) || d == nil {
				t.Fatalf("runSteps = %+v, %v, %v; want the first hour's data and the 16-camera hour's error apart", d, sixteenErr, err)
			}
			if d.Sixteen != nil || d.SixteenStopped != c.want {
				t.Errorf("sixteen %+v, stopped %q; want none, stopped %q", d.Sixteen, d.SixteenStopped, c.want)
			}
			if got := stepsText(d.Steps); got != "8x60#1 pass" || d.CPUPctTotal != 55 || d.StepsStopped != "" {
				t.Errorf("steps %s, CPU %v %%, stopped %q; want the first hour's", got, d.CPUPctTotal, d.StepsStopped)
			}
			j := judgePC(d)
			if j.Verdict != results.Pass {
				t.Errorf("Judge = %q, reasons %q; want trece on the first hour", j.Verdict, j.Reasons)
			}
			if r, want := row(t, j, "MI3 pc: 16 camere"), "estimare, nemăsurată ("+c.want+")"; r.Value != want || r.Pass != nil {
				t.Errorf("16-camera row %+v, want %q as information", r, want)
			}
		})
	}
}

// TestMI3_StepErrorKeepsTheMeasuredFailure (G20, G27): a step after a
// failed first hour that cannot be measured stops the steps with its error,
// but the steps measured so far are kept with why they stopped: the first
// hour's failure decides G12, and the number goes down, its value open. No
// hour at 16 follows.
func TestMI3_StepErrorKeepsTheMeasuredFailure(t *testing.T) {
	const elsewhere = "eroarea este în jurnalele pasului (work/mi3; după o nouă pornire, work/mi3-anterior)"
	linkErr := fmt.Errorf("dial tcp %s: i/o timeout; jurnalele sunt în %s", hostAddr, hostPath)
	cases := []struct {
		name    string
		at      call
		err     error
		steps   string
		stopped string
	}{
		{"the first step down", call{4, 15, 1}, linkErr, "8x60#1 fail", "4 camere 15 min: nu s-a putut măsura; " + elsewhere},
		{"the confirmation", call{4, 60, 1}, fmt.Errorf("pasul 3 (4 camere): %w", hubExited(-1)), "8x60#1 fail, 4x15#1 pass",
			"4 camere 1 h: nu s-a putut măsura; ipcam-hub s-a oprit în timpul pasului (codul -1)"},
		{"a redone attempt", call{4, 15, 2}, fmt.Errorf("pasul 3 (4 camere): %w", loadEnded(LoadExport)), "8x60#1 fail, 4x15#1 invalid",
			"4 camere 15 min, încercarea 2: nu s-a putut măsura; încărcarea export s-a oprit în timpul pasului"},
		{"the owner stops it", call{4, 15, 1}, fmt.Errorf("pasul 2 (4 camere): %w", context.Canceled), "8x60#1 fail",
			"4 camere 15 min: nu s-a putut măsura; " + ownersStop},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeMeasure{
				figures: func(cl call) Figures {
					f := passIf(cl.cameras <= 4)
					if c.at.attempt == 2 && cl == (call{c.at.cameras, c.at.minutes, 1}) {
						f.PacingP99Ms = 110
					}
					return f
				},
				fail: func(cl call) error {
					if cl == c.at {
						return c.err
					}
					return nil
				},
			}
			d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
			if !errors.Is(err, c.err) || sixteenErr != nil || d == nil {
				t.Fatalf("runSteps = %+v, %v, %v; want the steps so far and %v", d, sixteenErr, err, c.err)
			}
			if got := stepsText(d.Steps); got != c.steps || d.StepsStopped != c.stopped {
				t.Errorf("steps %s, stopped %q\nwant  %s, stopped %q", got, d.StepsStopped, c.steps, c.stopped)
			}
			if strings.Contains(d.StepsStopped, hostPath) || strings.Contains(d.StepsStopped, hostAddr) {
				t.Errorf("the envelope names the host: %q", d.StepsStopped)
			}
			if d.Cameras != 8 || d.CPUPctTotal != 70 || d.Budgets["cpuTotal"].Pass {
				t.Errorf("the envelope's figures: %d cameras, %v %% CPU; want the first hour's failure", d.Cameras, d.CPUPctTotal)
			}
			if d.Sixteen != nil || d.SixteenStopped != "" || m.sixteenCalled() {
				t.Errorf("calls %v, sixteen %+v %q; want no hour at 16 after an error", m.calls, d.Sixteen, d.SixteenStopped)
			}
			j := judgePC(d)
			if j.Verdict != results.Fail || j.PlanB != "pc: numărul recomandat coboară; pașii nu s-au încheiat" {
				t.Errorf("Judge = %q → %q; want the measured failure to decide, the number open", j.Verdict, j.PlanB)
			}
			if r := row(t, j, "MI3 pc: pașii"); !strings.HasSuffix(r.Value, "; "+c.stopped) {
				t.Errorf("steps row %q does not end with why they stopped", r.Value)
			}
			if !strings.Contains(strings.Join(j.Reasons, "\n"), c.stopped) {
				t.Errorf("reasons %q do not say why the steps stopped", j.Reasons)
			}
		})
	}
}

// TestMI3_RunWritesWhatWasMeasured (G27): Run writes the envelope of what
// the steps measured also when a later step stopped them, then returns that
// step's error; an hour at 16 cameras that could not be measured is said on
// the owner's screen, and the run succeeds.
func TestMI3_RunWritesWhatWasMeasured(t *testing.T) {
	boom := errors.New("boom")
	dir := t.TempDir()
	out := &strings.Builder{}
	o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Results: dir, Out: out}

	failed := dataOf("H3", 8, passIf(false))
	failed.Steps = []results.Mi3Step{step(8, 60, 1, false)}
	failed.StepsStopped = "4 camere 15 min: nu s-a putut măsura; " + ownersStop
	if err := o.finish(failed, nil, boom); !errors.Is(err, boom) {
		t.Errorf("finish = %v, want the step's error %v", err, boom)
	}
	e, err := results.Read(filepath.Join(dir, "MI3", "pc-mi3", "pc-mi3-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if d := e.MI3; stepsText(d.Steps) != "8x60#1 fail" || d.StepsStopped != failed.StepsStopped || d.CPUPctTotal != 70 || len(d.Proxies) == 0 {
		t.Errorf("the envelope %+v is not what the steps measured", d)
	}

	passed := dataOf("H3", 8, passIf(true))
	passed.Steps = []results.Mi3Step{step(8, 60, 1, true)}
	passed.SixteenStopped = "16 camere 1 h: nu s-a putut măsura; ipcam-hub s-a oprit în timpul pasului (codul 2)"
	out.Reset()
	if err := o.finish(passed, fmt.Errorf("pasul 2 (16 camere): %w", hubExited(2)), nil); err != nil {
		t.Errorf("finish = %v; an unmeasured 16-camera hour fails no run", err)
	}
	e, err = results.Read(filepath.Join(dir, "MI3", "pc-mi3", "pc-mi3-2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if e.MI3.SixteenStopped != passed.SixteenStopped || e.MI3.Sixteen != nil {
		t.Errorf("the envelope's 16-camera hour: %+v, %q", e.MI3.Sixteen, e.MI3.SixteenStopped)
	}
	if !strings.Contains(out.String(), "16 camere") || !strings.Contains(out.String(), "codul 2") {
		t.Errorf("the screen does not say why the 16-camera hour has no result:\n%s", out)
	}
}

// TestMI3_EnvelopeCarriesItsTime (Task 12 review, M1): the run's envelope
// is stamped with the time it was stored (AtMs), as MI4's is, so that the
// runs of merged results folders order by time in the report.
func TestMI3_EnvelopeCarriesItsTime(t *testing.T) {
	dir := t.TempDir()
	o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Results: dir, Out: &strings.Builder{}}
	d := dataOf("H3", 8, passIf(true))
	d.Steps = []results.Mi3Step{step(8, 60, 1, true)}
	before := time.Now().UnixMilli()
	if err := o.finish(d, nil, nil); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UnixMilli()
	e, err := results.Read(filepath.Join(dir, "MI3", "pc-mi3", "pc-mi3-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if e.AtMs < before || e.AtMs > after {
		t.Errorf("the envelope's time %d ms is not when finish stored it, within [%d, %d]", e.AtMs, before, after)
	}
}

// ownersStop is why a step has no result when the owner stopped the probe.
const ownersStop = "proprietarul a oprit proba"

// TestMI3_OwnersStopIsAnInterruption (R07; Task 9 review): Ctrl+C is the
// owner's stop, not a failure to report. During the hour at 16 cameras, or
// a step after the first hour, the run keeps what it measured, the envelope
// says in a fixed Romanian text that the owner stopped the probe, and
// recspike ends as its other Ctrl+C paths end, MI2's: a Romanian line, then
// an error that is context.Canceled, never a success. Nothing on the
// screen or in the envelope is an error's English text.
func TestMI3_OwnersStopIsAnInterruption(t *testing.T) {
	stop := fmt.Errorf("pasul 2: %w; jurnalele sunt în %s", context.Canceled, hostPath)
	for _, c := range []struct {
		name string
		at   func(call) bool
	}{
		{"the hour at 16 cameras", func(cl call) bool { return cl.cameras == 16 }},
		{"a step down", func(cl call) bool { return cl.cameras == 4 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeMeasure{figures: func(cl call) Figures { return passIf(cl.cameras != 8) }, fail: func(cl call) error {
				if c.at(cl) {
					return stop
				}
				return nil
			}}
			d, sixteenErr, err := runSteps(t.Context(), "H3", 8, true, m.measure)
			if d == nil {
				t.Fatalf("runSteps = nil, %v, %v; want the first hour kept", sixteenErr, err)
			}
			stopped := d.StepsStopped + d.SixteenStopped
			if !strings.HasSuffix(stopped, ": nu s-a putut măsura; "+ownersStop) {
				t.Errorf("the envelope says %q, want the owner's stop", stopped)
			}
			out := &strings.Builder{}
			dir := t.TempDir()
			o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Results: dir, Out: out}
			err = o.finish(*d, sixteenErr, err)
			if !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "proba MI3 s-a întrerupt") {
				t.Errorf("finish = %v, want the interruption, context.Canceled, as recspike's other Ctrl+C paths end", err)
			}
			e, rerr := results.Read(filepath.Join(dir, "MI3", "pc-mi3", "pc-mi3-1.json"))
			if rerr != nil {
				t.Fatalf("the measured first hour was not kept: %v", rerr)
			}
			if e.MI3.CPUPctTotal != 70 || e.MI3.StepsStopped+e.MI3.SixteenStopped != stopped {
				t.Errorf("the envelope %+v is not what the steps measured", e.MI3)
			}
			if !strings.Contains(out.String(), "Proba MI3 s-a întrerupt") || strings.Contains(out.String(), "context canceled") {
				t.Errorf("the screen does not say in Romanian that the probe was stopped:\n%s", out)
			}
		})
	}
}

// TestMI3_RunKeepsThePreviousRunsStepLogs (Task 9 review): the screen and
// the envelope of a run that stopped send the owner to the step logs, and
// the owner then starts the probe again. The new run keeps the step logs of
// the run before, one generation, in work/mi3-anterior, as MI2 keeps its
// last log, and nothing else of that run: no recording, key or fixture. A
// start that stops before any step keeps that generation as it is.
func TestMI3_RunKeepsThePreviousRunsStepLogs(t *testing.T) {
	work := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(work, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const stepDir = "mi3/02-4cam-15min-1/"
	write(stepDir+"ipcam-hub.log", "the step's error")
	write(stepDir+"loads.log", "the loads' log")
	write(stepDir+"ri_g.key", "an identity")
	write(stepDir+"run.json", "{}")
	write(stepDir+"data/rec/cam01/main/x.mp4", "a recording")
	write("mi3/fixture/segment.mp4", "the fixture")
	write("mi3-anterior/01-8cam-60min-1/ipcam-hub.log", "two runs ago")
	o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Controller: "nowhere", Work: work, Results: t.TempDir(), Out: io.Discard}
	for range 2 { // the second start finds no step of the first
		if err := Run(t.Context(), o); err == nil {
			t.Fatal("Run without a controller succeeded")
		}
	}

	var files []string
	if err := filepath.WalkDir(work, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(work, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"mi3-anterior/02-4cam-15min-1/ipcam-hub.log", "mi3-anterior/02-4cam-15min-1/loads.log"}
	if !slices.Equal(files, want) {
		t.Errorf("work holds %q, want only the step logs of the run before, in mi3-anterior: %q", files, want)
	}
	if b, err := os.ReadFile(filepath.Join(work, "mi3-anterior", "02-4cam-15min-1", "ipcam-hub.log")); err != nil || string(b) != "the step's error" { //nolint:gosec // the test's own file
		t.Errorf("the kept log: %q, %v", b, err)
	}
}

// TestMI3_OpeningSaysHowLongTheRunCanTake (G20, R07; Task 9 review): Run's
// first line says what can make the run longer than its hour and 15-min
// steps: on a PC with 6 cores or more, the extra hour at 16 cameras, and,
// on every host, up to 3 attempts of a step that the controller paced
// unevenly.
func TestMI3_OpeningSaysHowLongTheRunCanTake(t *testing.T) {
	pc := openingText("H3", 8, true)
	for _, want := range []string{"încă o oră la 16 camere", "6 nuclee", "3 încercări"} {
		if !strings.Contains(pc, want) {
			t.Errorf("the PC's opening %q lacks %q", pc, want)
		}
	}
	pi := openingText("H1b", 4, false)
	if strings.Contains(pi, "16 camere") || !strings.Contains(pi, "3 încercări") {
		t.Errorf("the Pi's opening %q: want the attempts and no hour at 16 cameras", pi)
	}
}
