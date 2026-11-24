package controller

import (
	"math/rand/v2"
	"testing"
	"time"
)

// TestSchedulePrompt_Ranges: an automated stop is drawn uniformly in
// [3, 12] min after boot, a real cut's prompt in [3, 10] min.
func TestSchedulePrompt_Ranges(t *testing.T) {
	boot := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	rnd := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed, so the test is reproducible
	check := func(name string, draw func() time.Time, lo, hi time.Duration) {
		t.Helper()
		first, last := hi, lo
		for range 10000 {
			d := draw().Sub(boot)
			if d < lo || d > hi {
				t.Fatalf("%s: drew %v after boot, want within [%v, %v]", name, d, lo, hi)
			}
			first, last = min(first, d), max(last, d)
		}
		// 10,000 uniform draws come within 0.5 % of the range of both ends.
		if margin := (hi - lo) / 200; first > lo+margin || last < hi-margin {
			t.Errorf("%s: draws span [%v, %v], want close to [%v, %v]", name, first, last, lo, hi)
		}
	}
	for _, kind := range automatedKinds {
		check(string(kind), func() time.Time { return SchedulePrompt(boot, kind, rnd) }, 3*time.Minute, 12*time.Minute)
	}
	for _, kind := range realKinds {
		check(string(kind), func() time.Time { return SchedulePrompt(boot, kind, rnd) }, 3*time.Minute, 10*time.Minute)
	}
	check("smoke", func() time.Time { return SchedulePromptWithin(boot, 20*time.Second, 40*time.Second, rnd) }, 20*time.Second, 40*time.Second)
}

// TestSchedulePromptWithin_HiBelowLo: a window whose upper end lies below
// its lower end is clamped to its lower end instead of panicking.
func TestSchedulePromptWithin_HiBelowLo(t *testing.T) {
	boot := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	rnd := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // a fixed seed, so the test is reproducible
	for _, hi := range []time.Duration{40 * time.Second, 39 * time.Second, 0, -time.Minute} {
		if got := SchedulePromptWithin(boot, 40*time.Second, hi, rnd); !got.Equal(boot.Add(40 * time.Second)) {
			t.Errorf("SchedulePromptWithin(lo 40s, hi %v) = %v after boot, want 40s", hi, got.Sub(boot))
		}
	}
}
