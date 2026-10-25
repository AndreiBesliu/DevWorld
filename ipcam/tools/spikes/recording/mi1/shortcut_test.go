package mi1

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// lnkHeader is the 76-byte header of a .lnk file (MS-SHLLINK 2.1) with the
// given LinkFlags, followed by rest.
func lnkHeader(flags uint32, rest []byte) []byte {
	b := make([]byte, 0x4c, 0x4c+len(rest))
	binary.LittleEndian.PutUint32(b, 0x4c)
	copy(b[4:], []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46})
	binary.LittleEndian.PutUint32(b[0x14:], flags)
	for i := 0x18; i < 0x4c; i++ {
		b[i] = byte(i)
	}
	return append(b, rest...)
}

// TestShortcut_RunsAsAdministrator (G14): the real cuts' shortcut starts
// recspike elevated, as its «Executare ca administrator» box does: its
// RunAsUser flag (0x2000) is set and every other byte of the shortcut is
// kept; a file that is not a shell link is refused, unchanged.
func TestShortcut_RunsAsAdministrator(t *testing.T) {
	const hasArgumentsUnicode = 0x00000020 | 0x00000080 | 0x00000001
	path := filepath.Join(t.TempDir(), "Proba.lnk")
	before := lnkHeader(hasArgumentsUnicode, []byte("the id list and strings"))
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RunAsAdministrator(path); err != nil {
		t.Fatalf("RunAsAdministrator: %v", err)
	}
	after, err := os.ReadFile(path) //nolint:gosec // the test's file
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(after[0x14:]); got != hasArgumentsUnicode|0x2000 {
		t.Errorf("LinkFlags = %#x, want %#x", got, hasArgumentsUnicode|0x2000)
	}
	want := bytes.Clone(before)
	binary.LittleEndian.PutUint32(want[0x14:], hasArgumentsUnicode|0x2000)
	if !bytes.Equal(after, want) {
		t.Errorf("the shortcut changed beyond its flags:\n% x\nwant\n% x", after, want)
	}

	for name, data := range map[string][]byte{
		"too short":   before[:0x20],
		"wrong size":  append([]byte{0x4d, 0, 0, 0}, before[4:]...),
		"wrong CLSID": append(bytes.Clone(before[:4]), append(make([]byte, 16), before[20:]...)...),
	} {
		p := filepath.Join(t.TempDir(), "x.lnk")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RunAsAdministrator(p); err == nil {
			t.Errorf("%s: RunAsAdministrator = nil, want a refusal", name)
		}
		if got, _ := os.ReadFile(p); !bytes.Equal(got, data) { //nolint:gosec // the test's file
			t.Errorf("%s: the file was changed", name)
		}
	}
}
