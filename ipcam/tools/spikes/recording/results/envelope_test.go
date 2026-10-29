package results

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// sample is a two-run MI1 envelope with every field set.
func sample() Envelope {
	return Envelope{
		Schema: Schema, Code: "MI1", RunID: "pi-kill9-07", Set: "pi-kill9", Host: "pi", OS: "linux",
		PiModel: &PiModelInfo{
			Raw: "Raspberry Pi 4 Model B Rev 1.5", Revision: "d03115", Tier: "H1b", Source: "cpuinfo",
			Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4,
		},
		Storage:        "sd",
		SdConfirmation: &SdConfirmation{Text: "DA", AtMs: 1792000000000},
		Runs: []Run{
			{
				RunID: "pi-kill9-07", Valid: true, Slot: 0, Attempt: 1, ShutdownKind: "kill9", Encrypted: true, PadLowBitrate: true,
				Streams: []StreamLoss{
					{Stream: "cam01_main", BitrateKbps: 4000, Encrypted: true, PadLowBitrate: true, LossMs: 820, LimitMs: 2000},
					{Stream: "cam02_main", BitrateKbps: 4000, Encrypted: true, PadLowBitrate: true, LossMs: 1130, LimitMs: 2000},
				},
				LossMs: 1130, ErrBoundMs: 51, Reconciled: true, Hub04Clear: true, PacingP99Ms: 4,
				ResumeMs: ptr[int64](2400), Cameras: 4, NtpWaitMs: 1200, NtpSynced: true, WriteCache: "on",
			},
			{
				RunID: "pi-kill9-08", Valid: false, InvalidReason: "oprire în afara ferestrei", Attempt: 2, ShutdownKind: "sysrq",
				Streams: []StreamLoss{
					{Stream: "cam01_main", BitrateKbps: 4000, LossMs: 700, LimitMs: 2000},
				},
				LossMs: 700, Unreadable: 1, TornParts: 2, SkippedAUs: 3, ColdBootMs: ptr[int64](19000), Cameras: 1, WriteCache: "unknown",
			},
		},
	}
}

// TestEnvelope_RoundTripAndSchema: Write puts an envelope at
// <code>/<set>/<runId>.json and Read gives the same envelope back; an
// envelope of another schema, or with a field the schema does not have, is
// refused.
func TestEnvelope_RoundTripAndSchema(t *testing.T) {
	dir := t.TempDir()
	want := sample()
	if err := Write(dir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	path := filepath.Join(dir, "MI1", "pi-kill9", "pi-kill9-07.json")
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v\nwant %+v", got, want)
	}
	data, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schema":"ipcam.phase0c1.result/1"`) {
		t.Errorf("the file does not name the schema: %s", data)
	}

	t.Run("an overwrite replaces the file", func(t *testing.T) {
		again := sample()
		again.Runs = again.Runs[:1]
		if err := Write(dir, again); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got, err := Read(path); err != nil || len(got.Runs) != 1 {
			t.Errorf("Read = %d runs, %v; want 1 run", len(got.Runs), err)
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 {
			t.Errorf("the set folder holds %d entries (%v), want only the envelope", len(entries), err)
		}
	})

	for name, data := range map[string]string{
		"another schema":   strings.Replace(string(data), Schema, "ipcam.phase0c1.result/2", 1),
		"no schema":        strings.Replace(string(data), `"schema":"ipcam.phase0c1.result/1",`, "", 1),
		"an unknown field": strings.Replace(string(data), `"code":`, `"extra":1,"code":`, 1),
		"not JSON":         "{",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(data)); err == nil {
				t.Errorf("Parse(%s) = nil error", data)
			}
		})
	}

	for name, mutate := range map[string]func(*Envelope){
		"another schema":           func(e *Envelope) { e.Schema = "x" },
		"an unknown code":          func(e *Envelope) { e.Code = "MI7" },
		"no run id":                func(e *Envelope) { e.RunID = "" },
		"a run id with a slash":    func(e *Envelope) { e.RunID = "a/b" },
		"a run id of ..":           func(e *Envelope) { e.RunID = ".." },
		"a set with a backslash":   func(e *Envelope) { e.Set = `a\b` },
		"no set":                   func(e *Envelope) { e.Set = "" },
		"an unknown storage":       func(e *Envelope) { e.Storage = "nvme" },
		"a part beyond the parts":  func(e *Envelope) { e.Part, e.Parts = 22, 21 },
		"a part without the parts": func(e *Envelope) { e.Part = 3 },
		"parts without a part":     func(e *Envelope) { e.Parts = 21 },
	} {
		t.Run("Write refuses "+name, func(t *testing.T) {
			e := sample()
			mutate(&e)
			if err := Write(t.TempDir(), e); err == nil {
				t.Error("Write = nil error")
			}
		})
	}
	t.Run("a lab part is valid", func(t *testing.T) {
		e := sample()
		e.Part, e.Parts = 21, 21
		if err := e.Validate(); err != nil {
			t.Errorf("Validate = %v", err)
		}
	})
}

// TestEnvelope_HostIsPseudonym: only the five pseudonyms of PH0-22 name a
// host; anything else, a host name or an address above all, is refused by
// Validate, Write and Parse.
func TestEnvelope_HostIsPseudonym(t *testing.T) {
	for _, host := range []string{"pc", "pi", "lab-linux", "lab-windows", "lab-qemu"} {
		e := sample()
		e.Host = host
		if err := e.Validate(); err != nil {
			t.Errorf("host %q: Validate = %v", host, err)
		}
	}
	for _, host := range []string{"", "DESKTOP-4F2K9QL", "raspberrypi", "raspberrypi.local", "192.168.1.20", "PC", "lab"} {
		e := sample()
		e.Host = host
		if err := e.Validate(); err == nil {
			t.Errorf("host %q: Validate = nil error", host)
		}
		dir := t.TempDir()
		if err := Write(dir, e); err == nil {
			t.Errorf("host %q: Write = nil error", host)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("host %q: Write left %d entries", host, len(entries))
		}
		good := sample()
		data := strings.Replace(mustMarshal(t, good), `"host":"pi"`, `"host":"`+host+`"`, 1)
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("host %q: Parse = nil error", host)
		}
	}
}

func TestDeferred(t *testing.T) {
	if got, want := Deferred("Pi armv6"), Verdict("amânat în Faza 0-bis: Pi armv6"); got != want {
		t.Errorf("Deferred = %q, want %q", got, want)
	}
	for v, want := range map[Verdict]string{Pass: "trece", Fail: "pică", Incomplete: "fără verdict: lipsesc măsurători"} {
		if string(v) != want {
			t.Errorf("verdict %q, want %q", v, want)
		}
	}
}

// TestEnvelope_MI2 (G11): an MI2 envelope carries its 72 h run in mi2, and
// reads back the same; an MI2 envelope without it, or another code's with
// it, is refused.
func TestEnvelope_MI2(t *testing.T) {
	dir := t.TempDir()
	want := Envelope{
		Schema: Schema, Code: "MI2", RunID: "pi-72h-2", Set: "pi-72h", Host: "pi", OS: "linux", Storage: "sd",
		SdConfirmation: &SdConfirmation{Text: "DA", AtMs: 1792000000000},
		MI2: &Mi2Data{Hours: 72.02, Cameras: 4, Crashes72h: 0, ChildRestarts: 1, OnSdCard: true,
			CardStates:  []CardState{{State: "ok", AtMs: 1792000001000}, {State: "degraded", AtMs: 1792100000000}},
			WindowHours: 3.47, Invalidated: []string{"pi-72h-1: gazda a repornit în timpul probei"}},
	}
	if err := Write(dir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(filepath.Join(dir, "MI2", "pi-72h", "pi-72h-2.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v\nwant %+v", got, want)
	}
	without := want
	without.MI2 = nil
	if err := without.Validate(); err == nil {
		t.Error("an MI2 envelope without mi2 is valid")
	}
	misplaced := sample()
	misplaced.MI2 = want.MI2
	if err := misplaced.Validate(); err == nil {
		t.Error("an MI1 envelope with mi2 is valid")
	}
}

// TestEnvelope_MI3 (G12): an MI3 envelope carries its capacity run in mi3,
// its steps and its 16-camera hour, and reads back the same; an MI3
// envelope without it, or another code's with it, is refused.
func TestEnvelope_MI3(t *testing.T) {
	dir := t.TempDir()
	want := Envelope{
		Schema: Schema, Code: "MI3", RunID: "pc-mi3-1", Set: "pc-mi3", Host: "pc", OS: "windows", Storage: "ssd",
		MI3: &Mi3Data{Tier: "H3", Cameras: 8, CPUPctTotal: 65.2, RAMMb: 412.5,
			Budgets:   map[string]Budget{"cpuTotal": {Value: 65.2, Limit: 60}, "ram": {Value: 412.5, Limit: 455, Pass: true}},
			DiskRatio: map[string]float64{"clear": 1.012, "encrypted": 1.041}, FsyncPerSecPerFile: 1, LatencyP95Ms: 61.5,
			PacingP99Ms: 9, Steps: []Mi3Step{
				{Cameras: 8, Minutes: 60, Attempt: 1, PacingP99Ms: 120},
				{Cameras: 8, Minutes: 60, Attempt: 2, Valid: true, PacingP99Ms: 9},
				{Cameras: 4, Minutes: 15, Attempt: 1, Valid: true, Pass: true, PacingP99Ms: 7},
			},
			Sixteen: &Mi3Step{Cameras: 16, Minutes: 60, Attempt: 1, Valid: true, SkippedAUs: 0},
			Proxies: []string{"live: sesiuni WebRTC"}, DiskQueuePeakBytes: 6 << 20,
			StepsStopped:   "4 camere 1 h: nu s-a putut măsura; proprietarul a oprit proba",
			SixteenStopped: "16 camere 1 h: nu s-a putut măsura; proprietarul a oprit proba",
			LoadCPUPct:     map[string]float64{"live": 6.5, "playback": 0.8, "export": 11.2, "upload": 1.9}},
	}
	if err := Write(dir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(filepath.Join(dir, "MI3", "pc-mi3", "pc-mi3-1.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v\nwant %+v", got, want)
	}
	without := want
	without.MI3 = nil
	if err := without.Validate(); err == nil {
		t.Error("an MI3 envelope without mi3 is valid")
	}
	misplaced := sample()
	misplaced.MI3 = want.MI3
	if err := misplaced.Validate(); err == nil {
		t.Error("an MI1 envelope with mi3 is valid")
	}
}

// TestEnvelope_MI4 (G13): an MI4 envelope carries the age rig's run in mi4
// and reads back the same; the ENC-06 envelope that recreport builds from
// CI's annotation carries its result there too; an MI4 envelope without
// mi4, or another code's with it, is refused.
func TestEnvelope_MI4(t *testing.T) {
	dir := t.TempDir()
	agerange := 13.5
	want := Envelope{
		Schema: Schema, Code: "MI4", RunID: "pi-mi4-1", Set: "pi-mi4", Host: "pi", OS: "linux", Storage: "sd",
		PiModel: &PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4},
		MI4: &Mi4Data{Model: "Raspberry Pi 4 Model B Rev 1.5", ReadMs: []float64{12.5, 30.25}, ReadMaxMs: 30.25, ReadP50Ms: 12.5, ReadP99Ms: 30.25,
			ReadSize: 524288, Cold: true, InfoReads: []Mi4Reads{{Size: 65536, MaxMs: 4.5, P50Ms: 2, P99Ms: 4}},
			SeekMs: []float64{1700}, SeekRAMMs: []float64{1300}, Seek2MinMs: []float64{600},
			CryptoMBs: 18, CryptoMBsAllCores: 60, ThresholdMBs: 20, RecommendedAfter: 3, AgerangeMbps: &agerange},
	}
	if err := Write(dir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(filepath.Join(dir, "MI4", "pi-mi4", "pi-mi4-1.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v\nwant %+v", got, want)
	}
	pass := true
	enc06 := Envelope{Schema: Schema, Code: "ENC-06", RunID: "ci-enc06-1", Set: "ci-enc06", Host: "lab-linux", OS: "linux", MI4: &Mi4Data{Enc06: &pass}}
	if err := enc06.Validate(); err != nil {
		t.Errorf("the ENC-06 envelope is refused: %v", err)
	}
	without := want
	without.MI4 = nil
	if err := without.Validate(); err == nil {
		t.Error("an MI4 envelope without mi4 is valid")
	}
	misplaced := sample()
	misplaced.MI4 = want.MI4
	if err := misplaced.Validate(); err == nil {
		t.Error("an MI1 envelope with mi4 is valid")
	}
}
