// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// ErrNeedsSudo is MI4's refusal without root on Linux, where dropping the
// page cache needs it: a whole line for the owner, which says what to run.
var ErrNeedsSudo = errors.New("MI4 trebuie pornit cu sudo: ./recspike mi4 …")

// Options are what MI4's run on a host needs.
type Options struct {
	// Controller is reccontroller's link address, on the PC: MI4 records
	// its cam01_main.
	Controller string
	// Hub is ipcam-hub; MediaMTX is the fork, "" for the one next to Hub.
	Hub, MediaMTX string
	// Results is recspike's published results folder; Work its private
	// one, which holds the recordings, their keys and the logs.
	Results, Work string
	// Out is the owner's screen.
	Out io.Writer
	// Host reads the host: diag host and, on an SD card, the owner's
	// confirmation (mi1.OwnerHost). Run calls it only once it knows that it
	// may drop the page cache.
	Host func(ctx context.Context) (mi1.HostState, error)
	// Euid is the effective user's id, nil for os.Geteuid; DropCaches drops
	// the page cache, nil for the system's (root on Linux, nothing
	// elsewhere). Tests replace them.
	Euid       func() int
	DropCaches func() error
	// Pace is how much MI4 measures, nil for ENC-08's (RealPace); tests
	// shorten it.
	Pace *Pace

	rig rig // nil: ipcam-hub's recordings and diag benches (hubRig)
}

// Pace is how much MI4 measures: the segments' seconds, the reads of each
// set, the seeks, the encryption's seconds and the information reads'
// sizes.
type Pace struct {
	SegmentSecs, TwoMinSecs, Reads, Seeks, CryptoSeconds int
	InfoSizes                                            []int
}

// RealPace is ENC-08's: a 5-min segment, then a 2-min one when the seeks
// fail (G21), 1000 reads of a part (G22) and of 64 KiB and 4 MiB, 20 seeks,
// 60 s of encryption on one core (G23).
var RealPace = Pace{SegmentSecs: 300, TwoMinSecs: 120, Reads: 1000, Seeks: 20, CryptoSeconds: 60, InfoSizes: []int{64 << 10, 4 << 20}}

// readSet is a set of reads as `ipcam-hub diag readbench` prints it.
type readSet struct {
	Schema    string    `json:"schema"`
	Size      string    `json:"size"`
	ReadBytes int64     `json:"readBytes"`
	Reads     int       `json:"reads"`
	ReadMs    []float64 `json:"readMs"`
	MaxMs     float64   `json:"maxMs"`
	P50Ms     float64   `json:"p50Ms"`
	P99Ms     float64   `json:"p99Ms"`
}

// cryptoRates are the rates that `ipcam-hub diag cryptobench` prints.
type cryptoRates struct {
	Schema             string  `json:"schema"`
	MBPerSecSingleCore float64 `json:"mbPerSecSingleCore"`
	MBPerSecAllCores   float64 `json:"mbPerSecAllCores"`
	Seconds            float64 `json:"seconds"`
}

// rig is what MI4 runs on the host: ipcam-hub's recordings of the
// controller's camera and its diag benches (C01), and the end of its link
// to the controller, at which a Pi's results reach the PC (G15).
type rig interface {
	record(ctx context.Context, name string, segmentSecs int) (hubrec.Segment, error)
	readbench(ctx context.Context, seg hubrec.Segment, size string, reads int) (readSet, error)
	seekbench(ctx context.Context, seg hubrec.Segment, seeks int, wholeRAM bool) ([]float64, error)
	cryptobench(ctx context.Context, seconds int) (cryptoRates, error)
	end(ctx context.Context) error
}

// Run is MI4 on this host (G13, ENC-08, HUB-33): it records one encrypted
// segment of the controller's cam01_main (4 Mbps), 5 min, through
// ipcam-hub, then, each set after a drop of the page cache (G22), times
// 1000 reads of one full fMP4 part's length at random offsets, which
// decide, and 1000 of 64 KiB and of 4 MiB, information; then 20 seeks to
// the first IDR (G21), and when one takes more than 1.5 s, the same with
// the whole segment in RAM and in a 2-min segment recorded for it; then the
// encryption on one core for 60 s (G23). It writes the envelope
// MI4/<host>-mi4/<run id>.json, and a Pi sends it to the PC. On Linux it
// must run as root, which drops the cache; without root it refuses before
// it reads the host or records anything. Every line it prints is Romanian.
func Run(ctx context.Context, o Options) (err error) {
	if err := requireRoot(o.euid()); err != nil {
		return err
	}
	host, err := o.Host(ctx)
	if err != nil {
		return err
	}
	if m := host.PiModel; m != nil && m.Known && !m.Supported {
		return errors.New("acest Raspberry Pi nu este suportat (armv6): proba MI4 se amână în Faza 0-bis")
	}
	// The recordings of the runs before go first; each segment's recording
	// and identity stay until the last bench, the 2-min one's included, and
	// go however the run ends (PH0-22). When they cannot, the owner deletes
	// them by hand: a run that would otherwise succeed fails with that
	// error, and a failed run keeps its own.
	runs := hubrec.Runs(o.Work, "mi4")
	runs.RemoveAll = removeAll
	dir, err := runs.Start(time.Now())
	if err != nil {
		return err
	}
	defer runs.End(&err, o.say)
	r := o.rig
	if r == nil {
		r = &hubRig{o: o, host: host, dir: dir}
	}
	release, err := mi1.KeepAwake()
	if err != nil {
		return fmt.Errorf("nu pot ține dispozitivul treaz: %w", err)
	}
	defer release()

	p := o.pacing()
	o.say("Proba MI4 începe: înregistrez un segment criptat de %s de la camera cam01 a controlorului, apoi măsor %s aleatoare, "+
		"%s și criptarea. Durează în jur de %d min; lăsați dispozitivul pornit și nefolosit până la final. Jurnalele sunt în %s.",
		duration(p.SegmentSecs), count(p.Reads, "citire", "citiri"), count(p.Seeks, "salt", "salturi"), p.SegmentSecs/60+10, dir)
	d, err := o.measure(ctx, r, host, p)
	if err != nil {
		_ = r.end(ctx) // the run's error is the one to say
		if ctx.Err() != nil {
			return fmt.Errorf("proba MI4 s-a întrerupt: %w", ctx.Err())
		}
		return fmt.Errorf("proba MI4 s-a oprit fără rezultat: %w", err)
	}
	runID, err := o.write(host, d)
	if err != nil {
		_ = r.end(ctx)
		return err
	}
	o.say("Proba MI4 s-a încheiat: rezultatul este %s. Citiri: max %s ms; primul cadru după salt: max %s ms; criptare: %s pe un nucleu.",
		runID, ms(d.ReadMaxMs), ms(slices.Max(d.SeekMs)), mbs(d.CryptoMBs))
	if err := r.end(ctx); err != nil && host.Pseudonym == "pi" {
		o.say("Rezultatul nu a ajuns la PC (%v): trimiteți-l la final cu recspike push.", err)
	}
	return nil
}

// measure records the segments and runs the benches.
func (o Options) measure(ctx context.Context, r rig, host mi1.HostState, p Pace) (results.Mi4Data, error) {
	d := results.Mi4Data{Model: "PC"}
	if host.PiModel != nil {
		d.Model = host.PiModel.Raw
	}
	seg, err := r.record(ctx, hubrec.FiveMin, p.SegmentSecs)
	if err != nil {
		return d, err
	}
	sizes := []string{"de lungimea unei părți"}
	for _, size := range p.InfoSizes {
		sizes = append(sizes, "câte "+sizeText(size))
	}
	o.say("Segmentul s-a închis. Citiri aleatoare, %s, de %s fiecare.", strings.Join(sizes, ", apoi "), count(p.Reads, "dată", "ori"))
	parts, err := o.readSet(ctx, r, seg, "part", p.Reads)
	if err != nil {
		return d, err
	}
	d.ReadMs, d.ReadMaxMs, d.ReadP50Ms, d.ReadP99Ms, d.ReadSize = parts.ReadMs, parts.MaxMs, parts.P50Ms, parts.P99Ms, int(parts.ReadBytes)
	d.Cold = coldReads
	for _, size := range p.InfoSizes {
		s, err := o.readSet(ctx, r, seg, strconv.Itoa(size), p.Reads)
		if err != nil {
			return d, err
		}
		d.InfoReads = append(d.InfoReads, results.Mi4Reads{Size: size, MaxMs: s.MaxMs, P50Ms: s.P50Ms, P99Ms: s.P99Ms})
	}
	o.say("Măsor %s, fiecare până la primul cadru întreg (IDR).", count(p.Seeks, "salt", "salturi"))
	if d.SeekMs, err = o.seekSet(ctx, r, seg, p.Seeks, false); err != nil {
		return d, err
	}
	if slices.Max(d.SeekMs) > MaxSeekMs {
		o.say("Un salt a durat peste 1,5 s: măsor și planul B, cu segmentul întreg în RAM, apoi într-un segment de %s.", duration(p.TwoMinSecs))
		if d.SeekRAMMs, err = o.seekSet(ctx, r, seg, p.Seeks, true); err != nil {
			return d, err
		}
		two, err := r.record(ctx, "2min", p.TwoMinSecs)
		if err != nil {
			return d, err
		}
		if d.Seek2MinMs, err = o.seekSet(ctx, r, two, p.Seeks, true); err != nil {
			return d, err
		}
	}
	o.say("Criptare: %d s pe un nucleu, apoi %d s pe toate.", p.CryptoSeconds, p.CryptoSeconds)
	rates, err := r.cryptobench(ctx, p.CryptoSeconds)
	if err != nil {
		return d, err
	}
	d.CryptoMBs, d.CryptoMBsAllCores = rates.MBPerSecSingleCore, rates.MBPerSecAllCores
	d.ThresholdMBs, _, d.RecommendedAfter = Throughput(host.PiModel, d.CryptoMBs)
	return d, nil
}

// readSet runs one set of reads of size after a drop of the page cache.
func (o Options) readSet(ctx context.Context, r rig, seg hubrec.Segment, size string, reads int) (readSet, error) {
	if err := o.drop(); err != nil {
		return readSet{}, err
	}
	s, err := r.readbench(ctx, seg, size, reads)
	if err == nil && len(s.ReadMs) == 0 {
		err = errors.New("ipcam-hub diag readbench nu a dat nicio citire")
	}
	return s, err
}

// seekSet runs one set of seeks after a drop of the page cache.
func (o Options) seekSet(ctx context.Context, r rig, seg hubrec.Segment, seeks int, wholeRAM bool) ([]float64, error) {
	if err := o.drop(); err != nil {
		return nil, err
	}
	s, err := r.seekbench(ctx, seg, seeks, wholeRAM)
	if err == nil && len(s) == 0 {
		err = errors.New("ipcam-hub diag seekbench nu a dat niciun salt")
	}
	return s, err
}

// drop drops the page cache, which a cold set needs (G22).
func (o Options) drop() error {
	f := o.DropCaches
	if f == nil {
		f = dropCaches
	}
	if err := f(); err != nil {
		return fmt.Errorf("nu pot goli memoria cache a discului: %w", err)
	}
	return nil
}

// write writes d as the run's envelope, stamped with the host's clock now,
// and returns its run id.
func (o Options) write(host mi1.HostState, d results.Mi4Data) (string, error) {
	set := SetFor(host.Pseudonym)
	e := results.Envelope{Schema: results.Schema, Code: "MI4", AtMs: time.Now().UnixMilli(), Set: set, Host: host.Pseudonym, OS: host.OS,
		PiModel: host.PiModel, Storage: host.Storage, SdConfirmation: host.SdConfirmation, MI4: &d}
	for n := 1; ; n++ {
		e.RunID = fmt.Sprintf("%s-%d", set, n)
		err := results.Create(o.Results, e)
		switch {
		case errors.Is(err, results.ErrExists):
			continue
		case err != nil:
			return "", fmt.Errorf("nu pot scrie rezultatul probei: %w", err)
		}
		return e.RunID, nil
	}
}

// removeAll removes a recording or an identity file; tests replace it.
var removeAll = os.RemoveAll

func (o Options) euid() int {
	if o.Euid != nil {
		return o.Euid()
	}
	return os.Geteuid()
}

func (o Options) pacing() Pace {
	if o.Pace != nil {
		return *o.Pace
	}
	return RealPace
}

func (o Options) say(format string, args ...any) {
	if o.Out != nil {
		_, _ = fmt.Fprintf(o.Out, format+"\n", args...)
	}
}

// count is n of a noun in Romanian: "1 citire", "5 citiri", "20 de
// citiri", "1000 de citiri": "de" comes before the plural when the last two
// digits of n are 00 or 20 to 99, from 20 on.
func count(n int, one, many string) string {
	switch rest := n % 100; {
	case n == 1:
		return "1 " + one
	case n >= 20 && (rest == 0 || rest >= 20):
		return fmt.Sprintf("%d de %s", n, many)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// duration is secs as the owner reads it: "5 min", "2 min", "45 s".
func duration(secs int) string {
	if secs%60 == 0 {
		return fmt.Sprintf("%d min", secs/60)
	}
	return fmt.Sprintf("%d s", secs)
}
