//go:build !windows

package crypto

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// sealedIn seals the round trip's secret in a new data directory and
// returns the directory, its key's path and the blob.
func sealedIn(t *testing.T) (dataDir, key string, sealed []byte) {
	t.Helper()
	dataDir = t.TempDir()
	s, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if sealed, err = s.Seal(t.Context(), roundTripLabel, roundTripPlain()); err != nil {
		t.Fatal(err)
	}
	return dataDir, filepath.Join(dataDir, "state", "keys", "sealer.key"), sealed
}

// unsealWithin unseals sealed with a new sealer of dataDir and fails the
// test when the call does not return within 2 s.
func unsealWithin(t *testing.T, dataDir string, sealed []byte) ([]byte, error) {
	t.Helper()
	s, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		plain []byte
		err   error
	}
	done := make(chan result, 1)
	go func() {
		plain, err := s.Unseal(t.Context(), roundTripLabel, sealed)
		done <- result{plain, err}
	}()
	select {
	case r := <-done:
		return r.plain, r.err
	case <-time.After(2 * time.Second):
		t.Fatal("Unseal blocked for 2 s")
		return nil, nil
	}
}

// TestSealer_KeyPathFollowsNoLinkAndNeverBlocks (security review SEAL-1):
// root's diag commands read the key of a data directory that the hub's
// identity owns, so the key is opened without following a link on state/,
// state/keys or the key itself, and without blocking on a FIFO; state/ and
// state/keys must not be writable by their group or others; and none of
// these refusals reports the mode or the size of what it found.
func TestSealer_KeyPathFollowsNoLinkAndNeverBlocks(t *testing.T) {
	refused := func(t *testing.T, dataDir string, sealed []byte, what string) {
		t.Helper()
		plain, err := unsealWithin(t, dataDir, sealed)
		switch {
		case err == nil || plain != nil:
			t.Errorf("%s: Unseal = %d bytes, %v; want the key refused", what, len(plain), err)
		case !errors.Is(err, ErrKeyUnavailable) || errors.Is(err, ErrSealedLabel):
			t.Errorf("%s: Unseal = %v; want ErrKeyUnavailable, not ErrSealedLabel", what, err)
		}
	}
	t.Run("a FIFO", func(t *testing.T) {
		dataDir, key, sealed := sealedIn(t)
		if err := os.Remove(key); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(key, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			// Free a reader that the sealer left blocked in open(2).
			if f, err := os.OpenFile(key, os.O_WRONLY|unix.O_NONBLOCK, 0); err == nil { //nolint:gosec // G304: the test's FIFO
				_ = f.Close()
			}
		})
		refused(t, dataDir, sealed, "a FIFO in the key's place")
	})
	t.Run("a link as the key", func(t *testing.T) {
		dataDir, key, sealed := sealedIn(t)
		elsewhere := filepath.Join(t.TempDir(), "elsewhere.key")
		b, err := os.ReadFile(key) //nolint:gosec // G304: the test's key
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(elsewhere, b, 0o600); err != nil { //nolint:gosec // G703: the test's temporary directory
			t.Fatal(err)
		}
		if err := os.Remove(key); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, key); err != nil {
			t.Fatal(err)
		}
		refused(t, dataDir, sealed, "a link to a valid key")
	})
	t.Run("a link to a device", func(t *testing.T) {
		dataDir, key, sealed := sealedIn(t)
		if err := os.Remove(key); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/null", key); err != nil {
			t.Fatal(err)
		}
		refused(t, dataDir, sealed, "a link to /dev/null")
		if _, err := unsealWithin(t, dataDir, sealed); err != nil && strings.Contains(err.Error(), "mode") {
			t.Errorf("the refusal %q reports the mode of the link's target", err)
		}
	})
	for _, dir := range []string{"state", filepath.Join("state", "keys")} {
		t.Run("a link as "+dir, func(t *testing.T) {
			dataDir, _, sealed := sealedIn(t)
			other := filepath.Join(t.TempDir(), "elsewhere")
			if err := os.Rename(filepath.Join(dataDir, dir), other); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, filepath.Join(dataDir, dir)); err != nil {
				t.Fatal(err)
			}
			refused(t, dataDir, sealed, "a link as "+dir)
		})
		t.Run(dir+" writable by others", func(t *testing.T) {
			dataDir, _, sealed := sealedIn(t)
			path := filepath.Join(dataDir, dir)
			if err := os.Chmod(path, 0o777); err != nil { //nolint:gosec // G302: the mode under test
				t.Fatal(err)
			}
			refused(t, dataDir, sealed, dir+" with mode 777")
			if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // G302: a directory, its owner's alone
				t.Fatal(err)
			}
			if plain, err := unsealWithin(t, dataDir, sealed); err != nil || !bytes.Equal(plain, roundTripPlain()) {
				t.Errorf("%s back at 0700: Unseal = %v; want the plaintext", dir, err)
			}
		})
	}
}

// TestSealer_KeyOwner (security review SEAL-1): the key must be the
// process's own file; root, which reads the key of the hub's identity,
// takes the key of the owner of state/keys and no other.
func TestSealer_KeyOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can give the key to another user")
	}
	const other = 54321
	dataDir, key, sealed := sealedIn(t)
	if err := os.Chown(key, other, other); err != nil {
		t.Fatal(err)
	}
	if _, err := unsealWithin(t, dataDir, sealed); !errors.Is(err, ErrKeyUnavailable) {
		t.Errorf("root and a key of uid %d in its own state/keys: Unseal = %v; want ErrKeyUnavailable", other, err)
	}
	if err := os.Chown(filepath.Dir(key), other, other); err != nil {
		t.Fatal(err)
	}
	if plain, err := unsealWithin(t, dataDir, sealed); err != nil || !bytes.Equal(plain, roundTripPlain()) {
		t.Errorf("root and the key of the owner of state/keys (the hub's layout on the Pi): Unseal = %v; want the plaintext", err)
	}
}

// TestSealer_ErrorKinds (security review SEAL-2): a key that is missing or
// refused is ErrKeyUnavailable, a fault of the sealer, which is never
// ErrSealedLabel; a blob of another backend is ErrSealedLabel; and a done
// ctx fails neither Seal nor Unseal, which are local and finish what they
// start (a refresh token that a provider rotated is sealed even when its
// caller went away).
func TestSealer_ErrorKinds(t *testing.T) {
	dataDir, key, sealed := sealedIn(t)
	if _, err := unsealWithin(t, t.TempDir(), sealed); !errors.Is(err, ErrKeyUnavailable) || errors.Is(err, ErrSealedLabel) {
		t.Errorf("no key: Unseal = %v; want ErrKeyUnavailable", err)
	}
	if err := os.Chmod(key, 0o640); err != nil { //nolint:gosec // G302: the refused mode under test
		t.Fatal(err)
	}
	if _, err := unsealWithin(t, dataDir, sealed); !errors.Is(err, ErrKeyUnavailable) || errors.Is(err, ErrSealedLabel) {
		t.Errorf("a 0640 key: Unseal = %v; want ErrKeyUnavailable", err)
	}
	fresh, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Seal(t.Context(), roundTripLabel, roundTripPlain()); !errors.Is(err, ErrKeyUnavailable) {
		t.Errorf("a 0640 key: Seal = %v; want ErrKeyUnavailable", err)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := unsealWithin(t, dataDir, []byte{blobDPAPI, 1, 2, 3}); !errors.Is(err, ErrSealedLabel) {
		t.Errorf("a blob of another backend: Unseal = %v; want ErrSealedLabel", err)
	}
	done, cancel := context.WithCancel(t.Context())
	cancel()
	s, err := NewSealer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Seal(done, roundTripLabel, roundTripPlain())
	if err != nil {
		t.Fatalf("Seal with a done ctx = %v; want the blob", err)
	}
	if plain, err := s.Unseal(done, roundTripLabel, again); err != nil || !bytes.Equal(plain, roundTripPlain()) {
		t.Errorf("Unseal with a done ctx = %v; want the plaintext", err)
	}
}

// TestSealer_KeyLeftoversRemoved (C2-R16 (e)): the temporary file that a
// cut first key creation leaves in state/keys is removed at the hub's next
// start, beside a whole key; the key stays.
func TestSealer_KeyLeftoversRemoved(t *testing.T) {
	dataDir, key, sealed := sealedIn(t)
	leftover := key + ".123456"
	if err := os.WriteFile(leftover, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveKeyLeftovers(dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(leftover); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the leftover is still there (%v)", err)
	}
	if plain, err := unsealWithin(t, dataDir, sealed); err != nil || !bytes.Equal(plain, roundTripPlain()) {
		t.Errorf("Unseal after the clean-up = %v; want the key kept", err)
	}
	if err := RemoveKeyLeftovers(t.TempDir()); err != nil {
		t.Errorf("RemoveKeyLeftovers of a data directory with no key = %v; want nothing to do", err)
	}
}
