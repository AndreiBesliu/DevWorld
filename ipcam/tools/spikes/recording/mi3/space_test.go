package mi3

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// TestMI3_StepNeedsTheSpaceItWrites (G20; Task 9 review): before each step,
// MI3 estimates the bytes that it will write, cameras × 4 Mbps × its window
// × 1.03, plus 2 GiB, and refuses a step that the free space cannot hold
// before anything starts, with a Romanian error that gives the space
// needed. The refusal is a step's error (C1-R21, Task 9's I1): after a
// valid first hour, the envelope keeps that hour and says why the steps
// stopped.
func TestMI3_StepNeedsTheSpaceItWrites(t *testing.T) {
	if got, want := stepBytes(8, time.Hour), uint64(8*500_000*3600*103/100+2<<30); got != want {
		t.Errorf("stepBytes(8, 1 h) = %d, want %d", got, want)
	}
	r := &stepRunner{o: Options{Host: mi1.HostState{Pseudonym: "pc"}, Out: io.Discard, FreeBytes: func() (uint64, error) { return 5e9, nil }},
		dir: t.TempDir(), tier: TierH3}
	_, err := r.measure(t.Context(), 8, FullMinutes, 1)
	var failure *stepFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "17,0 GB") || !strings.Contains(err.Error(), "5,0 GB") {
		t.Fatalf("measure on 5 GB free = %v, want the step refused, with the 17,0 GB that it needs", err)
	}
	if got := stopText(step(4, 15, 1, false), err); !strings.Contains(got, "17,0 GB") {
		t.Errorf("the envelope's text %q does not give the space needed", got)
	}
}
