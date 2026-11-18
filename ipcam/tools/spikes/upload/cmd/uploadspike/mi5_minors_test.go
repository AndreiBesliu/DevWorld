package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
)

// isOwnerLine reports whether err's text is a whole line for the owner,
// which report prints without «uploadspike: eroare:».
func isOwnerLine(err error) bool {
	var owner ownerError
	var line interface{ OwnerLine() }
	return errors.As(err, &owner) || errors.As(err, &line)
}

// TestMI5_DestProbeFailureIsAnOwnerLine (C2-R29 (1), Task 9B M9): a
// destination that diag dest-probe cannot check before the run ends it
// with the owner's Romanian line, the diag's error kept as its cause.
func TestMI5_DestProbeFailureIsAnOwnerLine(t *testing.T) {
	hub, _ := spiketest.InstallFakeHub(t, t.TempDir()) // its dest-probe exits 1 without a quota
	e, _ := unitEnv(t)
	e.hubBin = hub
	cfg := mi5.Mi5Config{Cameras: 4, SegmentSecs: 300, Drive: mi5.DriveTest, BlockFor: 15 * time.Minute}
	c := &mi5Cmd{e: e, h: &holder{e: e}}
	err := c.preChecks(t.Context(), cfg, &mi5Comp{cfg: cfg, dests: mi5.Destinations(cfg), phase: mi5.PhaseFinal})
	var de *hubproc.DiagError
	if want := fmt.Sprintf(mi5.MsgDestProbeFailed, "dst-1"); err == nil || err.Error() != want || !isOwnerLine(err) || !errors.As(err, &de) {
		t.Errorf("the pre-checks' failure %v (an owner's line: %v); want %q, the diag's error its cause", err, isOwnerLine(err), want)
	}
}

// TestMI5_FailureDetailOnlyWithSeams (C2-R29 (1)): a run that ends with
// one of MI5's own failures shows the owner its Romanian line only; the
// lab's and the tests' runs, which test seams mark and nobody judges, also
// print the failure's cause on a detail line, which their annotations
// keep.
func TestMI5_FailureDetailOnlyWithSeams(t *testing.T) {
	for _, seams := range []bool{false, true} {
		e, out := unitEnv(t)
		e.seams = seams
		e.firewallFor = func(dir string) mi5.WanBlock { // records nothing, runs nothing
			return mi5.FirewallFor(runtime.GOOS, dir, func(context.Context, string, []string, string) (string, error) { return "", nil })
		}
		c := &mi5Cmd{e: e, h: &holder{e: e}, host: "pc", open: mi5Open{RunID: "mi5-1", StartMs: time.Now().UnixMilli()}}
		cause := errors.New("ipcam-hub: diag uploads: the cause's own words")
		_ = c.fail(t.Context(), &mi5.Failure{Line: mi5.MsgAuditFailed, Cause: cause})
		if got := strings.Contains(out.String(), cause.Error()); got != seams {
			t.Errorf("seams %v: the cause printed %v; want %v:\n%s", seams, got, seams, out)
		}
	}
}

// TestMI5_CamerasCappedOnAPi (P17, P38, C2-R29 (2)): --cameras never
// takes a Pi above min(4, its model's RecommendedMax): on a Pi 3 (2),
// --cameras 4 records 2; on the PC, --cameras 3 records 3 and 9 records 4.
func TestMI5_CamerasCappedOnAPi(t *testing.T) {
	for _, tc := range []struct {
		pi            string
		flag, cameras int
	}{
		{`{"raw":"Raspberry Pi 3 Model B Plus Rev 1.3","tier":"pi3","known":true,"supported":true,"recommendedMax":2}`, 4, 2},
		{`{"raw":"Raspberry Pi 5 Model B Rev 1.0","tier":"pi5","known":true,"supported":true,"recommendedMax":8}`, 3, 3},
		{"", 3, 3},
		{"", 9, 4},
	} {
		t.Setenv(spiketest.FakePiModelEnv, tc.pi)
		hub, _ := spiketest.InstallFakeHub(t, t.TempDir())
		e, _ := unitEnv(t)
		e.hubBin = hub
		c := &mi5Cmd{e: e, f: mi5Flags{drive: mi5.DriveTest, block: 15 * time.Minute, segmentSecs: 300, camera: tc.flag}}
		cfg, err := c.config(t.Context())
		if err != nil || cfg.Cameras != tc.cameras {
			t.Errorf("Pi %q, --cameras %d: %d cameras (%v); want %d", tc.pi, tc.flag, cfg.Cameras, err, tc.cameras)
		}
	}
}

// TestMI5_RefusalsWriteNoEnvelope (P38, P53, C2-R29 (3) and (4)): a run
// that its pre-checks refuse (a destination's quota, the spool, a daily
// refresh inside the block's window) never started: mi5 ends with the
// refusal's own Romanian line, writes no MI5 envelope, closes the run
// and starts neither a hub nor the cameras. The refresh check takes the
// block's start from the run's own timing: the lab's 2-min warm-up
// starts it 8 min earlier than the owner's.
func TestMI5_RefusalsWriteNoEnvelope(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name     string
		args     []string
		quota    string
		refresh  time.Time // the MI6 token's first daily refresh
		wantLine string
	}{
		{"quota", []string{"--block", "15m"}, "1000", now.Add(22 * time.Hour),
			fmt.Sprintf(mi5.MsgQuotaLow, "dst-1", "0,0 GB", "0,5 GB", "15 min")},
		{"refresh", []string{"--block", "15m"}, "1000000000000", now.Add(40 * time.Minute),
			"Blocarea ar acoperi reîmprospătarea zilnică a g-android ("},
		{"refresh-lab", []string{"--lab", "--block", "15m"}, "1000000000000", now.Add(15 * time.Minute),
			"Blocarea ar acoperi reîmprospătarea zilnică a g-android ("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(spiketest.FakeQuotaFreeEnv, tc.quota)
			r := newRig(t)
			r.openMI6("android")
			r.editMI6(func(run *mi6.Run) {
				run.Completed("g-android", mi6.PlanA, "android", tc.refresh.Add(-24*time.Hour).UnixMilli())
			})
			_, seam := r.firewallLog()
			p := r.run("", []string{seam, envLabLAN + "=127.0.0.1"}, append([]string{"mi5"}, tc.args...)...)
			if p.code != 1 || !strings.Contains(p.stderr.String(), tc.wantLine) || strings.Contains(p.stderr.String(), msgError) {
				t.Errorf("mi5: exit %d, stderr %q; want 1 and the refusal's own line %q", p.code, p.stderr, tc.wantLine)
			}
			if files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI5", "*", "*.json")); len(files) != 0 {
				t.Errorf("a refused run wrote MI5 envelopes %v", files)
			}
			if _, open := r.state().Open["MI5"]; open {
				t.Error("the refused run is still open")
			}
			if starts := r.records("start"); len(starts) != 0 {
				t.Errorf("a refused run started hubs %v", starts)
			}
		})
	}
}

// TestMI5_DeviceLoginBounded (C2-R29 (7)): a device login whose status the
// hub never answers (its hub restarted, the login's state gone) ends after
// 31 min, as the holder's pollDevice does, with the owner's line
// «Autentificarea pentru … nu a reușit …», instead of leaving the owner on
// the code's line for ever.
func TestMI5_DeviceLoginBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/control/v1/oauth/begin" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"st-1","plan":"B1","verificationUrl":"https://example.invalid/device","userCode":"ABCD-EFGH"}`))
			return
		}
		http.Error(w, `{"reason":"unknown state"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	e, _ := unitEnv(t)
	var calls atomic.Int64
	start := time.Now()
	e.now = func() time.Time { return start.Add(time.Duration(calls.Add(1)) * 10 * time.Minute) }
	c := &mi5Cmd{e: e, h: &holder{e: e, ctl: hubproc.NewControlClient(strings.TrimPrefix(srv.URL, "http://"), strings.Repeat("ab", 32))}}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, err := c.deviceLogin(ctx, mi5.DestConfig{ID: "dst-1", Kind: mi5.KindDrive, Account: "acct-1", TokenRef: "dst-1"})
	if want := fmt.Sprintf(msgMI5LoginFailed, "dst-1", msgMI5LoginNoAnswer); err == nil || err.Error() != want || !isOwnerLine(err) {
		t.Errorf("the login's end %v; want %q", err, want)
	}
}

// staleBlock is a firewall whose stale block will not go: RemoveStale
// fails with err, and nothing else is called.
type staleBlock struct{ err error }

func (s staleBlock) Block(context.Context) error                      { return nil }
func (s staleBlock) Unblock(context.Context) error                    { return nil }
func (s staleBlock) RemoveStale(context.Context) error                { return s.err }
func (s staleBlock) ScheduleCleanup(context.Context, time.Time) error { return nil }

// TestMI5_StaleBlockThatStaysIsAnOwnerLine (C2-R29 (1)): mi5 removes a
// block that an earlier run left before it logs in; when the firewall
// will not remove it, the owner reads how to remove it (uploadspike
// unblock), in Romanian, instead of the firewall's own message.
func TestMI5_StaleBlockThatStaysIsAnOwnerLine(t *testing.T) {
	t.Setenv(envLabLAN, "127.0.0.1")
	hub, _ := spiketest.InstallFakeHub(t, t.TempDir())
	e, _ := unitEnv(t)
	e.hubBin = hub
	refused := errors.New("nft: exit status 1: Error: Could not process rule")
	e.firewallFor = func(string) mi5.WanBlock { return staleBlock{refused} }
	c := &mi5Cmd{e: e, f: mi5Flags{drive: mi5.DriveTest, block: 15 * time.Minute, segmentSecs: 300}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := c.run(ctx)
	if err == nil || err.Error() != mi5.MsgUnblockFailed || !isOwnerLine(err) || !errors.Is(err, refused) {
		t.Errorf("mi5's end %v; want %q, the firewall's error its cause", err, mi5.MsgUnblockFailed)
	}
}
