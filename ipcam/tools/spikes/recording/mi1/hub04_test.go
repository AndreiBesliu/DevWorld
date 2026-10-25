package mi1

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"filippo.io/age"
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
// first child is its mfhd, and an mdat that holds the IDR, length-prefixed.
func fmp4Like(idr []byte) []byte {
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
	b.Write([]byte{0, 0, 0, byte(len(idr))}) //nolint:gosec // a test IDR of 46 bytes
	b.Write(idr)
	return b.Bytes()
}

// TestHub04_ScanFindsNeedles (G08, HUB-04): the needles are ftyp with its
// brand, a moof with its mfhd, and the first 16 bytes of each SPS, PPS and
// IDR of the media, those under 8 bytes left out because they would match
// ciphertext by chance. A clear file with an IDR prefix is found, and so is
// each structural needle alone; an .age file of the same content is clean,
// and so is a clear stream's folder that the scan skips.
func TestHub04_ScanFindsNeedles(t *testing.T) {
	sps := []byte{0x67, 0x64, 0x00, 0x28, 0xac, 0xd9, 0x40, 0x78, 0x02, 0x27, 0xe5, 0xc0, 0x44, 0x00, 0x00, 0x03, 0x00, 0x04}
	pps := []byte{0x68, 0xee, 0x3c, 0xb0} // 4 bytes: too short to search for
	idr := append([]byte{0x65, 0x88, 0x84, 0x00, 0x33, 0xff}, bytes.Repeat([]byte{0x5a, 0xc3}, 20)...)
	other := append([]byte{0x41, 0x9a, 0x02}, bytes.Repeat([]byte{0x11}, 30)...)
	needles, err := MediaNeedles(annexB(t, sps, pps, idr, other, sps, pps, idr))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{sps[:16], idr[:16]}
	if !slices.EqualFunc(needles, want, bytes.Equal) {
		t.Errorf("MediaNeedles = %x, want the SPS's and the IDR's first 16 bytes, each once: %x", needles, want)
	}

	recording := fmp4Like(idr)
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
		"a clear file with an IDR prefix": {append([]byte("garbage before "), idr[:16]...), "media"},
		"ftyp with its brand":             {[]byte("....ftypmp42...."), "ftyp"},
		"a moof and its mfhd":             {fmp4Like(nil)[24:56], "moof/mfhd"},
		"a whole clear recording":         {recording, "ftyp"},
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
		if bytes.Contains(sealed.Bytes(), idr[:16]) || strings.Contains(sealed.String(), "ftyp") {
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
	if err := os.WriteFile(segment, fmp4Like(nil), 0o600); err != nil {
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
