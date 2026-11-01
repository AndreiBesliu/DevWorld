//go:build !windows

package crypto

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// keySize is the software key's size: an XChaCha20-Poly1305 key.
const keySize = chacha20poly1305.KeySize

// softwareSealer is the sealer of every system but Windows (P19): a random
// 32-byte key in <dataDir>/state/keys/sealer.key, mode 0600, which Task 8's
// install gives to the ipcam-spike user, and XChaCha20-Poly1305 with the
// label as the additional data. A blob is blobSoftware ‖ a 24-byte random
// nonce ‖ the ciphertext and its tag.
type softwareSealer struct {
	path string

	mu   sync.Mutex
	aead cipher.AEAD // nil until the key is read
}

func newSealer(dataDir string) (Sealer, error) {
	if dataDir == "" {
		return nil, errors.New("crypto: the sealer needs a data directory")
	}
	return &softwareSealer{path: filepath.Join(dataDir, "state", "keys", "sealer.key")}, nil
}

func (s *softwareSealer) Backend() string { return BackendSoftware }

// Seal seals plain under label, creating the key on the first seal of the
// data directory.
func (s *softwareSealer) Seal(ctx context.Context, label string, plain []byte) ([]byte, error) {
	if err := checkSeal(ctx, label, plain); err != nil {
		return nil, err
	}
	aead, err := s.key(true)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: seal: a nonce: %w", err)
	}
	blob := make([]byte, 0, 1+len(nonce)+len(plain)+aead.Overhead())
	blob = append(append(blob, blobSoftware), nonce...)
	return aead.Seal(blob, nonce, plain, []byte(label)), nil
}

// Unseal opens sealed under label. It never creates the key: without one,
// nothing was sealed on this data directory.
func (s *softwareSealer) Unseal(ctx context.Context, label string, sealed []byte) ([]byte, error) {
	if err := checkUnseal(ctx, label, sealed, blobSoftware); err != nil {
		return nil, err
	}
	aead, err := s.key(false)
	if err != nil {
		return nil, err
	}
	if len(sealed) < 1+aead.NonceSize()+aead.Overhead() {
		return nil, ErrSealedLabel
	}
	nonce, ciphertext := sealed[1:1+aead.NonceSize()], sealed[1+aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, []byte(label))
	if err != nil {
		return nil, ErrSealedLabel
	}
	return plain, nil
}

// key returns the AEAD of the data directory's key, read once, and created
// first when create is set and there is none.
func (s *softwareSealer) key(create bool) (cipher.AEAD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aead != nil {
		return s.aead, nil
	}
	key, err := readKey(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return nil, errors.New("crypto: no sealer key in the data directory: nothing was sealed there")
		}
		key, err = makeKey(s.path)
	}
	if err != nil {
		return nil, err
	}
	defer clear(key) // the AEAD keeps its own copy
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	}
	s.aead = aead
	return aead, nil
}

// readKey reads the key at path: a regular file of keySize bytes that
// neither its group nor others may read or write.
func readKey(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the data directory's key
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	switch {
	case err != nil:
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	case !info.Mode().IsRegular():
		return nil, errors.New("crypto: the sealer key is not a regular file")
	case info.Mode().Perm()&0o077 != 0:
		return nil, fmt.Errorf("crypto: the sealer key is open to its group or others (mode %o); it must be 0600", info.Mode().Perm())
	case info.Size() != keySize:
		return nil, fmt.Errorf("crypto: the sealer key is %d bytes, not %d", info.Size(), keySize)
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(f, key); err != nil {
		clear(key)
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	}
	return key, nil
}

// makeKey creates a random key at path, in its directory made 0700: written
// to a 0600 temporary file, fsynced, then hard-linked to path, which never
// replaces a key that another process made first (that one is read and
// returned instead), and the directory fsynced. A cut leaves either no key
// or a whole one, and at most an unused temporary file.
func makeKey(path string) ([]byte, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("crypto: the sealer key's directory: %w", err)
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: a sealer key: %w", err)
	}
	if err := linkKey(dir, path, key); err != nil {
		clear(key)
		if errors.Is(err, fs.ErrExist) {
			return readKey(path)
		}
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	}
	return key, nil
}

// linkKey writes key to a temporary file in dir and links it to path.
func linkKey(dir, path string, key []byte) error {
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*") // 0600
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(key)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir fsyncs dir, so that a name linked into it survives a power cut.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // G304: the key's directory
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
