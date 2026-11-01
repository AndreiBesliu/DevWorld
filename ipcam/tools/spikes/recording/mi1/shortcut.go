// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
)

// A .lnk file starts with its header (MS-SHLLINK 2.1): HeaderSize, 0x4C,
// then the shell link's CLSID, 00021401-0000-0000-C000-000000000046, then
// LinkFlags, whose RunAsUser bit makes Windows start the target elevated,
// as the shortcut's «Executare ca administrator» box does.
const (
	lnkHeaderSize = 0x4c
	lnkFlagsAt    = 0x14
	lnkRunAsUser  = 0x00002000
)

var lnkCLSID = []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}

// RunAsAdministrator marks the shortcut at path to start its target
// elevated: Windows asks once (UAC) each time the owner opens it (G14). A
// file that is not a shell link is refused and left as it is.
func RunAsAdministrator(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the shortcut that recspike just wrote
	if err != nil {
		return err
	}
	if len(data) < lnkHeaderSize || binary.LittleEndian.Uint32(data) != lnkHeaderSize || !bytes.Equal(data[4:4+len(lnkCLSID)], lnkCLSID) {
		return errors.New("mi1: the shortcut is not a shell link")
	}
	flags := binary.LittleEndian.Uint32(data[lnkFlagsAt:])
	binary.LittleEndian.PutUint32(data[lnkFlagsAt:], flags|lnkRunAsUser)
	return os.WriteFile(path, data, info.Mode().Perm()) //nolint:gosec // the shortcut that recspike just wrote
}

// shortcutEnv is the environment of CreateShortcut's PowerShell: environ,
// the elevated recspike's own, with TEMP and TMP set to temp, a folder that
// only Administrators and SYSTEM may write, and vars after it. Its Add-Type
// writes C# into TEMP, compiles it there and loads what it built, inside
// the elevated install, and a program of the owner's user may change that
// user's own %TEMP% meanwhile (Final fix D review, M1). Every TEMP and TMP
// of environ goes, whatever the case of its name: Windows' names ignore
// case.
func shortcutEnv(environ []string, temp string, vars ...string) []string {
	env := make([]string, 0, len(environ)+2+len(vars))
	for _, kv := range environ {
		if name, _, _ := strings.Cut(kv, "="); !strings.EqualFold(name, "TEMP") && !strings.EqualFold(name, "TMP") {
			env = append(env, kv)
		}
	}
	return append(append(env, "TEMP="+temp, "TMP="+temp), vars...)
}
