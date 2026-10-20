package crypto_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	agetest "c2sp.org/CCTV/age"
	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

var updateCCTV = flag.Bool("update-cctv", false, "copy the vectors of the pinned c2sp.org/CCTV/age into testdata/cctv")

const (
	cctvModule = "c2sp.org/CCTV/age"
	cctvDir    = "testdata/cctv"
	// cctvLicense is the module file that holds the vectors' license: the
	// module has no LICENSE file, and README.md's "License" section puts the
	// vectors under 0BSD, CC0 1.0 or the Unlicense.
	cctvLicense = "README.md"
)

// TestRecording_CCTVVectors is C21: every C2SP CCTV age vector behaves
// through OpenReaderAt, with the vector's identities, as the vector expects.
// The vectors also live in testdata/cctv for the TS tests (Task 10a): with
// -update-cctv this test copies them there, and without it checks that the
// copies are the module's bytes.
func TestRecording_CCTVVectors(t *testing.T) {
	entries, err := fs.ReadDir(agetest.Vectors, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the CCTV module has no vectors")
	}
	if *updateCCTV {
		writeCCTVCopies(t, entries)
	}
	checkCCTVCopies(t, entries)

	for _, e := range entries {
		contents, err := fs.ReadFile(agetest.Vectors, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			checkCCTVVector(t, parseCCTVVector(t, contents))
		})
	}
}

// cctvVector is a parsed vector, in the format of the module's README.
type cctvVector struct {
	expect      string
	payloadHash []byte
	identities  []age.Identity
	armored     bool
	file        []byte
}

func parseCCTVVector(t *testing.T, contents []byte) cctvVector {
	t.Helper()
	var v cctvVector
	compressed := false
	rest := contents
	for {
		line, after, ok := bytes.Cut(rest, []byte("\n"))
		if !ok {
			t.Fatal("the vector has no payload")
		}
		rest = after
		if len(line) == 0 {
			break
		}
		key, value, _ := strings.Cut(string(line), ": ")
		switch key {
		case "expect":
			v.expect = value
		case "payload":
			h, err := hex.DecodeString(value)
			if err != nil {
				t.Fatal(err)
			}
			v.payloadHash = h
		case "identity":
			id, err := parseCCTVIdentity(value)
			if err != nil {
				t.Fatalf("identity: %v", err)
			}
			v.identities = append(v.identities, id)
		case "passphrase":
			id, err := age.NewScryptIdentity(value)
			if err != nil {
				t.Fatal(err)
			}
			v.identities = append(v.identities, id)
		case "armored":
			v.armored = true
		case "compressed":
			if value != "zlib" {
				t.Fatalf("unknown compression %q", value)
			}
			compressed = true
		case "file key", "comment":
		default:
			t.Fatalf("unknown header key %q", key)
		}
	}
	v.file = rest
	if compressed {
		r, err := zlib.NewReader(bytes.NewReader(rest))
		if err != nil {
			t.Fatal(err)
		}
		if v.file, err = io.ReadAll(r); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

// parseCCTVIdentity parses an X25519 or a hybrid identity.
func parseCCTVIdentity(s string) (age.Identity, error) {
	if id, err := age.ParseX25519Identity(s); err == nil {
		return id, nil
	}
	id, err := age.ParseHybridIdentity(s)
	if err != nil {
		return nil, err
	}
	return id, nil
}

// checkCCTVVector opens the vector with OpenReaderAt and reads all of it. An
// armored vector is first decoded with filippo.io/age/armor, because
// recordings are binary age files (§10.2).
func checkCCTVVector(t *testing.T, v cctvVector) {
	t.Helper()
	switch v.expect {
	case "success", "payload failure", "header failure", "HMAC failure", "no match", "armor failure":
	default:
		t.Fatalf("unknown expect value %q", v.expect)
	}
	file := v.file
	if v.armored {
		decoded, err := io.ReadAll(armor.NewReader(bytes.NewReader(file)))
		if v.expect == "armor failure" {
			if err == nil {
				t.Fatal("expected an armor failure, the armor decoded")
			}
			t.Log(err)
			return
		}
		if err != nil {
			t.Fatalf("expected %s, got an armor failure: %v", v.expect, err)
		}
		file = decoded
	}

	r, size, err := crypto.NewRecording(v.identities...).OpenReaderAt(t.Context(), bytes.NewReader(file), int64(len(file)))
	if v.expect == "success" {
		if err != nil {
			t.Fatalf("expected success, OpenReaderAt: %v", err)
		}
		plaintext, err := io.ReadAll(io.NewSectionReader(r, 0, size))
		if err != nil {
			t.Fatalf("expected success, read: %v", err)
		}
		if sum := sha256.Sum256(plaintext); !bytes.Equal(sum[:], v.payloadHash) {
			t.Errorf("payload SHA-256 %x, the vector has %x", sum, v.payloadHash)
		}
		return
	}
	if err != nil {
		t.Logf("expected %s, OpenReaderAt: %v", v.expect, err)
		return
	}
	if _, err := io.ReadAll(io.NewSectionReader(r, 0, size)); err == nil {
		t.Fatalf("expected %s, OpenReaderAt and every read succeeded", v.expect)
	} else {
		t.Logf("expected %s, read: %v", v.expect, err)
	}
}

// cctvSource is the content of testdata/cctv/SOURCE.
func cctvSource(version string) string {
	return fmt.Sprintf(`module: %s
version: %s
vectors: vectors/ holds every file of the module's testdata/, unchanged
license: %s, from the module root; its "License" section puts the vectors under
  0BSD, CC0 1.0 or the Unlicense. The module has no LICENSE file.
update: go test ./hub/internal/crypto -run TestRecording_CCTVVectors -update-cctv
`, cctvModule, version, cctvLicense)
}

// cctvModuleInfo returns the version and directory of the pinned CCTV module.
func cctvModuleInfo(t *testing.T) (version, dir string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-json", cctvModule).Output()
	if err != nil {
		t.Fatalf("go list -m %s: %v", cctvModule, err)
	}
	var m struct{ Version, Dir string }
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("go list -m %s: %v", cctvModule, err)
	}
	if m.Version == "" || m.Dir == "" {
		t.Fatalf("go list -m %s gives no version or directory: %s", cctvModule, out)
	}
	return m.Version, m.Dir
}

func writeCCTVCopies(t *testing.T, entries []fs.DirEntry) {
	t.Helper()
	version, dir := cctvModuleInfo(t)
	if err := os.RemoveAll(cctvDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cctvDir, "vectors"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		contents, err := fs.ReadFile(agetest.Vectors, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cctvDir, "vectors", e.Name()), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	license, err := os.ReadFile(filepath.Join(dir, cctvLicense)) //nolint:gosec // the module cache directory of a pinned module
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cctvDir, cctvLicense), license, 0o600); err != nil { //nolint:gosec // a fixed path under testdata
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cctvDir, "SOURCE"), []byte(cctvSource(version)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("copied %d vectors of %s %s", len(entries), cctvModule, version)
}

func checkCCTVCopies(t *testing.T, entries []fs.DirEntry) {
	t.Helper()
	version, dir := cctvModuleInfo(t)
	const hint = " (run the test with -update-cctv)"

	copies, err := os.ReadDir(filepath.Join(cctvDir, "vectors"))
	if err != nil {
		t.Fatalf("read the copies%s: %v", hint, err)
	}
	names := func(es []fs.DirEntry) []string {
		var ns []string
		for _, e := range es {
			ns = append(ns, e.Name())
		}
		return ns
	}
	if got, want := names(copies), names(entries); !slices.Equal(got, want) {
		t.Errorf("testdata/cctv/vectors holds %d files, the module %d%s", len(got), len(want), hint)
	}
	for _, e := range entries {
		want, err := fs.ReadFile(agetest.Vectors, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(cctvDir, "vectors", e.Name()))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("the copy of %s differs from the module's%s", e.Name(), hint)
		}
	}
	license, err := os.ReadFile(filepath.Join(dir, cctvLicense)) //nolint:gosec // the module cache directory of a pinned module
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(cctvDir, cctvLicense)); err != nil || !bytes.Equal(got, license) {
		t.Errorf("the copy of %s differs from the module's%s", cctvLicense, hint)
	}
	if got, err := os.ReadFile(filepath.Join(cctvDir, "SOURCE")); err != nil || string(got) != cctvSource(version) {
		t.Errorf("SOURCE does not name %s %s%s", cctvModule, version, hint)
	}
}
