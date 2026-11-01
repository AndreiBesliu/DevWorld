// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

// WriteHubConfig writes cfg, the config of one start of ipcam-hub, as the
// file name in dir, the protected folder of the owner's probes, and returns
// its path, which that start reads. The guide runs every probe from an
// administrator window, or with sudo, so ipcam-hub runs elevated, while
// the probes' folders lie in work/, which the owner's own token may change
// (Final fix C review, I2 and M9). So cfg holds only trusted values: the
// release's MediaMTX, and the data folder and RI_g's file of the run's own
// folder, whose name the probe made; dir is made, or taken, for
// Administrators and SYSTEM alone, or root (ProtectConfigDir), and the file
// is written anew (WriteProtected) before each start. Each probe has its
// own name (HubConfigNames), so that two probes never share one file.
func WriteHubConfig(dir, name string, cfg hubproc.Config) (string, error) {
	return writeHubConfig(ProtectConfigDir, dir, name, cfg)
}

// writeHubConfig is WriteHubConfig with protect in place of
// ProtectConfigDir; tests replace it.
func writeHubConfig(protect func(string) error, dir, name string, cfg hubproc.Config) (string, error) {
	if dir == "" {
		return "", errors.New("lipsește folderul protejat din care pornește ipcam-hub")
	}
	path := filepath.Join(dir, name)
	data, err := hubproc.EncodeConfig(cfg)
	if err == nil {
		err = protect(dir)
	}
	if err == nil {
		err = WriteProtected(path, data)
	}
	if err != nil {
		return "", fmt.Errorf("nu pot proteja folderul %s, din care pornește ipcam-hub: %w", dir, err)
	}
	return path, nil
}

// WriteProtected writes data into a new file at path, in the folder that
// ProtectConfigDir protects, which only Administrators and SYSTEM, or root,
// may change. A file already at path is removed, never written through
// (Final fix C review, I2): the new file has no owner, DACL or link of the
// one that was there, which on the PC another account might change, with
// its DACL, between this write and ipcam-hub's start, and a link would
// carry the write to wherever it points. The new file is created, never
// opened, so one put back at its name meanwhile makes the write fail.
func WriteProtected(path string, data []byte) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := createProtected(path)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
