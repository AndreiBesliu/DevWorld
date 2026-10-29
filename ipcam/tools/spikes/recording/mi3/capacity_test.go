package mi3

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// call is one step that runSteps asked for.
type call struct{ cameras, minutes, attempt int }

// fakeMeasure measures a step as figures says, and records the calls.
type fakeMeasure struct {
	calls   []call
	figures func(c call) Figures
}

func (m *fakeMeasure) measure(_ context.Context, cameras, minutes, attempt int) (Figures, error) {
	c := call{cameras, minutes, attempt}
	m.calls = append(m.calls, c)
	return m.figures(c), nil
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
			d, err := runSteps(t.Context(), "H3", 8, false, m.measure)
			if err != nil {
				t.Fatal(err)
			}
			if got := stepsText(d.Steps); got != c.steps {
				t.Errorf("steps %s\nwant  %s", got, c.steps)
			}
			if d.Cameras != 8 || d.CPUPctTotal != 70 || d.Tier != "H3" {
				t.Errorf("the envelope's figures: %d cameras on %s, %v %% CPU; want the first hour's (8 on H3, 70 %%)", d.Cameras, d.Tier, d.CPUPctTotal)
			}
			envs := owners()
			envs[0] = results.Envelope{Schema: results.Schema, Code: "MI3", RunID: "pc-mi3-1", Set: SetFor("pc"), Host: "pc", OS: "windows", Storage: "ssd", MI3: &d}
			j := Judge(envs)
			if j.Verdict != results.Fail || j.PlanB != c.want {
				t.Errorf("Judge = %q → %q, want pică → %q", j.Verdict, j.PlanB, c.want)
			}
		})
	}

	t.Run("a pass needs no step", func(t *testing.T) {
		m := &fakeMeasure{figures: func(call) Figures { return passIf(true) }}
		d, err := runSteps(t.Context(), "H1b", 4, false, m.measure)
		if err != nil || stepsText(d.Steps) != "4x60#1 pass" {
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
			d, err := runSteps(t.Context(), "H3", 8, false, m.measure)
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
		d, err := runSteps(t.Context(), "H3", 8, true, m.measure)
		if err != nil {
			t.Fatal(err)
		}
		if got := stepsText(d.Steps); got != "8x60#1 invalid, 8x60#2 invalid, 8x60#3 invalid" {
			t.Errorf("steps %s, want three attempts and no more", got)
		}
		if d.Sixteen != nil || d.PacingP99Ms != 110 {
			t.Errorf("sixteen %+v, p99 %d: want no 16-camera hour and the last attempt's pacing", d.Sixteen, d.PacingP99Ms)
		}
		envs := owners()
		envs[0] = results.Envelope{Schema: results.Schema, Code: "MI3", RunID: "pc-mi3-1", Set: SetFor("pc"), Host: "pc", OS: "windows", Storage: "ssd", MI3: &d}
		if j := Judge(envs); j.Verdict != results.Incomplete {
			t.Errorf("Judge = %q, want %q", j.Verdict, results.Incomplete)
		}
	})
}

// TestMI3_SixteenOnlyWithSixCores (R07): with 6 cores or more, a PC whose
// recommended count passed also runs one hour at 16 cameras, as
// information; without them it never does.
func TestMI3_SixteenOnlyWithSixCores(t *testing.T) {
	m := &fakeMeasure{figures: func(c call) Figures { return passIf(c.cameras <= 8) }}
	d, err := runSteps(t.Context(), "H3", 8, true, m.measure)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(m.calls, call{16, 60, 1}) || d.Sixteen == nil || d.Sixteen.Cameras != 16 || d.Sixteen.Minutes != 60 || d.Sixteen.Pass || !d.Sixteen.Valid {
		t.Errorf("with 6 cores: calls %v, sixteen %+v; want one failing hour at 16", m.calls, d.Sixteen)
	}
	if got := stepsText(d.Steps); got != "8x60#1 pass" {
		t.Errorf("steps %s: the 16-camera hour is no step", got)
	}

	m = &fakeMeasure{figures: func(call) Figures { return passIf(true) }}
	d, err = runSteps(t.Context(), "H3", 8, false, m.measure)
	if err != nil || d.Sixteen != nil || slices.ContainsFunc(m.calls, func(c call) bool { return c.cameras == 16 }) {
		t.Errorf("without 6 cores: calls %v, sixteen %+v, %v; want no hour at 16", m.calls, d.Sixteen, err)
	}

	m = &fakeMeasure{figures: func(c call) Figures { return passIf(c.cameras <= 4) }}
	d, err = runSteps(t.Context(), "H3", 8, true, m.measure)
	if err != nil || d.Sixteen != nil || slices.ContainsFunc(m.calls, func(c call) bool { return c.cameras == 16 }) {
		t.Errorf("8 failed: calls %v, sixteen %+v, %v; want no hour at 16", m.calls, d.Sixteen, err)
	}
}

// TestMI3_MeasureErrorEndsTheSteps: a step that cannot be measured ends
// the run with its error, and no envelope is made of it.
func TestMI3_MeasureErrorEndsTheSteps(t *testing.T) {
	boom := errors.New("boom")
	_, err := runSteps(t.Context(), "H3", 8, false, func(context.Context, int, int, int) (Figures, error) { return Figures{}, boom })
	if !errors.Is(err, boom) {
		t.Errorf("runSteps = %v, want %v", err, boom)
	}
}
