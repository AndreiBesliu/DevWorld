package mi1

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// annexB writes an Annex B file of the NAL units and returns its path.
func annexB(t *testing.T, nalus ...[]byte) string {
	t.Helper()
	var b bytes.Buffer
	for _, n := range nalus {
		b.Write([]byte{0, 0, 0, 1})
		b.Write(n)
	}
	path := filepath.Join(t.TempDir(), "media.h264")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fmp4Like is a clear recording's bytes: ftyp with its brand, a moof whose
// first child is its mfhd, and an mdat that holds the access unit's NAL
// units, each length-prefixed as MediaMTX writes a sample.
func fmp4Like(au ...[]byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0, 0, 24})
	b.WriteString("ftypmp42")
	b.Write(make([]byte, 12))
	b.Write([]byte{0, 0, 0, 40})
	b.WriteString("moof")
	b.Write([]byte{0, 0, 0, 16})
	b.WriteString("mfhd")
	b.Write(make([]byte, 24))
	b.Write([]byte{0, 0, 1, 0})
	b.WriteString("mdat")
	for _, nalu := range au {
		b.Write([]byte{0, 0, 0, byte(len(nalu))}) //nolint:gosec // test NAL units under 256 bytes
		b.Write(nalu)
	}
	return b.Bytes()
}

// framed is the NAL unit as an fMP4 sample holds it (a 4-byte length, as
// MediaMTX's recorder writes every NAL unit) and as Annex B does (a 4-byte
// start code), each through mediacommon's own marshalling.
func framed(t *testing.T, nalu []byte) (avcc, annexB []byte) {
	t.Helper()
	avcc, err := h264.AVCC{nalu}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	annexB, err = h264.AnnexB{nalu}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return avcc, annexB
}

// TestHub04_ScanFindsNeedles (G08, HUB-04): the needles are ftyp with its
// brand, a moof with its mfhd, and the first 16 bytes of each SPS, PPS and
// IDR of the media; a NAL unit under 8 bytes, as x264's 4-byte PPS, is
// searched whole in its two framings on disk, 4 bytes longer each, because
// alone it would match ciphertext by chance. A clear file with an IDR
// prefix is found, and so is each structural needle alone and the PPS
// alone; an .age file of the same content is clean, and so is a clear
// stream's folder that the scan skips.
func TestHub04_ScanFindsNeedles(t *testing.T) {
	sps := []byte{0x67, 0x64, 0x00, 0x28, 0xac, 0xd9, 0x40, 0x78, 0x02, 0x27, 0xe5, 0xc0, 0x44, 0x00, 0x00, 0x03, 0x00, 0x04}
	pps := []byte{0x68, 0xee, 0x3c, 0xb0} // 4 bytes, as x264's
	idr := append([]byte{0x65, 0x88, 0x84, 0x00, 0x33, 0xff}, bytes.Repeat([]byte{0x5a, 0xc3}, 20)...)
	other := append([]byte{0x41, 0x9a, 0x02}, bytes.Repeat([]byte{0x11}, 30)...)
	needles, err := MediaNeedles(annexB(t, sps, pps, idr, other, sps, pps, idr))
	if err != nil {
		t.Fatal(err)
	}
	ppsSample, ppsAnnexB := framed(t, pps)
	want := [][]byte{sps[:16], ppsSample, ppsAnnexB, idr[:16]}
	if !slices.EqualFunc(needles, want, bytes.Equal) {
		t.Errorf("MediaNeedles = %x, want the SPS's and the IDR's first 16 bytes and the PPS in its two framings, each once: %x", needles, want)
	}

	recording := fmp4Like(sps, pps, idr)
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var sealed bytes.Buffer
	w, err := age.Encrypt(&sealed, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(recording); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	write := func(t *testing.T, root, rel string, data []byte) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"a clear file with an IDR prefix":  {append([]byte("garbage before "), idr[:16]...), "media"},
		"a PPS as an fMP4 sample holds it": {append(append([]byte("garbage before "), ppsSample...), "and after"...), "media"},
		"a PPS in Annex B":                 {append(append([]byte("garbage before "), ppsAnnexB...), "and after"...), "media"},
		"ftyp with its brand":              {[]byte("....ftypmp42...."), "ftyp"},
		"a moof and its mfhd":              {fmp4Like()[24:56], "moof/mfhd"},
		"a whole clear recording":          {recording, "ftyp"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "rec/cam01/main/2026-10-25/120000_000000.mp4.age", sealed.Bytes())
			write(t, root, "tmp/leftover", tc.data)
			found, err := ScanHub04(root, nil, needles)
			if err != nil {
				t.Fatal(err)
			}
			if len(found) == 0 || found[0].Path != filepath.Join("tmp", "leftover") || found[0].What != tc.want {
				t.Errorf("ScanHub04 = %+v, want %s in tmp/leftover", found, tc.want)
			}
		})
	}

	t.Run("an .age file of the same content", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "rec/cam01/main/2026-10-25/120000_000000.mp4.age", sealed.Bytes())
		write(t, root, "rec/cam02/main/2026-10-25/120000_000000.mp4", recording)
		found, err := ScanHub04(root, []string{filepath.Join("rec", "cam02", "main")}, needles)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 0 {
			t.Errorf("ScanHub04 = %+v, want nothing: the .age file is ciphertext and cam02 is a clear stream", found)
		}
		if !bytes.Contains(recording, ppsSample) {
			t.Fatal("the clear recording lacks its PPS sample: the test proves nothing")
		}
		if slices.ContainsFunc(needles, func(n []byte) bool { return bytes.Contains(sealed.Bytes(), n) }) || strings.Contains(sealed.String(), "ftyp") {
			t.Fatal("the ciphertext holds a needle: the test proves nothing")
		}
	})

	t.Run("a needle across a read boundary", func(t *testing.T) {
		root := t.TempDir()
		data := make([]byte, scanChunk+64)
		copy(data[scanChunk-5:], idr[:16])
		write(t, root, "state/run/blob", data)
		found, err := ScanHub04(root, nil, needles)
		if err != nil || len(found) != 1 || found[0].Offset != scanChunk-5 {
			t.Errorf("ScanHub04 = %+v, %v; want the IDR at offset %d", found, err, scanChunk-5)
		}
	})
}

// TestHub04_ShortNALsAreSearchedFramed (G08, HUB-04): no SPS, PPS or IDR
// of the media is left out of the scan. One under 8 bytes is searched only
// in its framings, 8 bytes or more each: its bare bytes, as random
// ciphertext holds a 4-byte string about once in 4 GiB, are no finding. One
// under 4 bytes, which no framing makes long enough, is an error, and so is
// any needle under 8 bytes that ScanHub04 is given: none is dropped
// silently.
func TestHub04_ShortNALsAreSearchedFramed(t *testing.T) {
	sps := []byte{0x67, 0x4d, 0x40, 0x1f, 0xda, 0x02, 0x80, 0xbf, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x03, 0x00, 0xc8, 0x3c}
	pps5 := []byte{0x68, 0xef, 0x02, 0x1c, 0x80} // x264's PPS in sub-256k.h264
	pps7 := []byte{0x68, 0xef, 0x02, 0x1c, 0x80, 0x11, 0x22}
	idr := append([]byte{0x65, 0x88, 0x84, 0x11}, bytes.Repeat([]byte{0x3c}, 20)...)
	needles, err := MediaNeedles(annexB(t, sps, pps5, idr), annexB(t, sps, pps7, idr))
	if err != nil {
		t.Fatal(err)
	}
	pps5Sample, pps5AnnexB := framed(t, pps5)
	pps7Sample, pps7AnnexB := framed(t, pps7)
	want := [][]byte{sps[:16], pps5Sample, pps5AnnexB, idr[:16], pps7Sample, pps7AnnexB}
	if !slices.EqualFunc(needles, want, bytes.Equal) {
		t.Errorf("MediaNeedles = %x, want %x", needles, want)
	}
	for _, n := range needles {
		if len(n) < minNeedle {
			t.Errorf("needle %x is %d bytes, under %d", n, len(n), minNeedle)
		}
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bare"), append(append([]byte("random bytes "), pps5...), " more"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := ScanHub04(root, nil, needles); err != nil || len(found) != 0 {
		t.Errorf("ScanHub04 of the bare PPS = %+v, %v; want no finding", found, err)
	}

	if _, err := MediaNeedles(annexB(t, sps, []byte{0x68, 0xce, 0x38}, idr)); err == nil {
		t.Error("MediaNeedles of a 3-byte PPS: no error, want one")
	}
	if _, err := ScanHub04(root, nil, [][]byte{pps5}); err == nil {
		t.Error("ScanHub04 with a 5-byte needle: no error, want one")
	}
}

// TestHub04_DriverSkipsOnlyAClearRunsStreams (G08): the driver's scan of a
// run leaves out the streams' own folders only when the run records in
// clear, where ftyp and the frames belong; in an encrypted run, the same
// bytes there are HUB-04's failure.
func TestHub04_DriverSkipsOnlyAClearRunsStreams(t *testing.T) {
	root := t.TempDir()
	segment := filepath.Join(root, "rec", "cam01", "main", "2026-10-25", "120000_000000.mp4")
	if err := os.MkdirAll(filepath.Dir(segment), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(segment, fmp4Like(), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Driver{}
	for _, encrypted := range []bool{false, true} {
		cur := &CurrentRun{RunID: "pc-kill9-01-1", Encrypted: encrypted, Streams: []string{MainStream}}
		if got := d.hub04(cur, dirVolume(root), nil); got == encrypted {
			t.Errorf("an encrypted run %v: Hub04Clear = %v, want %v", encrypted, got, !encrypted)
		}
	}
}

// dirtyAtStopVolumes give every run a plain folder whose raw scan finds
// clear bytes the first time only: at the first run's stop, before
// ipcam-hub reconciles, as on a device whose clear bytes the reconcile
// overwrote.
type dirtyAtStopVolumes struct {
	Dirs
	scans *atomic.Int32
}

func (v dirtyAtStopVolumes) Prepare(ctx context.Context, runID string, encrypted bool) (Volume, error) {
	vol, err := v.Dirs.Prepare(ctx, runID, encrypted)
	return dirtyAtStop{Volume: vol, scans: v.scans}, err
}

type dirtyAtStop struct {
	Volume
	scans *atomic.Int32
}

func (v dirtyAtStop) ScanRaw([][]byte) ([]Finding, error) {
	if v.scans.Add(1) == 1 {
		return []Finding{{Path: "imagine", Offset: 4096, What: "media"}}, nil
	}
	return nil, nil
}

// TestHub04_RawScanAtTheStopCounts (G08): a run on an image of its own is
// scanned raw right after its stop, before ipcam-hub starts again and
// reconciles, and again after the reconcile; clear bytes that only the
// first scan finds fail HUB-04 all the same, and the owner's screen names
// them.
func TestHub04_RawScanAtTheStopCounts(t *testing.T) {
	r := newRig(t, "lab-linux")
	d := r.driver(t)
	scans := &atomic.Int32{}
	d.Volumes = dirtyAtStopVolumes{Dirs: Dirs{Root: d.Work}, scans: scans}
	if err := d.RunSet(t.Context(), SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true}); err != nil {
		t.Fatalf("RunSet: %v\nscreen:\n%s", err, r.out.String())
	}
	reports := r.f.runReports()
	if len(reports) != smokeRuns || reports[0].Hub04Clear || !reports[1].Hub04Clear {
		t.Errorf("reports %+v: want the first run's HUB-04 failed by its scan at the stop, the second clear", reports)
	}
	if got := scans.Load(); got != 2*smokeRuns {
		t.Errorf("%d raw scans, want 2 per run: at the stop and after the reconcile", got)
	}
	if !strings.Contains(r.out.String(), "HUB-04: media în clar în imagine, la 4096") {
		t.Errorf("the screen does not name the finding:\n%s", r.out.String())
	}
}

// watchedVolumes give every run a plain folder whose raw scans call onScan.
type watchedVolumes struct {
	Dirs
	onScan func()
}

func (v watchedVolumes) Prepare(ctx context.Context, runID string, encrypted bool) (Volume, error) {
	vol, err := v.Dirs.Prepare(ctx, runID, encrypted)
	return watchedVolume{Volume: vol, onScan: v.onScan}, err
}

type watchedVolume struct {
	Volume
	onScan func()
}

func (v watchedVolume) ScanRaw(needles [][]byte) ([]Finding, error) {
	v.onScan()
	return v.Volume.ScanRaw(needles)
}

// TestHub04_ScansWithIpcamHubStopped (G08): the scan after the reconcile
// runs once ipcam-hub has stopped, so that no file changes under it and
// none is locked. On Windows, SQLite locks bytes of the catalog's WAL index
// while ipcam-hub writes to it, and a read of those bytes fails: the
// windows job's lab smoke lost a clear run's HUB-04 that way.
func TestHub04_ScansWithIpcamHubStopped(t *testing.T) {
	r := newRig(t, "lab-linux")
	d := r.driver(t)
	var last []string
	d.Volumes = watchedVolumes{Dirs: Dirs{Root: d.Work}, onScan: func() {
		entries := logEntries(t, r.log)
		last = append(last, entries[len(entries)-1])
	}}
	if err := d.RunSet(t.Context(), SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true}); err != nil {
		t.Fatalf("RunSet: %v\nscreen:\n%s", err, r.out.String())
	}
	// Each kill9 run: the scan at the stop, the killed ipcam-hub the last
	// one started; then the scan after the reconcile.
	if want := []string{"hub", "stopped", "hub", "stopped"}; !slices.Equal(last, want) {
		t.Errorf("the fake ipcam-hub's last log entry at each scan: %q, want %q", last, want)
	}
}

// TestResume_Kill9WithoutTheRawScanAtTheStop (C07, G13): a kill9 run's
// ResumeMs is ipcam-hub's resume alone, from the kill to the restarted
// child's last `recording` event. The raw scan of the run's own device runs
// between the kill and the restart, before ipcam-hub reconciles (G08), and
// on the Linux lab's encrypted runs it reads a whole 1 GiB image: that time
// is the harness's, and ResumeMs leaves it out.
func TestResume_Kill9WithoutTheRawScanAtTheStop(t *testing.T) {
	const scanTime = 3 * time.Second
	r := newRig(t, "lab-linux")
	d := r.driver(t)
	atStop := &atomic.Int32{}
	d.Volumes = watchedVolumes{Dirs: Dirs{Root: d.Work}, onScan: func() {
		// The scan at the stop follows the kill of ipcam-hub, whose last
		// log entry is still "hub"; the one after the reconcile follows its
		// clean stop (TestHub04_ScansWithIpcamHubStopped).
		if entries := logEntries(t, r.log); entries[len(entries)-1] == "hub" {
			atStop.Add(1)
			time.Sleep(scanTime)
		}
	}}
	if err := d.RunSet(t.Context(), SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true}); err != nil {
		t.Fatalf("RunSet: %v\nscreen:\n%s", err, r.out.String())
	}
	if got := atStop.Load(); got != smokeRuns {
		t.Errorf("%d raw scans at the stop, want one per run", got)
	}
	reports := r.f.runReports()
	if len(reports) != smokeRuns {
		t.Fatalf("%d reports, want %d", len(reports), smokeRuns)
	}
	for _, rep := range reports {
		if rep.ResumeMs == nil {
			t.Errorf("run %s: no ResumeMs", rep.RunID)
			continue
		}
		// The restarted fake ipcam-hub records fakeRecordingAfter after its
		// ready, and it starts only once the scan has ended.
		if ms := *rep.ResumeMs; ms < fakeRecordingAfter.Milliseconds() || ms >= scanTime.Milliseconds() {
			t.Errorf("run %s: ResumeMs = %d ms, want at least %v (the fake's own resume) and under the %v scan at the stop",
				rep.RunID, ms, fakeRecordingAfter, scanTime)
		}
		t.Logf("run %s: ResumeMs %d ms after a %v scan at the stop", rep.RunID, *rep.ResumeMs, scanTime)
	}
}

// TestHub04_ScanRawReadsTheWholeDevice (G08): the raw scan of a run's own
// image, a plain file standing in for the device, finds a needle in its
// freed blocks anywhere, across the scan's chunks too, and names the
// device; a zero-filled image is clean.
func TestHub04_ScanRawReadsTheWholeDevice(t *testing.T) {
	needle := []byte{0x65, 0x88, 0x84, 0x11, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c}
	device := filepath.Join(t.TempDir(), "loop")
	if err := os.WriteFile(device, make([]byte, 3*scanChunk), 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := ScanRaw(device, "imagine", [][]byte{needle}); err != nil || len(found) != 0 {
		t.Fatalf("ScanRaw of zeros = %+v, %v; want clean", found, err)
	}
	f, err := os.OpenFile(device, os.O_WRONLY, 0) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	at := int64(2*scanChunk - 5) // across two chunks
	if _, err := f.WriteAt(needle, at); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	found, err := ScanRaw(device, "imagine", [][]byte{needle})
	if err != nil || len(found) != 1 || found[0] != (Finding{Path: "imagine", Offset: at, What: "media"}) {
		t.Errorf("ScanRaw = %+v, %v; want the needle at %d in imagine", found, err, at)
	}
	if _, err := ScanRaw(device, "imagine", [][]byte{needle[:4]}); err == nil {
		t.Error("ScanRaw with a 4-byte needle: no error, want one")
	}
}

// BenchmarkScanHub04 is the scan's speed on random bytes, as ciphertext,
// and on zeros, as a fresh image's free space: the framed needles begin
// with zeros, so the prefilter looks past them (prefilterAt). With
// MEDIAGEN_DIR it also scans random bytes for the real needle set, every
// needle of the test media as reccontroller sends them to the host (about
// 144), which is what a run's HUB-04 scan searches.
func BenchmarkScanHub04(b *testing.B) {
	sps := []byte{0x67, 0x4d, 0x40, 0x1f, 0xda, 0x02, 0x80, 0xbf, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x03, 0x00, 0xc8, 0x3c}
	pps := []byte{0x68, 0xef, 0x07, 0x72}
	idr := append([]byte{0x65, 0x88, 0x84, 0x11}, bytes.Repeat([]byte{0x3c}, 20)...)
	var media bytes.Buffer
	for _, n := range [][]byte{sps, pps, idr} {
		media.Write([]byte{0, 0, 0, 1})
		media.Write(n)
	}
	path := filepath.Join(b.TempDir(), "media.h264")
	if err := os.WriteFile(path, media.Bytes(), 0o600); err != nil {
		b.Fatal(err)
	}
	needles, err := MediaNeedles(path)
	if err != nil {
		b.Fatal(err)
	}
	random := make([]byte, 16<<20)
	if _, err := rand.Read(random); err != nil {
		b.Fatal(err)
	}
	type scan struct {
		name    string
		data    []byte
		needles [][]byte
	}
	scans := []scan{{"random", random, needles}, {"zeros", make([]byte, 16<<20), needles}}
	if dir := os.Getenv("MEDIAGEN_DIR"); dir != "" {
		media, err := MediaNeedles(filepath.Join(dir, "main.h264"), filepath.Join(dir, "sub.h264"), filepath.Join(dir, "sub-256k.h264"))
		if err != nil {
			b.Fatal(err)
		}
		b.Logf("the test media's needles: %d", len(media))
		scans = append(scans, scan{"random-real-needles", random, media})
	}
	for _, tc := range scans {
		root := b.TempDir()
		if err := os.WriteFile(filepath.Join(root, "blob"), tc.data, 0o600); err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.data)))
			for b.Loop() {
				if found, err := ScanHub04(root, nil, tc.needles); err != nil || len(found) != 0 {
					b.Fatalf("ScanHub04 = %+v, %v", found, err)
				}
			}
		})
	}
}
