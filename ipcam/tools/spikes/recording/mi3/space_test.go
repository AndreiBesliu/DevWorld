package mi3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// deadLink is a controller link whose controller is gone: each request
// fails at once with the link's error, so a step that gets past its checks
// stops there instead of panicking on a nil link.
func deadLink(t *testing.T) *controllerLink {
	t.Helper()
	local, remote := net.Pipe()
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
	in := make(chan received)
	close(in)
	l := &controllerLink{c: link.NewConn(local), in: in, done: make(chan struct{}), host: "192.0.2.1",
		session: link.Session{RTSPBase: "rtsp://192.0.2.1:8554"}}
	t.Cleanup(func() { _ = local.Close() })
	return l
}

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
		l: deadLink(t), dir: t.TempDir(), tier: TierH3}
	_, err := r.measure(t.Context(), 8, FullMinutes, 1)
	var failure *stepFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "17,0 GB") || !strings.Contains(err.Error(), "5,0 GB") {
		t.Fatalf("measure on 5 GB free = %v, want the step refused, with the 17,0 GB that it needs", err)
	}
	if got := stopText(step(4, 15, 1, false), err); !strings.Contains(got, "17,0 GB") {
		t.Errorf("the envelope's text %q does not give the space needed", got)
	}
}

// TestMI3_ErrorsSayOnceToStartAgainAndWhereTheLogsGo (Task 9b review): a
// step that the free space refuses says to free space, and the run's error
// says once to start the probe again, in the first hour and after it. The
// first hour's error also says where its step logs go at the next start,
// work/mi3-anterior, as the error after the first hour does.
func TestMI3_ErrorsSayOnceToStartAgainAndWhereTheLogsGo(t *testing.T) {
	work := t.TempDir()
	o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Results: t.TempDir(), Work: work, Out: io.Discard,
		FreeBytes: func() (uint64, error) { return 5e9, nil }}
	r := &stepRunner{o: o, l: deadLink(t), dir: filepath.Join(work, "mi3"), tier: TierH3}
	_, refused := r.measure(t.Context(), 8, FullMinutes, 1)
	if !strings.Contains(fmt.Sprint(refused), "eliberați spațiu") {
		t.Fatalf("measure on 5 GB free = %v, want the step refused for space", refused)
	}
	previous := filepath.Join(work, "mi3-anterior")

	first := o.unmeasured(refused)
	if n := strings.Count(first.Error(), "porniți din nou proba"); n != 1 || !strings.Contains(first.Error(), previous) {
		t.Errorf("the first hour's error says to start again %d times, want once, and names %s:\n%v", n, previous, first)
	}
	if !errors.Is(first, refused) {
		t.Errorf("the first hour's error %v does not wrap the step's", first)
	}

	failed := dataOf("H3", 8, passIf(false))
	failed.Steps = []results.Mi3Step{step(8, 60, 1, false)}
	later := o.finish(failed, nil, refused)
	if n := strings.Count(later.Error(), "porniți din nou proba"); n != 1 || !strings.Contains(later.Error(), previous) {
		t.Errorf("the error after the first hour says to start again %d times, want once, and names %s:\n%v", n, previous, later)
	}

	stopped := o.unmeasured(fmt.Errorf("pasul 1: %w", context.Canceled))
	if !errors.Is(stopped, context.Canceled) || strings.Contains(stopped.Error(), "mi3-anterior") {
		t.Errorf("the owner's stop in the first hour = %v, want the interruption alone", stopped)
	}
}

// TestMI3_SixteenErrorOnScreenNamesTheStepLogs (Task 9b review): an hour at
// 16 cameras that fails on an error which is not the owner's stop keeps the
// error's own text, which may hold English, a path or an address, in its
// step's log only (recspike.log); the screen says in a fixed Romanian
// sentence where those logs are, and where they go at the next start.
func TestMI3_SixteenErrorOnScreenNamesTheStepLogs(t *testing.T) {
	work := t.TempDir()
	out := &strings.Builder{}
	o := Options{Host: mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd"}, Results: t.TempDir(), Work: work, Out: out,
		FreeBytes: func() (uint64, error) { return 1e12, nil }}
	r := &stepRunner{o: o, l: deadLink(t), dir: filepath.Join(work, "mi3"), tier: TierH3, n: 1}
	_, sixteenErr := r.measure(t.Context(), 16, FullMinutes, 1)
	if sixteenErr == nil {
		t.Fatal("measure on a dead link succeeded")
	}
	raw := errors.Unwrap(sixteenErr)
	for raw != nil && errors.Unwrap(raw) != nil {
		raw = errors.Unwrap(raw)
	}
	stepDir := filepath.Join(work, "mi3", "02-16cam-60min-1")
	logged, err := os.ReadFile(filepath.Join(stepDir, "recspike.log")) //nolint:gosec // the test's own folder
	if err != nil || !strings.Contains(string(logged), sixteenErr.Error()) {
		t.Errorf("the step's recspike.log holds %q (%v), want the error %q", logged, err, sixteenErr)
	}

	passed := dataOf("H3", 8, passIf(true))
	passed.Steps = []results.Mi3Step{step(8, 60, 1, true)}
	passed.SixteenStopped = stopText(step(16, 60, 1, false), sixteenErr)
	if err := o.finish(passed, sixteenErr, nil); err != nil {
		t.Fatalf("finish = %v; an unmeasured 16-camera hour fails no run", err)
	}
	screen := out.String()
	for _, want := range []string{"Ora la 16 camere nu s-a putut măsura", stepDir, filepath.Join(work, "mi3-anterior")} {
		if !strings.Contains(screen, want) {
			t.Errorf("the screen lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, raw.Error()) {
		t.Errorf("the screen shows the error's own text %q:\n%s", raw, screen)
	}
	if want := "16 camere 1 h: nu s-a putut măsura; eroarea este în jurnalele pasului (work/mi3; după o nouă pornire, work/mi3-anterior)"; passed.SixteenStopped != want {
		t.Errorf("the envelope says %q, want %q", passed.SixteenStopped, want)
	}
}

// TestMI3_OneLiveSessionIsSingular (Task 9b review): one live session reads
// «1 sesiune live», in the opening text and the judgement's row.
func TestMI3_OneLiveSessionIsSingular(t *testing.T) {
	if got := openingText(TierH1b, 4, false); !strings.Contains(got, "1 sesiune live,") || strings.Contains(got, "1 sesiuni") {
		t.Errorf("the H1b opening %q, want «1 sesiune live»", got)
	}
	if got := openingText(TierH3, 8, false); !strings.Contains(got, "3 sesiuni live,") {
		t.Errorf("the H3 opening %q, want «3 sesiuni live»", got)
	}
	d := dataOf(TierH1b, 4, passIf(true))
	d.Steps = []results.Mi3Step{step(4, 60, 1, true)}
	e := results.Envelope{Schema: results.Schema, Code: "MI3", RunID: "pi-mi3-1", Set: "pi-mi3", Host: "pi", OS: "linux", MI3: &d}
	if r := row(t, Judge([]results.Envelope{e}), "MI3 pi: nivel și camere"); r.Value != "H1b, 4 camere, 1 sesiune live" {
		t.Errorf("the row reads %q, want «H1b, 4 camere, 1 sesiune live»", r.Value)
	}
}
