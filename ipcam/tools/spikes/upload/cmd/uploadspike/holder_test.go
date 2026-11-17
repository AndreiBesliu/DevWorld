package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// TestSetup_PiTerminalTakesJSONOrValues (P46): in the terminal, each Google
// client is a pasted client JSON or its ID, the Desktop and TV secrets
// come from the JSON or a prompt, and the Microsoft client is skipped with
// Enter; the hub gets exactly those clients through its control endpoint,
// the run state keeps only the web client's ID, and nothing of the
// secrets stays on disk under the base.
func TestSetup_PiTerminalTakesJSONOrValues(t *testing.T) {
	r := newRig(t)
	p := r.run(clientsInput(), nil, "setup", "--terminal")
	if p.code != 0 {
		t.Fatalf("setup exited %d\nstdout:\n%s\nstderr:\n%s", p.code, p.stdout, p.stderr)
	}
	reqs := r.records(spiketest.RecordRequest)
	if len(reqs) != 1 || reqs[0].Detail != "POST /control/v1/clients" {
		t.Fatalf("the hub's requests %+v; want the clients alone", reqs)
	}
	var got clients
	if err := json.Unmarshal([]byte(reqs[0].Body), &got); err != nil {
		t.Fatal(err)
	}
	want := clients{Google: map[string]client{
		"android": {ID: "android-client-0001.apps.example"},
		"desktop": {ID: "desktop-client-0001.apps.example", Secret: "SENT" + "INEL-desktop-secret-01"},
		"tv":      {ID: "tv-client-0001.apps.example", Secret: "SENT" + "INEL-tv-secret-000001"},
		"web":     {ID: "web-client-0001.apps.example"},
	}}
	if got.Microsoft != nil || len(got.Google) != 4 {
		t.Errorf("clients %+v; want %+v", got, want)
	}
	for k, v := range want.Google {
		if got.Google[k] != v {
			t.Errorf("client %s = %+v, want %+v", k, got.Google[k], v)
		}
	}
	if st := r.state(); st.WebClientID != "web-client-0001.apps.example" || st.EntraClientID != "" {
		t.Errorf("the run state's client IDs: %q, %q", st.WebClientID, st.EntraClientID)
	}
	if !strings.Contains(p.stdout.String(), msgSetupSaved) {
		t.Errorf("stdout %q; want %q", p.stdout, msgSetupSaved)
	}
	assertNotUnder(t, r.base, "SENT"+"INEL-desktop-secret-01", "SENT"+"INEL-tv-secret-000001")
	if stops := r.records(spiketest.RecordEOFStop); len(stops) != 1 {
		t.Errorf("the hub's clean stops: %+v; want setup to give the hub back", stops)
	}
	// A client missing: refused, in Romanian.
	p = r.run("android-client-0001.apps.example\n", nil, "setup", "--terminal")
	if p.code == 0 || !strings.Contains(p.stderr.String(), "Clienții nu sunt completi") {
		t.Errorf("an incomplete setup: exit %d, stderr %q", p.code, p.stderr)
	}
}

// assertNotUnder fails when a file under dir holds one of values.
func assertNotUnder(t *testing.T, dir string, values ...string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // the test's own files
		if err != nil {
			return nil
		}
		for _, v := range values {
			if strings.Contains(string(b), v) {
				t.Errorf("%s holds a secret", p)
			}
		}
		return nil
	})
}

// startHolder starts `uploadspike mi6 --platforms android` on an open MI6
// run, which holds the hub while it waits for the Android login, and
// waits until its hub started.
func startHolder(t *testing.T, r *rig, extra ...string) *proc {
	t.Helper()
	p := r.start(extra, "mi6", "--platforms", "android")
	until(t, time.Minute, "the holder's hub", func() bool { return len(r.startsBy(p.cmd.Process.Pid)) == 1 })
	until(t, 30*time.Second, "the holder's server", func() bool { code, _ := r.get("/spike/v1/status"); return code == http.StatusOK })
	return p
}

// TestHubLock_SecondHubOnSameDataDirRefused (One hub per device): while a
// holder runs the hub, hub.lock cannot be taken, and a second command gets
// it only after the holder's handover: its hub starts after the holder's
// hub stopped, so two hubs never share the data directory.
func TestHubLock_SecondHubOnSameDataDirRefused(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	a := startHolder(t, r)
	if release, err := runstate.Lock(filepath.Join(r.base, "state", runstate.HubLock)); !errors.Is(err, runstate.ErrLocked) {
		if release != nil {
			release()
		}
		t.Fatalf("hub.lock while the holder runs: %v; want ErrLocked", err)
	}
	b := r.run(clientsInput(), nil, "setup", "--terminal")
	if b.code != 0 {
		t.Fatalf("the second command exited %d\nstdout:\n%s\nstderr:\n%s", b.code, b.stdout, b.stderr)
	}
	if !a.wait(30*time.Second) || a.code != 0 {
		t.Fatalf("the holder after the handover: ended %v, exit %d\nstdout:\n%s", a.code != -1, a.code, a.stdout)
	}
	if !strings.Contains(a.stdout.String(), msgHandedOver) {
		t.Errorf("the holder said %q; want %q", a.stdout, msgHandedOver)
	}
	starts := r.records(spiketest.RecordStart)
	stops := r.records(spiketest.RecordEOFStop)
	if len(starts) != 2 || len(stops) != 2 || starts[0].PPID != a.cmd.Process.Pid || starts[1].PPID != b.cmd.Process.Pid {
		t.Fatalf("starts %+v, stops %+v; want the holder's, then the second command's", starts, stops)
	}
	if stops[0].PID != starts[0].PID || stops[0].AtMs > starts[1].AtMs {
		t.Errorf("the holder's hub stopped at %d, the second hub started at %d; want the stop first", stops[0].AtMs, starts[1].AtMs)
	}
}

// TestHandover_SecondCommandTakesTheHubAndKeepsOpenRuns (One hub per
// device): a second command takes the hub through the handover, and its
// hub's run.json keeps MI6's destinations; a handover request without the
// run state's token gets 401.
func TestHandover_SecondCommandTakesTheHubAndKeepsOpenRuns(t *testing.T) {
	r := newRig(t)
	r.openMI6("android", "windows", "web")
	a := startHolder(t, r)
	for _, auth := range []string{"", "Bearer " + strings.Repeat("0", 64)} {
		var h []string
		if auth != "" {
			h = []string{"Authorization", auth}
		}
		if code, _ := r.do(http.MethodPost, "/spike/v1/run/handover", "", h...); code != http.StatusUnauthorized {
			t.Errorf("a handover with %q: %d; want 401", auth, code)
		}
	}
	b := r.run(clientsInput(), nil, "setup", "--terminal")
	if b.code != 0 || !a.wait(30*time.Second) {
		t.Fatalf("setup exited %d, the holder ended %v\n%s\n%s", b.code, a.code != -1, b.stdout, b.stderr)
	}
	starts := r.startsBy(b.cmd.Process.Pid)
	if len(starts) != 1 || !slices.Equal(starts[0].Destinations, []string{"g-android", "g-windows", "g-web"}) {
		t.Errorf("the second command's hub: %+v; want MI6's destinations", starts)
	}
	if _, ok := r.state().Open["MI6"]; !ok {
		t.Error("the MI6 run is gone from the run state")
	}
}

// TestHandover_RefusedWhileMI5Open (MI5 is never handed over): with an open
// MI5 run whose recorded mi5 is the live holder, the holder answers a
// direct handover with 409 and keeps its hub; `mi6 --b1 android` finds the
// open run, sends no handover, exits 1 with the MI5 line and records no
// claim and no B1 outcome; one whose check passed before the run opened
// gets the 409, removes its claim and exits 1 the same way; and a failed
// g-android refresh day makes status and status.html ask for B1 after
// MI5.
func TestHandover_RefusedWhileMI5Open(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	r.editMI6(func(run *mi6.Run) {
		run.Completed("g-android", mi6.PlanA, "android", time.Now().Add(-49*time.Hour).UnixMilli())
		run.Refreshed("g-android", 1, 96, "failed", time.Now().Add(-time.Hour).UnixMilli())
	})
	a := startHolder(t, r)
	hubPID := r.startsBy(a.cmd.Process.Pid)[0].PID
	openMI5 := func() {
		start, err := runstate.ProcStartMs(a.cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(mi5Holder{PID: a.cmd.Process.Pid, ProcStartMs: start})
		r.update(func(s *runstate.State) { s.Open["MI5"] = raw })
	}
	// The second case first: a caller whose check passes before the run
	// opens.
	pause := filepath.Join(t.TempDir(), "pause")
	late := r.start([]string{envPauseAfter + "=" + pause}, "mi6", "--b1", "android")
	until(t, 30*time.Second, "the caller's check", func() bool { _, err := os.Stat(pause + ".ready"); return err == nil })
	if r.state().Claim == nil {
		t.Fatal("the caller recorded no claim before its request")
	}
	openMI5()
	if err := os.WriteFile(pause, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !late.wait(time.Minute) || late.code != 1 || !strings.Contains(late.stderr.String(), msgMI5Running) {
		t.Fatalf("the late caller: exit %d, stderr %q; want 1 and %q", late.code, late.stderr, msgMI5Running)
	}
	if c := r.state().Claim; c != nil {
		t.Errorf("the late caller left its claim %+v", c)
	}
	// A direct handover: 409, and the holder keeps its hub.
	st := r.state()
	if code, _ := r.do(http.MethodPost, "/spike/v1/run/handover", "", "Authorization", "Bearer "+st.HandoverToken); code != http.StatusConflict {
		t.Errorf("a direct handover with MI5 open: %d; want 409", code)
	}
	refusals := strings.Count(a.stdout.String(), msgHandoverMI5)
	// The command that finds the run sends no request.
	b := r.run("", nil, "mi6", "--b1", "android")
	if b.code != 1 || !strings.Contains(b.stderr.String(), msgMI5Running) {
		t.Errorf("mi6 --b1 android: exit %d, stderr %q; want 1 and %q", b.code, b.stderr, msgMI5Running)
	}
	time.Sleep(500 * time.Millisecond)
	if n := strings.Count(a.stdout.String(), msgHandoverMI5); n != refusals {
		t.Errorf("the holder refused %d handovers after mi6 --b1, want none (it sends no request)", n-refusals)
	}
	if c := r.state().Claim; c != nil {
		t.Errorf("mi6 --b1 recorded a claim %+v", c)
	}
	if live := r.liveHubs(); !slices.Equal(live, []int{hubPID}) || a.code != -1 {
		t.Errorf("live hubs %v, holder ended %v; want the holder's hub %d alone", live, a.code != -1, hubPID)
	}
	run, err := mi6.Decode(r.state().Open["MI6"])
	if err != nil {
		t.Fatal(err)
	}
	if p := run.Pair("g-android"); p.Plan != mi6.PlanA || p.B1Failure != "" {
		t.Errorf("g-android after the refusals: %+v; want no B1 outcome", p)
	}
	want := "Reconectați Android cu B1, după proba MI5: uploadspike mi6 --b1 android"
	if s := r.run("", nil, "status"); !strings.Contains(s.stdout.String(), want) {
		t.Errorf("status:\n%s\nwant %q", s.stdout, want)
	}
	if _, page := r.get("/status"); !strings.Contains(page, want) {
		t.Errorf("status.html:\n%s\nwant %q", page, want)
	}
}

// TestStatus_IsAClient (Inbound listeners): status opens no listener (it
// runs while the holder holds the port), and reports the same open runs
// as the holder's status endpoint.
func TestStatus_IsAClient(t *testing.T) {
	r := newRig(t)
	r.openMI6("android", "windows")
	_ = startHolder(t, r)
	s := r.run("", nil, "status")
	if s.code != 0 {
		t.Fatalf("status exited %d: %s", s.code, s.stderr)
	}
	code, body := r.get("/spike/v1/status")
	var holder struct {
		Lines []string `json:"lines"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &holder) != nil {
		t.Fatalf("the holder's status: %d %s", code, body)
	}
	lines := strings.Split(strings.TrimSpace(s.stdout.String()), "\n")
	if !slices.Equal(lines, holder.Lines) || len(lines) < 3 {
		t.Errorf("status %q, the holder %q; want the same lines", lines, holder.Lines)
	}
}

// TestResume_YieldsToLiveClaim: a resume that gets the lock while another
// live process's claim is valid releases it and starts no hub, and the
// claimant takes the lock; once the claim is gone, resume takes the hub.
func TestResume_YieldsToLiveClaim(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	claim, err := runstate.NewClaim(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.update(func(s *runstate.State) { s.Claim = claim })
	res := r.start(nil, "resume")
	time.Sleep(2 * time.Second)
	if starts := r.records(spiketest.RecordStart); len(starts) != 0 {
		t.Fatalf("resume started a hub under a live claim: %+v", starts)
	}
	release, err := runstate.Lock(filepath.Join(r.base, "state", runstate.HubLock))
	if err != nil {
		t.Fatalf("the claimant could not take the lock: %v", err)
	}
	time.Sleep(time.Second)
	release()
	r.update(func(s *runstate.State) { s.Claim = nil })
	until(t, 30*time.Second, "resume's hub", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) == 1 })
}

// TestResume_WaitsForPreviousHubToExit: a hub that hub.pid names and that
// still runs is waited for, killed after the grace (3 s in the tests,
// 60 s in production), and only then does resume start its own.
func TestResume_WaitsForPreviousHubToExit(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	runJSON := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(runJSON, []byte(`{"sdCardConfirmed":true,"jobs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := startStandIn(t, r.hub, runJSON)
	start, err := runstate.ProcStartMs(old.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := json.Marshal(map[string]any{"pid": old.Process.Pid, "procStartMs": start})
	if err := os.MkdirAll(filepath.Join(r.base, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.base, "state", runstate.HubPID), pid, 0o600); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	res := r.start(nil, "resume")
	until(t, time.Minute, "resume's hub", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) == 1 })
	if took := time.Since(began); took < 2500*time.Millisecond {
		t.Errorf("resume started its hub after %v, before the previous hub's grace", took)
	}
	if alive(old.Process.Pid) {
		t.Error("the previous hub still runs")
	}
}

// startStandIn starts the stand-in hub as a previous holder's hub, whose
// stdin stays open.
func startStandIn(t *testing.T, hub, runJSON string) *execCmd {
	t.Helper()
	cmd := newExecCmd(hub, "run", "--config", runJSON, "--stop-on-stdin-eof")
	if _, err := cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return cmd
}

// TestClose_NotElevatedTellsTheOwner (C2-R20): a close without the
// administrator's rights, which the scan's quarantine needs on Windows,
// tells the owner in Romanian what to do, and writes nothing.
func TestClose_NotElevatedTellsTheOwner(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	p := r.run("", []string{envNotElevated + "=1"}, "results")
	if p.code != 1 || !strings.Contains(p.stderr.String(), msgNeedsElevation) {
		t.Errorf("results without elevation: exit %d, stderr %q; want 1 and %q", p.code, p.stderr, msgNeedsElevation)
	}
	if _, err := os.Stat(filepath.Join(r.base, "results")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("results/ after the refusal: %v; want none", err)
	}
}
