package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// seekbenchSchema names the output of diag seekbench.
const seekbenchSchema = "ipcam.diag.seekbench/1"

// maxRAMBuffer bounds the RAM buffer of a whole segment (G21, HUB-33's plan
// B): 256 MiB, about 8 min of a 4 Mbps stream.
const maxRAMBuffer = 256 << 20

// seekbenchReport is the output of diag seekbench: each seek's time in ms.
type seekbenchReport struct {
	Schema   string    `json:"schema"`
	WholeRAM bool      `json:"wholeRam"`
	SeekMs   []float64 `json:"seekMs"`
}

// diagSeekbenchCommand is "ipcam-hub diag seekbench --file <seg.mp4.age>
// --identity <file> [--seeks 20] [--whole-ram]" (ENC-08, G21): it times
// seeks to uniform random targets in a recording, each from the request,
// with the segment not open yet, until the plaintext bytes of the first IDR
// at or after the target have been read through the hub's read path:
// crypto's random-access reader, then the moofs of the parts up to that
// sample's (firstIDR). With --whole-ram, each seek first decrypts the whole
// segment into a buffer of at most 256 MiB, and the moofs and the sample
// are read from it. The targets go up to the last IDR. As readbench does,
// it drops the file's pages from the page cache before the seeks, once it
// has found the targets' range.
func diagSeekbenchCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag seekbench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "the encrypted segment, a .mp4.age")
	identity := flags.String("identity", "", "the file of an age identity that decrypts it")
	seeks := flags.Int("seeks", 20, "how many seeks")
	wholeRAM := flags.Bool("whole-ram", false, "decrypt the whole segment into RAM at each seek")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if flags.NArg() != 0 || *file == "" || *identity == "" || *seeks < 1 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	report, err := seekbench(*file, *identity, *seeks, *wholeRAM)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag seekbench:", err)
		return exitFatal
	}
	return printJSONLine(stdout, stderr, report)
}

func seekbench(file, identity string, seeks int, wholeRAM bool) (seekbenchReport, error) {
	seg, err := openSegment(file, identity)
	if err != nil {
		return seekbenchReport{}, err
	}
	defer seg.close()
	last, err := lastIDR(seg)
	if err != nil {
		return seekbenchReport{}, err
	}
	if wholeRAM && seg.size > maxRAMBuffer {
		return seekbenchReport{}, fmt.Errorf("the plaintext is %d MiB, more than the %d MiB RAM buffer", seg.size>>20, maxRAMBuffer>>20)
	}
	if err := dropFileCache(seg.f); err != nil {
		return seekbenchReport{}, fmt.Errorf("drop the segment from the page cache: %w", err)
	}
	r := seekbenchReport{Schema: seekbenchSchema, WholeRAM: wholeRAM}
	for range seeks {
		target := time.Duration(rand.Int64N(int64(last) + 1)) //nolint:gosec // a bench's draw, not a secret
		start := time.Now()
		if err := seek(seg.rec, file, target, wholeRAM); err != nil {
			return seekbenchReport{}, fmt.Errorf("seek to %v: %w", target, err)
		}
		r.SeekMs = append(r.SeekMs, float64(time.Since(start).Microseconds())/1000)
	}
	return r, nil
}

// lastIDR is the time of the recording's last IDR from its first sample.
func lastIDR(seg *segment) (time.Duration, error) {
	m, err := openFMP4(seg.r, seg.size)
	if err != nil {
		return 0, err
	}
	var first, last uint64
	seen, sync := false, false
	err = m.eachPart(func(_ span, samples []fmp4Sample) (bool, error) {
		for _, s := range samples {
			if !seen {
				first, seen = s.time, true
			}
			if s.sync {
				last, sync = s.time, true
			}
		}
		return true, nil
	})
	if err != nil {
		return 0, err
	}
	if !sync {
		return 0, fmt.Errorf("%w: no IDR", errNotFMP4)
	}
	return m.duration(last - first), nil
}

// seek is one seek: it opens the segment with rec, as the hub's playback
// would, the file key from rec after the first seek, and, with wholeRAM,
// first decrypts it whole into RAM; then it reads the first IDR at or after
// target.
func seek(rec crypto.Recording, file string, target time.Duration, wholeRAM bool) error {
	seg, err := openWith(rec, file)
	if err != nil {
		return err
	}
	defer seg.close()
	r := seg.r
	if wholeRAM {
		if r, err = readWhole(seg); err != nil {
			return err
		}
	}
	_, err = firstIDR(r, seg.size, target)
	return err
}

// readWhole decrypts the whole plaintext of seg into RAM.
func readWhole(seg *segment) (io.ReaderAt, error) {
	buf := make([]byte, seg.size)
	if err := readFullAt(seg.r, buf, 0); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf), nil
}
