// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// microsoft is the signer that NotMyFault must carry.
const microsoft = "Microsoft Corporation"

// DeviceChecks are the PC's preflight checks (G14): NotMyFault in dir,
// recspike's folder, the elevation, AutoReboot and Windows Update's pause.
func DeviceChecks(dir string, _ int) []Check {
	return PCChecks(PCProbes{
		NotMyFault:         func() error { return signedByMicrosoft(filepath.Join(dir, NotMyFault)) },
		Elevated:           func() bool { return windows.GetCurrentProcessToken().IsElevated() },
		AutoReboot:         autoReboot,
		UpdatesPausedUntil: updatesPausedUntil,
		Now:                time.Now,
	})
}

// autoReboot reads CrashControl\AutoReboot.
func autoReboot() (uint32, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\CrashControl`, registry.QUERY_VALUE)
	if err != nil {
		return 0, err
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetIntegerValue("AutoReboot")
	return uint32(v), err //nolint:gosec // a DWORD
}

// updatesPausedUntil reads PauseUpdatesExpiryTime, which Settings writes
// when the owner pauses Windows Update.
func updatesPausedUntil() (time.Time, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\WindowsUpdate\UX\Settings`, registry.QUERY_VALUE)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetStringValue("PauseUpdatesExpiryTime")
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, v)
}

var (
	crypt32          = windows.NewLazySystemDLL("crypt32.dll")
	cryptMsgGetParam = crypt32.NewProc("CryptMsgGetParam")
	cryptMsgClose    = crypt32.NewProc("CryptMsgClose")
)

// cmsgSignerCertInfoParam is CMSG_SIGNER_CERT_INFO_PARAM (wincrypt.h).
const cmsgSignerCertInfoParam = 7

// signedByMicrosoft checks the file's Authenticode signature with
// WinVerifyTrust and that its signer's certificate is Microsoft
// Corporation's.
func signedByMicrosoft(path string) error {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	data := &windows.WinTrustData{
		Size:             uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:         windows.WTD_UI_NONE,
		RevocationChecks: windows.WTD_REVOKE_NONE,
		UnionChoice:      windows.WTD_CHOICE_FILE,
		StateAction:      windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&windows.WinTrustFileInfo{ //nolint:gosec // WinVerifyTrust's own structure, as x/sys/windows's test passes it
			Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
			FilePath: path16,
		}),
	}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	if verifyErr != nil {
		return verifyErr
	}
	signer, err := signerName(path16)
	if err != nil {
		return err
	}
	if signer != microsoft {
		return fmt.Errorf("mi1: %s is signed by %q", filepath.Base(path), signer)
	}
	return nil
}

// signerName is the simple name of the certificate that signed the file.
func signerName(path16 *uint16) (string, error) {
	var encoding, contentType, formatType uint32
	var store, msg windows.Handle
	if err := windows.CryptQueryObject(windows.CERT_QUERY_OBJECT_FILE, unsafe.Pointer(path16), //nolint:gosec // the file name, as CryptQueryObject takes it
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED, windows.CERT_QUERY_FORMAT_FLAG_BINARY, 0,
		&encoding, &contentType, &formatType, &store, &msg, nil); err != nil {
		return "", err
	}
	defer func() { _ = windows.CertCloseStore(store, 0) }()
	defer func() { _, _, _ = cryptMsgClose.Call(uintptr(msg)) }()
	var size uint32
	if r, _, err := cryptMsgGetParam.Call(uintptr(msg), cmsgSignerCertInfoParam, 0, 0, uintptr(unsafe.Pointer(&size))); r == 0 { //nolint:gosec // CryptMsgGetParam's size out-parameter
		return "", err
	}
	buf := make([]byte, size)
	if r, _, err := cryptMsgGetParam.Call(uintptr(msg), cmsgSignerCertInfoParam, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 { //nolint:gosec // CryptMsgGetParam fills buf, of the size it asked for
		return "", err
	}
	info := (*windows.CertInfo)(unsafe.Pointer(&buf[0])) //nolint:gosec // CMSG_SIGNER_CERT_INFO_PARAM is a CERT_INFO
	cert, err := windows.CertFindCertificateInStore(store, windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, 0,
		windows.CERT_FIND_SUBJECT_CERT, unsafe.Pointer(info), nil) //nolint:gosec // CERT_FIND_SUBJECT_CERT takes the signer's CERT_INFO
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CertFreeCertificateContext(cert) }()
	name := make([]uint16, 256)
	n := windows.CertGetNameString(cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, &name[0], uint32(len(name))) //nolint:gosec // 256
	if n <= 1 {
		return "", errors.New("mi1: the signer's certificate has no name")
	}
	return windows.UTF16ToString(name[:n]), nil
}
