package crypto

import (
	"context"
	"errors"
)

// Sealer seals the hub's secrets at rest (§4.4 L525, P19): the OAuth clients'
// IDs and secrets and the refresh tokens, which the catalog keeps only as
// sealed rows of its secrets table, each with the Backend that sealed it.
// A blob is bound to the label it was sealed under, the row's id: it opens
// under that label alone, so a row copied to another id does not open.
//
// What the sealers promise, and what they do not:
//   - No error text holds a secret, a key, a label's blob or a path's
//     content; a label is a row id, which is no secret.
//   - Keys and blobs are never readable by the group or others: the
//     software key is 0600 in a 0700 directory, and the blobs live in the
//     catalog under the data directory's 0700 state/.
//   - Zeroing: the software sealer clears the key bytes it read once the
//     AEAD holds them, and the DPAPI sealer clears the buffer that Windows
//     returned once it copied the plaintext. Go gives no further promise:
//     the AEAD keeps the key for the process's life, the runtime may copy
//     any value it moves, and Unseal's result is the caller's to clear.
//
// TPM and service-account DPAPI (§11.8) come in Faza 1, as «Limitări» says.
type Sealer interface {
	// Seal returns plain sealed under label. Neither may be empty.
	Seal(ctx context.Context, label string, plain []byte) ([]byte, error)
	// Unseal returns the plaintext of sealed, which must have been sealed
	// under label: ErrSealedLabel when it does not open under it.
	Unseal(ctx context.Context, label string, sealed []byte) ([]byte, error)
	// Backend names the sealer: BackendDPAPIMachine or BackendSoftware.
	Backend() string
}

// The backends' names, which the catalog's rows and every envelope carry.
const (
	BackendDPAPIMachine = "dpapi-machine" // Windows: DPAPI, machine scope (P19)
	BackendSoftware     = "software"      // other systems: a 0600 key file
)

// ErrSealedLabel means a blob does not open under the label given: it was
// sealed under another label, or it was altered since.
var ErrSealedLabel = errors.New("crypto: the sealed blob does not open under this label")

// The first byte of each backend's blobs, so that a blob of the other
// backend, or of no sealer, is refused before any decryption.
const (
	blobSoftware byte = 1
	blobDPAPI    byte = 2
)

// NewSealer returns the system's sealer for the data directory dataDir:
// machine-scope DPAPI on Windows, which ignores dataDir, and elsewhere the
// software sealer, whose key is <dataDir>/state/keys/sealer.key. NewSealer
// touches no file: the first Seal creates the key, and Unseal never does,
// so a diag command that only unseals leaves nothing behind.
func NewSealer(dataDir string) (Sealer, error) { return newSealer(dataDir) }

// checkSeal refuses an empty label or plaintext, and a done ctx.
func checkSeal(ctx context.Context, label string, plain []byte) error {
	switch {
	case label == "":
		return errors.New("crypto: seal: no label")
	case len(plain) == 0:
		return errors.New("crypto: seal: no plaintext")
	}
	return ctx.Err()
}

// checkUnseal refuses an empty label, a done ctx, and a blob that the
// backend whose first byte is version did not seal.
func checkUnseal(ctx context.Context, label string, sealed []byte, version byte) error {
	switch {
	case label == "":
		return errors.New("crypto: unseal: no label")
	case len(sealed) == 0 || sealed[0] != version:
		return errors.New("crypto: unseal: not a blob of this sealer")
	}
	return ctx.Err()
}
