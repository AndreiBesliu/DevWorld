package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// unitEnv is an env for an in-process test: a temporary base, the tests'
// short waits, and out for both of uploadspike's streams.
func unitEnv(t *testing.T) (*env, *syncBuffer) {
	t.Helper()
	out := &syncBuffer{}
	return &env{base: t.TempDir(), out: out, errOut: out, now: time.Now, fast: true, port: freePort(t), runAs: ""}, out
}

// TestPollDevice_StopsOnceHandedOver (Task 9B review M2): a device
// login's status poll stops as soon as the holder accepted a handover,
// before the handover's scan ends, so a login cannot complete into a hub
// that is being handed over.
func TestPollDevice_StopsOnceHandedOver(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	t.Cleanup(srv.Close)
	e, _ := unitEnv(t)
	ctl := hubproc.NewControlClient(strings.TrimPrefix(srv.URL, "http://"), strings.Repeat("ab", 32))
	h := &holder{e: e, ctl: ctl, open: map[string]string{"st-1": "g-web"}, handover: make(chan struct{})}
	done := make(chan struct{})
	go func() { h.pollDevice(ctl, "st-1"); close(done) }()
	until(t, 10*time.Second, "the first status poll", func() bool { return calls.Load() > 0 })
	if code := (&holder{e: e}).handedOverFor(h); code != http.StatusNoContent {
		t.Fatalf("the handover: %d", code)
	}
	before := calls.Load()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("the device poll goes on after the handover (%d calls)", calls.Load())
	}
	if after := calls.Load(); after > before+1 {
		t.Errorf("%d status polls after the handover", after-before)
	}
}

// handedOverFor accepts a handover on h, as Handover does once the run
// state allows it.
func (*holder) handedOverFor(h *holder) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.handedOver {
		h.handedOver = true
		close(h.handover)
	}
	return http.StatusNoContent
}

// TestAllowBegin_CapRefusalOnTheTerminal (Task 9B review M7): a login
// refused at the handoff cap says so on the holder's terminal too, not
// only to the app.
func TestAllowBegin_CapRefusalOnTheTerminal(t *testing.T) {
	e, out := unitEnv(t)
	if err := runstate.Update(e.statePath(), func(s *runstate.State) error {
		s.Handoffs[mi6.HandoffKey("google:android")] = mi6.HandoffCap
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := &holder{e: e}
	if err := h.AllowBegin("g-android", "android", mi6.PlanA); !errors.Is(err, mi6.ErrCap) {
		t.Fatalf("AllowBegin at the cap: %v", err)
	}
	if !strings.Contains(out.String(), mi6.CapRefused) {
		t.Errorf("the terminal %q does not say %q", out.String(), mi6.CapRefused)
	}
}

// TestRestartAfterCrash_NotAfterTheEnd: a hub that ends once the holder's
// context ended (a signal) is not restarted, and no crash is said.
func TestRestartAfterCrash_NotAfterTheEnd(t *testing.T) {
	e, out := unitEnv(t)
	e.hubBin = filepath.Join(t.TempDir(), "missing-hub")
	h := &holder{e: e, handover: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 2 {
		h.restartAfterCrash(ctx)
	}
	if out.String() != "" {
		t.Errorf("after the end, a crash said %q", out.String())
	}
	if h.crashAt != 0 {
		t.Error("after the end, a crash opened a gap")
	}
}

// TestClosingScan_RewriteFailureSaid: when the envelope cannot be
// rewritten after a failed scan, the owner reads why, results/ is scanned
// once more, and the last scan is not clean (no zip).
func TestClosingScan_RewriteFailureSaid(t *testing.T) {
	hub, _ := spiketest.InstallFakeHub(t, t.TempDir())
	e, out := unitEnv(t)
	e.hubBin = hub
	if err := runstate.Update(e.statePath(), func(s *runstate.State) error {
		s.Open["MI6"] = mi6.NewRun("mi6-1", time.Now(), "test", false, []string{"android"}).Encode()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := runstate.Load(e.statePath())
	if err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(e.resultsDir(), "MI6", "x.json")
	if err := os.MkdirAll(filepath.Dir(planted), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planted, []byte(st.HandoverToken), 0o600); err != nil {
		t.Fatal(err)
	}
	rewrites := 0
	failed, last := closingScan(t.Context(), e, nil, func() error { rewrites++; return errors.New("disc plin") })
	if !failed || last == nil || last.clean || !last.rewriteFailed {
		t.Fatalf("closingScan = %v, %+v; want a failure, not clean, whose rewrite failed", failed, last)
	}
	if rewrites != 1 || !strings.Contains(out.String(), "disc plin") {
		t.Errorf("the rewrite's failure (%d rewrites) was not said: %q", rewrites, out.String())
	}
}

// TestMI6_HubCrashDuringLoginSaid (Task 9B review M6): a hub that ends on
// its own while mi6 waits for a day-0 login is said on the terminal and
// restarted, and the login is asked again on the new hub.
func TestMI6_HubCrashDuringLoginSaid(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := r.start([]string{envSignalFile + "=" + signalFile}, "mi6", "--platforms", "android")
	waiting := strings.SplitN(promptWaiting, "%", 2)[0]
	until(t, time.Minute, "the login's wait", func() bool { return strings.Contains(m.stdout.String(), waiting) })
	first := r.hubPID()
	if p, err := os.FindProcess(first); first == 0 || err != nil || p.Kill() != nil {
		t.Fatalf("killing mi6's hub %d: %v", first, err)
	}
	until(t, time.Minute, "the crash line, a new hub and the login asked again", func() bool {
		out := m.stdout.String()
		return strings.Contains(out, msgHubCrashed) && len(r.startsBy(m.cmd.Process.Pid)) == 2 && strings.Count(out, waiting) >= 2
	})
	stopHolder(t, m, signalFile)
}

// TestOwnerTexts_Romanian (Task 9B review M9): what the owner's browser or
// terminal shows of a failure is Romanian, from labels.go: the status
// page without a readable run state, the setup page's refused body, a
// bad port, and the clients' line (spelled «compleți»).
func TestOwnerTexts_Romanian(t *testing.T) {
	e, _ := unitEnv(t)
	if err := os.MkdirAll(e.stateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.statePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &holder{e: e}
	w := httptest.NewRecorder()
	h.Status(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	if !strings.Contains(w.Body.String(), pageRunStateUnread) {
		t.Errorf("the status page without a run state: %q", w.Body.String())
	}
	w = httptest.NewRecorder()
	h.setupPageHandler()(w, httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("{not json")))
	if !strings.Contains(w.Body.String(), pageSetupBadBody) {
		t.Errorf("the setup page's refused body: %q", w.Body.String())
	}
	t.Setenv(envPort, "port")
	if _, err := newEnv(nil, io.Discard, io.Discard); err == nil || err.Error() != fmt.Sprintf(msgNotAPort, envPort) {
		t.Errorf("a bad port: %v", err)
	}
	if !strings.Contains(msgSetupClientsWrong, "compleți") {
		t.Errorf("%q is misspelt", msgSetupClientsWrong)
	}
}
