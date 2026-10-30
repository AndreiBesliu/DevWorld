package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// TestServe_InTheProtectedReleaseFolder (security-1): `recspike install`
// gives the PC's release folder a protected DACL (mi1.ProtectFolderCommands),
// so that only an administrator changes the programs that the boot task,
// the shortcut and the bugcheck run elevated. reccontroller still runs
// there from both windows: an administrator's, as the guide says, and a
// standard one, whose token UAC filters. Each keeps its log in work/ and a
// host's MI9 envelope in results/. A standard window cannot add a file
// next to the programs, overwrite one, or change the DACL of the folder or
// of a program, although the owner's user extracted and owns them; once
// uninstall's reset (mi1.UnprotectFolderCommands) has run, it can delete
// the folder. The windows CI job runs elevated, as install does.
func TestServe_InTheProtectedReleaseFolder(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal("the windows job runs elevated, and this test needs it")
		}
		t.Skip("needs an elevated test process, as recspike install")
	}
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	sid, err := mi1.UserSID()
	if err != nil {
		t.Fatal(err)
	}
	// The release folder in the user's profile, whose permissions the
	// folder gets back from uninstall's reset, as C:\ gives them back on
	// the owner's PC: the user may delete what it holds.
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(cache, "proba-inregistrare-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, cmd := range mi1.UnprotectFolderCommands(dir) {
			_ = exec.Command(cmd[0], cmd[1:]...).Run() //nolint:gosec // icacls
		}
		_ = os.RemoveAll(dir)
	})

	// The release, as the owner's user extracted it: the programs (this
	// test binary as reccontroller.exe) and the media, owned by that user.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "reccontroller.exe")
	copyFile(t, self, program)
	for _, name := range []string{"main.h264", "sub.h264", "sub-256k.h264"} {
		copyFile(t, filepath.Join(media, name), filepath.Join(dir, "media", name))
	}
	setOwner(t, dir, sid)
	setOwner(t, program, sid)
	for _, name := range mi1.DataFolders {
		if err := os.MkdirAll(mi1.DataFolder(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range mi1.ProtectFolderCommands(dir, sid) {
		icacls(t, cmd)
	}
	t.Logf("the protected folder: %s", aclOf(dir))
	t.Logf("a program in it: %s", aclOf(program))

	standard := standardToken(t)
	asStandard(t, standard, func() {
		for what, change := range map[string]func() error{
			"added a file next to the programs": func() error { return os.WriteFile(filepath.Join(dir, "version.dll"), []byte("MZ"), 0o600) },
			"overwrote a program":               func() error { return os.WriteFile(program, []byte("MZ"), 0o600) },
			"changed the folder's DACL":         func() error { return grantFull(dir, sid) },
			"changed a program's DACL":          func() error { return grantFull(program, sid) },
		} {
			if err := change(); err == nil {
				t.Errorf("a standard window %s of the protected folder", what)
			}
		}
	})

	for _, tc := range []struct {
		window string
		token  windows.Token
		runID  string
	}{{"an administrator window", 0, "pc-mi9-1"}, {"a standard window", standard, "pc-mi9-2"}} {
		serveOnce(t, tc.window, tc.token, dir, tc.runID)
	}

	for _, cmd := range mi1.UnprotectFolderCommands(dir) {
		icacls(t, cmd)
	}
	asStandard(t, standard, func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("after uninstall's reset, a standard window cannot delete the folder: %v", err)
		}
	})
}

// serveOnce runs reccontroller.exe -local in dir with token (0: this
// process's own, an administrator's), sends it a host's MI9 envelope of
// runID, and checks that it kept it in results/ and its log in work/.
func serveOnce(t *testing.T, window string, token windows.Token, dir, runID string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(dir, "reccontroller.exe"), "-local") //nolint:gosec // the test's own binary
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), asMainEnv+"="+dir)
	if token != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(token)}
	}
	screen, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr syncBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("%s: reccontroller did not start: %v", window, err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	var shown syncBuffer
	port := make(chan string, 1)
	go func() {
		portRE := regexp.MustCompile(`legătură (\d+),`)
		lines := bufio.NewScanner(screen)
		for lines.Scan() {
			_, _ = shown.Write([]byte(lines.Text() + "\n"))
			if m := portRE.FindStringSubmatch(lines.Text()); m != nil {
				port <- m[1]
			}
		}
		close(port)
	}()
	p, ok := <-port
	if !ok {
		_ = cmd.Wait()
		t.Fatalf("%s: reccontroller ended before it listened; screen:\n%s\nstderr:\n%s", window, shown.String(), stderr.String())
	}
	conn, err := link.Dial(t.Context(), "127.0.0.1:"+p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.Send(link.Hello{Role: "host", Host: "pc", Version: link.Version}); err != nil {
		t.Fatal(err)
	}
	if m, err := conn.Receive(); err != nil {
		t.Fatalf("%s: no session: %v", window, err)
	} else if _, ok := m.(link.Session); !ok {
		t.Fatalf("%s: the host got %s, want its session", window, m.Type())
	}
	name := "MI9/lan/" + runID + ".json"
	if err := conn.Send(link.Results{Name: name, Data: mi9Envelope(t, runID, 300)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, window+": "+name+" in results", func() bool {
		_, err := os.Stat(filepath.Join(dir, "results", filepath.FromSlash(name)))
		return err == nil
	})
	// reccontroller says that it got the envelope once the envelope is in
	// results/: the line follows the file, so the test waits for it, as
	// TestMain_HangupKeepsServing does.
	logged := func() bool {
		log, err := os.ReadFile(filepath.Join(dir, "work", "reccontroller.log")) //nolint:gosec // the test's folder
		return err == nil && strings.Contains(string(log), "Am primit "+name+".")
	}
	for deadline := time.Now().Add(10 * time.Second); !logged(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: work/reccontroller.log lacks the envelope's line; screen:\n%s", window, shown.String())
		}
	}
}

var procCreateRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// standardToken is this elevated process's token as UAC filters it for a
// standard window: Administrators for deny only, no privilege beyond
// SeChangeNotifyPrivilege (DISABLE_MAX_PRIVILEGE, LUA_TOKEN), and medium
// integrity.
func standardToken(t *testing.T) windows.Token {
	t.Helper()
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &self); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = self.Close() }()
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	const disableMaxPrivilege, luaToken = 0x1, 0x4
	deny := windows.SIDAndAttributes{Sid: admins}
	var token windows.Token
	if r, _, err := procCreateRestrictedToken.Call(uintptr(self), disableMaxPrivilege|luaToken,
		1, uintptr(unsafe.Pointer(&deny)), 0, 0, 0, 0, uintptr(unsafe.Pointer(&token))); r == 0 { //nolint:gosec // CreateRestrictedToken's own arguments
		t.Fatalf("CreateRestrictedToken: %v", err)
	}
	t.Cleanup(func() { _ = token.Close() })
	medium, err := windows.CreateWellKnownSid(windows.WinMediumLabelSid)
	if err != nil {
		t.Fatal(err)
	}
	label := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: medium, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)), label.Size()); err != nil { //nolint:gosec // a TOKEN_MANDATORY_LABEL
		t.Fatal(err)
	}
	return token
}

// asStandard runs f on this goroutine's thread while it impersonates
// token, a standard window's.
func asStandard(t *testing.T, token windows.Token, f func()) {
	t.Helper()
	var imp windows.Token
	if err := windows.DuplicateTokenEx(token, windows.TOKEN_IMPERSONATE|windows.TOKEN_QUERY, nil,
		windows.SecurityImpersonation, windows.TokenImpersonation, &imp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = imp.Close() }()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.SetThreadToken(nil, imp); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.RevertToSelf(); err != nil {
			t.Fatal(err)
		}
	}()
	f()
}

// grantFull tries to give sid Full control of path, as only a DACL's
// writer may.
func grantFull(path, sid string) error {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(s)},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// setOwner makes sid the owner of path, as when that user made it.
func setOwner(t *testing.T, path, sid string) {
	t.Helper()
	s, err := windows.StringToSid(sid)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, s, nil, nil, nil); err != nil {
		t.Fatalf("owner of %s: %v", path, err)
	}
}

// icacls runs an icacls command, which must succeed.
func icacls(t *testing.T, cmd []string) {
	t.Helper()
	if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil { //nolint:gosec // icacls
		t.Fatalf("%q: %v: %s", cmd, err, out)
	}
}

// aclOf is what icacls says of path, on one line.
func aclOf(path string) string {
	out, err := exec.Command("icacls", path).CombinedOutput() //nolint:gosec // icacls
	if err != nil {
		return err.Error()
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// copyFile copies from to a new file to, making its folder.
func copyFile(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(from) //nolint:gosec // the test's files
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700) //nolint:gosec // the test's folder
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}
