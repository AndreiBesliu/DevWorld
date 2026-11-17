package mi6

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

var t0 = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

// at is t0 plus d.
func at(d time.Duration) int64 { return t0.Add(d).UnixMilli() }

// TestRunState_RebootGapMarkedBlockedByTest (P49, P53): the run state's
// heartbeat finds a reboot that covers a refresh day's window; that day,
// with its attempts all inside the gap, is blocked-by-test, never failed,
// and the run's envelope carries the gap; a day that the hub had time for
// stays failed, and a hub crash is never relabelled.
func TestRunState_RebootGapMarkedBlockedByTest(t *testing.T) {
	r := NewRun("mi6-1", t0, "test", false, []string{"android", "windows", "web"})
	r.Completed("g-android", PlanA, "android", at(0))
	var s runstate.State
	s.Heartbeat(t0.Add(47*time.Hour), 47*time.Hour, "boot-1")
	// The host is off from hour 47 to hour 73: day 2's window, [48 h, 72 h).
	if g := s.Heartbeat(t0.Add(73*time.Hour), time.Minute, "boot-2"); g == nil || g.Reason != "reboot" {
		t.Fatalf("the heartbeat after the reboot gave %+v", g)
	}
	r.Refreshed("g-android", 1, 1, "ok", at(24*time.Hour))
	r.Refreshed("g-android", 2, 96, "failed", at(72*time.Hour-time.Minute))
	r.Refreshed("g-android", 3, 96, "failed", at(96*time.Hour))
	gaps := append(slices.Clone(s.Gaps), results.Gap{StartMs: at(96 * time.Hour), EndMs: at(120 * time.Hour), Reason: "hub-crash"})
	r.Refreshed("g-android", 4, 96, "failed", at(120*time.Hour))
	env := r.Envelope(Meta{Host: "pc", Set: "pc", OS: "windows", Storage: "ssd"}, gaps, nil, at(121*time.Hour))
	want := []results.RefreshDay{{Day: 1, Attempts: 1, Outcome: "ok"}, {Day: 2, Attempts: 96, Outcome: "blocked-by-test"},
		{Day: 3, Attempts: 96, Outcome: "failed"}, {Day: 4, Attempts: 96, Outcome: "failed"}}
	if got := env.MI6.Pairs[0].Refresh; !slices.Equal(got, want) {
		t.Errorf("g-android's days %+v; want %+v", got, want)
	}
	if g := env.Runs[0].Gaps; len(g) != 2 || g[0].Reason != "reboot" || g[1].Reason != "hub-crash" {
		t.Errorf("the run's gaps %+v; want the reboot and the crash", g)
	}
	if err := env.Validate(); err != nil {
		t.Error(err)
	}
	// A day without any event, whose window the host slept through, is the
	// test's too.
	r2 := NewRun("mi6-2", t0, "test", false, []string{"android"})
	r2.Completed("g-android", PlanA, "android", at(0))
	sleep := []results.Gap{{StartMs: at(23 * time.Hour), EndMs: at(49 * time.Hour), Reason: "sleep"}}
	if got := r2.Pair("g-android").Series(sleep, at(50*time.Hour)); !slices.Equal(got, []results.RefreshDay{{Day: 1, Outcome: "blocked-by-test"}}) {
		t.Errorf("a slept-through day: %+v", got)
	}
}

// TestMI6_HandoffCapIs20 (P39): a client's 21st login of the account is
// refused with the owner's text; another client still logs in.
func TestMI6_HandoffCapIs20(t *testing.T) {
	handoffs := map[string]int{}
	for i := range HandoffCap {
		if err := AllowHandoff(handoffs, ClientFor(Drive[0], PlanA)); err != nil {
			t.Fatalf("login %d: %v", i+1, err)
		}
		CountHandoff(handoffs, ProviderDrive, "android")
	}
	err := AllowHandoff(handoffs, "google:android")
	if !errors.Is(err, ErrCap) || err.Error() != "Ați atins limita de 20 de autentificări pentru acest client. Spuneți-ne." {
		t.Errorf("the 21st login: %v; want the cap's text", err)
	}
	if err := AllowHandoff(handoffs, ClientFor(Drive[1], PlanA)); err != nil {
		t.Errorf("the Desktop client after Android's cap: %v", err)
	}
	if handoffs["acct-1/google:android"] != 20 {
		t.Errorf("handoffs %v", handoffs)
	}
	for _, tc := range []struct {
		def  PairDef
		plan string
		want string
	}{
		{Drive[0], PlanA, "google:android"}, {Drive[1], PlanA, "google:desktop"}, {Drive[2], PlanB1, "google:tv"},
		{Drive[0], PlanB1, "google:tv"}, {OneDrive[1], PlanA, "microsoft:entra"},
	} {
		if got := ClientFor(tc.def, tc.plan); got != tc.want {
			t.Errorf("ClientFor(%s, %s) = %s, want %s", tc.def.Dest, tc.plan, got, tc.want)
		}
	}
}

// TestMI6_FailuresByKind (C2-R16 (c)): a refusal of plan A's redemption is
// the pair's plan A failure; of B1's device flow, B1's; a transient or
// unknown failure counts apart and judges nothing; an expired state
// records nothing.
func TestMI6_FailuresByKind(t *testing.T) {
	r := NewRun("mi6-1", t0, "test", false, []string{"android"})
	p := r.Pair("g-android")
	if k := r.Failed("g-android", "complete", "transient"); k != FailTransient || p.Transient != 1 || p.PlanAFailure != "" {
		t.Errorf("transient: %s, %+v", k, p)
	}
	if k := r.Failed("g-android", "complete", "error"); k != FailTransient || p.Transient != 2 {
		t.Errorf("error: %s, %+v", k, p)
	}
	if k := r.Failed("g-android", "complete", "state"); k != FailExpired || p.Transient != 2 || p.PlanAFailure != "" {
		t.Errorf("state: %s, %+v", k, p)
	}
	if k := r.Failed("g-android", "complete", "invalid_grant"); k != FailRefusal || p.PlanAFailure != "invalid_grant" || p.Plan != PlanNone {
		t.Errorf("plan A's refusal: %s, %+v", k, p)
	}
	if k := r.Failed("g-android", "device", "access_denied"); k != FailRefusal || p.Plan != PlanB1 || p.HandoffOK || p.B1Failure != "access_denied" {
		t.Errorf("B1's refusal: %s, %+v", k, p)
	}
	r.Completed("g-android", PlanB1, "tv", at(time.Hour))
	if !p.HandoffOK || p.B1Failure != "" || p.PlanAFailure != "invalid_grant" {
		t.Errorf("after B1's login: %+v", p)
	}
	env := r.Envelope(Meta{Host: "pc", Set: "pc", Storage: "ssd"}, nil, nil, at(2*time.Hour))
	if got := env.MI6.Pairs[0]; got.RefreshFrom != "g-web" || got.Transient != 2 {
		t.Errorf("the B1 pair's envelope: %+v; want g-web's series as its evidence", got)
	}
}

// TestMI6_StatusAsksB1ForAFailedDay: a failed refresh day of g-android
// under plan A makes the status ask for its B1, worded for after MI5 while
// an MI5 run is open; a blocked day does not.
func TestMI6_StatusAsksB1ForAFailedDay(t *testing.T) {
	r := NewRun("mi6-1", t0, "test", false, []string{"android", "windows"})
	r.Completed("g-android", PlanA, "android", at(0))
	r.Completed("g-windows", PlanA, "desktop", at(0))
	r.Refreshed("g-android", 1, 96, "failed", at(48*time.Hour))
	r.Refreshed("g-windows", 1, 1, "ok", at(25*time.Hour))
	now := t0.Add(49 * time.Hour)
	lines := StatusLines(r, nil, now, false)
	if !slices.Contains(lines, "Reconectați Android cu B1: uploadspike mi6 --b1 android") ||
		slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "Reconectați Windows") }) {
		t.Errorf("status %q; want the Android reconnection alone", lines)
	}
	if lines := StatusLines(r, nil, now, true); !slices.Contains(lines, "Reconectați Android cu B1, după proba MI5: uploadspike mi6 --b1 android") {
		t.Errorf("status with MI5 open %q", lines)
	}
	if lines[0] != "MI6: ziua 2 din 7 (pornită pe 17.11.2026)." {
		t.Errorf("header %q", lines[0])
	}
	reboot := []results.Gap{{StartMs: at(23 * time.Hour), EndMs: at(49 * time.Hour), Reason: "reboot"}}
	if lines := StatusLines(r, reboot, now, false); slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "Reconectați") }) {
		t.Errorf("status with the day blocked by the test %q; want no reconnection", lines)
	}
}

// TestRun_CloseAtCoversEveryDay0Login (Review Focus 1, P53): pair days
// count from each login's issuance, so the run closes 7 days and 2 h after
// the last of day 0's first logins, not after the run's open: a login
// made a day after the open (a second sitting of day 0) still has its
// days 1 to 6 before the close. A later B1 login (after a failed refresh
// day) does not extend the 7-day schedule.
func TestRun_CloseAtCoversEveryDay0Login(t *testing.T) {
	r := NewRun("mi6-1", t0, "test", false, []string{"android", "windows", "web"})
	r.Completed("g-android", PlanA, "android", at(time.Hour))
	r.Completed("g-windows", PlanA, "desktop", at(20*time.Hour))
	r.Completed("g-web", PlanB1, "tv", at(30*time.Hour))
	if want := t0.Add(30*time.Hour + Days*day + 2*time.Hour); !r.CloseAt().Equal(want) {
		t.Errorf("CloseAt %v; want %v, 7 days and 2 h after the last day-0 login", r.CloseAt(), want)
	}
	for _, p := range r.Pairs {
		for d := 1; d <= 6; d++ {
			r.Refreshed(p.Dest, d, 1, results.OutcomeOK, p.IssuedAtMs+int64(d)*day.Milliseconds()+time.Minute.Milliseconds())
		}
	}
	env := r.Envelope(Meta{Host: "pc", Set: "pc", OS: "windows", Storage: "ssd"}, nil, nil, r.CloseAt().UnixMilli())
	for _, p := range env.MI6.Pairs {
		if len(p.Refresh) != 6 || p.Refresh[5].Day != 6 || p.Refresh[5].Outcome != results.OutcomeOK {
			t.Errorf("%s at the close: %+v; want days 1 to 6 ok", p.Dest, p.Refresh)
		}
	}
	r.LoginsDone = true
	closeAt := r.CloseAt()
	r.Completed("g-android", PlanB1, "tv", at(3*24*time.Hour))
	if !r.CloseAt().Equal(closeAt) {
		t.Errorf("a B1 login on day 3 moved the close from %v to %v", closeAt, r.CloseAt())
	}
}

// TestRun_ReanchorWhileNoLogin (P53, C2-R1): a run whose pairs have no
// login yet, nor any measured refusal, takes a new day 0 and the new
// command's settings, keeping its ID and its transient failures; a run
// with a login, or with a measured refusal, keeps its day 0.
func TestRun_ReanchorWhileNoLogin(t *testing.T) {
	old := NewRun("mi6-1", t0, "test", false, []string{"android"})
	old.Failed("g-android", "complete", "transient")
	if !old.Reanchorable() {
		t.Fatal("a run with a transient failure alone is not reanchorable")
	}
	later := t0.Add(30 * time.Hour)
	r := old.Reanchor(later, "personal", false, []string{"android", "web"})
	if r.RunID != "mi6-1" || r.StartMs != later.UnixMilli() || r.Drive != "personal" || !slices.Equal(r.Platforms, []string{"android", "web"}) ||
		len(r.Pairs) != 2 || r.Pair("g-android").Transient != 1 || r.Pair("g-web") == nil {
		t.Errorf("the reanchored run %+v; want day 0 at %v, the new settings, the ID and the transient count", r, later)
	}
	for name, edit := range map[string]func(*Run){
		"a login":        func(r *Run) { r.Completed("g-android", PlanA, "android", at(time.Hour)) },
		"plan A refused": func(r *Run) { r.Failed("g-android", "complete", "invalid_grant") },
		"B1 refused":     func(r *Run) { r.Failed("g-android", "device", "access_denied") },
		"visibility cell": func(r *Run) {
			r.Visibility = append(r.Visibility, results.VisibilityCell{Creator: "android", Reader: "tv"})
		},
	} {
		run := NewRun("mi6-1", t0, "test", false, []string{"android"})
		edit(run)
		if run.Reanchorable() {
			t.Errorf("a run with %s is reanchorable", name)
		}
	}
}
