package mi1

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

// BenchmarkScanHub04 is the scan's speed on random bytes, as ciphertext,
// and on zeros, as a fresh image's free space: the framed needles begin
// with zeros, so the prefilter looks past them (prefilterAt).
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
	for _, tc := range []struct {
		name string
		data []byte
	}{{"random", random}, {"zeros", make([]byte, 16<<20)}} {
		root := b.TempDir()
		if err := os.WriteFile(filepath.Join(root, "blob"), tc.data, 0o600); err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.data)))
			for b.Loop() {
				if found, err := ScanHub04(root, nil, needles); err != nil || len(found) != 0 {
					b.Fatalf("ScanHub04 = %+v, %v", found, err)
				}
			}
		})
	}
}
