package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// readEnvelope reads the MI6 envelope that results wrote.
func readEnvelope(t *testing.T, r *rig) results.Envelope {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(r.base, "results", "MI6", "*", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("MI6 envelopes %v; want one", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var e results.Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// stopHolder ends a holder through the signal file and waits for it.
func stopHolder(t *testing.T, p *proc, signalFile string) {
	t.Helper()
	if err := os.WriteFile(signalFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !p.wait(time.Minute) {
		t.Fatalf("the holder did not end\nstdout:\n%s", p.stdout)
	}
}

// TestMI6_SdCardNeedsConfirmation (P02, P17): with diag host reporting
// storage sd and no DA, no hub starts, and no run.json holds
// sdCardConfirmed: true; on Linux setup asks the warning and keeps the
// answer, DA; then the mi6 and resume hub starts carry sdCardConfirmed:
// true and the envelope carries the confirmation.
func TestMI6_SdCardNeedsConfirmation(t *testing.T) {
	t.Setenv(spiketest.FakeStorageEnv, "sd")
	r := newRig(t)
	r.openMI6("android")
	p := r.run("", nil, "mi6", "--platforms", "android")
	if p.code != 1 || !strings.Contains(p.stderr.String(), "cardul SD nu este confirmat") {
		t.Fatalf("mi6 on an unconfirmed card: exit %d, stderr %q", p.code, p.stderr)
	}
	if len(r.records(spiketest.RecordSdGate)) == 0 || slices.ContainsFunc(r.records(spiketest.RecordStart), func(s spiketest.FakeRecord) bool { return s.SdCardConfirmed }) {
		t.Fatalf("starts %+v; want only refused ones", r.records(spiketest.RecordStart))
	}
	if runtime.GOOS == "linux" {
		p = r.run("nu\n", nil, "setup", "--terminal")
		if p.code != 1 || r.state().Sd != nil || !strings.Contains(p.stdout.String(), "Cardul SD se uzează repede") {
			t.Fatalf("setup without DA: exit %d, Sd %+v, stdout %q", p.code, r.state().Sd, p.stdout)
		}
		p = r.run("DA\n"+clientsInput(), nil, "setup", "--terminal")
		if p.code != 0 || r.state().Sd == nil || r.state().Sd.Text != "DA" {
			t.Fatalf("setup with DA: exit %d, Sd %+v\n%s\n%s", p.code, r.state().Sd, p.stdout, p.stderr)
		}
	} else {
		r.update(func(s *runstate.State) { s.Sd = &results.SdConfirmation{Text: "DA", AtMs: time.Now().UnixMilli()} })
	}
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := startHolder(t, r, envSignalFile+"="+signalFile)
	res := r.start(nil, "resume")
	stopHolder(t, m, signalFile)
	until(t, time.Minute, "resume's hub", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) == 1 })
	for _, who := range []*proc{m, res} {
		if s := r.startsBy(who.cmd.Process.Pid); len(s) != 1 || !s[0].SdCardConfirmed {
			t.Errorf("the hub start of %v: %+v; want sdCardConfirmed", who.cmd.Args, s)
		}
	}
	if p := r.run("", nil, "results"); p.code != 0 {
		t.Fatalf("results: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	if e := readEnvelope(t, r); e.SdConfirmation == nil || e.SdConfirmation.Text != "DA" || e.Storage != "sd" {
		t.Errorf("the envelope: sd %+v, storage %q; want the confirmation", e.SdConfirmation, e.Storage)
	}
}

// TestClose_ScanAfterRestartUsesSealedClients (P45, P46): a new
// uploadspike process, with no secret in RAM, closes MI6: a client secret
// planted in results/ is found through diag scan-secrets, which unseals
// the clients that setup gave the hub, the file leaves results/ for the
// quarantine, and the run fails with a reason that names no local path.
func TestClose_ScanAfterRestartUsesSealedClients(t *testing.T) {
	r, _ := realRig(t)
	if p := r.run(clientsInput(), nil, "setup", "--terminal"); p.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	r.openMI6("android")
	planted := filepath.Join(r.base, "results", "MI6", "lab-planted", "planted.json")
	if err := os.MkdirAll(filepath.Dir(planted), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planted, []byte(`{"note":"`+"SENT"+`INEL-desktop-secret-01"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := r.run("", nil, "results")
	if p.code != 1 || !strings.Contains(p.stdout.String(), "Scanarea rezultatelor a găsit un secret") {
		t.Fatalf("results: exit %d\nstdout:\n%s\nstderr:\n%s", p.code, p.stdout, p.stderr)
	}
	if _, err := os.Stat(planted); !os.IsNotExist(err) {
		t.Errorf("the planted file is still in results/: %v", err)
	}
	moved, _ := filepath.Glob(filepath.Join(r.base, "results-quarantine", "*", "MI6", "lab-planted", "planted.json"))
	if len(moved) != 1 {
		t.Errorf("the quarantine holds %v; want the planted file", moved)
	}
	run, err := mi6.Decode(r.state().Open["MI6"])
	if err != nil || run.ScanFailed != "scan-secrets: moved 1, unmoved 0" {
		t.Errorf("the run: %+v, %v; want it failed by the scan, with the fixed reason", run, err)
	}
	e, raw := envelopeBytes(t, r)
	if e.Runs[0].Valid || e.Runs[0].InvalidReason != run.ScanFailed {
		t.Errorf("the envelope's run: %+v; want it invalid with the run's reason", e.Runs[0])
	}
	// PH0-22: the quarantine's folder, under the owner's profile on
	// Windows, never reaches the envelope or the zip.
	assertNoLocalPath(t, "the envelope", raw, r.base)
	for name, body := range zipEntries(t, r) {
		assertNoLocalPath(t, "the zip's "+name, []byte(body), r.base)
	}
}

// TestMI6_TokenNotInResults (Review Focus 5, end to end): the fakes issue
// a known code, access token and refresh token, each assembled from parts,
// and record the PKCE verifiers that the hub sent; after a full MI6 run
// on the fakes (the three Drive logins, the visibility matrix, results),
// none of them appears in results/, in uploadspike's stdout or stderr, or
// in ipcam-hub's stderr or events. The payloads that the handoff server
// received carry the code by design: their keys are exactly code and
// state, and the verifier and both tokens appear in them 0 times.
func TestMI6_TokenNotInResults(t *testing.T) {
	code, access, refresh := "SENT"+"INEL-known-code-0001", "SENT"+"INEL-known-access-0001", "SENT"+"INEL-known-refresh-01"
	r, f := realRig(t, "code="+code, "access="+access, "refresh="+refresh)
	events := filepath.Join(t.TempDir(), "events.log")
	r.env = append(r.env, envEventLog+"="+events)
	var outputs []*proc
	setup := r.run(clientsInput(), nil, "setup", "--terminal")
	if setup.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", setup.code, setup.stdout, setup.stderr)
	}
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := r.start([]string{envSignalFile + "=" + signalFile}, "mi6", "--platforms", "android,windows,web")
	a := &app{r: r}
	a.loginA(t, "android", 0, false)
	a.loginA(t, "windows", 53123, false)
	a.loginB1(t, "web")
	a.webCells(t, "android", "desktop", "tv")
	until(t, 3*time.Minute, "the end of day 0", func() bool { return strings.Contains(m.stdout.String(), msgKeepWindowOpen) })
	run, err := mi6.Decode(r.state().Open["MI6"])
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range run.Pairs {
		if !p.HandoffOK {
			t.Errorf("pair %+v has no login", p)
		}
	}
	if len(run.Visibility) != 12 {
		t.Errorf("the matrix has %d cells, want 12: %+v", len(run.Visibility), run.Visibility)
	}
	res := r.run("", nil, "results")
	if res.code != 0 {
		t.Fatalf("results: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	stopHolder(t, m, signalFile)
	outputs = append(outputs, setup, m, res)
	minted, verifiers := f.issued(t)
	if len(verifiers) != 2 || !slices.Contains(minted["code"], code) || !slices.Contains(minted["access"], access) ||
		!slices.Contains(minted["refresh"], refresh) {
		t.Fatalf("the fakes minted %v and got verifiers %v; want the known values and two verifiers", minted, verifiers)
	}
	needles := map[string]string{"code": code, "access token": access, "refresh token": refresh}
	for i, v := range verifiers {
		needles["verifier "+string(rune('1'+i))] = v
	}
	for _, kind := range []string{"code", "access", "refresh"} {
		for i, v := range minted[kind] {
			needles[kind+" minted "+string(rune('1'+i))] = v
		}
	}
	haystacks := filesUnder(t, filepath.Join(r.base, "results"))
	if b, err := os.ReadFile(events); err == nil && len(b) > 0 { //nolint:gosec // the test's own file
		haystacks["ipcam-hub's events"] = string(b)
	} else {
		t.Errorf("no hub events were logged: %v", err)
	}
	for i, p := range outputs {
		haystacks["stdout of command "+string(rune('1'+i))] = p.stdout.String()
		haystacks["stderr of command "+string(rune('1'+i))+" (with ipcam-hub's)"] = p.stderr.String()
	}
	for where, text := range haystacks {
		for what, v := range needles {
			if strings.Contains(text, v) {
				t.Errorf("%s holds the %s", where, what)
			}
		}
	}
	if len(a.payloads) != 2 || !strings.Contains(a.payloads[0], code) {
		t.Errorf("payloads %d; want two, the first with the known code", len(a.payloads))
	}
	for _, payload := range a.payloads {
		var keys map[string]string
		if err := json.Unmarshal([]byte(payload), &keys); err != nil || len(keys) != 2 || keys["code"] == "" || keys["state"] == "" {
			t.Errorf("a payload %q; want exactly code and state", payload)
		}
		for _, v := range append(slices.Clone(verifiers), access, refresh) {
			if strings.Contains(payload, v) {
				t.Errorf("a payload holds a verifier or a token")
			}
		}
	}
}

// TestMI6_PlanAFails_RunsB1SameDay (P39): a refused Android redemption
// prompts B1 the same day, in mi6's own window; its device login sets
// Plan "B1", and the pair's refresh evidence is g-web's series.
func TestMI6_PlanAFails_RunsB1SameDay(t *testing.T) {
	r, _ := realRig(t)
	if p := r.run(clientsInput(), nil, "setup", "--terminal"); p.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := r.start([]string{envSignalFile + "=" + signalFile}, "mi6", "--platforms", "android,web")
	a := &app{r: r}
	a.loginA(t, "android", 0, true)
	until(t, time.Minute, "B1 on Android", func() bool { _, _, plan := a.next(t, "android"); return plan == mi6.PlanB1 })
	until(t, 10*time.Second, "the plan A refusal's line", func() bool {
		return strings.Contains(m.stdout.String(), "Planul A pentru Android × Google Drive a fost refuzat (invalid_grant)")
	})
	a.loginB1(t, "android")
	a.loginB1(t, "web")
	a.webCells(t, "tv")
	until(t, 3*time.Minute, "the end of day 0", func() bool { return strings.Contains(m.stdout.String(), msgKeepWindowOpen) })
	if p := r.run("", nil, "results"); p.code != 0 {
		t.Fatalf("results: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	stopHolder(t, m, signalFile)
	e := readEnvelope(t, r)
	var android results.Mi6Pair
	for _, p := range e.MI6.Pairs {
		if p.Dest == "g-android" {
			android = p
		}
	}
	if android.Plan != "B1" || !android.HandoffOK || android.PlanAFailure != "invalid_grant" || android.RefreshFrom != "g-web" {
		t.Errorf("g-android: %+v; want B1 after plan A's invalid_grant, with g-web's series", android)
	}
}

// TestMI6_FailedRefreshDay_StatusAsksB1 (P39, Handover): a refresh day of
// g-android that failed under resume makes status and status.html ask for
// B1. A first `mi6 --b1 android`, whose hub `uploadspike setup` in another
// process takes through a handover between its Begin and the device
// flow's completion, exits 0 saying the login was cut and which command to
// run again, and leaves the pair, its days and the client's handoffs as
// they were, status still asking for B1; a second `mi6 --b1 android`
// takes the hub through the handover, completes the device flow on the
// fakes, sets Plan "B1", and the hub returns to the waiting resume.
func TestMI6_FailedRefreshDay_StatusAsksB1(t *testing.T) {
	r, _ := realRig(t)
	if p := r.run(clientsInput(), nil, "setup", "--terminal"); p.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	r.openMI6("android", "web")
	r.editMI6(func(run *mi6.Run) {
		run.Completed("g-android", mi6.PlanA, "android", time.Now().Add(-49*time.Hour).UnixMilli())
		run.Refreshed("g-android", 1, 96, "failed", time.Now().Add(-time.Hour).UnixMilli())
		run.LoginsDone = true
	})
	r.update(func(s *runstate.State) { s.Autostart, s.Handoffs["acct-1/google:android"] = true, 1 })
	res := r.start(nil, "resume")
	served := func() bool { code, _ := r.get("/spike/v1/status"); return code == 200 }
	until(t, 2*time.Minute, "resume's hub", served)
	ask := "Reconectați Android cu B1: uploadspike mi6 --b1 android"
	if s := r.run("", nil, "status"); !strings.Contains(s.stdout.String(), ask) {
		t.Fatalf("status:\n%s\nwant %q", s.stdout, ask)
	}
	if _, page := r.get("/status"); !strings.Contains(page, ask) {
		t.Errorf("status.html:\n%s\nwant %q", page, ask)
	}
	before := r.state()
	first := r.start(nil, "mi6", "--b1", "android")
	a := &app{r: r}
	a.loginB1(t, "android")
	taker := r.start(nil, "setup", "--terminal")
	_, _ = taker.stdin.Write([]byte(clientsInput()))
	_ = taker.stdin.Close()
	if !first.wait(2*time.Minute) || first.code != 0 {
		t.Fatalf("the first mi6 --b1: exit %d\n%s\n%s", first.code, first.stdout, first.stderr)
	}
	cut := "Autentificarea s-a întrerupt: alt program al probei a preluat hub-ul. Rulați din nou: uploadspike mi6 --b1 android."
	if !strings.Contains(first.stdout.String(), cut) {
		t.Errorf("the first mi6 --b1 said:\n%s\nwant %q", first.stdout, cut)
	}
	if !taker.wait(2*time.Minute) || taker.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", taker.code, taker.stdout, taker.stderr)
	}
	after := r.state()
	if string(after.Open["MI6"]) != string(before.Open["MI6"]) || after.Handoffs["acct-1/google:android"] != 1 || after.Handoffs["acct-1/google:tv"] != 0 {
		t.Errorf("the cut login changed the run:\n%s\n→\n%s\nhandoffs %v", before.Open["MI6"], after.Open["MI6"], after.Handoffs)
	}
	if s := r.run("", nil, "status"); !strings.Contains(s.stdout.String(), ask) {
		t.Errorf("status after the cut:\n%s\nwant %q", s.stdout, ask)
	}
	until(t, time.Minute, "resume's hub again", served)
	second := r.start(nil, "mi6", "--b1", "android")
	a.loginB1(t, "android")
	if !second.wait(2*time.Minute) || second.code != 0 || !strings.Contains(second.stdout.String(), msgBackToAutostart) {
		t.Fatalf("the second mi6 --b1: exit %d\n%s\n%s", second.code, second.stdout, second.stderr)
	}
	run, err := mi6.Decode(r.state().Open["MI6"])
	if err != nil {
		t.Fatal(err)
	}
	if p := run.Pair("g-android"); p.Plan != mi6.PlanB1 || !p.HandoffOK || p.ClientType != "tv" {
		t.Errorf("g-android after B1: %+v", p)
	}
	until(t, time.Minute, "resume's hub after B1", served)
	if res.code != -1 {
		t.Error("resume ended")
	}
}
