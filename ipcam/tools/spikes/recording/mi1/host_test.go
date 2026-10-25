package mi1

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// TestPreflight_RefusesWithRomanianReason (G14, R08): before a window
// opens, recspike checks the host and the link; each failing check gives
// its own Romanian reason, all of them at once, and with every check
// passing the window opens.
func TestPreflight_RefusesWithRomanianReason(t *testing.T) {
	now := time.Date(2026, 10, 25, 21, 0, 0, 0, time.UTC)
	pcOK := PCProbes{
		NotMyFault:         func() error { return nil },
		Elevated:           func() bool { return true },
		AutoReboot:         func() (uint32, error) { return 1, nil },
		UpdatesPausedUntil: func() (time.Time, error) { return now.Add(7 * 24 * time.Hour), nil },
		Now:                func() time.Time { return now },
	}
	piOK := PiProbes{Euid: func() int { return 0 }, FreeBytes: func() (uint64, error) { return 100 << 30, nil }, Cameras: 4}
	linkOK := LinkProbes{
		Beacon: func(context.Context) error { return nil },
		RTSP:   func(context.Context) error { return nil },
		NTP:    func(context.Context) (bool, error) { return true, nil },
	}
	if err := Preflight(t.Context(), append(PCChecks(pcOK), LinkChecks(linkOK)...)); err != nil {
		t.Errorf("a PC that passes every check: %v", err)
	}
	if err := Preflight(t.Context(), append(PiChecks(piOK), LinkChecks(linkOK)...)); err != nil {
		t.Errorf("a Pi that passes every check: %v", err)
	}

	cases := []struct {
		name   string
		checks []Check
		want   string
	}{
		{"NotMyFault missing or unsigned", PCChecks(func() PCProbes {
			p := pcOK
			p.NotMyFault = func() error { return errors.New("TRUST_E_NOSIGNATURE") }
			return p
		}()), "notmyfault64.exe lipsește sau nu e semnat de Microsoft"},
		{"not elevated", PCChecks(func() PCProbes { p := pcOK; p.Elevated = func() bool { return false }; return p }()), "administrator"},
		{"AutoReboot 0", PCChecks(func() PCProbes {
			p := pcOK
			p.AutoReboot = func() (uint32, error) { return 0, nil }
			return p
		}()), "AutoReboot"},
		{"Windows Update not paused", PCChecks(func() PCProbes {
			p := pcOK
			p.UpdatesPausedUntil = func() (time.Time, error) { return now.Add(-time.Hour), nil }
			return p
		}()), "Windows Update"},
		{"Windows Update never paused", PCChecks(func() PCProbes {
			p := pcOK
			p.UpdatesPausedUntil = func() (time.Time, error) { return time.Time{}, errors.New("not found") }
			return p
		}()), "Windows Update"},
		{"not root", PiChecks(func() PiProbes { p := piOK; p.Euid = func() int { return 1000 }; return p }()), "sudo"},
		{"too little space", PiChecks(func() PiProbes {
			p := piOK
			p.FreeBytes = func() (uint64, error) { return 3 << 30, nil } // 4 cameras need 1.44 GB + 2 GiB
			return p
		}()), "spațiu liber"},
		{"no beacon at the controller", LinkChecks(func() LinkProbes {
			p := linkOK
			p.Beacon = func(context.Context) error { return errors.New("no ack") }
			return p
		}()), "UDP"},
		{"no RTSP pull", LinkChecks(func() LinkProbes {
			p := linkOK
			p.RTSP = func(context.Context) error { return errors.New("401") }
			return p
		}()), "RTSP"},
		{"NTP not synced", LinkChecks(func() LinkProbes {
			p := linkOK
			p.NTP = func(context.Context) (bool, error) { return false, nil }
			return p
		}()), "NTP"},
	}
	for _, tc := range cases {
		err := Preflight(t.Context(), tc.checks)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "Proba nu pornește") {
			t.Errorf("%s: Preflight = %v, want «Proba nu pornește…» with %q", tc.name, err, tc.want)
		}
	}

	// Several failures are listed together.
	p := pcOK
	p.Elevated = func() bool { return false }
	p.AutoReboot = func() (uint32, error) { return 0, nil }
	if err := Preflight(t.Context(), PCChecks(p)); err == nil || !strings.Contains(err.Error(), "administrator") || !strings.Contains(err.Error(), "AutoReboot") {
		t.Errorf("two failures: %v, want both reasons", err)
	}

	// The free space that a Pi needs: cameras × 4 Mbps × 12 min + 2 GiB.
	if got, want := RequiredFreeBytes(4), uint64(4*4_000_000/8*12*60+2<<30); got != want {
		t.Errorf("RequiredFreeBytes(4) = %d, want %d", got, want)
	}
}

// TestPreflight_PCLooksForNotMyFaultNextToRecspike (G14): the PC's
// preflight checks notmyfault64.exe where the bugcheck runs it, in
// recspike's folder (next to ipcam-hub), where the refusal tells the owner
// to put it; not in the private work folder. The free space is the work
// folder's, where the runs record.
func TestPreflight_PCLooksForNotMyFaultNextToRecspike(t *testing.T) {
	dir := t.TempDir() // recspike's folder
	d := &Driver{Hub: filepath.Join(dir, "ipcam-hub.exe"), Results: filepath.Join(dir, "results"), Work: filepath.Join(dir, "work"),
		GOOS: "windows", Host: HostState{Pseudonym: hostPC, OS: "windows", Cameras: 8}}
	if err := os.MkdirAll(d.Work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, NotMyFault), []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	var bin, data string
	var cameras int
	d.deviceChecks = func(binDir, dataDir string, n int) []Check {
		bin, data, cameras = binDir, dataDir, n
		return PCChecks(PCProbes{
			NotMyFault: func() error {
				_, err := os.Stat(filepath.Join(binDir, NotMyFault))
				return err
			},
			Elevated:           func() bool { return true },
			AutoReboot:         func() (uint32, error) { return 1, nil },
			UpdatesPausedUntil: func() (time.Time, error) { return time.Now().Add(time.Hour), nil },
			Now:                time.Now,
		})
	}
	var stoppedFrom string
	d.osStopIn = func(_ context.Context, _ controller.ShutdownKind, binDir string) error {
		stoppedFrom = binDir
		return nil
	}
	linkOK := LinkProbes{
		Beacon: func(context.Context) error { return nil },
		RTSP:   func(context.Context) error { return nil },
		NTP:    func(context.Context) (bool, error) { return true, nil },
	}
	if err := d.preflight(t.Context(), ModeAutomated, linkOK); err != nil {
		t.Errorf("preflight with notmyfault64.exe next to recspike: %v", err)
	}
	if err := d.osStop(t.Context(), controller.KindBugcheck); err != nil {
		t.Fatal(err)
	}
	if bin != dir || stoppedFrom != dir {
		t.Errorf("the preflight looks for NotMyFault in %q and the bugcheck runs it from %q, want both in recspike's folder %q", bin, stoppedFrom, dir)
	}
	if data != d.Work || cameras != 8 {
		t.Errorf("free space of %q for %d cameras, want the work folder %q for 8", data, cameras, d.Work)
	}
}

// TestPreflight_NTPRefusalNamesTheRemedy (G14): when w32tm cannot answer, the
// preflight says why and what to do, in Romanian: run recspike as
// administrator, or start the Windows time service; a clock that is merely
// not synced says so.
func TestPreflight_NTPRefusalNamesTheRemedy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check NTPCheck
		want  []string
	}{
		{"no administrator rights", func(context.Context) (bool, error) { return false, w32tmError("Access is denied. (0x80070005)", 1) },
			[]string{"administrator"}},
		{"the time service stopped", func(context.Context) (bool, error) {
			return false, w32tmError("The service has not been started. (0x80070426)", 1)
		}, []string{"net start w32time", "w32tm /resync"}},
		{"not synced", func(context.Context) (bool, error) { return false, nil }, []string{"nu este sincronizat prin NTP"}},
	} {
		err := Preflight(t.Context(), LinkChecks(LinkProbes{
			Beacon: func(context.Context) error { return nil },
			RTSP:   func(context.Context) error { return nil },
			NTP:    tc.check,
		}))
		for _, want := range tc.want {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: Preflight = %v, want %q", tc.name, err, want)
			}
		}
	}
}
