package mi1

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
)

// requireElevated skips t outside an elevated test process. The windows CI
// job runs elevated, as the boot task and the shortcut do.
func requireElevated(t *testing.T) {
	t.Helper()
	if !windows.GetCurrentProcessToken().IsElevated() {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal("the windows job runs elevated, and this test needs it")
		}
		t.Skip("needs an elevated test process, as the boot task and the shortcut")
	}
}

// TestHubConfig_RefusesAFolderThatAnotherAccountMade (Final fix C review,
// I2): %ProgramData% lets any user make a folder in it first. Before
// install, the owner's user (anything that runs as the owner without a UAC
// prompt) made %ProgramData%\ipcam-recspike and put its own
// ipcam-hub-run.json there. It owns both and has Full control of each:
// through an entry of its own beside the ones that they inherit; or in a
// DACL of its own that denies Administrators the right to change it, or to
// read who owns it; or through a handle to the folder, opened with the
// right to change its DACL and kept open, which no later change of that
// DACL takes back. So
// hubConfig, as the elevated starts of ipcam-hub run it, never takes such a
// folder as its own: it refuses it with the Romanian line that tells the
// owner to delete it, and changes nothing in it.
func TestHubConfig_RefusesAFolderThatAnotherAccountMade(t *testing.T) {
	requireElevated(t)
	sid, err := UserSID()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, path string)
		// hold keeps a standard window's handle to the folder open, with
		// the right to change its DACL.
		hold bool
	}{
		{"an entry of its own", func(t *testing.T, path string) {
			if err := spiketest.AddFull(path, sid); err != nil {
				t.Fatalf("the plant's own entry in %s: %v", path, err)
			}
		}, false},
		{"Administrators denied", func(t *testing.T, path string) {
			// DELETE, WRITE_DAC and WRITE_OWNER; FILE_DELETE_CHILD too on
			// the folder.
			deny, inherit := "0xd0000", ""
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				deny, inherit = "0xd0040", "OICI"
			}
			setDACL(t, path, fmt.Sprintf("D:P(D;%s;%s;;;BA)(A;%s;FA;;;%s)", inherit, deny, inherit, sid))
		}, false},
		{"a handle that it keeps open", func(*testing.T, string) {}, true},
		{"hidden from Administrators", func(t *testing.T, path string) {
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				setDACL(t, path, fmt.Sprintf("D:P(A;;FA;;;%s)", sid))
				return
			}
			// Administrators may not read the folder's owner or DACL, and
			// an OWNER RIGHTS entry takes the owner's own right to them
			// away: only the backup privilege still reads them.
			setDACL(t, path, fmt.Sprintf("D:P(D;OICI;RC;;;BA)(A;OICI;0x100000;;;OW)(A;OICI;FA;;;%s)", sid))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, st, cur, vol := hubConfigRig(t) // d.protectDir is nil: the PC's own
			config := filepath.Join(d.ConfigDir, HubConfigName)
			standard := spiketest.StandardToken(t)
			var held windows.Handle
			spiketest.AsStandard(t, standard, func() {
				if err := os.MkdirAll(d.ConfigDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, plantedConfig, 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.hold {
					held = openToChangeDACL(t, d.ConfigDir)
				}
			})
			// The config first: a folder's new DACL passes its entries on to
			// what still inherits from it.
			for _, path := range []string{config, d.ConfigDir} {
				spiketest.SetOwner(t, path, sid)
				tc.plant(t, path)
				t.Logf("the planted %s: %s", filepath.Base(path), sddl(path))
			}

			path, err := d.hubConfig(st, cur, vol)
			if err == nil {
				t.Errorf("hubConfig took %s, which a standard window made, as its own, and wrote %s", d.ConfigDir, path)
				if held != 0 {
					spiketest.AsStandard(t, standard, func() { regainThroughHandle(t, held, sid, config) })
				}
				return
			}
			if want := "a fost făcut de alt cont, nu de probă"; !strings.Contains(err.Error(), want) {
				t.Errorf("hubConfig in a folder that a standard window made = %v, want a Romanian refusal with %q", err, want)
			}
			if got, err := os.ReadFile(config); err != nil || !bytes.Equal(got, plantedConfig) { //nolint:gosec // the test's file
				t.Errorf("hubConfig changed the config planted in the folder that it refused: %q (%v)", got, err)
			}
			t.Logf("after the refusal, %s: %s", filepath.Base(d.ConfigDir), sddl(d.ConfigDir))
		})
	}
}

// TestHubConfig_AdministratorsOnlyInItsOwnFolder (Final fix C review, I2):
// the folder that hubConfig makes, and makes its own again before each
// start of ipcam-hub, belongs to Administrators, as does the config in it,
// each with a protected DACL that only Administrators and SYSTEM are in,
// whatever entry an administrator added since. A standard window can
// neither change the config, its DACL or the folder's, nor delete the
// config, nor add a file to the folder.
func TestHubConfig_AdministratorsOnlyInItsOwnFolder(t *testing.T) {
	requireElevated(t)
	sid, err := UserSID()
	if err != nil {
		t.Fatal(err)
	}
	d, st, cur, vol := hubConfigRig(t) // d.protectDir is nil: the PC's own
	config := filepath.Join(d.ConfigDir, HubConfigName)
	standard := spiketest.StandardToken(t)
	for _, start := range []struct {
		name   string
		before func(t *testing.T)
	}{
		{"the first start, with no folder", func(*testing.T) {}},
		{"a start after an administrator gave the owner's user an entry in the folder and the config", func(t *testing.T) {
			for _, path := range []string{d.ConfigDir, config} {
				if err := spiketest.AddFull(path, sid); err != nil {
					t.Fatalf("an entry for the owner's user in %s: %v", path, err)
				}
			}
		}},
	} {
		start.before(t)
		path, err := d.hubConfig(st, cur, vol)
		if err != nil {
			t.Fatalf("%s: hubConfig: %v", start.name, err)
		}
		if path != config {
			t.Fatalf("%s: hubConfig wrote %s, want %s", start.name, path, config)
		}
		for _, p := range []string{d.ConfigDir, config} {
			t.Logf("%s: after hubConfig, %s: %s", start.name, filepath.Base(p), sddl(p))
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
					t.Errorf("%s: after hubConfig, a standard window %s", start.name, what)
				}
			}
		})
		checkTrusted(t, config, d, vol)
	}
}

// openToChangeDACL opens the folder dir with the right to change its DACL,
// as a program of the owner's user that made the folder may, and keeps the
// handle open until the test ends.
func openToChangeDACL(t *testing.T, dir string) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("a standard window cannot open the folder that it made, %s, to change its DACL: %v", dir, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
	return h
}

// regainThroughHandle tries, as a standard window holding held, a handle
// to the config's folder opened before hubConfig, to give the owner's user
// Full control of that folder again and to put its own config in place of
// hubConfig's, and reports each step that succeeds.
func regainThroughHandle(t *testing.T, held windows.Handle, sid, config string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)(A;OICI;FA;;;%s)", sid))
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(held, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Logf("through the handle that it opened before hubConfig, a standard window cannot change the folder's DACL: %v", err)
		return
	}
	t.Errorf("through the handle that it opened before hubConfig, a standard window gave itself Full control of the folder again: %s", sddl(filepath.Dir(config)))
	if err := os.Remove(config); err != nil {
		t.Logf("it cannot delete hubConfig's config: %v", err)
		return
	}
	if err := os.WriteFile(config, plantedConfig, 0o600); err != nil {
		t.Logf("it cannot put its own config in its place: %v", err)
		return
	}
	t.Errorf("and put its own config in the place of hubConfig's, for ipcam-hub to start from")
}

// TestHubConfig_RefusesAJunctionForItsFolder (Final fix C review, I2): a
// junction planted at the protected folder's name, to a folder that the
// owner's user may rename, would let it swap the config after hubConfig
// wrote it. hubConfig refuses it, and writes nothing where it points.
func TestHubConfig_RefusesAJunctionForItsFolder(t *testing.T) {
	requireElevated(t)
	d, st, cur, vol := hubConfigRig(t)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(d.ConfigDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", d.ConfigDir, target).CombinedOutput(); err != nil { //nolint:gosec // the test's folders
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	_, err := d.hubConfig(st, cur, vol)
	if err == nil || !strings.Contains(err.Error(), "ci o legătură spre alt loc") || !strings.Contains(err.Error(), `(rd "`+d.ConfigDir+`")`) {
		t.Errorf("hubConfig through a junction = %v, want a Romanian refusal that names the link and gives rd", err)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Errorf("the junction's target holds %v (%v), want nothing", entries, err)
	}
}

// The system's errors of a full disk and of a file that another program
// holds open, here.
var (
	diskFullErrno error = windows.ERROR_DISK_FULL
	inUseErrno    error = windows.ERROR_SHARING_VIOLATION
)

// TestHubConfig_SaysWhenItsFileIsInUse (Final fix C review, minor): a
// config that another program holds open, as an editor or a scanner may,
// cannot be replaced; the owner reads that it is in use, not that the
// folder could not be protected.
func TestHubConfig_SaysWhenItsFileIsInUse(t *testing.T) {
	requireElevated(t)
	d, st, cur, vol := hubConfigRig(t)
	path, err := d.hubConfig(st, cur, vol)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path) //nolint:gosec // the test's config; it shares reading and writing, not deleting
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := d.hubConfig(st, cur, vol); err == nil || err.Error() != "nu pot scrie setările ipcam-hub în "+path+": fișierul este folosit de alt program" {
		t.Errorf("hubConfig over an open config = %v, want the Romanian line that it is in use", err)
	}
}

// TestHubConfig_RefusesAFileForItsFolder (Final fix C review, minor): a
// file at the protected folder's place, which any user may put there, is
// refused with the line that names a file and gives the del command, and
// stays as it is.
func TestHubConfig_RefusesAFileForItsFolder(t *testing.T) {
	requireElevated(t)
	d, st, cur, vol := hubConfigRig(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigDir, plantedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := d.hubConfig(st, cur, vol)
	if err == nil || !strings.Contains(err.Error(), "nu este un folder, ci un fișier") || !strings.Contains(err.Error(), `(del /f /a "`+d.ConfigDir+`")`) {
		t.Errorf("hubConfig over a file = %v, want a Romanian refusal that names a file and gives del", err)
	}
	if got, err := os.ReadFile(d.ConfigDir); err != nil || !bytes.Equal(got, plantedConfig) {
		t.Errorf("the file is now %q (%v), want it as planted", got, err)
	}
}

// setDACL gives path the DACL of sddl, as its owner may, through a handle
// that may read and change it (SetSecurityInfo reads the DACL that it
// replaces): what a folder holds keeps its own DACL, and nothing reads
// path's new one, which may hide itself.
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
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("open %s to change its DACL: %v", path, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
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

// TestBusy_SharingLockAndPendingDelete (Final fix D review, M6): what
// WriteProtected waits for is Windows' own refusal of a file that another
// program holds for a moment: a sharing or lock violation, or the access
// denied of a delete still pending. A full disk, or a file that is there
// again, is not waited for.
func TestBusy_SharingLockAndPendingDelete(t *testing.T) {
	for _, c := range []struct {
		err  windows.Errno
		want bool
	}{
		{windows.ERROR_SHARING_VIOLATION, true},
		{windows.ERROR_LOCK_VIOLATION, true},
		{windows.ERROR_ACCESS_DENIED, true},
		{windows.ERROR_DISK_FULL, false},
		{windows.ERROR_FILE_EXISTS, false},
	} {
		if got := busy(&os.PathError{Op: "create", Path: `C:\ProgramData\ipcam-recspike\ipcam-hub-mi2.json`, Err: c.err}); got != c.want {
			t.Errorf("busy(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// TestWriteProtected_WaitsForAConfigHeldOpenForAMoment (Final fix D
// review, M6): a config that another program holds open for a moment, as
// a scanner does just after it was written, is replaced once that program
// lets it go, within WriteProtected's 5 s.
func TestWriteProtected_WaitsForAConfigHeldOpenForAMoment(t *testing.T) {
	requireElevated(t)
	d, st, cur, vol := hubConfigRig(t)
	path, err := d.hubConfig(st, cur, vol)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path) //nolint:gosec // the test's config; it shares reading and writing, not deleting
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(time.Second)
		_ = f.Close()
	}()
	if err := WriteProtected(path, plantedConfig); err != nil {
		t.Errorf("WriteProtected over a config held open for 1 s: %v", err)
	}
	<-released
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, plantedConfig) { //nolint:gosec // the test's file
		t.Errorf("the config is %q (%v), want the new one", got, err)
	}
}
