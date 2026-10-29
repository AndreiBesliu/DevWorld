package main

import (
	"io"
	"slices"
	"strconv"
	"sync"
	"testing"
)

// readbenchOutput is diag readbench's line.
type readbenchOutput struct {
	Schema    string    `json:"schema"`
	Size      string    `json:"size"`
	ReadBytes int64     `json:"readBytes"`
	Reads     int       `json:"reads"`
	ReadMs    []float64 `json:"readMs"`
	MaxMs     float64   `json:"maxMs"`
	P50Ms     float64   `json:"p50Ms"`
	P99Ms     float64   `json:"p99Ms"`
}

// recordReads makes the bench's timed reads go through to the plaintext
// and returns what they read, in order.
func recordReads(t *testing.T) func() []span {
	t.Helper()
	var mu sync.Mutex
	var reads []span
	read := readPlaintext
	readPlaintext = func(r io.ReaderAt, p []byte, off int64) (int, error) {
		mu.Lock()
		reads = append(reads, span{off: off, n: int64(len(p))})
		mu.Unlock()
		return read(r, p, off)
	}
	t.Cleanup(func() { readPlaintext = read })
	return func() []span {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(reads)
	}
}

// TestReadbench_EachReadIsOnePart (G22, ENC-08): with --size part, each of
// the reads is as long as one fMP4 part of the recording, its moof and mdat
// (the HLS byte-range unit), the part drawn at random, and starts at a
// uniform random offset in the plaintext, not at that part's own start:
// MTX-2 pads most parts to the 64 KiB chunk boundary, so a part-aligned read
// would start on a chunk and touch one age chunk less, which would favour
// the 50 ms gate (C26). The reads go through the hub's read path, and the
// line gives every read's time, its max, p50 and p99, and the mean bytes
// read.
func TestReadbench_EachReadIsOnePart(t *testing.T) {
	const n = 200
	seg := writeBenchSegment(t, t.TempDir(), 6, 30, 4000)
	plaintext := seg.parts[len(seg.parts)-1].off + seg.parts[len(seg.parts)-1].n
	reads := recordReads(t)

	code, line := diag(t, "diag", "readbench", "--file", seg.path, "--identity", seg.identity, "--reads", strconv.Itoa(n), "--size", "part")
	if code != exitOK {
		t.Fatalf("diag readbench exited %d", code)
	}
	var got readbenchOutput
	decodeLine(t, line, &got)
	if got.Schema != "ipcam.diag.readbench/1" || got.Size != "part" || got.Reads != n || len(got.ReadMs) != n {
		t.Fatalf("diag readbench printed %+v, want %d reads of a part's length", got, n)
	}
	if got.MaxMs != slices.Max(got.ReadMs) || got.P99Ms > got.MaxMs || got.P50Ms > got.P99Ms || got.P50Ms < 0 {
		t.Errorf("max %v, p99 %v, p50 %v of %v", got.MaxMs, got.P99Ms, got.P50Ms, got.ReadMs)
	}
	done := reads()
	if len(done) != n {
		t.Fatalf("%d timed reads, want %d", len(done), n)
	}
	var total int64
	lengths := map[int64]bool{}
	aligned, low, high := 0, false, false
	for _, r := range done {
		if !slices.ContainsFunc(seg.parts, func(p span) bool { return p.n == r.n }) {
			t.Errorf("read [%d, +%d) is not as long as one part of %v", r.off, r.n, seg.parts)
		}
		if r.off < 0 || r.off+r.n > plaintext {
			t.Errorf("read [%d, +%d) is not inside the %d-byte plaintext", r.off, r.n, plaintext)
		}
		if slices.Contains(seg.parts, r) {
			aligned++
		}
		// Where the offset falls in [0, plaintext - n], the range it is drawn from.
		at := float64(r.off) / float64(plaintext-r.n)
		low, high = low || at < 0.25, high || at > 0.75
		lengths[r.n] = true
		total += r.n
	}
	// Each of the 6 parts' lengths is drawn: P(one is missed) ≈ 6 × (5/6)^200.
	if len(lengths) != len(seg.parts) {
		t.Errorf("the reads have %d lengths, want each of the %d parts' lengths", len(lengths), len(seg.parts))
	}
	// A uniform offset is a part's start about once in 10^5 reads, and falls
	// in each quarter of its range about 50 times in 200.
	if aligned > n/2 || !low || !high {
		t.Errorf("%d of %d reads start at their part, low quarter %v, high quarter %v: want uniform offsets in the plaintext", aligned, n, low, high)
	}
	if got.ReadBytes != total/n {
		t.Errorf("readBytes %d, want the mean %d", got.ReadBytes, total/n)
	}
}

// TestReadbench_FixedSizeReadsInsidePlaintext (G22): the information sizes,
// 64 KiB and 4 MiB in MI4, read that many bytes at a random offset inside
// the plaintext; a size larger than the plaintext is refused.
func TestReadbench_FixedSizeReadsInsidePlaintext(t *testing.T) {
	seg := writeBenchSegment(t, t.TempDir(), 6, 30, 4000)
	plaintext := seg.parts[len(seg.parts)-1].off + seg.parts[len(seg.parts)-1].n
	reads := recordReads(t)

	code, line := diag(t, "diag", "readbench", "--file", seg.path, "--identity", seg.identity, "--reads", "20", "--size", "65536")
	if code != exitOK {
		t.Fatalf("diag readbench exited %d", code)
	}
	var got readbenchOutput
	decodeLine(t, line, &got)
	if got.Size != "65536" || got.ReadBytes != 65536 || len(got.ReadMs) != 20 {
		t.Errorf("diag readbench printed %+v, want 20 reads of 65536 bytes", got)
	}
	for _, r := range reads() {
		if r.n != 65536 || r.off < 0 || r.off+r.n > plaintext {
			t.Errorf("read [%d, +%d) is not 65536 bytes inside the %d-byte plaintext", r.off, r.n, plaintext)
		}
	}
	for _, size := range []string{strconv.FormatInt(plaintext+1, 10), "0", "-1", "a"} {
		if code, _ := diag(t, "diag", "readbench", "--file", seg.path, "--identity", seg.identity, "--reads", "2", "--size", size); code != exitFatal {
			t.Errorf("diag readbench --size %s exited %d, want an error", size, code)
		}
	}
}
