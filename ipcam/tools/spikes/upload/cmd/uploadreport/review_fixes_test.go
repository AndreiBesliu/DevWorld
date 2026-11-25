package main

import (
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// mi6WithOneDrive is mi6Env with measured OneDrive pairs and A5 passed.
func mi6WithOneDrive() results.Envelope {
	e := mi6Env()
	d := *e.MI6
	d.OneDriveEnabled = true
	pairs := append([]results.Mi6Pair{}, d.Pairs...)
	for _, p := range d.Pairs {
		q := p
		q.Provider = "ONEDRIVE_PERSONAL"
		q.Dest = "m-" + strings.TrimPrefix(p.Dest, "g-")
		pairs = append(pairs, q)
	}
	d.Pairs = pairs
	e.MI6 = &d
	return e
}

// TestRender_OneDriveUnknownSaysNoAnswer: with -onedrive unknown the
// deferred OneDrive rows say the A5 answer is missing, never that
// Microsoft asked for a paid directory; -onedrive paid keeps that reason.
func TestRender_OneDriveUnknownSaysNoAnswer(t *testing.T) {
	opts := defaultOpts()
	md := renderOf([]results.Envelope{mi6Env()}, opts)
	if strings.Contains(md, "director plătit") {
		t.Error("the -onedrive unknown report says Microsoft asks for a paid directory")
	}
	if !strings.Contains(md, "OneDrive personal: "+string(results.Deferred(oneDriveNoAnswer))) {
		t.Errorf("the -onedrive unknown report has no OneDrive row deferred with %q", oneDriveNoAnswer)
	}
	opts.oneDrive = oneDrivePaid
	if md := renderOf([]results.Envelope{mi6Env()}, opts); !strings.Contains(md, "director plătit (A5)") {
		t.Error("the -onedrive paid report lost the A5 reason")
	}
}

// TestRender_PairsTableSkipsDeferredOneDrive: without -onedrive free the
// «Perechile» table shows no OneDrive pair the gate rows call deferred.
func TestRender_PairsTableSkipsDeferredOneDrive(t *testing.T) {
	opts := defaultOpts()
	md := renderOf([]results.Envelope{mi6WithOneDrive()}, opts)
	pairs := section(md, "### Perechile")
	if strings.Contains(pairs, "OneDrive") {
		t.Errorf("the pairs table shows deferred OneDrive pairs:\n%s", pairs)
	}
	opts.oneDrive = oneDriveFree
	if pairs := section(renderOf([]results.Envelope{mi6WithOneDrive()}, opts), "### Perechile"); !strings.Contains(pairs, "OneDrive") {
		t.Errorf("with -onedrive free the pairs table lost the OneDrive pairs:\n%s", pairs)
	}
}

// TestRender_SeamedEnvelopesStayOutOfDetails: an envelope written with
// test seams enters no gate, so neither its details nor the D3-C logic
// treat it as the owner's run.
func TestRender_SeamedEnvelopesStayOutOfDetails(t *testing.T) {
	m8 := mi8Env()
	m8.TestSeams = true
	in := inputsOf(m8)
	if in.hasMI8() {
		t.Error("hasMI8() counts a seamed MI8 envelope")
	}
	opts := defaultOpts()
	opts.d3 = d3AfterPhase
	md := newReport(in, opts).markdown()
	if !strings.Contains(md, noteD3C) {
		t.Error("-d3 c with only a seamed MI8 envelope lost the D3-C note")
	}
	if strings.Contains(md, "### Pe marcă") {
		t.Error("a seamed MI8 envelope's cameras are rendered as the owner's")
	}
	m5 := mi5Env("pc", "ssd")
	m5.TestSeams = true
	r := newReport(inputsOf(m5), defaultOpts())
	if n := len(r.judged()); n != 0 {
		t.Errorf("judged() holds %d seamed envelopes; want 0", n)
	}
}

// section is md from heading up to the next heading of any level.
func section(md, heading string) string {
	i := strings.Index(md, heading)
	if i < 0 {
		return ""
	}
	rest := md[i+len(heading):]
	if j := strings.Index(rest, "\n#"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
