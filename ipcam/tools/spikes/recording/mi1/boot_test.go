package mi1

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// TestRebootedSince_ComparesTheBoot (item 3): after an OS stop, the host
// rebooted when its boot is not the stop's: another boot id on Linux, or on
// Windows a boot time more than bootTimeTolerance from the stop's, whatever
// the uptimes say. Two readings of one Windows boot, a few seconds apart,
// are no reboot, and a boot that is not known on either side is no reboot
// either: the run is redone, never given a false ColdBootMs.
func TestRebootedSince_ComparesTheBoot(t *testing.T) {
	stopAt := time.Date(2026, 10, 25, 22, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time {
		t := stopAt.Add(d)
		return &t
	}
	for _, tc := range []struct {
		name     string
		stop     *Boot
		now      Boot
		nowErr   error
		rebooted bool
	}{
		{"Linux: another boot id", &Boot{ID: "a5f1"}, Boot{ID: "77c2"}, nil, true},
		{"Linux: the same boot id", &Boot{ID: "a5f1"}, Boot{ID: "a5f1"}, nil, false},
		{"Windows: the boot 4 min later", &Boot{At: at(-3 * time.Hour)}, Boot{At: at(4 * time.Minute)}, nil, true},
		{"Windows: the same boot, read 2 s apart", &Boot{At: at(-3 * time.Hour)}, Boot{At: at(-3*time.Hour + 2*time.Second)}, nil, false},
		{"no boot at the stop", nil, Boot{ID: "77c2"}, nil, false},
		{"no boot now", &Boot{ID: "a5f1"}, Boot{}, errors.New("no boot_id"), false},
	} {
		d := &Driver{ThisBoot: func() (Boot, error) { return tc.now, tc.nowErr }}
		if got := d.rebootedSince(&CurrentRun{StopBoot: tc.stop}); got != tc.rebooted {
			t.Errorf("%s: rebootedSince = %v, want %v", tc.name, got, tc.rebooted)
		}
	}
}

// TestResume_AfterARebootWithALongWaitForTheController (item 3, G09): a
// sysrq run stopped 3.5 min after its host booted; after the reboot, the
// controller is out of reach for minutes, so the resumed recspike connects
// at an uptime of 9 min, past the uptime of the stop. Its boot is another,
// so the run is finished as rebooted: reported, with its ColdBootMs, not
// refused as a stop after which the host did not reboot.
func TestResume_AfterARebootWithALongWaitForTheController(t *testing.T) {
	r := newRig(t, "pi")
	r.cutPower(t, SetOptions{Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: windowAroundNow(t)},
		func(d *Driver, down func()) {
			d.Uptime = func() (time.Duration, error) { return 3*time.Minute + 30*time.Second, nil }
			d.OSStop = func(context.Context, controller.ShutdownKind) error {
				down()
				return nil
			}
		})
	d := r.driver(t) // another boot
	d.Uptime = func() (time.Duration, error) { return 9 * time.Minute, nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.OSStop = func(context.Context, controller.ShutdownKind) error {
		cancel() // the next run's stop: the test has what it needs
		return nil
	}
	if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume = %v, want the test's cancel at the next run's stop", err)
	}
	killFakeHubs(t, r.log)
	r.f.mu.Lock()
	refused := append([]link.Refused(nil), r.f.refused...)
	r.f.mu.Unlock()
	reports := r.f.runReports()
	if len(refused) != 0 || len(reports) != 1 || reports[0].RunID != "pi-sysrq-01-1" || reports[0].ColdBootMs == nil ||
		*reports[0].ColdBootMs < (8*time.Minute).Milliseconds() || *reports[0].ColdBootMs > (9*time.Minute).Milliseconds() {
		t.Errorf("refusals %+v, reports %+v; want run 1 reported with the 9 min of uptime as its ColdBootMs", refused, reports)
	}
}

// TestThisBoot_ReadTwiceIsOneBoot: the system's boot, read twice, is known
// and is one boot, so that a host that did not reboot is never taken for
// one that did.
func TestThisBoot_ReadTwiceIsOneBoot(t *testing.T) {
	a, errA := ThisBoot()
	time.Sleep(20 * time.Millisecond)
	b, errB := ThisBoot()
	if errA != nil || errB != nil || (a.ID == "" && a.At == nil) || !a.same(b) {
		t.Errorf("ThisBoot = %+v, %v, then %+v, %v; want one known boot", a, errA, b, errB)
	}
}
