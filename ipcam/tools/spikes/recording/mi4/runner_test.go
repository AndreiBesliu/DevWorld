package mi4

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// fakeRig stands for ipcam-hub's recordings and diag benches: it records
// each call, in order, with the cache drops, and answers with fixed
// figures; seekMs gives the seeks of each segment, by name, and RAM.
type fakeRig struct {
	calls  []string
	seekMs map[string][]float64
	fail   string // the call that fails
}

func (f *fakeRig) call(c string) error {
	f.calls = append(f.calls, c)
	if c == f.fail {
		return errors.New("boom")
	}
	return nil
}

func (f *fakeRig) record(_ context.Context, name string, segmentSecs int) (segment, error) {
	return segment{path: name + ".mp4.age", identity: name + ".key"}, f.call(fmt.Sprintf("record %s %d s", name, segmentSecs))
}

func (f *fakeRig) readbench(_ context.Context, seg segment, size string, reads int) (readSet, error) {
	err := f.call(fmt.Sprintf("readbench %s %s ×%d", seg.path, size, reads))
	if size != "part" {
		return readSet{ReadBytes: 65536, ReadMs: repeat(3, reads), MaxMs: 3, P50Ms: 3, P99Ms: 3}, err
	}
	return readSet{ReadBytes: 524288, ReadMs: append(repeat(10, reads-1), 30), MaxMs: 30, P50Ms: 10, P99Ms: 10}, err
}

func (f *fakeRig) seekbench(_ context.Context, seg segment, seeks int, wholeRAM bool) ([]float64, error) {
	key := fmt.Sprintf("%s ram=%v", seg.path, wholeRAM)
	return f.seekMs[key], f.call(fmt.Sprintf("seekbench %s ×%d", key, seeks))
}

func (f *fakeRig) cryptobench(_ context.Context, seconds int) (cryptoRates, error) {
	return cryptoRates{MBPerSecSingleCore: 22, MBPerSecAllCores: 70}, f.call(fmt.Sprintf("cryptobench %d s", seconds))
}

func (f *fakeRig) end(context.Context) error { return f.call("end") }

// piHost is a Pi 4 on an SD card.
func piHost(context.Context) (mi1.HostState, error) {
	return mi1.HostState{Pseudonym: "pi", OS: "linux", Storage: "sd", PiModel: pi4, Cameras: 4,
		SdConfirmation: &results.SdConfirmation{Text: "DA", AtMs: 1}}, nil
}

// runWith runs MI4 as root with rig, and returns the screen and the cache
// drops' place among rig's calls.
func runWith(t *testing.T, rig *fakeRig, dir string) (string, error) {
	t.Helper()
	var out strings.Builder
	o := Options{Controller: "192.0.2.1:7081", Hub: filepath.Join(dir, "ipcam-hub"), Results: filepath.Join(dir, "results"),
		Work: filepath.Join(dir, "work"), Out: &out, Host: piHost, Euid: func() int { return 0 },
		DropCaches: func() error { rig.calls = append(rig.calls, "drop"); return nil }, rig: rig}
	err := Run(t.Context(), o)
	return out.String(), err
}

// TestMI4_RunMeasuresEachSetCold (G21, G22, G23): MI4 records one 5-min
// encrypted segment, drops the page cache before each set of reads and
// before the seeks, reads 1000 parts (the verdict) and 1000 reads of 64 KiB
// and 4 MiB (information), seeks 20 times, measures the encryption, writes
// the Pi's envelope with the threshold of its model, and sends it to the
// PC. Fast seeks need no plan B measured.
func TestMI4_RunMeasuresEachSetCold(t *testing.T) {
	dir := t.TempDir()
	rig := &fakeRig{seekMs: map[string][]float64{"5min.mp4.age ram=false": repeat(900, 20)}}
	screen, err := runWith(t, rig, dir)
	if err != nil {
		t.Fatalf("Run = %v\n%s", err, screen)
	}
	want := []string{
		"record 5min 300 s",
		"drop", "readbench 5min.mp4.age part ×1000",
		"drop", "readbench 5min.mp4.age 65536 ×1000",
		"drop", "readbench 5min.mp4.age 4194304 ×1000",
		"drop", "seekbench 5min.mp4.age ram=false ×20",
		"cryptobench 60 s",
		"end",
	}
	if !slices.Equal(rig.calls, want) {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(rig.calls, "\n"), strings.Join(want, "\n"))
	}
	e, err := results.Read(filepath.Join(dir, "results", "MI4", "pi-mi4", "pi-mi4-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := e.MI4
	if d.Model != pi4.Raw || len(d.ReadMs) != 1000 || d.ReadMaxMs != 30 || d.ReadP50Ms != 10 || d.ReadP99Ms != 10 || d.ReadSize != 524288 || d.Cold != coldReads {
		t.Errorf("the reads in the envelope: %+v", d)
	}
	if len(d.InfoReads) != 2 || d.InfoReads[0].Size != 65536 || d.InfoReads[1].Size != 4194304 || d.InfoReads[1].MaxMs != 3 {
		t.Errorf("the information reads: %+v", d.InfoReads)
	}
	if len(d.SeekMs) != 20 || d.SeekRAMMs != nil || d.Seek2MinMs != nil {
		t.Errorf("the seeks: %v, RAM %v, 2 min %v", d.SeekMs, d.SeekRAMMs, d.Seek2MinMs)
	}
	if d.CryptoMBs != 22 || d.CryptoMBsAllCores != 70 || d.ThresholdMBs != 20 || d.RecommendedAfter != 4 {
		t.Errorf("the encryption: %+v", d)
	}
	if e.PiModel == nil || e.Storage != "sd" || e.SdConfirmation == nil || e.Host != "pi" {
		t.Errorf("the envelope's host: %+v", e)
	}
	if j := Judge([]results.Envelope{e, agerangeRun(13), enc06Run(true)}); j.Verdict != results.Pass {
		t.Errorf("Judge of the run = %q, reasons %q", j.Verdict, j.Reasons)
	}
}

// TestMI4_SlowSeeksAlsoTimeRAMAndTwoMinuteSegments (G21): when the seeks
// in the 5-min segment take more than 1.5 s, MI4 also times them with the
// whole segment in RAM, records a 2-min segment and times its seeks with
// it in RAM, each set after a cache drop, for the plan B's two stages.
func TestMI4_SlowSeeksAlsoTimeRAMAndTwoMinuteSegments(t *testing.T) {
	dir := t.TempDir()
	rig := &fakeRig{seekMs: map[string][]float64{
		"5min.mp4.age ram=false": append(repeat(900, 19), 1700),
		"5min.mp4.age ram=true":  repeat(1600, 20),
		"2min.mp4.age ram=true":  repeat(1400, 20),
	}}
	if screen, err := runWith(t, rig, dir); err != nil {
		t.Fatalf("Run = %v\n%s", err, screen)
	}
	want := []string{
		"drop", "seekbench 5min.mp4.age ram=false ×20",
		"drop", "seekbench 5min.mp4.age ram=true ×20",
		"record 2min 120 s",
		"drop", "seekbench 2min.mp4.age ram=true ×20",
		"cryptobench 60 s",
	}
	at := slices.Index(rig.calls, want[1]) - 1
	if at < 0 || at+len(want) > len(rig.calls) || !slices.Equal(rig.calls[at:at+len(want)], want) {
		t.Errorf("calls:\n%s\nwant, in a row:\n%s", strings.Join(rig.calls, "\n"), strings.Join(want, "\n"))
	}
	e, err := results.Read(filepath.Join(dir, "results", "MI4", "pi-mi4", "pi-mi4-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.MI4.SeekRAMMs) != 20 || len(e.MI4.Seek2MinMs) != 20 {
		t.Errorf("RAM %v, 2 min %v; want both measured", e.MI4.SeekRAMMs, e.MI4.Seek2MinMs)
	}
	if j := Judge([]results.Envelope{e, agerangeRun(13), enc06Run(true)}); j.PlanB != PlanBTwoMin {
		t.Errorf("plan B %q, want %s", j.PlanB, PlanBTwoMin)
	}
}

// TestMI4_StopsOnAFailedStep: a cache drop that fails, as a bench or a
// recording that fails, stops MI4 with an error and no envelope: reads
// that could not be made cold would pass for cold ones. The link to the
// controller still ends with its goodbye.
func TestMI4_StopsOnAFailedStep(t *testing.T) {
	for _, fail := range []string{"drop", "record 5min 300 s", "readbench 5min.mp4.age part ×1000", "cryptobench 60 s"} {
		t.Run(fail, func(t *testing.T) {
			dir := t.TempDir()
			rig := &fakeRig{seekMs: map[string][]float64{"5min.mp4.age ram=false": repeat(900, 20)}, fail: fail}
			var out strings.Builder
			o := Options{Controller: "192.0.2.1:7081", Results: filepath.Join(dir, "results"), Work: filepath.Join(dir, "work"), Out: &out,
				Host: piHost, Euid: func() int { return 0 }, rig: rig,
				DropCaches: func() error { return rig.call("drop") }}
			if err := Run(t.Context(), o); err == nil {
				t.Fatalf("Run succeeded when %q failed", fail)
			}
			if _, err := results.Read(filepath.Join(dir, "results", "MI4", "pi-mi4", "pi-mi4-1.json")); err == nil {
				t.Error("an envelope was written")
			}
			if last := rig.calls[len(rig.calls)-1]; last != "end" {
				t.Errorf("calls %v, want the end of the link last", rig.calls)
			}
		})
	}
}

// TestMI4_UnsupportedPiIsDeferred (G31): an armv6 Pi runs no MI4.
func TestMI4_UnsupportedPiIsDeferred(t *testing.T) {
	rig := &fakeRig{}
	o := Options{Results: t.TempDir(), Work: t.TempDir(), Euid: func() int { return 0 }, rig: rig,
		Host: func(context.Context) (mi1.HostState, error) {
			return mi1.HostState{Pseudonym: "pi", OS: "linux", PiModel: pi1}, nil
		}}
	if err := Run(t.Context(), o); err == nil || !strings.Contains(err.Error(), "Faza 0-bis") || len(rig.calls) != 0 {
		t.Errorf("Run on an armv6 Pi = %v, calls %v; want it deferred before anything runs", err, rig.calls)
	}
}

// TestMI4_CountsInRomanian: the screen counts with Romanian's «de» from 20
// on.
func TestMI4_CountsInRomanian(t *testing.T) {
	for n, want := range map[int]string{1: "1 citire", 5: "5 citiri", 19: "19 citiri", 20: "20 de citiri", 101: "101 citiri", 1000: "1000 de citiri"} {
		if got := count(n, "citire", "citiri"); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}
}
