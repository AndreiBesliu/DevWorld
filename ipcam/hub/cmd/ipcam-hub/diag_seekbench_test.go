package main

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// openBenchSegment opens seg's plaintext as the benches do.
func openBenchSegment(t *testing.T, seg benchSegment) (io.ReaderAt, int64) {
	t.Helper()
	ids, err := readIdentities(seg.identity)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(seg.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	r, size, err := crypto.NewRecording(ids...).OpenReaderAt(t.Context(), f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	return r, size
}

// at is the time of sample i from the start of the recording.
func at(i int) time.Duration {
	return time.Duration(i) * benchSampleDur * time.Second / benchTimeScale
}

// TestSeekbench_FirstIDRAtOrAfterTarget (G21): a seek delivers the
// plaintext bytes of the first IDR sample of the video track at or after
// its target, found through the parts' moofs: in a recording with an IDR
// every 20 samples and parts of 30, a target inside a GOP gives the next
// IDR, in its part or the next one; a target on an IDR gives that IDR.
// Read from the whole segment in RAM, it gives the same bytes.
func TestSeekbench_FirstIDRAtOrAfterTarget(t *testing.T) {
	seg := writeBenchSegment(t, t.TempDir(), 4, 30, 400)
	r, size := openBenchSegment(t, seg)
	whole := make([]byte, size)
	if err := readFullAt(r, whole, 0); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		target time.Duration
		want   int
	}{
		{"the start", 0, 0},
		{"inside the first GOP", at(7) + time.Millisecond, 20},
		{"on an IDR", at(20), 20},
		{"just after an IDR, its next IDR in the next part", at(20) + time.Millisecond, 40},
		{"inside a GOP across two parts", at(25), 40},
		{"the last IDR", at(100), 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			for name, src := range map[string]io.ReaderAt{"read path": r, "RAM": bytes.NewReader(whole)} {
				got, err := firstIDR(src, size, c.target)
				if err != nil {
					t.Fatalf("%s: firstIDR(%v) = %v", name, c.target, err)
				}
				if !bytes.Equal(got, seg.samples[c.want].payload) {
					t.Errorf("%s: firstIDR(%v) gave %d bytes starting % x, want sample %d's", name, c.target, len(got), got[:min(8, len(got))], c.want)
				}
			}
		})
	}
	if _, err := firstIDR(r, size, at(101)); err == nil {
		t.Error("a target after the last IDR gave an IDR")
	}
}

// TestSeekbench_PrintsEachSeek (G21): diag seekbench prints each seek's
// time, from the request (the segment not yet open) until the IDR's bytes
// are read, through the read path or, with --whole-ram, after the whole
// segment is decrypted into RAM.
func TestSeekbench_PrintsEachSeek(t *testing.T) {
	seg := writeBenchSegment(t, t.TempDir(), 4, 30, 400)
	for _, wholeRAM := range []bool{false, true} {
		args := []string{"diag", "seekbench", "--file", seg.path, "--identity", seg.identity, "--seeks", "5"}
		if wholeRAM {
			args = append(args, "--whole-ram")
		}
		code, line := diag(t, args...)
		if code != exitOK {
			t.Fatalf("diag seekbench (whole RAM %v) exited %d", wholeRAM, code)
		}
		var got struct {
			Schema   string    `json:"schema"`
			WholeRAM bool      `json:"wholeRam"`
			SeekMs   []float64 `json:"seekMs"`
		}
		decodeLine(t, line, &got)
		if got.Schema != "ipcam.diag.seekbench/1" || got.WholeRAM != wholeRAM || len(got.SeekMs) != 5 {
			t.Errorf("diag seekbench printed %+v, want 5 seeks, whole RAM %v", got, wholeRAM)
		}
	}
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	wrong := seg.identity + ".other"
	if err := os.WriteFile(wrong, []byte(other.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _ := diag(t, "diag", "seekbench", "--file", seg.path, "--identity", wrong, "--seeks", "1"); code != exitFatal {
		t.Errorf("diag seekbench with another identity exited %d, want an error", code)
	}
}
