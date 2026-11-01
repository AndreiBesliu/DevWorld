package mi1

import (
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWindowsFolders_NotFromTheEnvironment (Final fix C review, M3): the
// boot task and the shortcut run recspike elevated with the owner's user's
// environment, which a program of that user may change without a UAC
// prompt. ProgramData and WindowsFolder still name C:\ProgramData and
// C:\Windows when every variable that could name them points elsewhere.
func TestWindowsFolders_NotFromTheEnvironment(t *testing.T) {
	elsewhere := t.TempDir()
	for _, name := range []string{"ProgramData", "ALLUSERSPROFILE", "PUBLIC", "SystemDrive", "SystemRoot", "windir", "USERPROFILE"} {
		t.Setenv(name, filepath.Join(elsewhere, name))
	}
	for _, c := range []struct {
		name string
		get  func() (string, error)
		want string
	}{{"ProgramData", ProgramData, `C:\ProgramData`}, {"WindowsFolder", WindowsFolder, `C:\Windows`}} {
		got, err := c.get()
		if err != nil || !strings.EqualFold(got, c.want) {
			t.Errorf("%s() = %q, %v; want %s, with the environment pointing at %s", c.name, got, err, c.want, elsewhere)
		}
	}
}

// TestCreateShortcut_CompilesInTheGivenTemp (Final fix D review, M1): the
// elevated install creates the desktop shortcut through PowerShell's
// Add-Type, which writes C# into TEMP, compiles it there and loads what it
// built. The child's TEMP and TMP are the folder that CreateShortcut is
// given, one only Administrators and SYSTEM may write, never the owner's
// user's, which a program of that user may change meanwhile.
func TestCreateShortcut_CompilesInTheGivenTemp(t *testing.T) {
	users := filepath.Join(t.TempDir(), "AppData", "Local", "Temp")
	t.Setenv("TEMP", users)
	t.Setenv("TMP", users)
	var env []string
	system := runShortcut
	t.Cleanup(func() { runShortcut = system })
	runShortcut = func(c *exec.Cmd) ([]byte, error) {
		env = c.Env
		return nil, errors.New("not run")
	}
	temp := filepath.Join(t.TempDir(), "ipcam-recspike", "temp")
	_ = CreateShortcut(filepath.Join(t.TempDir(), "Proba.lnk"), `C:\proba-inregistrare\recspike.exe`, "", `C:\proba-inregistrare`, temp, true)
	var temps []string
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); strings.EqualFold(name, "TEMP") || strings.EqualFold(name, "TMP") {
			temps = append(temps, kv)
		}
	}
	if want := []string{"TEMP=" + temp, "TMP=" + temp}; !slices.Equal(temps, want) {
		t.Errorf("the shortcut's PowerShell has %q, want %q alone", temps, want)
	}
}
