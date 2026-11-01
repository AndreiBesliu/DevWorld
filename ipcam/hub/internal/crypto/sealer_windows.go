package crypto

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The DPAPI calls, which a test replaces to see their flags.
var (
	cryptProtectData   = windows.CryptProtectData
	cryptUnprotectData = windows.CryptUnprotectData
)

// dpapiSealer is the Windows sealer (P19): DPAPI with machine scope
// (CRYPTPROTECT_LOCAL_MACHINE), so any process of the machine unseals,
// whatever its logon: the S4U task of uploadspike's autostart and an
// interactive start alike, also after a reboot; and never a prompt
// (CRYPTPROTECT_UI_FORBIDDEN). The label is DPAPI's optional entropy, so a
// blob opens only under its label. A blob is blobDPAPI ‖ DPAPI's blob. The
// data directory keeps the user profile's ACL, which is what keeps other
// local accounts from the catalog's blobs.
type dpapiSealer struct{}

func newSealer(string) (Sealer, error) { return dpapiSealer{}, nil }

func (dpapiSealer) Backend() string { return BackendDPAPIMachine }

func (dpapiSealer) Seal(ctx context.Context, label string, plain []byte) ([]byte, error) {
	if err := checkSeal(ctx, label, plain); err != nil {
		return nil, err
	}
	in, entropy := blobOf(plain), blobOf([]byte(label))
	var out windows.DataBlob
	err := cryptProtectData(&in, nil, &entropy, 0, nil, windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, fmt.Errorf("crypto: seal with DPAPI: %w", err)
	}
	defer freeBlob(&out, false)
	return append([]byte{blobDPAPI}, bytesOf(&out)...), nil
}

func (dpapiSealer) Unseal(ctx context.Context, label string, sealed []byte) ([]byte, error) {
	if err := checkUnseal(ctx, label, sealed, blobDPAPI); err != nil {
		return nil, err
	}
	if len(sealed) < 2 {
		return nil, ErrSealedLabel
	}
	in, entropy := blobOf(sealed[1:]), blobOf([]byte(label))
	var out windows.DataBlob
	err := cryptUnprotectData(&in, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	switch {
	case errors.Is(err, windows.ERROR_INVALID_DATA):
		// Another entropy, or an altered blob.
		return nil, ErrSealedLabel
	case err != nil:
		return nil, fmt.Errorf("crypto: unseal with DPAPI: %w", err)
	}
	defer freeBlob(&out, true)
	return append([]byte(nil), bytesOf(&out)...), nil
}

// blobOf is a DATA_BLOB over b, which must not be empty.
func blobOf(b []byte) windows.DataBlob {
	return windows.DataBlob{Size: uint32(len(b)), Data: &b[0]} //nolint:gosec // G115: a secret or a blob, far below 4 GiB
}

// bytesOf is the buffer that DPAPI allocated for b, as a slice.
func bytesOf(b *windows.DataBlob) []byte {
	return unsafe.Slice(b.Data, b.Size) //nolint:gosec // G103: DPAPI's own buffer, of its own size
}

// freeBlob frees the buffer that DPAPI allocated for b, cleared first when
// it holds a plaintext.
func freeBlob(b *windows.DataBlob, plaintext bool) {
	if b.Data == nil {
		return
	}
	if plaintext {
		clear(bytesOf(b))
	}
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data))) //nolint:gosec // G103: the buffer that DPAPI allocated
}
