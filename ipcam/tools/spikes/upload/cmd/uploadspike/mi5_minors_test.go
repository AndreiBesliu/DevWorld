package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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

// TestOwnerTiming_FinalDrainCoversTheHolder (C2-R30 (3)): the owner's
// final drain's own allowance holds the holder's stop and start at their
// longest (hubStopWait, hubStartWait) and Quiet, with room left for the
// segments that the stop finalised.
func TestOwnerTiming_FinalDrainCoversTheHolder(t *testing.T) {
	tm := mi5.OwnerTiming(mi5.OwnerBlock)
	if need := hubStopWait + hubStartWait + tm.Quiet; tm.FinalDrain < need+5*time.Minute {
		t.Errorf("the final drain's allowance %v; want the stop and start (%v), Quiet (%v) and at least 5 min more", tm.FinalDrain,
			hubStopWait+hubStartWait, tm.Quiet)
	}
}

// holdCameraPort takes one of reccontroller --local's TCP ports on
// 127.0.0.1 for the test, as another program would; when every one is
// taken already, another program holds them, which mi5 refuses all the
// same.
func holdCameraPort(t *testing.T) {
	t.Helper()
	for _, p := range cameraPorts {
		if p.network != "tcp4" {
			continue
		}
		if ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(p.port))); err == nil {
			t.Cleanup(func() { _ = ln.Close() })
			return
		}
	}
}

// cameraPortLine reports whether line is msgCameraPortBusy for one of the
// camera ports.
func cameraPortLine(line string) bool {
	return slices.ContainsFunc(cameraPorts, func(p struct {
		network string
		port    int
	}) bool {
		return line == fmt.Sprintf(msgCameraPortBusy, p.port)
	})
}

// TestCheckCameraPorts_RefusesATakenPort (C2-R30 (2)): each of the
// fixed ports of the simulated cameras (TCP 7080, 7081, 7083, 8554; UDP
// 7082) that another program holds refuses the run with the owner's line,
// which names a taken port.
func TestCheckCameraPorts_RefusesATakenPort(t *testing.T) {
	for _, p := range cameraPorts {
		t.Run(fmt.Sprintf("%s-%d", p.network, p.port), func(t *testing.T) {
			addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(p.port))
			var closer io.Closer
			var err error
			if p.network == "udp4" {
				closer, err = net.ListenPacket("udp4", addr)
			} else {
				closer, err = net.Listen("tcp4", addr)
			}
			if err != nil {
				t.Skipf("another program holds %s already: %v", addr, err)
			}
			defer func() { _ = closer.Close() }()
			err = checkCameraPorts()
			if err == nil || !isOwnerLine(err) || !cameraPortLine(err.Error()) {
				t.Errorf("%s taken: %v; want the owner's line %q", addr, err, msgCameraPortBusy)
			}
		})
	}
}

// TestMI5_CameraPortTakenRefusesBeforeTheLogin (C2-R30 (2)): with a
// camera port taken by another program, mi5 refuses before any device
// login, so the owner spends none: its own Romanian line, exit 1, no hub
// started, no request to a hub, no envelope, no open run.
func TestMI5_CameraPortTakenRefusesBeforeTheLogin(t *testing.T) {
	holdCameraPort(t)
	r := newRig(t)
	_, seam := r.firewallLog()
	p := r.start([]string{seam, envLabLAN + "=127.0.0.1"}, "mi5", "--block", "15m")
	if !p.wait(30 * time.Second) {
		t.Fatalf("mi5 went on with a camera port taken\nstdout:\n%s\nstderr:\n%s", p.stdout, p.stderr)
	}
	line := p.errorLine()
	if p.code != 1 || !cameraPortLine(line) || strings.Contains(p.stderr.String(), msgError) {
		t.Errorf("mi5: exit %d, line %q; want 1 and %q", p.code, line, msgCameraPortBusy)
	}
	if login := strings.SplitN(msgMI5DeviceLogin, "%", 2)[0]; strings.Contains(p.stdout.String(), login) {
		t.Errorf("mi5 asked a login with a camera port taken:\n%s", p.stdout)
	}
	if starts, reqs := r.records(spiketest.RecordStart), r.records(spiketest.RecordRequest); len(starts) != 0 || len(reqs) != 0 {
		t.Errorf("hub starts %v, requests %v; want none", starts, reqs)
	}
	if files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI5", "*", "*.json")); len(files) != 0 {
		t.Errorf("a refused run wrote MI5 envelopes %v", files)
	}
	if _, open := r.state().Open["MI5"]; open {
		t.Error("the refused run is open")
	}
}

// TestMI5_PreChecksComposeNoJob (C2-R30 (4)): the pre-checks write the
// run.json that diag dest-probe reads with the final drain's composition,
// no job, as the logins do: a run whose destinations needed no login is
// still in the pre-run's phase there, whose job names a camera that has
// not started.
func TestMI5_PreChecksComposeNoJob(t *testing.T) {
	t.Setenv(spiketest.FakeQuotaFreeEnv, "1000000000000")
	hub, _ := spiketest.InstallFakeHub(t, t.TempDir())
	e, _ := unitEnv(t)
	e.hubBin = hub
	cfg := mi5.Mi5Config{Cameras: 4, SegmentSecs: 300, Drive: mi5.DriveTest, BlockFor: 15 * time.Minute}
	comp := &mi5Comp{cfg: cfg, dests: mi5.Destinations(cfg), startMs: time.Now().UnixMilli(), phase: mi5.PhasePreRun}
	c := &mi5Cmd{e: e, h: &holder{e: e, mi5: comp}}
	if err := c.preChecks(t.Context(), cfg, comp); err != nil {
		t.Fatal(err)
	}
	var run struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(mustRead(t, e.runJSON())), &run); err != nil {
		t.Fatal(err)
	}
	if len(run.Jobs) != 0 || comp.phase != mi5.PhaseFinal {
		t.Errorf("the pre-checks' run.json has jobs %s (phase %s); want none (%s)", run.Jobs, comp.phase, mi5.PhaseFinal)
	}
}

// TestMI5_LoginFailureReasonsInRomanian (C2-R30 (5)): a device login that
// the hub reports expired or failed ends with the owner's line, whose
// reason is Romanian text for the hub's status and reason code, never the
// code itself.
func TestMI5_LoginFailureReasonsInRomanian(t *testing.T) {
	for _, tc := range []struct{ status, reason, want string }{
		{"expired", "", msgLoginReasonExpired},
		{"failed", "expired_token", msgLoginReasonExpired},
		{"failed", "state", msgLoginReasonState},
		{"failed", "access_denied", msgLoginReasonDenied},
		{"failed", "invalid_grant", msgLoginReasonRefused},
		{"failed", "invalid_client", msgLoginReasonRefused},
		{"failed", "redirect_uri_mismatch", msgLoginReasonRefused},
		{"failed", "transient", msgLoginReasonTransient},
		{"failed", "error", msgLoginReasonError},
		{"failed", "", msgLoginReasonError},
		{"failed", "some_new_code", msgLoginReasonError},
	} {
		t.Run(tc.status+"-"+tc.reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if req.URL.Path == "/control/v1/oauth/begin" {
					_, _ = w.Write([]byte(`{"state":"st-1","plan":"B1","verificationUrl":"https://example.invalid/device","userCode":"ABCD-EFGH"}`))
					return
				}
				b, _ := json.Marshal(map[string]string{"status": tc.status, "reason": tc.reason})
				_, _ = w.Write(b)
			}))
			t.Cleanup(srv.Close)
			e, _ := unitEnv(t)
			c := &mi5Cmd{e: e, h: &holder{e: e, ctl: hubproc.NewControlClient(strings.TrimPrefix(srv.URL, "http://"), strings.Repeat("ab", 32))}}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			_, err := c.deviceLogin(ctx, mi5.DestConfig{ID: "dst-1", Kind: mi5.KindDrive, Account: "acct-1", TokenRef: "dst-1"})
			want := fmt.Sprintf(msgMI5LoginFailed, "dst-1", tc.want)
			if err == nil || err.Error() != want || !isOwnerLine(err) {
				t.Errorf("the login's end %v; want %q", err, want)
			}
			if err != nil && (tc.reason != "" && strings.Contains(err.Error(), tc.reason) || strings.Contains(err.Error(), tc.status)) {
				t.Errorf("the owner's line %q shows the hub's code", err)
			}
		})
	}
}
