package mi1

import (
	"fmt"
	"slices"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

var (
	labSets   = []string{SetLabLinuxKill9, SetLabLinuxQEMUReset, SetLabWindowsKill9}
	ownerSets = []string{SetPCKill9, SetPCBugcheck, SetPiKill9, SetPiSysrq}
	realSets  = []string{SetRealPC, SetRealPi}
)

// TestMI1_SetComposition (G10, G12, O17): every automated set, lab and
// owner, has 20 default runs, 10 of them encrypted and exactly one with the
// 256 kbps stream (encrypted, the setting on), plus one O17-off run of
// cam01_main and cam99_main, encrypted, the setting off. Lab runs and the
// owner's reboot runs record cam01_main; the owner's default kill9 runs
// record the recommended cameras. Real sets are slots 1 to 5 of cam01_main,
// 1, 3 and 5 encrypted.
func TestMI1_SetComposition(t *testing.T) {
	const cameras = 4
	for _, set := range append(slices.Clone(labSets), ownerSets...) {
		t.Run(set, func(t *testing.T) {
			plan, err := Plan(set, cameras)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(plan) != 21 {
				t.Fatalf("Plan has %d runs, want 21", len(plan))
			}
			var defaults, encrypted, lowBitrate int
			for i, p := range plan {
				if p.Index != i+1 {
					t.Errorf("run %d has Index %d", i+1, p.Index)
				}
				if !p.PadLowBitrate {
					continue
				}
				defaults++
				if p.Encrypted {
					encrypted++
				}
				if slices.Contains(p.Streams, LowBitrateStream) {
					lowBitrate++
					if !p.Encrypted {
						t.Errorf("the 256 kbps run %d is clear, want encrypted", p.Index)
					}
				}
				want := []string{MainStream}
				if set == SetPCKill9 || set == SetPiKill9 {
					want = []string{"cam01_main", "cam02_main", "cam03_main", "cam04_main"}
				}
				if got := slices.DeleteFunc(slices.Clone(p.Streams), func(s string) bool { return s == LowBitrateStream }); !slices.Equal(got, want) {
					t.Errorf("default run %d records %q besides the 256 kbps stream, want %q", p.Index, got, want)
				}
			}
			if defaults != 20 || encrypted != 10 || lowBitrate != 1 {
				t.Errorf("%d default runs, %d encrypted, %d with the 256 kbps stream; want 20, 10, 1", defaults, encrypted, lowBitrate)
			}
			off := plan[20]
			if off.PadLowBitrate || !off.Encrypted || !slices.Equal(off.Streams, []string{MainStream, LowBitrateStream}) {
				t.Errorf("run 21 = %+v, want the O17-off run: encrypted cam01_main and cam99_main, the setting off", off)
			}
		})
	}

	t.Run("lab shards", func(t *testing.T) {
		plan, err := Plan(SetLabLinuxKill9, 1)
		if err != nil {
			t.Fatal(err)
		}
		one, err := Shard(plan, 1)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Shard(plan, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(one) != 10 || one[0].Index != 1 || len(two) != 11 || two[0].Index != 11 || two[10].Index != 21 {
			t.Fatalf("shards of %d and %d runs, want parts 1–10 and 11–21", len(one), len(two))
		}
		if !slices.ContainsFunc(one, func(p RunPlan) bool { return slices.Contains(p.Streams, LowBitrateStream) && p.PadLowBitrate }) {
			t.Error("shard 1 lacks the 256 kbps run with the setting on")
		}
		if !slices.ContainsFunc(two, func(p RunPlan) bool { return !p.PadLowBitrate }) {
			t.Error("shard 2 lacks the O17-off run")
		}
		for _, shard := range []int{0, 3} {
			if _, err := Shard(plan, shard); err == nil {
				t.Errorf("Shard(%d) = nil error", shard)
			}
		}
	})

	for _, set := range realSets {
		t.Run(set, func(t *testing.T) {
			plan, err := Plan(set, cameras)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(plan) != 5 {
				t.Fatalf("Plan has %d cuts, want 5", len(plan))
			}
			for i, p := range plan {
				slot := i + 1
				if p.Index != slot || p.Encrypted != (slot%2 == 1) || !p.PadLowBitrate || !slices.Equal(p.Streams, []string{MainStream}) {
					t.Errorf("slot %d = %+v, want cam01_main, encrypted %v", slot, p, slot%2 == 1)
				}
			}
		})
	}

	t.Run("bitrates", func(t *testing.T) {
		for stream, want := range map[string]int{
			"cam01_main": 4000, "cam16_main": 4000, "cam99_main": 256,
			"cam01_sub": 0, "cam00_main": 0, "cam17_main": 0, "cam1_main": 0, "07_main": 0, "x": 0,
		} {
			if got := BitrateKbps(stream); got != want {
				t.Errorf("BitrateKbps(%q) = %d, want %d", stream, got, want)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for _, c := range []struct {
			set     string
			cameras int
		}{{"pc-power", 1}, {"", 1}, {SetPCKill9, 0}, {SetPiKill9, 17}} {
			if _, err := Plan(c.set, c.cameras); err == nil {
				t.Errorf("Plan(%q, %d) = nil error", c.set, c.cameras)
			}
		}
		// Only the owner's kill9 sets record more than one camera.
		if _, err := Plan(SetLabLinuxKill9, 0); err != nil {
			t.Errorf("Plan(%q, 0) = %v, want the camera count ignored", SetLabLinuxKill9, err)
		}
	})
}

// counted is a run that counts toward its set.
func counted(id string) results.Run {
	return results.Run{RunID: id, Valid: true, Reconciled: true, Hub04Clear: true}
}

// TestMI1_NextRun_RedoesInvalidInPlace: an automated run that does not
// count is redone in the same position, with the same encryption and
// streams; after the last position the set is done.
func TestMI1_NextRun_RedoesInvalidInPlace(t *testing.T) {
	plan, err := Plan(SetPCKill9, 2)
	if err != nil {
		t.Fatal(err)
	}
	var done []results.Run
	for i := range 3 {
		done = append(done, counted(fmt.Sprint(i)))
	}
	next, ok := NextRun(plan, done)
	if !ok || next.Index != 4 {
		t.Fatalf("NextRun = %+v, %v; want run 4", next, ok)
	}
	invalid := counted("x")
	invalid.Valid = false
	if again, ok := NextRun(plan, append(done, invalid)); !ok || again.Index != 4 {
		t.Errorf("after an invalid run 4: NextRun = %+v, %v; want run 4 again", again, ok)
	}
	for len(done) < 21 {
		done = append(done, counted(fmt.Sprint(len(done))))
	}
	if next, ok := NextRun(plan, done); ok {
		t.Errorf("after 21 counted runs: NextRun = %+v, want the set done", next)
	}
}
