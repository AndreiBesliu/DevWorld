package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// cryptobenchSchema names the output of diag cryptobench.
const cryptobenchSchema = "ipcam.diag.cryptobench/1"

// cryptobenchReport is the output of diag cryptobench: the encryption rates
// in MB/s (10^6 bytes), on one core and on all of them, and the seconds that
// the single-core run took.
type cryptobenchReport struct {
	Schema             string  `json:"schema"`
	MBPerSecSingleCore float64 `json:"mbPerSecSingleCore"`
	MBPerSecAllCores   float64 `json:"mbPerSecAllCores"`
	Seconds            float64 `json:"seconds"`
}

// benchPart is what each write of the bench gives the writer: about one
// part of a 4 Mbps stream, flushed as MTX-2 flushes a part.
const benchPart = 512 << 10

// singleCoreEncrypt is the single-core run; tests watch it.
var singleCoreEncrypt = encryptFor

// diagCryptobenchCommand is "ipcam-hub diag cryptobench [--seconds 60]"
// (ENC-08, G23): one goroutine under GOMAXPROCS=1 encrypts through
// crypto.Writer, the recordings' age writer, into io.Discard for the seconds
// asked, which decides; then, as information, one goroutine per core does
// the same, under the GOMAXPROCS that it had before.
func diagCryptobenchCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag cryptobench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	seconds := flags.Int("seconds", 60, "how long each run encrypts")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if flags.NArg() != 0 || *seconds < 1 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	d := time.Duration(*seconds) * time.Second
	procs := runtime.GOMAXPROCS(1)
	n, single, err := singleCoreEncrypt(d)
	runtime.GOMAXPROCS(procs)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag cryptobench:", err)
		return exitFatal
	}
	all, err := allCoresMBPerSec(d, procs)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag cryptobench:", err)
		return exitFatal
	}
	return printJSONLine(stdout, stderr, cryptobenchReport{Schema: cryptobenchSchema, MBPerSecSingleCore: mbPerSec(n, single),
		MBPerSecAllCores: all, Seconds: single.Seconds()})
}

// allCoresMBPerSec runs encryptFor on n goroutines at once.
func allCoresMBPerSec(d time.Duration, n int) (float64, error) {
	var wg sync.WaitGroup
	bytes := make([]int64, n)
	took := make([]time.Duration, n)
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() { bytes[i], took[i], errs[i] = encryptFor(d) })
	}
	wg.Wait()
	var total int64
	var longest time.Duration
	for i := range n {
		if errs[i] != nil {
			return 0, errs[i]
		}
		total += bytes[i]
		longest = max(longest, took[i])
	}
	return mbPerSec(total, longest), nil
}

// encryptFor encrypts parts of random bytes through a crypto.Writer for a
// new key scope into io.Discard, flushing after each, until d has passed;
// it returns the bytes encrypted and the time it took.
func encryptFor(d time.Duration) (int64, time.Duration, error) {
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		return 0, 0, err
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		return 0, 0, err
	}
	w, err := crypto.NewWriter(io.Discard, crypto.KeyScope{RecordingIdentity: riG.Recipient(), Recovery: rk.Recipient()})
	if err != nil {
		return 0, 0, err
	}
	part := make([]byte, benchPart)
	_, _ = rand.Read(part) // crypto/rand.Read never fails
	var n int64
	start := time.Now()
	for time.Since(start) < d {
		if _, err := w.Write(part); err != nil {
			return 0, 0, err
		}
		if err := w.Flush(); err != nil {
			return 0, 0, err
		}
		n += benchPart
	}
	return n, time.Since(start), nil
}

func mbPerSec(n int64, d time.Duration) float64 { return float64(n) / 1e6 / d.Seconds() }
