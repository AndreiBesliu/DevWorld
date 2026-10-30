package mi2

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestStability_RollingWindow (G19): the 72 h run keeps a rolling window of
// N = min(6 h, ½ × free space ÷ write rate), at 4 Mbps per camera, and
// refuses to start when N < 1 h. With 8 cameras (4 MB/s, 14.4 GB/h), 100 GB
// free gives 3.47 h, 500 GB 6 h, and 20 GB (0.69 h) refuses; the host needs
// 2 × N × rate free: ≥ 29 GB for 8 cameras at 1 h (≈ 173 GB for 6 h), ≥ 15
// GB for 4 (≈ 87 GB) and ≥ 8 GB for 2 (≈ 44 GB), which recspike prints
// before it starts. Only segments older than N whose segment_verified
// recspike has seen are deleted.
func TestStability_RollingWindow(t *testing.T) {
	if got := WriteRate(8); got != 4_000_000 {
		t.Errorf("WriteRate(8) = %v B/s, want 4 MB/s", got)
	}
	for _, tc := range []struct {
		free     uint64
		hours    float64
		tooSmall bool
	}{
		{100e9, 3.47, false},
		{500e9, 6, false},
		{20e9, 0.69, true},
	} {
		n, err := Window(tc.free, 8)
		if math.Abs(n.Hours()-tc.hours) > 0.01 {
			t.Errorf("Window(%d GB, 8) = %.3f h, want %.2f h ± 0.01 h", tc.free/1e9, n.Hours(), tc.hours)
		}
		if errors.Is(err, ErrWindowTooSmall) != tc.tooSmall {
			t.Errorf("Window(%d GB, 8): error %v, want too small %v", tc.free/1e9, err, tc.tooSmall)
		}
	}
	for _, tc := range []struct {
		cameras int
		window  time.Duration
		gb      int
	}{
		{8, time.Hour, 29}, {8, 6 * time.Hour, 173},
		{4, time.Hour, 15}, {4, 6 * time.Hour, 87},
		{2, time.Hour, 8}, {2, 6 * time.Hour, 44},
	} {
		if got := NeededGB(tc.cameras, tc.window); got != tc.gb {
			t.Errorf("NeededGB(%d cameras, %v) = %d, want %d", tc.cameras, tc.window, got, tc.gb)
		}
	}

	t.Run("recspike prints the space before it starts", func(t *testing.T) {
		h := newHarness(t, t.TempDir(), pcHost, "boot-1")
		h.opts.FreeBytes = func() (uint64, error) { return 100e9, nil }
		h.start()
		h.nextHub()
		for _, want := range []string{"29 GB", "173 GB", "100 GB", "3,47 h"} {
			if !strings.Contains(h.out.String(), want) {
				t.Errorf("recspike printed %q, want %q in it", h.out, want)
			}
		}
		h.finish()
		if d := h.envelope("pc-72h-1").MI2; math.Abs(d.WindowHours-3.47) > 0.01 {
			t.Errorf("window %v h, want 3.47 h", d.WindowHours)
		}
	})

	t.Run("too little space refuses to start", func(t *testing.T) {
		h := newHarness(t, t.TempDir(), pcHost, "boot-1")
		h.opts.FreeBytes = func() (uint64, error) { return 20e9, nil }
		h.start()
		if err := h.wait(); !errors.Is(err, ErrWindowTooSmall) {
			t.Errorf("Run: %v, want ErrWindowTooSmall", err)
		}
		if !strings.Contains(h.out.String(), "29 GB") {
			t.Errorf("recspike printed %q, want the 29 GB it needs", h.out)
		}
		select {
		case <-h.hubs:
			t.Error("ipcam-hub started")
		default:
		}
	})

	t.Run("only verified segments older than N are deleted", func(t *testing.T) {
		dir := t.TempDir()
		now := time.Now()
		n := 3 * time.Hour
		files := map[string]time.Time{
			"old-verified.mp4":         now.Add(-n - time.Minute),
			"old-unverified.mp4.age":   now.Add(-n - time.Hour),
			"young-verified.mp4.age":   now.Add(-n + time.Minute),
			"old-verified-gone.mp4":    now.Add(-n - time.Minute),
			"old-verified-second.mp4":  now.Add(-2 * n),
			"young-unverified.mp4.age": now,
		}
		for name, at := range files {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, at, at); err != nil {
				t.Fatal(err)
			}
		}
		p := NewPruner(n)
		for _, name := range []string{"old-verified.mp4", "young-verified.mp4.age", "old-verified-gone.mp4", "old-verified-second.mp4"} {
			p.Verified(filepath.Join(dir, name))
		}
		if err := os.Remove(filepath.Join(dir, "old-verified-gone.mp4")); err != nil {
			t.Fatal(err)
		}
		deleted, err := p.Prune(now)
		if err != nil {
			t.Fatalf("Prune: %v", err)
		}
		var names []string
		for _, path := range deleted {
			names = append(names, filepath.Base(path))
		}
		slices.Sort(names)
		if want := []string{"old-verified-second.mp4", "old-verified.mp4"}; !slices.Equal(names, want) {
			t.Errorf("deleted %v, want %v", names, want)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var left []string
		for _, e := range entries {
			left = append(left, e.Name())
		}
		if want := []string{"old-unverified.mp4.age", "young-unverified.mp4.age", "young-verified.mp4.age"}; !slices.Equal(left, want) {
			t.Errorf("left %v, want %v", left, want)
		}
		// A deleted segment is no longer followed: a second prune finds nothing.
		if again, err := p.Prune(now); err != nil || len(again) != 0 {
			t.Errorf("second Prune = %v, %v; want nothing", again, err)
		}
	})
}
