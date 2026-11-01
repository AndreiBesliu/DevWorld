package crypto

import (
	"bytes"
	"testing"

	"golang.org/x/sys/windows"
)

// TestSealer_DPAPIUsesMachineScope (P19, P49): the Windows sealer calls
// CryptProtectData with CRYPTPROTECT_LOCAL_MACHINE, so any ipcam-hub start
// on the machine, an S4U logon's included, unseals after a reboot, and with
// CRYPTPROTECT_UI_FORBIDDEN, so it never prompts; the label is its
// entropy. Unseal forbids the UI too.
func TestSealer_DPAPIUsesMachineScope(t *testing.T) {
	var protectFlags, unprotectFlags uint32
	var entropy []byte
	wasProtect, wasUnprotect := cryptProtectData, cryptUnprotectData
	t.Cleanup(func() { cryptProtectData, cryptUnprotectData = wasProtect, wasUnprotect })
	cryptProtectData = func(in *windows.DataBlob, name *uint16, ent *windows.DataBlob, reserved uintptr,
		prompt *windows.CryptProtectPromptStruct, flags uint32, out *windows.DataBlob) error {
		protectFlags = flags
		entropy = bytes.Clone(bytesOf(ent))
		return wasProtect(in, name, ent, reserved, prompt, flags, out)
	}
	cryptUnprotectData = func(in *windows.DataBlob, name **uint16, ent *windows.DataBlob, reserved uintptr,
		prompt *windows.CryptProtectPromptStruct, flags uint32, out *windows.DataBlob) error {
		unprotectFlags = flags
		return wasUnprotect(in, name, ent, reserved, prompt, flags, out)
	}
	s, err := NewSealer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal(t.Context(), roundTripLabel, roundTripPlain())
	if err != nil {
		t.Fatal(err)
	}
	if protectFlags != windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN {
		t.Errorf("CryptProtectData flags %#x, want CRYPTPROTECT_LOCAL_MACHINE|CRYPTPROTECT_UI_FORBIDDEN", protectFlags)
	}
	if string(entropy) != roundTripLabel {
		t.Errorf("CryptProtectData entropy %q, want the label", entropy)
	}
	if got, err := s.Unseal(t.Context(), roundTripLabel, sealed); err != nil || !bytes.Equal(got, roundTripPlain()) {
		t.Fatalf("Unseal = %v", err)
	}
	if unprotectFlags != windows.CRYPTPROTECT_UI_FORBIDDEN {
		t.Errorf("CryptUnprotectData flags %#x, want CRYPTPROTECT_UI_FORBIDDEN", unprotectFlags)
	}
}
