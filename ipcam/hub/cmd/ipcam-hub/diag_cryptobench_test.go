package main

import (
	"runtime"
	"testing"
	"time"
)

// TestCryptobench_SingleGoroutineGOMAXPROCS1 (G23, ENC-08): diag
// cryptobench encrypts through crypto.Writer on one goroutine under
// GOMAXPROCS=1 for the seconds asked, then on every core as information,
// prints both rates in MB/s and the seconds that the single-core run
// took, and gives GOMAXPROCS back.
func TestCryptobench_SingleGoroutineGOMAXPROCS1(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	var procs []int
	encrypt := singleCoreEncrypt
	singleCoreEncrypt = func(d time.Duration) (int64, time.Duration, error) {
		procs = append(procs, runtime.GOMAXPROCS(0))
		return encrypt(d)
	}
	t.Cleanup(func() { singleCoreEncrypt = encrypt })

	code, line := diag(t, "diag", "cryptobench", "--seconds", "1")
	if code != exitOK {
		t.Fatalf("diag cryptobench exited %d", code)
	}
	var got struct {
		Schema             string  `json:"schema"`
		MBPerSecSingleCore float64 `json:"mbPerSecSingleCore"`
		MBPerSecAllCores   float64 `json:"mbPerSecAllCores"`
		Seconds            float64 `json:"seconds"`
	}
	decodeLine(t, line, &got)
	if got.Schema != "ipcam.diag.cryptobench/1" || got.MBPerSecSingleCore <= 0 || got.MBPerSecAllCores <= 0 || got.Seconds < 1 {
		t.Errorf("diag cryptobench printed %+v, want the schema, both rates and at least the 1 s asked", got)
	}
	if len(procs) != 1 || procs[0] != 1 {
		t.Errorf("the single-core run ran %d times with GOMAXPROCS %v, want once with 1", len(procs), procs)
	}
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS is %d after the bench, want %d back", after, before)
	}
	if code, _ := diag(t, "diag", "cryptobench", "--seconds", "0"); code != exitFatal {
		t.Errorf("diag cryptobench --seconds 0 exited %d, want a usage error", code)
	}
}
