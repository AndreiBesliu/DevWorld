package mi1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// diagRecording is a clear fMP4 recording with one part per span of SEI
// indexes, as MediaMTX writes it.
func diagRecording(t *testing.T, spans ...[2]uint64) []byte {
	t.Helper()
	sps := []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04,
		0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	var out, init seekablebuffer.Buffer
	if err := (fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{SPS: sps, PPS: pps}}}}).Marshal(&init); err != nil {
		t.Fatal(err)
	}
	_, _ = out.Write(init.Bytes())
	for i, span := range spans {
		track := &fmp4.PartTrack{ID: 1, BaseTime: span[0] * 4500}
		for index := span[0]; index <= span[1]; index++ {
			avcc, err := h264.AVCC(controller.StampIndex([][]byte{{0x41, 0x9a}}, index)).Marshal()
			if err != nil {
				t.Fatal(err)
			}
			track.Samples = append(track.Samples, &fmp4.Sample{Duration: 4500, Payload: avcc})
		}
		var part seekablebuffer.Buffer
		if err := (fmp4.Part{SequenceNumber: uint32(i + 1), Tracks: []*fmp4.PartTrack{track}}).Marshal(&part); err != nil { //nolint:gosec // few parts
			t.Fatal(err)
		}
		_, _ = out.Write(part.Bytes())
	}
	return out.Bytes()
}

// diagRun is a lab run whose cam01_main lost 10.85 s: its two segment files
// at the stop, its ipcam-hub log up to the kill and after it, and the trace
// of ipcam-hub's events.
func diagRun(t *testing.T) (*Driver, *CurrentRun, Volume, time.Time) {
	t.Helper()
	d := &Driver{Work: t.TempDir()}
	cur := &CurrentRun{RunID: "lab-linux-kill9-smoke-02-1", Streams: []string{MainStream}, trace: &cutTrace{}, Before: map[string][]string{
		MainStream: {"rec/cam01/main/2026-10-25/225510_000002.mp4.part", "rec/cam01/main/2026-10-25/225500_000001.mp4"},
	}}
	data := filepath.Join(d.runDir(cur.RunID), "data")
	stream := filepath.Join(data, "rec", "cam01", "main", "2026-10-25")
	if err := os.MkdirAll(stream, 0o700); err != nil {
		t.Fatal(err)
	}
	// Reconcile gave the .part its final name.
	for name, rec := range map[string][]byte{
		"225500_000001.mp4": diagRecording(t, [2]uint64{0, 19}),
		"225510_000002.mp4": diagRecording(t, [2]uint64{20, 39}, [2]uint64{40, 59}, [2]uint64{60, 79}, [2]uint64{80, 85}),
	} {
		if err := os.WriteFile(filepath.Join(stream, name), rec, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := strings.Join([]string{
		"mediamtx: 2026/10/25 22:55:00 INF [path cam01/main] [recorder] recording 1 track (H264)",
		"mediamtx: 2026/10/25 22:55:21 WAR [path cam01/main] [recorder] reader is too slow, discarding 405 frames",
		"mediamtx: 2026/10/25 22:55:22 ERR [path cam01/main] [RTSP source] rtsp://u:secret@192.168.1.20:8554/cam01_main: EOF",
		"ipcam-hub: open /home/andrei/proba/work/data/state/catalog.db: disk I/O error",
	}, "\n") + "\n"
	after := "mediamtx: 2026/10/25 22:55:32 WAR after the kill\n"
	if err := os.WriteFile(filepath.Join(d.runDir(cur.RunID), "ipcam-hub.log"), []byte(before+after), 0o600); err != nil {
		t.Fatal(err)
	}
	kill := time.Date(2026, 10, 25, 22, 55, 31, 250e6, time.UTC)
	cur.trace.event(hubproc.Event{Type: "recording", At: kill.Add(-30 * time.Second)})
	cur.trace.event(hubproc.Event{Type: "status", At: kill.Add(-2 * time.Second), Line: json.RawMessage(`{"type":"status","lastPartAgoMs":8100}`)})
	cur.trace.event(hubproc.Event{Type: "status", At: kill.Add(-time.Second), Line: json.RawMessage(`{"type":"status","lastPartAgoMs":9100}`)})
	cur.trace.kill(kill, int64(len(before)))
	cur.trace.event(hubproc.Event{Type: "ready", At: kill.Add(time.Second)}) // the restarted child's
	return d, cur, dirVolume(data), kill
}

// TestLossDiag_ExplainsAStreamOverItsLimit (Task 7.2d): a stream that lost
// more than its limit gets one line, mi1-diag: <json>, with its newest
// segment files and their last parts (names relative to the stream's
// folder, sizes, SEI indexes), ipcam-hub's events up to the kill (the
// newest status last), the warnings and errors that MediaMTX and ipcam-hub
// logged before the kill, redacted (no URL, address or user path, PH0-22),
// and the stop, the last recorded send and the kill, UTC. A stream within
// its limit gets none.
func TestLossDiag_ExplainsAStreamOverItsLimit(t *testing.T) {
	d, cur, vol, kill := diagRun(t)
	stopAt := kill.Add(-20 * time.Millisecond)
	v := link.Verdict{
		Run: results.Run{RunID: cur.RunID, Streams: []results.StreamLoss{{Stream: MainStream, LossMs: 10850, LimitMs: 2000}}},
		Timing: &link.Timing{StopAtMs: stopAt.UnixMilli(), LastSentMs: map[string]int64{
			MainStream: stopAt.Add(-10850 * time.Millisecond).UnixMilli(),
		}},
	}
	lines := d.lossDiags(cur, vol, v, nil)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], DiagPrefix) {
		t.Fatalf("lines = %q, want one %q line", lines, DiagPrefix)
	}
	var got lossDiag
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[0], DiagPrefix)), &got); err != nil {
		t.Fatalf("%v: %s", err, lines[0])
	}
	if got.Code != "mi1-diag" || got.RunID != cur.RunID || got.Stream != MainStream || got.LossMs != 10850 || got.LimitMs != 2000 ||
		got.StopAt != "22:55:31.230" || got.LastSent != "22:55:20.380" || got.Kill != "22:55:31.250" {
		t.Errorf("diag = %+v", got)
	}
	if len(got.Files) != 2 || got.Files[0].Name != "2026-10-25/225500_000001.mp4" || got.Files[1].Name != "2026-10-25/225510_000002.mp4.part" {
		t.Fatalf("files = %+v, want the two files of the stop, oldest first, named as listed", got.Files)
	}
	newest := got.Files[1]
	if info, err := os.Stat(filepath.Join(vol.DataDir(), "rec", "cam01", "main", "2026-10-25", "225510_000002.mp4")); err != nil || newest.Size != info.Size() {
		t.Errorf("newest file size %d, want %v (%v)", newest.Size, info.Size(), err)
	}
	wantParts := []lastindex.Part{{Seq: 2, First: 40, Last: 59}, {Seq: 3, First: 60, Last: 79}, {Seq: 4, First: 80, Last: 85}}
	if len(newest.Parts) != len(wantParts) {
		t.Fatalf("newest file's parts = %+v, want its last %d", newest.Parts, len(wantParts))
	}
	for i, p := range newest.Parts {
		if p.Seq != wantParts[i].Seq || p.First != wantParts[i].First || p.Last != wantParts[i].Last || p.Bytes <= 0 {
			t.Errorf("part %d = %+v, want %+v", i, p, wantParts[i])
		}
	}
	if len(got.Events) != 2 || !strings.HasSuffix(got.Events[0], " recording") || got.Events[1] != "22:55:30.250 status lastPartAgoMs=9100" {
		t.Errorf("events = %q, want the recording and the newest status before the kill", got.Events)
	}
	if len(got.Log) != 3 || !strings.Contains(got.Log[0], "WAR [path cam01/main] [recorder] reader is too slow, discarding 405 frames") ||
		!strings.Contains(lines[0], "[RTSP source] <url> EOF") {
		t.Errorf("log = %q, want the warning, the error and ipcam-hub's line before the kill", got.Log)
	}
	for _, private := range []string{"rtsp://", "secret", "192.168.1.20", "/home/", "andrei", "after the kill", d.Work} {
		if strings.Contains(lines[0], private) {
			t.Errorf("the line holds %q: %s", private, lines[0])
		}
	}

	v.Run.Streams[0].LossMs, v.Timing = 2000, nil
	if lines := d.lossDiags(cur, vol, v, nil); len(lines) != 0 {
		t.Errorf("a stream at its limit got %q, want no line", lines)
	}
}

// TestLossDiag_FitsOneAnnotation (C32): however much was logged, a line is
// at most 3,500 bytes after annotate.sh's escaping (% as %25); the oldest
// log lines and events go first, so the newest of each stay.
func TestLossDiag_FitsOneAnnotation(t *testing.T) {
	d, cur, vol, kill := diagRun(t)
	var log bytes.Buffer
	for i := range 200 {
		fmt.Fprintf(&log, "mediamtx: 2026/10/25 22:55:%02d WAR [path cam01/main] [recorder] 100%% %d %s\n", i%60, i, strings.Repeat("x", 300))
	}
	if err := os.WriteFile(filepath.Join(d.runDir(cur.RunID), "ipcam-hub.log"), log.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cur.trace = &cutTrace{}
	for i := range 50 {
		cur.trace.event(hubproc.Event{Type: "error", At: kill.Add(time.Duration(i-50) * time.Second), Message: strings.Repeat("%", 200)})
	}
	cur.trace.kill(kill, int64(log.Len()))
	v := link.Verdict{Run: results.Run{RunID: cur.RunID, Streams: []results.StreamLoss{{Stream: MainStream, LossMs: 10850, LimitMs: 2000}}}}
	lines := d.lossDiags(cur, vol, v, nil)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	body := strings.TrimPrefix(lines[0], DiagPrefix)
	if n := len(strings.ReplaceAll(body, "%", "%25")); n > maxDiagBytes {
		t.Errorf("the line is %d bytes after escaping, want at most %d", n, maxDiagBytes)
	}
	var got lossDiag
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 2 || len(got.Log) == 0 || !strings.Contains(got.Log[len(got.Log)-1], " 199 ") ||
		len(got.Events) == 0 || !strings.HasPrefix(got.Events[len(got.Events)-1], kill.Add(-time.Second).Format(diagTime)) {
		t.Errorf("diag kept files %d, log %q, events %q; want the files and the newest log lines and events", len(got.Files), got.Log, got.Events)
	}
}
