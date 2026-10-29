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
// the reads is one fMP4 part of the recording, its moof and mdat, the HLS
// byte-range unit, decrypted by the hub's read path; the line gives every
// read's time, its max, p50 and p99, and the mean bytes read.
func TestReadbench_EachReadIsOnePart(t *testing.T) {
	seg := writeBenchSegment(t, t.TempDir(), 6, 30, 4000)
	reads := recordReads(t)

	code, line := diag(t, "diag", "readbench", "--file", seg.path, "--identity", seg.identity, "--reads", "50", "--size", "part")
	if code != exitOK {
		t.Fatalf("diag readbench exited %d", code)
	}
	var got readbenchOutput
	decodeLine(t, line, &got)
	if got.Schema != "ipcam.diag.readbench/1" || got.Size != "part" || got.Reads != 50 || len(got.ReadMs) != 50 {
		t.Fatalf("diag readbench printed %+v, want 50 reads of a part", got)
	}
	if got.MaxMs != slices.Max(got.ReadMs) || got.P99Ms > got.MaxMs || got.P50Ms > got.P99Ms || got.P50Ms < 0 {
		t.Errorf("max %v, p99 %v, p50 %v of %v", got.MaxMs, got.P99Ms, got.P50Ms, got.ReadMs)
	}
	done := reads()
	if len(done) != 50 {
		t.Fatalf("%d timed reads, want 50", len(done))
	}
	var total int64
	for _, r := range done {
		if !slices.Contains(seg.parts, r) {
			t.Errorf("read [%d, +%d) is not one part of %v", r.off, r.n, seg.parts)
		}
		total += r.n
	}
	if got.ReadBytes != total/50 {
		t.Errorf("readBytes %d, want the mean %d", got.ReadBytes, total/50)
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
