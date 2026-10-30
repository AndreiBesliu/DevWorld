package mi1

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
)

// TestHubConfig_AdministratorsOnlyAfterAPlant (Final fix C review, I2):
// %ProgramData% lets any user make a folder in it first. Before install,
// the owner's user (anything that runs as the owner without a UAC prompt)
// made %ProgramData%\ipcam-recspike and put there its own
// ipcam-hub-run.json, owns both, and gave itself Full control of each,
// beside the entries that they inherit or, more hostile, in a DACL of its
// own that denies Administrators the right to change it. Once hubConfig
// has run, as the elevated starts of ipcam-hub run it, the folder and the
// config belong to Administrators, each with a protected DACL that only
// Administrators and SYSTEM are in, and a standard window can neither
// change the config, its DACL or the folder's, nor delete the config, nor
// add a file to the folder. The windows CI job runs elevated, as the boot
// task and the shortcut do.
func TestHubConfig_AdministratorsOnlyAfterAPlant(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal("the windows job runs elevated, and this test needs it")
		}
		t.Skip("needs an elevated test process, as the boot task and the shortcut")
	}
	sid, err := UserSID()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, path string)
	}{
		{"an entry of its own", func(t *testing.T, path string) {
			if err := spiketest.AddFull(path, sid); err != nil {
				t.Fatalf("the plant's own entry in %s: %v", path, err)
			}
		}},
		{"Administrators denied", func(t *testing.T, path string) {
			// DELETE, WRITE_DAC and WRITE_OWNER; FILE_DELETE_CHILD too on
			// the folder.
			deny, inherit := "0xd0000", ""
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				deny, inherit = "0xd0040", "OICI"
			}
			setDACL(t, path, fmt.Sprintf("D:P(D;%s;%s;;;BA)(A;%s;FA;;;%s)", inherit, deny, inherit, sid))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, st, cur, vol := hubConfigRig(t) // d.protectDir is nil: the PC's own
			config := filepath.Join(d.ConfigDir, HubConfigName)
			standard := spiketest.StandardToken(t)
			spiketest.AsStandard(t, standard, func() {
				if err := os.MkdirAll(d.ConfigDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, plantedConfig, 0o600); err != nil {
					t.Fatal(err)
				}
			})
			for _, path := range []string{d.ConfigDir, config} {
				spiketest.SetOwner(t, path, sid)
				tc.plant(t, path)
				t.Logf("the planted %s: %s", filepath.Base(path), sddl(path))
			}
			spiketest.AsStandard(t, standard, func() {
				if err := os.WriteFile(config, plantedConfig, 0o600); err != nil {
					t.Fatalf("a standard window cannot write the config that it planted: %v", err)
				}
			})

			path, err := d.hubConfig(st, cur, vol)
			if err != nil {
				t.Fatalf("hubConfig: %v", err)
			}
			if path != config {
				t.Fatalf("hubConfig wrote %s, want %s", path, config)
			}
			for _, p := range []string{d.ConfigDir, config} {
				t.Logf("after hubConfig, %s: %s", filepath.Base(p), sddl(p))
				checkAdministratorsOnly(t, p)
			}
			spiketest.AsStandard(t, standard, func() {
				for what, change := range map[string]func() error{
					"wrote the config":           func() error { return os.WriteFile(config, plantedConfig, 0o600) },
					"changed the config's DACL":  func() error { return spiketest.GrantFull(config, sid) },
					"deleted the config":         func() error { return os.Remove(config) },
					"changed the folder's DACL":  func() error { return spiketest.GrantFull(d.ConfigDir, sid) },
					"added a file to the folder": func() error { return os.WriteFile(filepath.Join(d.ConfigDir, "other.json"), plantedConfig, 0o600) },
				} {
					if err := change(); err == nil {
						t.Errorf("after hubConfig, a standard window %s", what)
					}
				}
			})
			checkTrusted(t, config, d, vol)
		})
	}
}

// TestHubConfig_RefusesAJunctionForItsFolder (Final fix C review, I2): a
// junction planted at the protected folder's name, to a folder that the
// owner's user may rename, would let it swap the config after hubConfig
// wrote it. hubConfig refuses it, and writes nothing where it points.
func TestHubConfig_RefusesAJunctionForItsFolder(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal("the windows job runs elevated, and this test needs it")
		}
		t.Skip("needs an elevated test process, as the boot task and the shortcut")
	}
	d, st, cur, vol := hubConfigRig(t)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(d.ConfigDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", d.ConfigDir, target).CombinedOutput(); err != nil { //nolint:gosec // the test's folders
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	_, err := d.hubConfig(st, cur, vol)
	if err == nil || !strings.Contains(err.Error(), "nu este un folder obișnuit") {
		t.Errorf("hubConfig through a junction = %v, want a Romanian refusal", err)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Errorf("the junction's target holds %v (%v), want nothing", entries, err)
	}
}

// setDACL gives path the DACL of sddl, as its owner may.
func setDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatalf("the DACL of %s: %v", path, err)
	}
}

// checkAdministratorsOnly checks that path belongs to Administrators, and
// that its DACL is protected and gives access to Administrators and SYSTEM
// alone.
func checkAdministratorsOnly(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if owner.String() != administratorsSID {
		t.Errorf("%s belongs to %s, want Administrators (%s)", path, owner, administratorsSID)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("%s inherits entries: %s", path, sd)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil {
		t.Fatalf("%s has no DACL, which lets everyone in", path)
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		who := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String() //nolint:gosec // an ACE's SID starts at SidStart
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || (who != administratorsSID && who != systemSID) {
			t.Errorf("%s's DACL has an entry for %s (type %d, access %#x); want Administrators and SYSTEM alone: %s",
				path, who, ace.Header.AceType, ace.Mask, sd)
		}
	}
}

// sddl is path's owner and DACL, in SDDL.
func sddl(path string) string {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err.Error()
	}
	return sd.String()
}
