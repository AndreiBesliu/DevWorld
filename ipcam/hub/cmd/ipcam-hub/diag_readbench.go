package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// readbenchSchema names the output of diag readbench.
const readbenchSchema = "ipcam.diag.readbench/1"

// readbenchReport is the output of diag readbench: each read's time in ms,
// in the order read, their max, p50 and p99, and the mean bytes read.
type readbenchReport struct {
	Schema    string    `json:"schema"`
	Size      string    `json:"size"`
	ReadBytes int64     `json:"readBytes"`
	Reads     int       `json:"reads"`
	ReadMs    []float64 `json:"readMs"`
	MaxMs     float64   `json:"maxMs"`
	P50Ms     float64   `json:"p50Ms"`
	P99Ms     float64   `json:"p99Ms"`
}

// readPlaintext is one timed read of the plaintext; tests watch it.
var readPlaintext = func(r io.ReaderAt, p []byte, off int64) (int, error) { return r.ReadAt(p, off) }

// diagReadbenchCommand is "ipcam-hub diag readbench --file <seg.mp4.age>
// --identity <file> [--reads 1000] [--size part|<bytes>]" (ENC-08, G22):
// it times reads of a recording's plaintext through the hub's read path,
// crypto's random-access reader. Each read starts at a uniform random offset
// in the plaintext. With --size part, it is as long as one fMP4 part of the
// recording, its moof and mdat (the HLS byte-range unit, about 512 KiB at 4
// Mbps), the part drawn uniformly: not that part itself, because MTX-2 pads
// most parts to the 64 KiB chunk boundary, so a read from a part's start
// would touch one age chunk less than a read of the same length elsewhere
// (C26 judges the slowest read). With a number of bytes, each read is that
// long. Before the reads, it drops the file's pages from the page cache
// where the system can (Linux), so that the index it reads first does not
// warm them: on a Pi, MI4 drops the whole cache before it starts the bench.
func diagReadbenchCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag readbench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "the encrypted segment, a .mp4.age")
	identity := flags.String("identity", "", "the file of an age identity that decrypts it")
	reads := flags.Int("reads", 1000, "how many reads")
	size := flags.String("size", "part", `"part", or the bytes of each read`)
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if flags.NArg() != 0 || *file == "" || *identity == "" || *reads < 1 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	report, err := readbench(*file, *identity, *reads, *size)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag readbench:", err)
		return exitFatal
	}
	return printJSONLine(stdout, stderr, report)
}

func readbench(file, identity string, reads int, size string) (readbenchReport, error) {
	seg, err := openSegment(file, identity)
	if err != nil {
		return readbenchReport{}, err
	}
	defer seg.close()
	ranges, err := readRanges(seg, reads, size)
	if err != nil {
		return readbenchReport{}, err
	}
	if err := dropFileCache(seg.f); err != nil {
		return readbenchReport{}, fmt.Errorf("drop the segment from the page cache: %w", err)
	}
	r := readbenchReport{Schema: readbenchSchema, Size: size, Reads: reads}
	var total int64
	buf := make([]byte, slices.MaxFunc(ranges, func(a, b span) int { return cmp.Compare(a.n, b.n) }).n)
	for _, s := range ranges {
		start := time.Now()
		n, err := readPlaintext(seg.r, buf[:s.n], s.off)
		ms := float64(time.Since(start).Microseconds()) / 1000
		if int64(n) != s.n {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return readbenchReport{}, fmt.Errorf("read [%d, +%d): %w", s.off, s.n, err)
		}
		r.ReadMs = append(r.ReadMs, ms)
		total += s.n
	}
	r.ReadBytes = total / int64(reads)
	r.MaxMs, r.P50Ms, r.P99Ms = slices.Max(r.ReadMs), percentile(r.ReadMs, 50), percentile(r.ReadMs, 99)
	return r, nil
}

// readRanges draws the reads of a set, each at a uniform offset in the
// plaintext: as long as a part drawn uniformly for "part", else size bytes.
func readRanges(seg *segment, reads int, size string) ([]span, error) {
	lengths, err := readLengths(seg, size)
	if err != nil {
		return nil, err
	}
	ranges := make([]span, reads)
	for i := range ranges {
		n := lengths[rand.IntN(len(lengths))]                      //nolint:gosec // a bench's draw, not a secret
		ranges[i] = span{off: rand.Int64N(seg.size - n + 1), n: n} //nolint:gosec // a bench's draw, not a secret
	}
	return ranges, nil
}

// readLengths are the lengths that a set's reads draw from: each part's,
// from its moof to the end of its mdat, for "part", else size bytes.
func readLengths(seg *segment, size string) ([]int64, error) {
	if size == "part" {
		m, err := openFMP4(seg.r, seg.size)
		if err != nil {
			return nil, err
		}
		var lengths []int64
		err = m.eachPart(func(p span, _ []fmp4Sample) (bool, error) {
			lengths = append(lengths, p.n)
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		if len(lengths) == 0 {
			return nil, errors.New("the segment has no part")
		}
		return lengths, nil
	}
	n, err := strconv.ParseInt(size, 10, 64)
	switch {
	case err != nil || n < 1:
		return nil, fmt.Errorf("--size %q is neither part nor a number of bytes", size)
	case n > seg.size:
		return nil, fmt.Errorf("--size %d is larger than the %d-byte plaintext", n, seg.size)
	}
	return []int64{n}, nil
}

// percentile is the nearest-rank p-th percentile of values.
func percentile(values []float64, p float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	return sorted[max(0, int(math.Ceil(p/100*float64(len(sorted))))-1)]
}

// segment is an encrypted recording opened through the hub's read path:
// rec opens it with the identities, and r is its plaintext, size bytes.
type segment struct {
	f    *os.File
	rec  crypto.Recording
	r    io.ReaderAt
	size int64
}

// openSegment opens the recording at file with the identities in the file
// identity.
func openSegment(file, identity string) (*segment, error) {
	ids, err := readIdentities(identity)
	if err != nil {
		return nil, err
	}
	return openWith(crypto.NewRecording(ids...), file)
}

// openWith opens the recording at file with rec, as the hub's playback
// would: its header unwrapped (once per file key, which rec keeps) and its
// final chunk authenticated.
func openWith(rec crypto.Recording, file string) (*segment, error) {
	f, err := os.Open(file) //nolint:gosec // the segment that the bench is asked to read
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	r, size, err := rec.OpenReaderAt(context.Background(), f, info.Size())
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("open %s: %w", info.Name(), err)
	}
	return &segment{f: f, rec: rec, r: r, size: size}, nil
}

func (s *segment) close() { _ = s.f.Close() } // read-only

// readIdentities reads the age identities in the file at path.
func readIdentities(path string) ([]age.Identity, error) {
	f, err := os.Open(path) //nolint:gosec // the identity file that the bench is given
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only
	ids, err := age.ParseIdentities(f)
	if err != nil {
		// age's error may quote the key.
		return nil, errors.New("the identity file holds no age identity that can be read")
	}
	return ids, nil
}
