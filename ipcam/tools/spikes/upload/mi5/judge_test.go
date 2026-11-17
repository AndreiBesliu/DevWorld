package mi5

import (
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// passing is an owner's MI5 envelope that meets every condition of G14.
func passing() results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "MI5", RunID: "mi5-1", Set: "pc", Host: "pc", OS: "windows", Storage: "ssd",
		Runs: []results.Run{{RunID: "mi5-1", Valid: true}},
		MI5: &results.Mi5Data{BlockSecs: OwnerBlock.Seconds(), Restart: &results.Mi5Restart{Size: 9 << 20, LastConfirmedOffset: 4 << 20, ResumedOffset: 4 << 20},
			ReadOpen: []results.ReadOpenRun{{Encrypted: true, Calls: 20, OK: 20}, {Calls: 24, OK: 24}},
			Dests: []results.Mi5Dest{
				{Dest: "dst-1", Kind: KindDrive, Account: "acct-1", Cameras: 4, Segments: 400, SegmentsDuringBlock: 288, Verified: 400, FailedDuringBlock: 12},
				{Dest: "dst-od", Kind: KindOneDrive, Account: "acct-1", Deferred: MsgOneDriveDeferred},
			}}}
}

func judgeOne(edit func(*results.Envelope)) results.Judgement {
	e := passing()
	edit(&e)
	return Judge([]results.Envelope{e})
}

func TestG14_NoGaps_Trece(t *testing.T) {
	if j := judgeOne(func(*results.Envelope) {}); j.Verdict != results.Pass {
		t.Errorf("a passing envelope: %+v", j)
	}
	// The hub's UNREACHABLE state is the block's evidence as well.
	j := judgeOne(func(e *results.Envelope) {
		e.MI5.Dests[0].FailedDuringBlock, e.MI5.Dests[0].UnreachableDuringBlock = 0, true
	})
	if j.Verdict != results.Pass {
		t.Errorf("UNREACHABLE during the block: %+v", j)
	}
}

func TestG14_NoSegmentsDuringBlock_FaraVerdict(t *testing.T) {
	j := judgeOne(func(e *results.Envelope) { e.MI5.Dests[0].SegmentsDuringBlock = 0 })
	if j.Verdict != results.Incomplete || !strings.Contains(strings.Join(j.Reasons, "\n"), "niciun segment") {
		t.Errorf("no segment during the block: %+v", j)
	}
}

func TestG14_NoResumeAboveZero_FaraVerdict(t *testing.T) {
	for name, edit := range map[string]func(*results.Envelope){
		"offset 0":   func(e *results.Envelope) { e.MI5.Restart.ResumedOffset = 0 },
		"no restart": func(e *results.Envelope) { e.MI5.Restart = nil },
	} {
		if j := judgeOne(edit); j.Verdict != results.Incomplete {
			t.Errorf("%s: %+v", name, j)
		}
	}
}

func TestG14_OpenSegmentReadFails_PlanB2MinSegments(t *testing.T) {
	j := judgeOne(func(e *results.Envelope) { e.MI5.ReadOpen[1].OK = 23 })
	if j.Verdict != results.FailWith("segmente de 2 min") {
		t.Errorf("a failed open-segment read: %+v", j)
	}
	// A measured failure decides even when a floor is missed.
	j = judgeOne(func(e *results.Envelope) { e.MI5.ReadOpen[0].OK = 19; e.MI5.Restart = nil })
	if j.Verdict != results.FailWith("segmente de 2 min") {
		t.Errorf("a failed read and no restart: %+v", j)
	}
}

func TestG14_Duplicate_PicaOutbox(t *testing.T) {
	for name, edit := range map[string]func(*results.Envelope){
		"duplicate": func(e *results.Envelope) { e.MI5.Dests[0].Duplicates = 1 },
		"missing":   func(e *results.Envelope) { e.MI5.Dests[0].Missing = 1; e.MI5.Dests[0].Verified = 399 },
		"mismatch":  func(e *results.Envelope) { e.MI5.Dests[0].HashMismatches = 1 },
		"missing, no segment during the block": func(e *results.Envelope) {
			e.MI5.Dests[0].Missing, e.MI5.Dests[0].SegmentsDuringBlock = 2, 0
		},
	} {
		if j := judgeOne(edit); j.Verdict != results.FailWith("outbox-ul se corectează înainte de Faza 3") {
			t.Errorf("%s: %+v", name, j)
		}
	}
}

func TestG14_EmptySet_FaraVerdict(t *testing.T) {
	if j := Judge(nil); j.Verdict != results.Incomplete {
		t.Errorf("no envelope: %+v", j)
	}
	// A pi-probe marker, a 15-min block and a cut run are not judged.
	short := passing()
	short.MI5.BlockSecs = 900
	cut := passing()
	cut.Runs[0].Valid = false
	probe := results.PiProbeMarker("Raspberry Pi Zero W Rev 1.1", "9000c1", "cpuinfo", 1)
	if j := Judge([]results.Envelope{short, cut, probe}); j.Verdict != results.Incomplete {
		t.Errorf("nothing judged: %+v", j)
	}
}

func TestG14_LabOnly_FaraVerdict(t *testing.T) {
	for name, edit := range map[string]func(*results.Envelope){
		"lab":        func(e *results.Envelope) { e.MI5.Lab = true },
		"test seams": func(e *results.Envelope) { e.TestSeams = true },
	} {
		if j := judgeOne(edit); j.Verdict != results.Incomplete {
			t.Errorf("%s alone: %+v", name, j)
		}
	}
}

func TestG14_UploadsDuringBlock_FaraVerdict(t *testing.T) {
	for name, edit := range map[string]func(*results.Envelope){
		"a verified upload": func(e *results.Envelope) { e.MI5.Dests[0].VerifiedDuringBlock = 1 },
		"no failure":        func(e *results.Envelope) { e.MI5.Dests[0].FailedDuringBlock = 0 },
		"rate limits only": func(e *results.Envelope) {
			e.MI5.Dests[0].FailedDuringBlock, e.MI5.Dests[0].RateLimitedDuringBlock = 0, 5
		},
	} {
		j := judgeOne(edit)
		if j.Verdict != results.Incomplete || !strings.Contains(strings.Join(j.Reasons, "\n"), "blocarea nu a oprit urcarea hub-ului") {
			t.Errorf("%s: %+v", name, j)
		}
	}
}
