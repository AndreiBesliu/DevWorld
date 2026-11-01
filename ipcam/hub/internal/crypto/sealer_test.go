package crypto

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// sealerBlob is the file of the windows job's S4U round trip (P49):
// TestSealer_SealToFile writes a blob there in a normal step, and
// TestSealer_UnsealFromFile, run inside an S4U task, unseals it.
var sealerBlob = flag.String("sealer-blob", "", "the sealed blob that TestSealer_SealToFile writes and TestSealer_UnsealFromFile reads")

// The round trip's label and plaintext, the plaintext assembled at run time
// so that no committed string is a secret's (P46).
const roundTripLabel = "oauth:round-trip"

func roundTripPlain() []byte { return []byte("SENT" + "INEL-" + "sealer-round-trip") }

// TestSealer_RoundTrip (P19): a sealed secret unseals under its label, and
// its blob holds no byte run of the plaintext; two seals of one secret
// differ.
func TestSealer_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSealer(dir)
	if err != nil {
		t.Fatal(err)
	}
	plain := roundTripPlain()
	sealed, err := s.Seal(t.Context(), roundTripLabel, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) || bytes.Contains(sealed, plain[4:12]) {
		t.Error("the sealed blob holds the plaintext")
	}
	again, err := s.Seal(t.Context(), roundTripLabel, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(again, sealed) {
		t.Error("two seals of one secret are equal: no fresh randomness")
	}
	for _, blob := range [][]byte{sealed, again} {
		got, err := s.Unseal(t.Context(), roundTripLabel, blob)
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("Unseal = %q, %v; want the plaintext", got, err)
		}
	}
	// Another Sealer of the same data directory, as after a restart.
	other, err := NewSealer(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := other.Unseal(t.Context(), roundTripLabel, sealed); err != nil || !bytes.Equal(got, plain) {
		t.Errorf("a new Sealer's Unseal = %q, %v; want the plaintext", got, err)
	}
}

// TestSealer_LabelBound: a blob opens only under the label it was sealed
// with; under another one, or altered, Unseal returns ErrSealedLabel and no
// plaintext. A blob of no sealer is refused.
func TestSealer_LabelBound(t *testing.T) {
	s, err := NewSealer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal(t.Context(), "oauth:dst-1", roundTripPlain())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Unseal(t.Context(), "oauth:dst-2", sealed); !errors.Is(err, ErrSealedLabel) || got != nil {
		t.Errorf("Unseal under another label = %q, %v; want nil, %v", got, err, ErrSealedLabel)
	}
	altered := bytes.Clone(sealed)
	altered[len(altered)-1] ^= 1
	if got, err := s.Unseal(t.Context(), "oauth:dst-1", altered); !errors.Is(err, ErrSealedLabel) || got != nil {
		t.Errorf("Unseal of an altered blob = %q, %v; want nil, %v", got, err, ErrSealedLabel)
	}
	for _, blob := range [][]byte{nil, {}, {0x7f, 1, 2, 3}} {
		if got, err := s.Unseal(t.Context(), "oauth:dst-1", blob); err == nil || got != nil {
			t.Errorf("Unseal of %x = %q, %v; want an error", blob, got, err)
		}
	}
	for _, label := range []string{""} {
		if _, err := s.Seal(t.Context(), label, roundTripPlain()); err == nil {
			t.Errorf("Seal under label %q succeeded, want an error", label)
		}
	}
	if _, err := s.Seal(t.Context(), "oauth:dst-1", nil); err == nil {
		t.Error("Seal of no bytes succeeded, want an error")
	}
}

// TestSealer_BackendNames (P19, P22): the backend that every envelope names.
func TestSealer_BackendNames(t *testing.T) {
	s, err := NewSealer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := BackendSoftware
	if runtime.GOOS == "windows" {
		want = BackendDPAPIMachine
	}
	if s.Backend() != want || BackendSoftware != "software" || BackendDPAPIMachine != "dpapi-machine" {
		t.Errorf("Backend() = %q, want %q", s.Backend(), want)
	}
}

// TestSealer_SealToFile is the first half of the windows job's S4U round
// trip (P49): run in a normal step, it seals the round trip's secret into
// -sealer-blob. Without -sealer-blob it is skipped.
func TestSealer_SealToFile(t *testing.T) {
	if *sealerBlob == "" {
		t.Skip("no -sealer-blob: the windows job's S4U round trip only")
	}
	s, err := NewSealer(filepath.Dir(*sealerBlob))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal(t.Context(), roundTripLabel, roundTripPlain())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(*sealerBlob, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("sealed %d bytes with %s into %s", len(sealed), s.Backend(), *sealerBlob)
}

// TestSealer_UnsealFromFile is the second half of the S4U round trip: run
// inside an S4U Task Scheduler task, the logon of uploadspike's autostart,
// it unseals -sealer-blob, which a normal logon sealed. Without -sealer-blob
// it is skipped.
func TestSealer_UnsealFromFile(t *testing.T) {
	if *sealerBlob == "" {
		t.Skip("no -sealer-blob: the windows job's S4U round trip only")
	}
	sealed, err := os.ReadFile(*sealerBlob)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSealer(filepath.Dir(*sealerBlob))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Unseal(t.Context(), roundTripLabel, sealed)
	if err != nil || !bytes.Equal(got, roundTripPlain()) {
		t.Fatalf("Unseal = %d bytes, %v; want the round trip's secret", len(got), err)
	}
	t.Logf("unsealed %d bytes with %s", len(got), s.Backend())
}

// TestSealer_SoftwareKeyIs0600 (P19, Linux): the software backend's key is
// <dataDir>/state/keys/sealer.key, 32 bytes that the first Seal creates
// with mode 0600 in a 0700 directory; Unseal never creates it; and a key
// that its group or others may read is refused, as is one of another size.
func TestSealer_SoftwareKeyIs0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the software backend is the sealer of every system but Windows")
	}
	dataDir := t.TempDir()
	keys := filepath.Join(dataDir, "state", "keys")
	key := filepath.Join(keys, "sealer.key")
	s, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unseal(t.Context(), roundTripLabel, []byte{1, 2, 3}); err == nil {
		t.Error("Unseal with no key succeeded")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Unseal made state/ (%v): only Seal creates the key", err)
	}
	sealed, err := s.Seal(t.Context(), roundTripLabel, roundTripPlain())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(key)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Fatalf("the key: %v, %v; want 32 bytes, mode 0600", info, err)
	}
	if info, err := os.Stat(keys); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("state/keys: %v, %v; want mode 0700", info, err)
	}
	if entries, err := os.ReadDir(keys); err != nil || len(entries) != 1 {
		t.Errorf("state/keys holds %v (%v), want the key alone", entries, err)
	}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604} {
		if err := os.Chmod(key, mode); err != nil {
			t.Fatal(err)
		}
		fresh, err := NewSealer(dataDir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fresh.Unseal(t.Context(), roundTripLabel, sealed); err == nil || errors.Is(err, ErrSealedLabel) {
			t.Errorf("a %o key: Unseal = %v, want the key refused", mode, err)
		}
		if _, err := fresh.Seal(t.Context(), roundTripLabel, roundTripPlain()); err == nil {
			t.Errorf("a %o key: Seal succeeded, want the key refused", mode)
		}
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, make([]byte, 31), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Seal(t.Context(), roundTripLabel, roundTripPlain()); err == nil {
		t.Error("a 31-byte key: Seal succeeded, want the key refused")
	}
}

// TestSealer_ConcurrentFirstSealsShareOneKey: two Sealers of one data
// directory that seal at once, as two processes may, end with one key, and
// each opens the other's blob.
func TestSealer_ConcurrentFirstSealsShareOneKey(t *testing.T) {
	dataDir := t.TempDir()
	const n = 8
	blobs := make([][]byte, n)
	errs := make(chan error, n)
	for i := range n {
		go func() {
			s, err := NewSealer(dataDir)
			if err == nil {
				blobs[i], err = s.Seal(t.Context(), roundTripLabel, roundTripPlain())
			}
			errs <- err
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, blob := range blobs {
		if got, err := s.Unseal(t.Context(), roundTripLabel, blob); err != nil || !bytes.Equal(got, roundTripPlain()) {
			t.Errorf("blob %d: Unseal = %v; want every blob to open with the one key", i, err)
		}
	}
}
