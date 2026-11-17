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
	"golang.org/x/sys/unix"
)

// keySize is the software key's size: an XChaCha20-Poly1305 key.
const keySize = chacha20poly1305.KeySize

// The software key's place under the data directory: state/keys/sealer.key.
const (
	keyStateDir = "state"
	keyDirName  = "keys"
	keyName     = "sealer.key"
)

// softwareSealer is the sealer of every system but Windows (P19): a random
// 32-byte key in <dataDir>/state/keys/sealer.key, mode 0600, which Task 8's
// install gives to the ipcam-spike user, and XChaCha20-Poly1305 with the
// label as the additional data. A blob is blobSoftware ‖ a 24-byte random
// nonce ‖ the ciphertext and its tag.
type softwareSealer struct {
	dataDir string

	mu   sync.Mutex
	aead cipher.AEAD // nil until the key is read
}

func newSealer(dataDir string) (Sealer, error) {
	if dataDir == "" {
		return nil, errors.New("crypto: the sealer needs a data directory")
	}
	return &softwareSealer{dataDir: dataDir}, nil
}

func (s *softwareSealer) Backend() string { return BackendSoftware }

// Seal seals plain under label, creating the key on the first seal of the
// data directory.
func (s *softwareSealer) Seal(_ context.Context, label string, plain []byte) ([]byte, error) {
	if err := checkSeal(label, plain); err != nil {
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
func (s *softwareSealer) Unseal(_ context.Context, label string, sealed []byte) ([]byte, error) {
	if err := checkUnseal(label, sealed, blobSoftware); err != nil {
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
// first when create is set and there is none. Its errors wrap
// ErrKeyUnavailable.
func (s *softwareSealer) key(create bool) (cipher.AEAD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aead != nil {
		return s.aead, nil
	}
	key, err := readKey(s.dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return nil, fmt.Errorf("crypto: no sealer key in the data directory: nothing was sealed there: %w", ErrKeyUnavailable)
		}
		key, err = makeKey(s.dataDir)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, ErrKeyUnavailable)
	}
	defer clear(key) // the AEAD keeps its own copy
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: the sealer key: %w: %w", err, ErrKeyUnavailable)
	}
	s.aead = aead
	return aead, nil
}

// keyDir opens <dataDir>/state/keys and returns its descriptor and owner.
// It follows no link on state/ or keys/, and refuses either when its group
// or others may write it (security review SEAL-1: on the Pi, root reads the
// key of the hub's identity, which owns the data directory). A missing one
// is fs.ErrNotExist. No error names a path's content.
func keyDir(dataDir string) (fd int, owner uint32, err error) {
	fd, err = unix.Open(dataDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, 0, fmt.Errorf("crypto: the data directory: %w", err)
	}
	var st unix.Stat_t
	for _, name := range []string{keyStateDir, keyDirName} {
		next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		switch {
		case isLink(err):
			return -1, 0, fmt.Errorf("crypto: the sealer key's directory %s is a link", name)
		case errors.Is(err, unix.ENOTDIR):
			return -1, 0, fmt.Errorf("crypto: the sealer key's directory %s is not a directory", name)
		case err != nil:
			return -1, 0, fmt.Errorf("crypto: the sealer key's directory %s: %w", name, err)
		}
		fd = next
		if err := unix.Fstat(fd, &st); err != nil {
			_ = unix.Close(fd)
			return -1, 0, fmt.Errorf("crypto: the sealer key's directory %s: %w", name, err)
		}
		if st.Mode&0o022 != 0 {
			_ = unix.Close(fd)
			return -1, 0, fmt.Errorf("crypto: the sealer key's directory %s is writable by its group or others (mode %o)", name, st.Mode&0o777)
		}
	}
	return fd, st.Uid, nil
}

// isLink reports the error of an O_NOFOLLOW open of a link: ELOOP, or
// EMLINK on the BSDs.
func isLink(err error) bool { return errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK) }

// readKey reads the key of dataDir: a regular file of keySize bytes that
// neither its group nor others may read or write, and that is the process's
// own, or for root the owner's of state/keys. It is opened without following
// a link and without blocking (a FIFO in its place is refused at once), so
// root never opens a device or waits on a pipe that the hub's identity put
// there (security review SEAL-1). A missing key is fs.ErrNotExist.
func readKey(dataDir string) ([]byte, error) {
	dir, dirOwner, err := keyDir(dataDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dir) }()
	fd, err := unix.Openat(dir, keyName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	switch {
	case isLink(err):
		return nil, errors.New("crypto: the sealer key is a link")
	case err != nil:
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	}
	f := os.NewFile(uintptr(fd), keyName)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	}
	euid := uint32(os.Geteuid()) //nolint:gosec // G115: a uid
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return nil, errors.New("crypto: the sealer key is not a regular file")
	case st.Uid != euid && (euid != 0 || st.Uid != dirOwner):
		return nil, errors.New("crypto: the sealer key belongs to another user")
	case st.Mode&0o077 != 0:
		return nil, fmt.Errorf("crypto: the sealer key is open to its group or others (mode %o); it must be 0600", st.Mode&0o777)
	case st.Size != keySize:
		return nil, fmt.Errorf("crypto: the sealer key is %d bytes, not %d", st.Size, keySize)
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(f, key); err != nil {
		clear(key)
		return nil, fmt.Errorf("crypto: the sealer key: %w", err)
	}
	return key, nil
}

// makeKey creates a random key for dataDir, in state/keys made 0700 when
// makeKey creates it: written to a 0600 temporary file, fsynced, then
// hard-linked to its name, which never replaces a key that another process
// made first (that one is read and returned instead), and the directory
// fsynced. A cut leaves either no key or a whole one, and at most an unused
// temporary file.
func makeKey(dataDir string) ([]byte, error) {
	dir := filepath.Join(dataDir, keyStateDir, keyDirName)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("crypto: the sealer key's directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: a directory, its owner's alone
			return nil, fmt.Errorf("crypto: the sealer key's directory: %w", err)
		}
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: a sealer key: %w", err)
	}
	if err := linkKey(dir, filepath.Join(dir, keyName), key); err != nil {
		clear(key)
		if errors.Is(err, fs.ErrExist) {
			return readKey(dataDir)
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
