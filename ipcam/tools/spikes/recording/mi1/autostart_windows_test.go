package mi1

import (
	"path/filepath"
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
