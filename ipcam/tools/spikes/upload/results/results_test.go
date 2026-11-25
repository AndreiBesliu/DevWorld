package results

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// envelope is a valid MI6 envelope.
func envelope() Envelope {
	return Envelope{Schema: Schema, Code: "MI6", RunID: "mi6-20261117T120000Z", Set: "pc", Host: "pc", OS: "windows",
		Storage: "ssd", Sealer: "dpapi-machine", Account: "acct-1", AccountKind: "test",
		Runs: []Run{{RunID: "mi6-20261117T120000Z", StartMs: 1, EndMs: 2, Valid: true}},
		MI6: &Mi6Data{Pairs: []Mi6Pair{{Provider: "GOOGLE_DRIVE", Platform: "android", Dest: "g-android", Plan: "A", HandoffOK: true,
			Refresh: []RefreshDay{{Day: 1, Attempts: 1, Outcome: OutcomeOK}}}}}}
}

// TestEnvelope_RoundTripAndSchema: Write stamps AtMs with its clock and
// writes <dir>/<code>/<set>/<runId>.json, 0600, through a temporary file in
// the same folder that is renamed over the envelope: at the rename the
// envelope's earlier version is still whole. The file reads back as the
// envelope, with the schema; a rewrite replaces it and leaves no temporary
// file.
func TestEnvelope_RoundTripAndSchema(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return stamp }
	t.Cleanup(func() { now = time.Now; beforeRename = nil })
	e := envelope()
	e.AtMs = 42 // Write's stamp wins
	if err := Write(dir, e); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "MI6", "pc", e.RunID+".json")
	read := func() Envelope {
		t.Helper()
		b, err := os.ReadFile(final) //nolint:gosec // the test's own file
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(b), "\n") != 1 || !strings.HasSuffix(string(b), "\n") {
			t.Errorf("the envelope is not one line of JSON: %q", b)
		}
		var got Envelope
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read()
	if got.Schema != "ipcam.phase0c2.result/1" || got.AtMs != stamp.UnixMilli() || got.MI6 == nil ||
		got.MI6.Pairs[0].Dest != "g-android" || got.Runs[0].RunID != e.RunID {
		t.Errorf("read back %+v; want the envelope with the schema and AtMs %d", got, stamp.UnixMilli())
	}
	if info, err := os.Stat(final); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("the envelope's mode is %v, want 0600", info.Mode().Perm())
	}
	// The rewrite: at its rename, a temporary file of the new envelope
	// sits in the same folder and the old envelope is still whole.
	var tmpSeen string
	beforeRename = func(tmp, to string) {
		tmpSeen = tmp
		if filepath.Dir(tmp) != filepath.Dir(final) || to != final {
			t.Errorf("rename %s → %s; want a temporary file beside %s", tmp, to, final)
		}
		if b, err := os.ReadFile(tmp); err != nil || !strings.Contains(string(b), `"attempts":2`) { //nolint:gosec // the test's own file
			t.Errorf("the temporary file holds %q, %v; want the new envelope", b, err)
		}
		if old := read(); old.MI6.Pairs[0].Refresh[0].Attempts != 1 {
			t.Errorf("the envelope before the rename: %+v; want the old one, whole", old)
		}
	}
	e.MI6.Pairs[0].Refresh[0].Attempts = 2
	if err := Write(dir, e); err != nil {
		t.Fatal(err)
	}
	if tmpSeen == "" {
		t.Fatal("Write renamed no temporary file over the envelope")
	}
	if got := read(); got.MI6.Pairs[0].Refresh[0].Attempts != 2 {
		t.Errorf("after the rewrite: %+v; want the new envelope", got)
	}
	entries, err := os.ReadDir(filepath.Dir(final))
	if err != nil || len(entries) != 1 {
		t.Errorf("the folder holds %v (%v); want the envelope alone", entries, err)
	}
}

// TestEnvelope_HostIsPseudonym: an envelope's host is one of the four
// pseudonyms; any other name, a host name or an address, is refused, and
// nothing is written.
func TestEnvelope_HostIsPseudonym(t *testing.T) {
	for _, host := range []string{"pc", "pi", "lab-linux", "lab-windows"} {
		e := envelope()
		e.Host = host
		if err := Write(t.TempDir(), e); err != nil {
			t.Errorf("host %q: %v", host, err)
		}
	}
	for _, host := range []string{"", "DESKTOP-ABC123", "raspberrypi", "192.168.1.20", "lab-qemu", "PC"} {
		dir := t.TempDir()
		e := envelope()
		e.Host = host
		if err := Write(dir, e); err == nil || !strings.Contains(err.Error(), "pseudonym") {
			t.Errorf("host %q: %v; want a refusal naming the pseudonyms", host, err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("host %q: wrote %v", host, entries)
		}
	}
}

// TestEnvelope_ValidateFields: the other closed fields.
func TestEnvelope_ValidateFields(t *testing.T) {
	for name, edit := range map[string]func(*Envelope){
		"schema":       func(e *Envelope) { e.Schema = "ipcam.phase0c1.result/1" },
		"code":         func(e *Envelope) { e.Code = "MI1" },
		"run id":       func(e *Envelope) { e.RunID = "../x" },
		"set":          func(e *Envelope) { e.Set = "a/b" },
		"storage":      func(e *Envelope) { e.Storage = "nvme" },
		"sealer":       func(e *Envelope) { e.Sealer = "tpm" },
		"account":      func(e *Envelope) { e.Account = "owner@example.com" },
		"account kind": func(e *Envelope) { e.AccountKind = "work" },
		"MI6 data":     func(e *Envelope) { e.Code = "MI5" },
	} {
		e := envelope()
		edit(&e)
		if err := e.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", name, e)
		}
	}
	if err := envelope().Validate(); err != nil {
		t.Errorf("a valid envelope: %v", err)
	}
}

// TestVerdicts: P22's four forms, exactly.
func TestVerdicts(t *testing.T) {
	for got, want := range map[Verdict]string{
		Pass:                  "trece",
		Incomplete:            "fără verdict: lipsesc măsurători",
		FailWith("B2"):        "pică → B2",
		Deferred("fără cont"): "amânat în Faza 0-bis: fără cont",
	} {
		if string(got) != want {
			t.Errorf("verdict %q, want %q", got, want)
		}
	}
}
