package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/install"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// cmdLog records the commands that install and uninstall run.
type cmdLog struct {
	mu   sync.Mutex
	cmds []string
}

func (l *cmdLog) run(_ context.Context, name string, args []string, stdin string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cmds = append(l.cmds, strings.TrimSpace(strings.Join(append([]string{name}, args...), " ")+" "+stdin))
	return "", nil
}

func (l *cmdLog) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.cmds, "\n")
}

// installEnv is an env whose install touches a temporary prefix through
// log, with this user as root and as the hub's identity.
func installEnv(t *testing.T, log *cmdLog) (*env, *installSys) {
	t.Helper()
	e, _ := unitEnv(t)
	sys := &installSys{prefix: t.TempDir(), run: log.run, rootUID: os.Getuid(), rootGID: os.Getgid(),
		ensureUser: func(ctx context.Context, run install.Commander) (int, int, error) {
			_ = install.RunAll(ctx, run, install.UserCommands())
			return os.Getuid(), os.Getgid(), nil
		}}
	e.sys, e.isRoot = sys, true
	e.base = filepath.Join(sys.prefix, install.VarDir)
	e.firewallFor = func(binDir string) mi5.WanBlock { return mi5.FirewallFor(runtime.GOOS, binDir, log.run) }
	return e, sys
}

// TestInstall_PiAsksSdConfirmation (P02, P49): on a Pi whose rec/ is on
// an SD card, install asks the confirmation, keeps the owner's DA in the
// run state, then installs and starts the unit and records the autostart.
func TestInstall_PiAsksSdConfirmation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Pi's install")
	}
	t.Setenv(spiketest.FakeStorageEnv, "sd")
	hub, _ := spiketest.InstallFakeHub(t, t.TempDir())
	log := &cmdLog{}
	e, sys := installEnv(t, log)
	e.hubBin, e.in = hub, strings.NewReader("DA\n")
	if err := installOS(t.Context(), e, false); err != nil {
		t.Fatal(err)
	}
	st, err := runstate.Load(e.statePath())
	if err != nil || st.Sd == nil || st.Sd.Text != "DA" || !st.Autostart {
		t.Fatalf("the run state: %+v, %v; want the SD confirmation and the autostart", st, err)
	}
	unit, err := os.ReadFile(filepath.Join(sys.prefix, install.UnitPath))
	if err != nil || !strings.Contains(string(unit), "ExecStart=/opt/ipcam-spike/uploadspike resume") {
		t.Errorf("the unit: %v\n%s", err, unit)
	}
	if j := log.joined(); !strings.Contains(j, "useradd --system --no-create-home --shell /usr/sbin/nologin ipcam-spike") ||
		!strings.Contains(j, "systemctl enable --now ipcam-uploadspike.service") {
		t.Errorf("commands:\n%s", j)
	}
	// --lab: the user and the folders, no question, no unit.
	log2 := &cmdLog{}
	e2, sys2 := installEnv(t, log2)
	e2.hubBin, e2.in = hub, strings.NewReader("")
	if err := installOS(t.Context(), e2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sys2.prefix, install.UnitPath)); err == nil || strings.Contains(log2.joined(), "systemctl") {
		t.Errorf("--lab installed the unit:\n%s", log2.joined())
	}
	if st, _ := runstate.Load(e2.statePath()); st.Sd != nil || st.Autostart {
		t.Errorf("--lab: %+v", st)
	}
}

// TestUninstall_RemovesTaskUnitRulesTimerAndUser (P49): uninstall stops
// and removes the unit (the task on Windows), the block's rules and its
// cleanup timer, the hub's user and /opt/ipcam-spike; the data stays.
func TestUninstall_RemovesTaskUnitRulesTimerAndUser(t *testing.T) {
	log := &cmdLog{}
	e, sys := installEnv(t, log)
	opt := filepath.Join(sys.prefix, install.OptDir)
	if err := os.MkdirAll(opt, 0o755); err != nil { //nolint:gosec // the test's folder
		t.Fatal(err)
	}
	data := filepath.Join(e.base, "hub", "catalog.db")
	if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setAutostart(e, true); err != nil {
		t.Fatal(err)
	}
	if err := runUninstall(t.Context(), e, nil); err != nil {
		t.Fatal(err)
	}
	j := log.joined()
	var want []string
	if runtime.GOOS == "windows" {
		want = []string{"schtasks /End /TN ipcam-uploadspike", "schtasks /Delete /TN ipcam-uploadspike /F", "Remove-NetFirewallRule -Group 'ipcam-spike-wanblock'",
			"Unregister-ScheduledTask -TaskName 'ipcam-spike-unblock'", "Remove-NetFirewallRule -Group 'ipcam-spike-inbound'"}
	} else {
		want = []string{"systemctl disable --now ipcam-uploadspike.service", "nft delete table inet ipcam_spike", "systemctl stop ipcam-spike-unblock.timer",
			"userdel ipcam-spike"}
		if _, err := os.Stat(opt); err == nil {
			t.Error("/opt/ipcam-spike stays")
		}
	}
	for _, w := range want {
		if !strings.Contains(j, w) {
			t.Errorf("uninstall did not run %q:\n%s", w, j)
		}
	}
	if _, err := os.Stat(data); err != nil {
		t.Errorf("the data went: %v", err)
	}
	if st, _ := runstate.Load(e.statePath()); st.Autostart {
		t.Error("the autostart stays recorded")
	}
}

// TestUnblock_RemovesTheBlock: unblock removes the rule group or table,
// and says so.
func TestUnblock_RemovesTheBlock(t *testing.T) {
	log := &cmdLog{}
	e, _ := installEnv(t, log)
	if err := runUnblock(t.Context(), e, nil); err != nil {
		t.Fatal(err)
	}
	want := "nft delete table inet ipcam_spike"
	if runtime.GOOS == "windows" {
		want = "Remove-NetFirewallRule -Group 'ipcam-spike-wanblock'"
	}
	if !strings.Contains(log.joined(), want) {
		t.Errorf("unblock ran:\n%s", log.joined())
	}
	e.isRoot = false
	if runtime.GOOS == "linux" {
		if err := runUnblock(t.Context(), e, nil); err == nil || err.Error() != msgNeedsSudo {
			t.Errorf("unblock without sudo: %v", err)
		}
	}
}

// TestAnnotate_EmitRawJSONLines (c1 C32, P09): one raw JSON line per
// envelope, with no prefix, each at most 3,500 bytes after annotate.sh's
// escaping, at most 10.
func TestAnnotate_EmitRawJSONLines(t *testing.T) {
	e, out := unitEnv(t)
	write := func(id string, pad int) {
		env := results.Envelope{Schema: results.Schema, Code: "MI5", RunID: id, Set: "lab-linux", Host: "lab-linux", OS: "linux", Storage: "ssd",
			Runs: []results.Run{{RunID: id, Valid: true}}, MI5: &results.Mi5Data{Lab: true, BlockSecs: 900,
				Dests: []results.Mi5Dest{{Dest: "dst-1", Kind: "GOOGLE_DRIVE", Account: "acct-1", Deferred: strings.Repeat("%", pad)}}}}
		if err := results.Write(e.resultsDir(), env); err != nil {
			t.Fatal(err)
		}
	}
	write("mi5-a", 0)
	write("mi5-b", 10)
	if err := runAnnotate(t.Context(), e, []string{"emit", "--code", "MI5", "--set", "lab-linux"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines:\n%s", len(lines), out.String())
	}
	for _, l := range lines {
		var env results.Envelope
		if err := json.Unmarshal([]byte(l), &env); err != nil || !strings.HasPrefix(l, "{") || env.MI5 == nil {
			t.Errorf("line %q: %v", l, err)
		}
		if n := len(l) + 2*strings.Count(l, "%"); n > maxAnnotationBytes {
			t.Errorf("a line of %d escaped bytes", n)
		}
	}
	// A line past 3,500 escaped bytes fails, as do 11 envelopes.
	write("mi5-c", 1200)
	if err := runAnnotate(t.Context(), e, []string{"emit", "--code", "MI5", "--set", "lab-linux"}); err == nil {
		t.Error("a line of 3,600 escaped bytes: no error")
	}
	if err := os.Remove(filepath.Join(e.resultsDir(), "MI5", "lab-linux", "mi5-c.json")); err != nil {
		t.Fatal(err)
	}
	for i := range 9 {
		write(fmt.Sprintf("mi5-%02d", i), 0)
	}
	if err := runAnnotate(t.Context(), e, []string{"emit", "--code", "MI5", "--set", "lab-linux"}); err == nil {
		t.Error("11 envelopes: no error")
	}
}
