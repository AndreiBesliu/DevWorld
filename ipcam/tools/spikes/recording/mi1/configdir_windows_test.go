package mi1

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
)

// TestHubConfig_AdministratorsOnlyAfterAPlant (Final fix C review, I2):
// %ProgramData% lets any user make a folder in it first. Before install,
// the owner's user (anything that runs as the owner without a UAC prompt)
// made %ProgramData%\ipcam-recspike, gave itself Full control of it, and
// put there its own ipcam-hub-run.json, which it owns too. Once hubConfig
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
		if err := spiketest.AddFull(path, sid); err != nil {
			t.Fatalf("the plant's own entry in %s: %v", path, err)
		}
	}
	t.Logf("the planted folder: %s", sddl(d.ConfigDir))
	t.Logf("the planted config: %s", sddl(config))
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
