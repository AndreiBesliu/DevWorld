package mi8

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// cam is a camera's result with matched of episodes and losses.
func cam(id, brand string, matched, episodes, losses int) results.Mi8Camera {
	c := results.Mi8Camera{Camera: id, Brand: brand, Model: "M1", Episodes: episodes, Matched: matched, SilentLosses: losses,
		ReferenceMethod: results.ReferenceSubstream, RefLagMs: 1200}
	if episodes > 0 {
		c.Coverage = float64(matched) / float64(episodes)
	}
	return c
}

// env8 is an owner's MI8 envelope of hours with cams.
func env8(hours float64, cams ...results.Mi8Camera) results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "MI8", RunID: "mi8-1", Set: "pi", Host: "pi", OS: "linux", Storage: "ssd",
		Runs: []results.Run{{RunID: "mi8-1", Valid: true}}, MI8: &results.Mi8Data{Hours: hours, QuietGapSecs: 10, Cameras: cams}}
}

// TestMI8_Coverage (G17): 24 of 25 matched (96 %) passes; 47 of 50 (94 %)
// fails with the brand's plan B.
func TestMI8_Coverage(t *testing.T) {
	if j := Judge([]results.Envelope{env8(72, cam("real-1", "Hikvision", 24, 25, 0))}); j.Verdict != results.Pass {
		t.Errorf("24/25: verdict %q (%v), want trece", j.Verdict, j.Reasons)
	}
	j := Judge([]results.Envelope{env8(72, cam("real-1", "Hikvision", 47, 50, 0))})
	if j.Verdict != results.FailWith(PlanBHikvision) {
		t.Errorf("47/50: verdict %q, want pică → %s", j.Verdict, PlanBHikvision)
	}
	if err := env8(72, cam("real-1", "Hikvision", 47, 50, 0)).Validate(); err != nil {
		t.Errorf("the envelope does not validate: %v", err)
	}
}

// TestMI8_MinEpisodes20 (P43): 19 of 20 passes; 18 of 19 reads «fără
// verdict: prea puține episoade (19)» and judges nothing.
func TestMI8_MinEpisodes20(t *testing.T) {
	if j := Judge([]results.Envelope{env8(72, cam("real-1", "Dahua", 19, 20, 0))}); j.Verdict != results.Pass {
		t.Errorf("19/20: verdict %q, want trece", j.Verdict)
	}
	j := Judge([]results.Envelope{env8(72, cam("real-1", "Dahua", 18, 19, 0))})
	if j.Verdict != results.Incomplete {
		t.Errorf("18/19: verdict %q, want fără verdict", j.Verdict)
	}
	if len(j.Rows) != 1 || !strings.Contains(j.Rows[0].Value, "fără verdict: prea puține episoade (19)") || j.Rows[0].Pass != nil {
		t.Errorf("rows = %+v, want one unjudged row with «fără verdict: prea puține episoade (19)»", j.Rows)
	}
}

// TestMI8_SilentLoss (HUB-11): one events_lost signal is one silent loss,
// and the camera fails.
func TestMI8_SilentLoss(t *testing.T) {
	st := CameraState{Camera: "real-1", Brand: "Reolink", Model: "RLC-510A"}
	tr := &tracker{st: &st, lag: func() time.Duration { return 0 }}
	tr.onSignal(KindUnhealthy)
	tr.onSignal(KindLost)
	tr.onSignal(KindHealthy)
	r := tr.snapshot().Result()
	if r.SilentLosses != 1 {
		t.Fatalf("silent losses = %d, want 1", r.SilentLosses)
	}
	for range 30 {
		r.EpisodeTimes = append(r.EpisodeTimes, results.EpisodeTime{Matched: true})
	}
	r.Episodes, r.Matched, r.Coverage = 30, 30, 1
	j := Judge([]results.Envelope{env8(80, r)})
	if j.Verdict != results.FailWith(PlanBReolink) {
		t.Errorf("verdict %q, want pică → %s", j.Verdict, PlanBReolink)
	}
}

// TestMI8_FewEpisodesWithSilentLoss_Fails (P43): a camera with 10 episodes
// and one events_lost fails G17 with its brand's plan B: a silent loss is
// judged whatever the episode count.
func TestMI8_FewEpisodesWithSilentLoss_Fails(t *testing.T) {
	j := Judge([]results.Envelope{env8(72, cam("real-1", "Hikvision", 10, 10, 1))})
	if j.Verdict != results.FailWith(PlanBHikvision) {
		t.Errorf("verdict %q, want pică → %s", j.Verdict, PlanBHikvision)
	}
}

// TestG17_SilentLossBelowEpisodeFloor_Pica is the same case under its G17
// name.
func TestG17_SilentLossBelowEpisodeFloor_Pica(t *testing.T) {
	j := Judge([]results.Envelope{env8(30, cam("real-1", "Dahua", 3, 10, 1), cam("real-2", "Hikvision", 25, 25, 0))})
	if j.Verdict != results.FailWith(PlanBDahua) {
		t.Errorf("verdict %q, want pică → %s (a measured failure decides even under 72 h)", j.Verdict, PlanBDahua)
	}
}

// TestG17_Under72Hours_FaraVerdict: 60 observed hours with every camera
// passing is fără verdict; the same with a silent loss fails.
func TestG17_Under72Hours_FaraVerdict(t *testing.T) {
	j := Judge([]results.Envelope{env8(60, cam("real-1", "Hikvision", 25, 25, 0))})
	if j.Verdict != results.Incomplete || !strings.Contains(strings.Join(j.Reasons, "|"), "60.0 h") {
		t.Errorf("60 h: verdict %q, reasons %v; want fără verdict naming 60 h", j.Verdict, j.Reasons)
	}
	if j := Judge([]results.Envelope{env8(60, cam("real-1", "Hikvision", 25, 25, 1))}); j.Verdict != results.FailWith(PlanBHikvision) {
		t.Errorf("60 h with a silent loss: verdict %q, want pică", j.Verdict)
	}
	// Observed hours add up over the envelopes too.
	if j := Judge([]results.Envelope{env8(40, cam("real-1", "Hikvision", 25, 25, 0)), env8(33)}); j.Verdict != results.Pass {
		t.Errorf("40 h + 33 h: verdict %q (%v), want trece", j.Verdict, j.Reasons)
	}
}

// TestG17_NoJudgedCamera_FaraVerdict: no envelope, or 72 h without a camera
// of 20 episodes, is fără verdict; a camera whose subscription failed is
// apart from coverage; test-seam envelopes are not judged.
func TestG17_NoJudgedCamera_FaraVerdict(t *testing.T) {
	if j := Judge(nil); j.Verdict != results.Incomplete {
		t.Errorf("no envelope: %q", j.Verdict)
	}
	if j := Judge([]results.Envelope{env8(72, cam("real-1", "Hikvision", 5, 5, 0))}); j.Verdict != results.Incomplete {
		t.Errorf("5 episodes: %q", j.Verdict)
	}
	failedSub := cam("real-2", "Dahua", 0, 0, 0)
	failedSub.SubscribeFailed = true
	j := Judge([]results.Envelope{env8(72, failedSub)})
	if j.Verdict != results.Incomplete || len(j.Rows) != 1 || !strings.Contains(j.Rows[0].Value, "abonare eșuată (configurare)") {
		t.Errorf("subscribe failed: verdict %q rows %+v", j.Verdict, j.Rows)
	}
	seams := env8(72, cam("real-1", "Hikvision", 25, 25, 0))
	seams.TestSeams = true
	if j := Judge([]results.Envelope{seams}); j.Verdict != results.Incomplete {
		t.Errorf("a test-seam envelope was judged: %q", j.Verdict)
	}
}

// TestG17_GroupedByBrand: a failing Hikvision gets «Hikvision `alertStream`»,
// a failing unknown brand «evenimente nesigure» + FLW-16, both in one
// verdict and in the decisions.
func TestG17_GroupedByBrand(t *testing.T) {
	j := Judge([]results.Envelope{env8(72,
		cam("real-1", "Hikvision", 40, 50, 0),
		cam("real-2", "Acme", 0, 0, 2),
		cam("real-3", "Hikvision", 30, 30, 1),
		cam("real-4", "Reolink", 25, 25, 0))})
	if !strings.Contains(string(j.Verdict), "Hikvision `alertStream`") || !strings.Contains(string(j.Verdict), "evenimente nesigure + FLW-16") {
		t.Errorf("verdict %q", j.Verdict)
	}
	if strings.Count(string(j.Verdict), "alertStream") != 1 {
		t.Errorf("the Hikvision plan B appears more than once: %q", j.Verdict)
	}
	if strings.Contains(string(j.Verdict), "Reolink") {
		t.Errorf("a passing brand has a plan B: %q", j.Verdict)
	}
	if len(j.Decisions) != 2 {
		t.Errorf("decisions = %q, want one per failing brand", j.Decisions)
	}
	if got := PlanBFor("DAHUA TECHNOLOGY"); got != PlanBDahua {
		t.Errorf("PlanBFor(Dahua) = %q", got)
	}
}

// TestMI8_OnePerBrandSelection (D3-B): --one-per-brand keeps the first camera
// of each brand that has events.
func TestMI8_OnePerBrandSelection(t *testing.T) {
	probes := []Probe{
		{Cam: "real-1", Brand: "Hikvision", EventsService: true, PullPoint: true},
		{Cam: "real-2", Brand: "HIKVISION", EventsService: true, PullPoint: true},
		{Cam: "real-3", Brand: "Dahua", EventsService: true, PullPoint: false},
		{Cam: "real-4", Brand: "Dahua", EventsService: true, PullPoint: true},
		{Cam: "real-5", Error: ProbeErrOnvifPort},
	}
	got := Select(probes, true)
	if ids := camIDs(got); ids != "real-1,real-4" {
		t.Errorf("one per brand = %s, want real-1,real-4", ids)
	}
}

// TestMI8_DefaultSelectsEveryCameraWithEvents (D3-A): by default every camera
// with an events service and PullPoint support takes part.
func TestMI8_DefaultSelectsEveryCameraWithEvents(t *testing.T) {
	probes := []Probe{
		{Cam: "real-1", Brand: "Hikvision", EventsService: true, PullPoint: true},
		{Cam: "real-2", Brand: "Hikvision", EventsService: true, PullPoint: true},
		{Cam: "real-3", Brand: "Dahua", EventsService: false, PullPoint: false},
		{Cam: "real-4", Brand: "Acme", EventsService: true, PullPoint: true},
		{Cam: "real-5", Error: ProbeErrAuth},
	}
	if ids := camIDs(Select(probes, false)); ids != "real-1,real-2,real-4" {
		t.Errorf("default = %s, want real-1,real-2,real-4", ids)
	}
}

func camIDs(ps []Probe) string {
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.Cam)
	}
	return strings.Join(ids, ",")
}

// TestMI8_SessionsAddUpAcrossReboot (P49): sessions of 40 h and 33 h around a
// reboot give Hours 73 and one reboot gap; a stop on the same boot is an
// mi8-stopped gap.
func TestMI8_SessionsAddUpAcrossReboot(t *testing.T) {
	const h = int64(3_600_000)
	same := func(a, b string) bool { return a == b }
	s := &State{RunID: "mi8-1", StartMs: 0}
	s.BeginSession(0, "boot-a", same)
	s.Sessions[0].EndMs, s.HeartbeatMs = 40*h, 40*h
	s.BeginSession(41*h, "boot-b", same)
	s.Sessions[1].EndMs = 74 * h
	if s.Hours() != 73 {
		t.Errorf("hours = %v, want 73", s.Hours())
	}
	if len(s.Gaps) != 1 || s.Gaps[0].Reason != results.GapReboot || s.Gaps[0].StartMs != 40*h || s.Gaps[0].EndMs != 41*h {
		t.Errorf("gaps = %+v, want one reboot gap from 40 h to 41 h", s.Gaps)
	}
	s.HeartbeatMs = 74 * h
	s.BeginSession(75*h, "boot-b", same)
	if s.Gaps[1].Reason != results.GapMI8Stopped {
		t.Errorf("a stop on one boot is %q, want mi8-stopped", s.Gaps[1].Reason)
	}
	e := s.Envelope(Meta{Host: "pi", Set: "pi", OS: "linux", Storage: "ssd"}, 76*h)
	if e.MI8.Hours != 73 || e.Validate() != nil || len(e.Runs[0].Gaps) != 2 {
		t.Errorf("envelope: hours %v, gaps %d, validate %v", e.MI8.Hours, len(e.Runs[0].Gaps), e.Validate())
	}
}

// TestMI8CameraRef_RedactsEverywhere: uploadspike's CameraRef shows neither
// address, user nor password through fmt, JSON or slog.
func TestMI8CameraRef_RedactsEverywhere(t *testing.T) {
	c := CameraRef{ID: "real-1", XAddr: "http://192.0.2.10:8000/onvif/device_service", User: testUser, Pass: testPass}
	var sb strings.Builder
	slog.New(slog.NewTextHandler(&sb, nil)).Info("x", "cam", c)
	slog.New(slog.NewJSONHandler(&sb, nil)).Info("x", "cam", c)
	b, _ := json.Marshal(c)
	all := fmt.Sprintf("%v %+v %#v %s %q", c, c, c, c, c) + string(b) + sb.String()
	for _, secret := range []string{"192.0.2.10", testUser, testPass} {
		if strings.Contains(all, secret) {
			t.Errorf("%q shows %q", all, secret)
		}
	}
	if !strings.Contains(all, "real-1") {
		t.Error("the ID is not shown")
	}
}

// TestMI8_CameraAddressWithOnvifPort (P42): IP:port gives an XAddr on that
// port; no port gives 80.
func TestMI8_CameraAddressWithOnvifPort(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.10:8000":  "http://192.0.2.10:8000/onvif/device_service",
		"192.0.2.10":       "http://192.0.2.10:80/onvif/device_service",
		" 192.0.2.10 ":     "http://192.0.2.10:80/onvif/device_service",
		"[2001:db8::1]:81": "http://[2001:db8::1]:81/onvif/device_service",
		"2001:db8::1":      "http://[2001:db8::1]:80/onvif/device_service",
		"cam.local:8000":   "http://cam.local:8000/onvif/device_service",
	} {
		if got, err := XAddrFor(in); err != nil || got != want {
			t.Errorf("XAddrFor(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "192.0.2.10:", "192.0.2.10:0", "192.0.2.10:70000", "http://192.0.2.10", "a b", "u@192.0.2.10"} {
		if got, err := XAddrFor(bad); err == nil {
			t.Errorf("XAddrFor(%q) = %q, want an error", bad, got)
		}
	}
}
