// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
)

// expandSystemFolders expands raw, a path that HKLM keeps with
// %SystemDrive%, %SystemRoot% or %windir% in it, with the drive and the
// folder of windowsDir, the system's own Windows folder, never with the
// process environment (Final fix C review, M3); any other variable is an
// error, since only the environment could give it.
func expandSystemFolders(raw, windowsDir string) (string, error) {
	if len(windowsDir) < 3 || windowsDir[1] != ':' || windowsDir[2] != '\\' {
		return "", fmt.Errorf("folderul Windows %q nu are o literă de disc", windowsDir)
	}
	var b strings.Builder
	for rest := raw; rest != ""; {
		i := strings.IndexByte(rest, '%')
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		j := strings.IndexByte(rest[i+1:], '%')
		if j < 0 {
			return "", fmt.Errorf("calea %q are un %% fără pereche: %s", raw, rest[i:])
		}
		switch name := rest[i+1 : i+1+j]; strings.ToLower(name) {
		case "systemdrive":
			b.WriteString(windowsDir[:2])
		case "systemroot", "windir":
			b.WriteString(windowsDir)
		default:
			return "", fmt.Errorf("calea %q numește %%%s%%, pe care doar mediul procesului o dă", raw, name)
		}
		rest = rest[i+2+j:]
	}
	return b.String(), nil
}

// notAFolder is the refusal of dir, the protected folder's place, where
// something other than a plain folder lies: a file when isDir is false, a
// link or a junction when isLink is true. Any user may put a file there,
// as ProgramData lets every user add files, and make a junction. The line
// says which it is and gives the command that removes exactly that from
// an administrator window (Final fix C review, minor): del for a file or
// a link to a file, rd for a link or a junction to a folder, which leaves
// the folder that it points to as it is.
func notAFolder(dir string, isDir, isLink bool) error {
	const again = "apoi porniți din nou comanda"
	switch {
	case isLink && isDir:
		return fmt.Errorf(`%s nu este un folder obișnuit, ci o legătură spre alt loc, pe care poate a pus-o alt cont: `+
			`ștergeți legătura dintr-o fereastră de administrator (rd "%s"), %s`, dir, dir, again)
	case isLink:
		return fmt.Errorf(`%s nu este un folder, ci o legătură spre un fișier, pe care poate a pus-o alt cont: `+
			`ștergeți legătura dintr-o fereastră de administrator (del "%s"), %s`, dir, dir, again)
	}
	return fmt.Errorf(`%s nu este un folder, ci un fișier, pe care poate l-a pus alt cont: `+
		`ștergeți-l dintr-o fereastră de administrator (del /f /a "%s"), %s`, dir, dir, again)
}

// WriteHubConfig writes cfg, the config of ipcam-hub's starts, as the file
// name in dir, the protected folder of the owner's probes, and returns its
// path, which those starts read: MI1 writes it before each start, MI2 once
// for all the starts of its run. The guide runs every probe from an
// administrator window, or with sudo, so ipcam-hub runs elevated, while
// the probes' folders lie in work/, which the owner's own token may change
// (Final fix C review, I2 and M9). So cfg holds only trusted values: the
// release's MediaMTX, and the data folder and RI_g's file of the run's own
// folder, whose name the probe made; dir is made, or taken, for
// Administrators and SYSTEM alone, or root (ProtectConfigDir), and the file
// is written anew (WriteProtected). Each probe has its own name
// (HubConfigNames), so that two probes never share one file.
func WriteHubConfig(dir, name string, cfg hubproc.Config) (string, error) {
	return writeHubConfig(ProtectConfigDir, WriteProtected, dir, name, cfg)
}

// writeHubConfig is WriteHubConfig with protect and write in place of
// ProtectConfigDir and WriteProtected; tests replace them.
func writeHubConfig(protect func(string) error, write func(string, []byte) error, dir, name string, cfg hubproc.Config) (string, error) {
	if dir == "" {
		return "", errors.New("lipsește folderul protejat din care pornește ipcam-hub")
	}
	path := filepath.Join(dir, name)
	data, err := hubproc.EncodeConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("nu pot pregăti setările ipcam-hub: %w", err)
	}
	if err := protect(dir); err != nil {
		return "", fmt.Errorf("nu pot proteja folderul %s, din care pornește ipcam-hub: %w", dir, err)
	}
	if err := write(path, data); err != nil {
		return "", WriteError("nu pot scrie setările ipcam-hub în", path, err)
	}
	return path, nil
}

// WriteError is the owner's line for err, the failed write of the file at
// path into the protected folder, after cannot, the line's start, which
// names the file: no failure of the folder's protection, which would send
// the owner to the administrator window for nothing (Final fix C review,
// minor). A full disk and a file that another program holds open at path
// each have their own; the boot task's definition says the same (Final fix
// D review, M3).
func WriteError(cannot, path string, err error) error {
	switch {
	case diskFull(err):
		return fmt.Errorf("%s %s: discul este plin", cannot, path)
	case inUse(err):
		return fmt.Errorf("%s %s: fișierul este folosit de alt program", cannot, path)
	}
	return fmt.Errorf("%s %s: %w", cannot, path, err)
}

// WriteProtected writes data into a new file at path, in the folder that
// ProtectConfigDir protects, which only Administrators and SYSTEM, or root,
// may change. A file already at path is removed, never written through
// (Final fix C review, I2): the new file has no owner, DACL or link of the
// one that was there, which on the PC another account might change, with
// its DACL, between this write and ipcam-hub's start, and a link would
// carry the write to wherever it points. The new file is created, never
// opened, so one put back at its name meanwhile makes the write fail.
//
// Windows may refuse the remove or the new file for a moment (busy), while
// a scanner still holds the file that was there or its delete is still
// pending: WriteProtected then tries again, after each of busyWaits (Final
// fix D review, M6). A probe that writes its config anew, an MI3 step, an
// MI4 or MI9 recording or an MI1 start, otherwise ends there.
func WriteProtected(path string, data []byte) error {
	return writeAnew(createProtected, busy, time.Sleep, path, data)
}

// busyWaits are WriteProtected's waits between its tries while the file is
// busy: five tries over 5 s.
var busyWaits = []time.Duration{500 * time.Millisecond, time.Second, 1500 * time.Millisecond, 2 * time.Second}

// writeAnew is WriteProtected with create, busy and sleep in place of
// createProtected, the system's busy and time.Sleep; tests replace them.
func writeAnew(create func(string) (*os.File, error), busy func(error) bool, sleep func(time.Duration), path string, data []byte) error {
	err := writeOnce(create, path, data)
	for _, wait := range busyWaits {
		if err == nil || !busy(err) {
			break
		}
		sleep(wait)
		err = writeOnce(create, path, data)
	}
	return err
}

// writeOnce removes the file at path, if there is one, and writes data
// into a new one that create makes there, synced.
func writeOnce(create func(string) (*os.File, error), path string, data []byte) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := create(path)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
