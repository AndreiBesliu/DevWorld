package recorder

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func appendBytes(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(make([]byte, n)); err != nil {
		t.Fatal(err)
	}
}

// TestStatus_LastPartAgoFromPartFileSize (G05): LastPartAgoMs is the time
// since the open .part file of the stream last grew, as polled every 250 ms;
// the first growth of a .part that was not there when the child started is
// the stream's first part.
func TestStatus_LastPartAgoFromPartFileSize(t *testing.T) {
	recDir := t.TempDir()
	key := streamKey{"cam1", "main"}
	w := newPartWatcher(recDir)
	w.setStreams([]streamKey{key})
	t0 := time.Date(2026, 10, 20, 10, 0, 0, 0, time.Local)

	// A .part that the last child left behind is not this child's first part.
	old := filepath.Join(recDir, "cam1", "main", "2026-10-20", "095500_000000.mp4.part")
	writeFile(t, old, make([]byte, 50))
	w.childStarted(t0)
	if s := w.status(key, t0); s.LastPartAgoMs != -1 || s.LastSegmentAgoMs != -1 {
		t.Errorf("before any part: %+v, want -1 for both", s)
	}
	if firsts, _ := w.poll(t0.Add(250 * time.Millisecond)); len(firsts) != 0 {
		t.Errorf("the old .part counted as a first part: %+v", firsts)
	}

	part := filepath.Join(recDir, "cam1", "main", "2026-10-20", "100001_000000.mp4.part")
	writeFile(t, part, make([]byte, 100))
	firsts, _ := w.poll(t0.Add(500 * time.Millisecond))
	if len(firsts) != 1 || firsts[0].key != key || !firsts[0].at.Equal(t0.Add(500*time.Millisecond)) {
		t.Fatalf("the new .part gave first parts %+v, want one for cam1/main", firsts)
	}
	if want := time.Date(2026, 10, 20, 10, 0, 1, 0, time.Local); !firsts[0].segmentStart.Equal(want) {
		t.Errorf("the first part's segment starts at %v, want %v from its name", firsts[0].segmentStart, want)
	}
	if s := w.status(key, t0.Add(500*time.Millisecond)); s.LastPartAgoMs != 0 {
		t.Errorf("right after the growth LastPartAgoMs = %d, want 0", s.LastPartAgoMs)
	}
	if got, _ := w.poll(t0.Add(1500 * time.Millisecond)); len(got) != 0 {
		t.Errorf("a .part that does not grow gave first parts %+v", got)
	}
	if s := w.status(key, t0.Add(1500*time.Millisecond)); s.LastPartAgoMs != 1000 {
		t.Errorf("1 s without growth: LastPartAgoMs = %d, want 1000", s.LastPartAgoMs)
	}
	appendBytes(t, part, 10)
	if got, _ := w.poll(t0.Add(1750 * time.Millisecond)); len(got) != 0 {
		t.Errorf("a second growth gave first parts %+v", got)
	}
	if s := w.status(key, t0.Add(2*time.Second)); s.LastPartAgoMs != 250 {
		t.Errorf("250 ms after the growth: LastPartAgoMs = %d, want 250", s.LastPartAgoMs)
	}

	w.segmentClosed(key, t0.Add(3*time.Second))
	if s := w.status(key, t0.Add(4*time.Second)); s.LastSegmentAgoMs != 1000 {
		t.Errorf("LastSegmentAgoMs = %d, want 1000", s.LastSegmentAgoMs)
	}

	// After a restart, the next new .part is a first part again, also in the
	// next day's directory.
	w.childStarted(t0.Add(5 * time.Second))
	next := filepath.Join(recDir, "cam1", "main", "2026-10-21", "000000_000000.mp4.age.part")
	writeFile(t, next, make([]byte, 1))
	if got, _ := w.poll(t0.Add(6 * time.Second)); len(got) != 1 {
		t.Errorf("after a restart: first parts %+v, want one", got)
	}
}

// TestPartWatcher_ReportsAPartThatANewerOneLeftBehind (MTX-1): MediaMTX
// closes a stream's segment before it creates the stream's next one, so a
// .part that this child created is no longer written once a .part that the
// polls first saw later, and that starts later, is there: it is reported,
// once. A .part that was there when the child started is not reported (the
// supervisor hands a dead child's files to the repair itself), nor is one
// first seen in the same poll as the newest. When the order of the names and
// the order in which the polls saw them disagree (a clock set back, or an
// older date directory that came into view), nothing is reported.
func TestPartWatcher_ReportsAPartThatANewerOneLeftBehind(t *testing.T) {
	recDir := t.TempDir()
	key := streamKey{"cam1", "main"}
	w := newPartWatcher(recDir)
	w.setStreams([]streamKey{key})
	t0 := time.Date(2026, 10, 20, 10, 0, 0, 0, time.Local)
	stream := filepath.Join(recDir, "cam1", "main")
	part := func(day, name string) string {
		p := filepath.Join(stream, day, name+".mp4.part")
		writeFile(t, p, make([]byte, 10))
		return p
	}
	step := 0
	poll := func() []string {
		step++
		_, left := w.poll(t0.Add(time.Duration(step) * pollInterval))
		return left
	}

	part("2026-10-20", "095500_000000")
	w.childStarted(t0)
	a := part("2026-10-20", "100000_000000")
	if left := poll(); len(left) != 0 {
		t.Errorf("the newest .part was reported left behind: %q", left)
	}
	if left := poll(); len(left) != 0 {
		t.Errorf("a .part was reported while no newer one exists: %q", left)
	}
	b := part("2026-10-20", "100100_000000")
	if left := poll(); !slices.Equal(left, []string{a}) {
		t.Errorf("left behind %q, want %q", left, []string{a})
	}
	if left := poll(); len(left) != 0 {
		t.Errorf("reported again: %q", left)
	}
	part("2026-10-20", "100200_000000")
	part("2026-10-20", "100300_000000")
	if left := poll(); !slices.Equal(left, []string{b}) {
		t.Errorf("with two new .part files in one poll, left behind %q, want only %q", left, []string{b})
	}

	// After a restart, every .part there is the dead child's.
	w.childStarted(t0.Add(time.Minute))
	e := part("2026-10-20", "100400_000000")
	if left := poll(); len(left) != 0 {
		t.Errorf("after a restart, a .part of the dead child was reported: %q", left)
	}
	// A clock set back gives the next segment an earlier name.
	f := part("2026-10-20", "093000_000000")
	if left := poll(); len(left) != 0 {
		t.Errorf("with the names in the other order, left behind %q, want none", left)
	}
	g := part("2026-10-20", "100500_000000")
	if left := poll(); !slices.Equal(left, []string{f, e}) {
		t.Errorf("left behind %q, want %q", left, []string{f, e})
	}

	// The three newest date directories are listed. When the newest one goes,
	// an older one comes into view, with a .part that the polls see only now.
	w.childStarted(t0.Add(2 * time.Minute))
	for _, day := range []string{"2026-10-21", "2026-10-22"} {
		if err := os.MkdirAll(filepath.Join(stream, day), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	part("2026-10-19", "235900_000000")
	h := part("2026-10-20", "100600_000000")
	if left := poll(); len(left) != 0 {
		t.Errorf("left behind %q, want none", left)
	}
	if err := os.Remove(filepath.Join(stream, "2026-10-22")); err != nil {
		t.Fatal(err)
	}
	if left := poll(); len(left) != 0 {
		t.Errorf("a .part that came into view made %q left behind, want none", left)
	}
	_, _ = g, h
}

// TestStatus_RunningOnceApplied: a stream runs once Apply has it and the
// child is up.
func TestStatus_RunningOnceApplied(t *testing.T) {
	r, _ := startFake(t, "sleep", Config{})
	if r.Status(t.Context(), "cam1", "main").Running {
		t.Error("a stream runs before Apply")
	}
	if err := r.Apply(t.Context(), []Job{NewJob("cam1", "main", "rtsp://127.0.0.1:1/cam1/main", nil)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the stream does not run", func() bool { return r.Status(t.Context(), "cam1", "main").Running })
	if r.Status(t.Context(), "cam1", "sub").Running {
		t.Error("a stream that Apply did not get runs")
	}
}
