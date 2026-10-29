package mi4

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	// work, when set, is MI4's work folder: each recording then leaves its
	// data and its identity file where ipcam-hub's would be, and a bench
	// whose identity file is gone is noted in keyless.
	work    string
	keyless []string
}

func (f *fakeRig) call(c string) error {
	f.calls = append(f.calls, c)
	if c == f.fail {
		return errors.New("boom")
	}
	return nil
}

func (f *fakeRig) record(_ context.Context, name string, segmentSecs int) (segment, error) {
	seg := segment{path: name + ".mp4.age", identity: name + ".key"}
	if f.work != "" {
		runs, err := filepath.Glob(filepath.Join(f.work, "mi4", "*"))
		if err != nil || len(runs) != 1 {
			return seg, fmt.Errorf("the run folders %q: %v", runs, err)
		}
		dir := filepath.Join(runs[0], name)
		seg.identity = filepath.Join(dir, identityFile)
		for _, file := range []string{filepath.Join(dir, recordingDir, "rec", seg.path), seg.identity} {
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				return seg, err
			}
			if err := os.WriteFile(file, []byte(name), 0o600); err != nil {
				return seg, err
			}
		}
	}
	return seg, f.call(fmt.Sprintf("record %s %d s", name, segmentSecs))
}

// bench notes a bench whose segment's identity file is gone.
func (f *fakeRig) bench(seg segment, c string) {
	if _, err := os.Stat(seg.identity); f.work != "" && err != nil {
		f.keyless = append(f.keyless, c)
	}
}

func (f *fakeRig) readbench(_ context.Context, seg segment, size string, reads int) (readSet, error) {
	f.bench(seg, "readbench "+seg.path+" "+size)
	err := f.call(fmt.Sprintf("readbench %s %s ×%d", seg.path, size, reads))
	if size != "part" {
		return readSet{ReadBytes: 65536, ReadMs: repeat(3, reads), MaxMs: 3, P50Ms: 3, P99Ms: 3}, err
	}
	return readSet{ReadBytes: 524288, ReadMs: append(repeat(10, reads-1), 30), MaxMs: 30, P50Ms: 10, P99Ms: 10}, err
}

func (f *fakeRig) seekbench(_ context.Context, seg segment, seeks int, wholeRAM bool) ([]float64, error) {
	key := fmt.Sprintf("%s ram=%v", seg.path, wholeRAM)
	f.bench(seg, "seekbench "+key)
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
// PC, stamped with the time it was written (AtMs). Fast seeks need no plan
// B measured.
func TestMI4_RunMeasuresEachSetCold(t *testing.T) {
	dir := t.TempDir()
	rig := &fakeRig{seekMs: map[string][]float64{"5min.mp4.age ram=false": repeat(900, 20)}}
	before := time.Now().UnixMilli()
	screen, err := runWith(t, rig, dir)
	after := time.Now().UnixMilli()
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
	if e.AtMs < before || e.AtMs > after {
		t.Errorf("the envelope's time %d ms is not when Run wrote it, within [%d, %d]", e.AtMs, before, after)
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

// TestMI4_RemovesTheIdentityWithTheRecording (PH0-22): RI_g's identity
// file goes with the recording it decrypts, when the run ends and on every
// error after a recording: no identity is left under work/mi4. Until then
// every bench of a segment, the 2-min one's included, still has it.
func TestMI4_RemovesTheIdentityWithTheRecording(t *testing.T) {
	for _, fail := range []string{"", "readbench 5min.mp4.age part ×1000", "record 2min 120 s", "seekbench 2min.mp4.age ram=true ×20", "cryptobench 60 s"} {
		t.Run(cmp.Or(fail, "a whole run"), func(t *testing.T) {
			dir := t.TempDir()
			work := filepath.Join(dir, "work")
			rig := &fakeRig{work: work, fail: fail, seekMs: map[string][]float64{
				"5min.mp4.age ram=false": append(repeat(900, 19), 1700),
				"5min.mp4.age ram=true":  repeat(1600, 20),
				"2min.mp4.age ram=true":  repeat(1400, 20),
			}}
			o := Options{Controller: "192.0.2.1:7081", Results: filepath.Join(dir, "results"), Work: work, Out: io.Discard,
				Host: piHost, Euid: func() int { return 0 }, DropCaches: func() error { return nil }, rig: rig}
			if err := Run(t.Context(), o); (err != nil) != (fail != "") {
				t.Fatalf("Run = %v when %q failed", err, fail)
			}
			if len(rig.keyless) != 0 {
				t.Errorf("benches without their identity: %q", rig.keyless)
			}
			var left []string
			err := filepath.WalkDir(filepath.Join(work, "mi4"), func(path string, d fs.DirEntry, err error) error {
				if err == nil && (strings.HasSuffix(path, ".key") || d.Name() == recordingDir) {
					left = append(left, path)
				}
				return err
			})
			if err != nil || len(left) != 0 {
				t.Errorf("left under work/mi4 after the run: %q (%v)", left, err)
			}
		})
	}
}

// TestMI4_SaysWhenTheIdentityStays (PH0-22; Task 10b review): when the
// recording and RI_g's identity cannot be removed at the end of the run,
// the owner reads a Romanian line that names the folder to delete by
// hand, and a run that would otherwise succeed returns that error; a run
// that fails keeps its own error.
func TestMI4_SaysWhenTheIdentityStays(t *testing.T) {
	realRemoveAll := removeAll
	t.Cleanup(func() { removeAll = realRemoveAll })
	removeAll = func(path string) error {
		if filepath.Base(path) == identityFile {
			return &fs.PathError{Op: "unlinkat", Path: path, Err: fs.ErrPermission}
		}
		return realRemoveAll(path)
	}
	for _, fail := range []string{"", "cryptobench 60 s"} {
		t.Run(cmp.Or(fail, "a whole run"), func(t *testing.T) {
			dir := t.TempDir()
			work := filepath.Join(dir, "work")
			rig := &fakeRig{work: work, fail: fail, seekMs: map[string][]float64{"5min.mp4.age ram=false": repeat(900, 20)}}
			var out strings.Builder
			o := Options{Controller: "192.0.2.1:7081", Results: filepath.Join(dir, "results"), Work: work, Out: &out,
				Host: piHost, Euid: func() int { return 0 }, DropCaches: func() error { return nil }, rig: rig}
			err := Run(t.Context(), o)
			folder := filepath.Join(work, "mi4")
			line := "Nu am putut șterge înregistrarea probei și cheia ei: ștergeți de mână dosarul " + folder + "."
			if !strings.Contains(out.String(), line) {
				t.Errorf("the screen does not say «%s»:\n%s", line, out.String())
			}
			switch {
			case fail == "" && (err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), folder)):
				t.Errorf("Run = %v, want the removal's error, which names %s", err, folder)
			case fail != "" && (err == nil || errors.Is(err, fs.ErrPermission)):
				t.Errorf("Run = %v, want the run's own error alone", err)
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
