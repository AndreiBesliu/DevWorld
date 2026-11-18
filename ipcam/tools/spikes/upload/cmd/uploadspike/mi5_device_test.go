//go:build linux || windows

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// mi5ShortTiming is the device tests' short MI5 run: a 5-s encrypted
// pre-run (a read a second), a 2-s warm-up, checks every 20 s, a read
// every 3 s, an 8-s quiet final drain, and 8 min for each wait of the
// restart or a drain (drainMax): above the outbox's 5-min backoff cap, so
// that a destination still waiting out failed attempts (the Drive
// driver's create pacing at a hub's start answers rate_limited) delays
// the restart's moment without failing the run.
const mi5ShortTiming = `{"preRun":5000,"preRunEvery":1000,"warmUp":2000,"checkEvery":20000,"readOpenEvery":3000,"floor":2000,"quiet":8000,"drainMax":480000}`

// mi6Device is the owner's device under D2-A or D2-C, as MI5 finds it: the
// real ipcam-hub on the fakes, setup's clients sealed in it, the
// autostart's resume holding the hub, and an MI6 run on Android and web
// whose two Drive pairs (the D1 test account) logged in on day 0, Android
// through plan A and web through the hub's device flow, in mi6, which then
// gave the hub back to resume. Every command of the rig logs the hub's
// events, uses the firewall seam (it records the block's commands and runs
// none) and records the simulated cameras of reccontroller --local.
type mi6Device struct {
	r              *rig
	f              *fakes
	wan            *wanProxy
	resume         *proc
	fwLog, events  string
	reccontroller  string
	resumeHubStart int
}

// wanProxy stands between mi5's hub and the fakes in the device tests: a
// reverse proxy that, while blocked, cuts every request to the fake
// providers' storage, as MI5's block cuts the uploads (the firewall seam
// blocks nothing), and passes their OAuth endpoints. Like the lab's
// fakeproviders -throttle, it paces the storage's request bodies
// (wanRate), so the restart lands between two parts of one segment
// instead of racing a whole upload on loopback. mi5 reaches the fakes
// through it (--fakes); setup, mi6 and resume reach them directly.
type wanProxy struct {
	base    string
	blocked atomic.Bool
}

// wanRate is the proxy's pace of the storage's request bodies, bytes/s:
// 256 KiB/s, so a 512 KiB part takes about 2 s. Two cameras' segments
// then go up two at a time over about 7 s after each rotation, and a
// part ends with a quarter of its segment still to go about 2 and 4 s
// in: the restart's moment comes tearAfter after the rotation even when
// no backlog is left. At the lab's 1 MiB/s, both segments were up within
// 2 s of every rotation, before the next segment had its first parts.
const wanRate = 256 << 10

// paced reads its body at wanRate.
type paced struct{ io.ReadCloser }

func (p paced) Read(b []byte) (int, error) {
	n, err := p.ReadCloser.Read(b[:min(len(b), 32<<10)])
	time.Sleep(time.Duration(n) * time.Second / wanRate)
	return n, err
}

func startWANProxy(t *testing.T, target string) *wanProxy {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	rp.Transport = &http.Transport{Proxy: nil}
	rp.ErrorLog = log.New(io.Discard, "", 0)
	p := &wanProxy{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		storage := !strings.HasPrefix(req.URL.Path, "/oauth/")
		if storage && req.Body != nil && req.Body != http.NoBody {
			req.Body = paced{req.Body}
		}
		if p.blocked.Load() && storage {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close() // no answer: the path is cut
					return
				}
			}
			http.Error(w, "blocked", http.StatusServiceUnavailable)
			return
		}
		rp.ServeHTTP(w, req)
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	p.base = srv.URL
	return p
}

// newMI6Device builds the device; it needs the fork (MEDIAMTX_BIN) and the
// media (MEDIAGEN_DIR): the upload job runs every device test, the
// windows job those of an mi5 that ends badly.
func newMI6Device(t *testing.T) *mi6Device {
	t.Helper()
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	r, f := realRig(t)
	d := &mi6Device{r: r, f: f, wan: startWANProxy(t, f.base), events: filepath.Join(t.TempDir(), "events.log"),
		reccontroller: binary(t, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")}
	log, seam := r.firewallLog()
	d.fwLog = log
	r.env = append(r.env, seam, envEventLog+"="+d.events, envReccontroller+"="+d.reccontroller, envMediaDir+"="+media,
		envLabLAN+"=127.0.0.1", envMI5Timing+"="+mi5ShortTiming)
	if p := r.run(clientsInput(), nil, "setup", "--terminal"); p.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	r.update(func(s *runstate.State) { s.Autostart = true })
	d.resume = r.start(nil, "resume")
	m := r.start(nil, "mi6", "--platforms", "android,web")
	a := &app{r: r}
	a.loginA(t, "android", 0, false)
	a.loginB1(t, "web")
	a.webCells(t, "android", "tv")
	if !m.wait(3*time.Minute) || m.code != 0 || !strings.Contains(m.stdout.String(), msgBackToAutostart) {
		t.Fatalf("mi6's day 0: exit %d\n%s\n%s", m.exitCode(), m.stdout, tail(m.stderr.String(), 30))
	}
	d.waitResumeHub(t)
	return d
}

// waitResumeHub waits until resume holds the hub again, with MI6's
// destinations only: exactly one ipcam-hub runs, the one that hub.pid
// names, resume's child, and resume serves the status; the MI5 run is
// closed.
func (d *mi6Device) waitResumeHub(t *testing.T) {
	t.Helper()
	until(t, 5*time.Minute, "resume's one hub with MI6's destinations", func() bool {
		if _, open := d.r.state().Open["MI5"]; open {
			return false
		}
		hubs := liveExes(d.r.hub)
		if len(hubs) != 1 || d.r.hubPID() != hubs[0] || parentOf(hubs[0]) != d.resume.cmd.Process.Pid {
			return false
		}
		code, _ := d.r.get("/spike/v1/status")
		return code == http.StatusOK
	})
	d.resumeHubStart = d.r.hubPID()
	if dests := d.runJSONDests(t); !slices.Equal(dests, []string{"g-android:g-android", "g-web:g-web"}) {
		t.Errorf("resume's hub has destinations %v; want MI6's only", dests)
	}
	if b := mustRead(t, filepath.Join(d.r.base, "hub", "spike", "run.json")); strings.Contains(b, "rtsp://") {
		t.Errorf("resume's run.json holds a camera's address:\n%s", b)
	}
}

// startMI5 starts uploadspike mi5 --lab on the device's fakes, through
// the WAN proxy, with two cameras, 30-s segments, 512 KiB parts and a
// block of block.
func (d *mi6Device) startMI5(block string, extra ...string) *proc {
	return d.r.start(extra, "mi5", "--lab", "--block", block, "--segment-secs", "30", "--part-size", "512KiB", "--cameras", "2", "--fakes", d.wan.base)
}

// waitBlock waits until the firewall seam's log shows the block
// (blockMarker) from its offset from on, and returns the log's length
// then.
func (d *mi6Device) waitBlock(t *testing.T, m *proc, from int) int {
	t.Helper()
	var n int
	until(t, 3*time.Minute, "the block", func() bool {
		if m.ended() {
			t.Fatalf("mi5 ended before its block: exit %d (%s)\n%s\n%s", m.code, m.errorLine(), m.stdout, tail(m.stderr.String(), 30))
		}
		b, _ := os.ReadFile(d.fwLog)
		n = len(b)
		return n > from && strings.Contains(string(b[from:]), blockMarker)
	})
	return n
}

// runJSONDests are the hub's run.json destinations, as id:tokenRef.
func (d *mi6Device) runJSONDests(t *testing.T) []string {
	t.Helper()
	var cfg runConfig
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(d.r.base, "hub", "spike", "run.json"))), &cfg); err != nil {
		t.Fatal(err)
	}
	var out []string
	if cfg.Uploads != nil {
		for _, dd := range cfg.Uploads.Destinations {
			out = append(out, dd.ID+":"+dd.TokenRef)
		}
	}
	slices.Sort(out)
	return out
}

// openMI5 is the run state's open MI5 run.
func (d *mi6Device) openMI5(t *testing.T) mi5Open {
	t.Helper()
	var o mi5Open
	raw, ok := d.r.state().Open["MI5"]
	if !ok || json.Unmarshal(raw, &o) != nil {
		t.Fatalf("no open MI5 run: %s", raw)
	}
	return o
}

// mi5Envelope reads the envelope of MI5 run runID.
func (d *mi6Device) mi5Envelope(t *testing.T, runID string) results.Envelope {
	t.Helper()
	var e results.Envelope
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(d.r.base, "results", "MI5", labHost(), runID+".json"))), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// loggedEvent is one line of the rig's event log, with the fields that the
// device tests read.
type loggedEvent struct {
	Type        string `json:"type"`
	AtMs        int64  `json:"atMs"`
	PlacementID string `json:"placementId"`
	Dest        string `json:"dest"`
	Cam         string `json:"cam"`
	StartMs     int64  `json:"startMs"`
	Addr        string `json:"addr"`
	Finalized   int    `json:"finalized"`
	Reason      string `json:"reason"`
}

// loggedEvents are the event log's events from byte offset from on.
func (d *mi6Device) loggedEvents(t *testing.T, from int) []loggedEvent {
	t.Helper()
	b := mustRead(t, d.events)
	var out []loggedEvent
	sc := bufio.NewScanner(strings.NewReader(b[min(from, len(b)):]))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var e loggedEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Type != "" {
			out = append(out, e)
		}
	}
	return out
}

// eventsLen is the event log's length now.
func (d *mi6Device) eventsLen() int {
	info, err := os.Stat(d.events)
	if err != nil {
		return 0
	}
	return int(info.Size())
}

// uploadSummary counts dest's upload events and failures by reason in
// events, for a failure's one line (the CI annotation keeps no other).
func uploadSummary(events []loggedEvent, dest string) string {
	n := map[string]int{}
	for _, e := range events {
		if e.Dest == dest && strings.HasPrefix(e.Type, "upload_") {
			k := strings.TrimPrefix(e.Type, "upload_")
			if e.Reason != "" {
				k += ":" + e.Reason
			}
			n[k]++
		}
	}
	keys := slices.Sorted(maps.Keys(n))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, n[k]))
	}
	return dest + " events: " + strings.Join(parts, ", ")
}

// pending are dst-1's placements that events queued and never verified.
func pending(events []loggedEvent, dest string) map[string]loggedEvent {
	out := map[string]loggedEvent{}
	for _, e := range events {
		switch {
		case e.Dest != dest:
		case e.Type == "upload_queued":
			out[e.PlacementID] = e
		case e.Type == "upload_verified":
			delete(out, e.PlacementID)
		}
	}
	return out
}

// refresh asks the running hub for a refresh of dest's token through its
// control endpoint (POST /control/v1/oauth/refresh), as resume's holder
// would: the endpoint's address from the hub's last control event, its
// token from the hub's environment (hubToken); false where the test
// cannot read it.
func (d *mi6Device) refresh(t *testing.T, hubPID int, dest string) (int, string, bool) {
	t.Helper()
	token, ok := hubToken(hubPID)
	if !ok {
		return 0, "", false
	}
	var addr string
	for _, e := range d.loggedEvents(t, 0) {
		if e.Type == "control" {
			addr = e.Addr
		}
	}
	if addr == "" || token == "" {
		t.Fatalf("the hub's control endpoint: address %q, token found %v", addr, token != "")
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+addr+"/control/v1/oauth/refresh",
		strings.NewReader(`{"destId":"`+dest+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Minute}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), true
}

// labHost is the lab envelopes' host on this OS.
func labHost() string {
	if runtime.GOOS == "windows" {
		return "lab-windows"
	}
	return "lab-linux"
}

// killPID kills process pid: SIGKILL, or TerminateProcess on Windows.
func killPID(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// TestMI5_TakesOverTheMI6Hub (D2-A, D2-C; Holding the hub): with an MI6
// run held by the autostart's resume, mi5 takes the hub through the
// handover; its hub's run.json holds MI6's destinations plus MI5's, dst-1
// on g-web, an MI6 row of the D1 account (web's on Linux), and no login
// is asked or counted. At its close mi5 releases the lock and the waiting
// resume restarts the hub with MI6's destinations only; the MI6 run stays
// open.
func TestMI5_TakesOverTheMI6Hub(t *testing.T) {
	d := newMI6Device(t)
	r := d.r
	handoffs := r.state().Handoffs
	m := d.startMI5("40s")
	d.waitBlock(t, m, 0)
	if !strings.Contains(d.resume.stdout.String(), msgHandedOver) {
		t.Errorf("resume did not give the hub over:\n%s", d.resume.stdout)
	}
	if hub := r.hubPID(); hub == d.resumeHubStart || parentOf(hub) != m.cmd.Process.Pid {
		t.Errorf("the hub %d (resume's was %d) is not mi5's child", hub, d.resumeHubStart)
	}
	if dests := d.runJSONDests(t); !slices.Equal(dests, []string{"dst-1:g-web", "g-android:g-android", "g-web:g-web"}) {
		t.Errorf("mi5's hub has destinations %v; want MI6's and dst-1 on g-web", dests)
	}
	login := strings.SplitN(msgMI5DeviceLogin, "%", 2)[0]
	if out := m.stdout.String(); strings.Contains(out, login) || strings.Contains(out, strings.TrimPrefix(msgMI5LoginDone, "%s")) {
		t.Errorf("mi5 asked for a login:\n%s", out)
	}
	if !maps.Equal(r.state().Handoffs, handoffs) {
		t.Errorf("handoffs %v → %v; want none counted", handoffs, r.state().Handoffs)
	}
	run := d.openMI5(t)
	if !m.wait(15 * time.Minute) {
		t.Fatalf("mi5 did not end\n%s\n%s", m.stdout, tail(m.stderr.String(), 30))
	}
	if m.code != 0 || !strings.Contains(m.stdout.String(), msgMI5Done) {
		t.Fatalf("mi5: exit %d (%s; %s)\n%s\n%s", m.code, m.errorLine(), uploadSummary(d.loggedEvents(t, 0), "dst-1"), m.stdout, tail(m.stderr.String(), 30))
	}
	e := d.mi5Envelope(t, run.RunID)
	if len(e.Runs) != 1 || !e.Runs[0].Valid || e.MI5 == nil || len(e.MI5.Dests) == 0 {
		t.Fatalf("the envelope: %+v", e)
	}
	if dst := e.MI5.Dests[0]; dst.Dest != "dst-1" || dst.Segments == 0 || dst.Verified != dst.Segments || dst.Missing != 0 || dst.Duplicates != 0 {
		t.Errorf("dst-1 %+v", dst)
	}
	d.waitResumeHub(t)
	if _, open := r.state().Open["MI6"]; !open || d.resume.ended() {
		t.Errorf("MI6 open %v, resume ended %v; want the 7-day run going on", open, d.resume.ended())
	}
}

// TestHandover_RefusedWhileMI5Runs (D2-A; Holding the hub): during mi5's
// block, mi6 --b1 android finds the open run in the run state, sends no
// handover request, exits 1 with «Proba MI5 rulează; …» and records no
// claim and no B1 outcome; a direct POST /spike/v1/run/handover with the
// run state's token gets 409 from mi5; mi5 and its hub keep running with
// the same PIDs and the run stays open and live; status meanwhile asks for
// Android's B1 after MI5.
func TestHandover_RefusedWhileMI5Runs(t *testing.T) {
	d := newMI6Device(t)
	r := d.r
	r.editMI6(func(run *mi6.Run) {
		run.Completed("g-android", mi6.PlanA, "android", time.Now().Add(-49*time.Hour).UnixMilli())
		run.Refreshed("g-android", 1, 96, "failed", time.Now().Add(-time.Hour).UnixMilli())
	})
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := d.startMI5("10m", envSignalFile+"="+signalFile)
	d.waitBlock(t, m, 0)
	hub := r.hubPID()
	pair := func() mi6.Pair {
		run, err := mi6.Decode(r.state().Open["MI6"])
		if err != nil {
			t.Fatal(err)
		}
		return *run.Pair("g-android")
	}
	before := pair()
	b := r.run("", nil, "mi6", "--b1", "android")
	if b.code != 1 || !strings.Contains(b.stderr.String(), msgMI5Running) {
		t.Errorf("mi6 --b1 android: exit %d, stderr %q; want 1 and %q", b.code, b.stderr, msgMI5Running)
	}
	if strings.Contains(m.stdout.String(), msgHandoverMI5) {
		t.Error("mi6 --b1 sent a handover request")
	}
	if c := r.state().Claim; c != nil {
		t.Errorf("mi6 --b1 recorded a claim %+v", c)
	}
	if after := pair(); after.Plan != before.Plan || after.B1Failure != before.B1Failure || after.HandoffOK != before.HandoffOK {
		t.Errorf("g-android %+v → %+v; want no B1 outcome", before, after)
	}
	st := r.state()
	if code, _ := r.do(http.MethodPost, "/spike/v1/run/handover", "", "Authorization", "Bearer "+st.HandoverToken); code != http.StatusConflict {
		t.Errorf("a direct handover during mi5: %d; want 409", code)
	}
	until(t, 10*time.Second, "mi5's refusal line", func() bool { return strings.Count(m.stdout.String(), msgHandoverMI5) == 1 })
	s := r.run("", nil, "status")
	ask := fmt.Sprintf(mi6.StatusReconnectAfterMI5, "Android", "android")
	if out := s.stdout.String(); !strings.Contains(out, ask) || !strings.Contains(out, strings.SplitN(msgMI5Running2, "%", 2)[0]) {
		t.Errorf("status during mi5:\n%s\nwant %q and the MI5 line", out, ask)
	}
	if open, live := mi5State(r.state()); !open || !live || m.ended() || r.hubPID() != hub || !alive(hub) {
		t.Errorf("open %v, live %v, mi5 ended %v, hub %d → %d; want mi5 and its hub going on", open, live, m.ended(), hub, r.hubPID())
	}
	stopHolder(t, m, signalFile)
}

// TestMI5_HolderEndsAnyWay_OneHubUnderResume (Holding the hub): on the
// device whose resume holds the MI6 hub, an mi5 that ends during its
// block by SIGHUP (its window or SSH session closed), and in a second run
// by SIGKILL (on Windows the one run, by TerminateProcess), leaves within
// 5 min exactly one hub, resume's, with MI6's destinations only: no block
// rule left (the firewall seam's log shows the removal after the cut), no
// orphaned hub or reccontroller, and the MI5 run invalid («proba MI5 s-a
// oprit înainte de final»). After the kill, while the catalog holds the
// cut run's pending dst-1 placements, that hub stays up for 3 min with
// the same PID, emits no upload event for dst-1, and refreshes an MI6
// token on the fakes (on Linux, whose /proc gives the test the hub's
// control token). mi5's first line asks to keep the window open.
func TestMI5_HolderEndsAnyWay_OneHubUnderResume(t *testing.T) {
	d := newMI6Device(t)
	r := d.r
	fw := 0
	for _, sig := range cutSignals() {
		from := d.eventsLen()
		m := d.startMI5("10m")
		fw = d.waitBlock(t, m, fw)
		d.wan.blocked.Store(true)
		run := d.openMI5(t)
		if first := strings.SplitN(m.stdout.String(), "\n", 2)[0]; first != msgMI5KeepOpen {
			t.Errorf("%v: mi5's first line %q", sig, first)
		}
		if sig == os.Kill {
			until(t, 2*time.Minute, "a dst-1 placement pending in the block", func() bool {
				return len(pending(d.loggedEvents(t, from), "dst-1")) > 0
			})
		}
		if err := m.cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		if !m.wait(time.Minute) {
			t.Fatalf("%v: mi5 did not end", sig)
		}
		d.waitResumeHub(t)
		if left := liveExes(d.reccontroller); len(left) != 0 {
			t.Errorf("%v: reccontroller outlived mi5: %v", sig, left)
		}
		if fwLog := mustRead(t, d.fwLog); !strings.Contains(fwLog[fw:], unblockMarker) {
			t.Errorf("%v: no removal of the block after the cut:\n%s", sig, fwLog[fw:])
		} else {
			fw = len(fwLog)
		}
		if e := d.mi5Envelope(t, run.RunID); len(e.Runs) != 1 || e.Runs[0].Valid || e.Runs[0].InvalidReason != invalidStopped {
			t.Errorf("%v: the cut run's envelope %+v; want invalid with %q", sig, e.Runs, invalidStopped)
		}
		d.wan.blocked.Store(false)
		if sig != os.Kill {
			continue
		}
		if left := pending(d.loggedEvents(t, from), "dst-1"); len(left) == 0 {
			t.Fatal("the cut run left no pending dst-1 placement")
		}
		hub, quiet := r.hubPID(), d.eventsLen()
		if code, body, ok := d.refresh(t, hub, "g-web"); !ok {
			t.Log("the hub's control token is not readable here: no refresh")
		} else if code != http.StatusOK {
			t.Errorf("MI6's refresh through resume's hub: %d %s", code, body)
		}
		for end := time.Now().Add(3 * time.Minute); time.Now().Before(end); time.Sleep(time.Second) {
			if r.hubPID() != hub || !alive(hub) {
				t.Fatalf("resume's hub %d ended or changed (hub.pid %d)", hub, r.hubPID())
			}
		}
		for _, e := range d.loggedEvents(t, quiet) {
			if strings.HasPrefix(e.Type, "upload_") && e.Dest == "dst-1" {
				t.Errorf("resume's hub touched the cut run's placements: %+v", e)
			}
		}
	}
}

// tornParts are the open segments' files (*.part) under the hub's rec/.
func tornParts(t *testing.T, r *rig) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(filepath.Join(r.base, "hub", "rec"), func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".part") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// waitSegmentOpen waits until, in the event log from byte offset from on,
// a camera's segment closed (segment_closed) less than 20 s ago and that
// camera's next segment is open (a .part under rec/<cam>/). A kill right
// then tears that segment, at least 10 s before its 30-s close: a
// segment's .part exists only from its first part to its close, so a kill
// at a moment left to chance can fall between two segments.
func (d *mi6Device) waitSegmentOpen(t *testing.T, from int) {
	t.Helper()
	until(t, 2*time.Minute, "a camera's next segment open after its rotation", func() bool {
		closed := map[string]int64{}
		for _, e := range d.loggedEvents(t, from) {
			if e.Type == "segment_closed" {
				closed[e.Cam] = max(closed[e.Cam], e.AtMs)
			}
		}
		for _, p := range tornParts(t, d.r) {
			for cam, at := range closed {
				if time.Since(time.UnixMilli(at)) < 20*time.Second && strings.Contains(filepath.ToSlash(p), "/rec/"+cam+"/") {
					return true
				}
			}
		}
		return false
	})
}

// TestMI5_RunAfterCutRun_CountsOnlyItsOwnSegments (Holding the hub, each
// run counts only its own segments): a first mi5 is cut during its block
// as a reboot cuts it, by SIGKILL to its hub (as hub.pid names it) and at
// once to mi5 (TerminateProcess of both on Windows, where a holder's death
// alone would stop the hub cleanly), while a camera's segment is open
// (waitSegmentOpen), leaving torn .part files under rec/ and pending
// dst-1 placements. resume's hub's startup reconcile finalises the torn
// segments, and none gets a placement (a torn segment is one with no
// segment_closed before the cut). A second mi5 on the same data
// directory then completes: its routes start at its own start; its audit
// counts only its own segments (the hub's events queued exactly that many
// dst-1 placements from its start on) with 0 missing, 0 duplicates and
// every one verified; the first run's leftover placements are verified
// during it.
func TestMI5_RunAfterCutRun_CountsOnlyItsOwnSegments(t *testing.T) {
	d := newMI6Device(t)
	r := d.r
	first := d.startMI5("10m")
	fw := d.waitBlock(t, first, 0)
	d.wan.blocked.Store(true)
	until(t, 2*time.Minute, "a dst-1 placement pending in the block", func() bool {
		return len(pending(d.loggedEvents(t, 0), "dst-1")) > 0
	})
	d.waitSegmentOpen(t, d.eventsLen())
	if err := killPID(r.hubPID()); err != nil {
		t.Fatal(err)
	}
	_ = first.cmd.Process.Kill()
	if !first.wait(time.Minute) {
		t.Fatal("the first mi5 did not end")
	}
	torn := tornParts(t, r)
	if len(torn) == 0 {
		t.Fatal("no torn .part under rec/: the test would not cover the reconcile's path")
	}
	cutAt := d.eventsLen()
	beforeCut := d.loggedEvents(t, 0)
	// The segments that closed before the cut, by camera and start: each
	// of the first run's got its placement in its own transaction, though
	// a destination in backoff queues it (upload_queued) only later. A
	// segment that the cut tore has no segment_closed before the cut.
	closed := map[string]bool{}
	for _, e := range beforeCut {
		if e.Type == "segment_closed" {
			closed[fmt.Sprint(e.Cam, e.StartMs)] = true
		}
	}
	leftovers := pending(beforeCut, "dst-1")
	d.wan.blocked.Store(false)
	d.waitResumeHub(t)
	until(t, time.Minute, "the torn segments finalised", func() bool {
		for _, p := range torn {
			if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
				return false
			}
		}
		return true
	})
	second := d.startMI5("40s")
	d.waitBlock(t, second, fw)
	run := d.openMI5(t)
	var cfg struct {
		Uploads struct {
			Routes []struct {
				Cam    string `json:"cam"`
				FromMs int64  `json:"fromMs"`
			} `json:"routes"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(r.base, "hub", "spike", "run.json"))), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Uploads.Routes) == 0 {
		t.Error("the second run's run.json has no route")
	}
	for _, rt := range cfg.Uploads.Routes {
		if rt.FromMs != run.StartMs {
			t.Errorf("route %+v; want fromMs %d, the second run's start", rt, run.StartMs)
		}
	}
	if !second.wait(15 * time.Minute) {
		t.Fatalf("the second mi5 did not end\n%s\n%s", second.stdout, tail(second.stderr.String(), 30))
	}
	if second.code != 0 {
		t.Fatalf("the second mi5: exit %d (%s; %s)\n%s\n%s", second.code, second.errorLine(), uploadSummary(d.loggedEvents(t, cutAt), "dst-1"),
			second.stdout, tail(second.stderr.String(), 30))
	}
	e := d.mi5Envelope(t, run.RunID)
	if len(e.Runs) != 1 || !e.Runs[0].Valid || e.MI5 == nil || len(e.MI5.Dests) == 0 {
		t.Fatalf("the second run's envelope: %+v", e)
	}
	after := d.loggedEvents(t, cutAt)
	own := map[string]bool{}
	verified := map[string]int64{}
	for _, ev := range after {
		switch {
		case ev.Dest != "dst-1":
		case ev.Type == "upload_queued" && ev.StartMs >= run.StartMs:
			own[fmt.Sprint(ev.Cam, ev.StartMs)] = true
		case ev.Type == "upload_queued" && !closed[fmt.Sprint(ev.Cam, ev.StartMs)]:
			t.Errorf("a segment that the cut tore got a placement: %+v", ev)
		case ev.Type == "upload_verified":
			verified[ev.PlacementID] = ev.AtMs
		}
	}
	dst := e.MI5.Dests[0]
	if dst.Dest != "dst-1" || len(own) == 0 || dst.Segments != len(own) || dst.Verified != dst.Segments || dst.Missing != 0 || dst.Duplicates != 0 ||
		dst.HashMismatches != 0 {
		t.Errorf("dst-1 %+v; want its own %d segments, all verified", dst, len(own))
	}
	for id, ev := range leftovers {
		if at, ok := verified[id]; !ok || at < run.StartMs {
			t.Errorf("the cut run's placement %s (%s at %d) was not verified during the second run", id, ev.Cam, ev.StartMs)
		}
	}
}

// TestMI5_OwnLoginWithoutMI6Run (D2-B on the PC; Tokens): with no MI6
// run, an owner's mi5 starts the device's hub itself and asks one
// device-flow login for dst-1 on it (B1: the address and the code in its
// window), which the fakes approve; the login is counted once in the
// handoff cap (the account's TV client) and recorded with its issuance in
// the open run (P53's check), and the run goes on to its block.
func TestMI5_OwnLoginWithoutMI6Run(t *testing.T) {
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	r, f := realRig(t)
	recc := binary(t, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")
	log, seam := r.firewallLog()
	r.env = append(r.env, seam, envReccontroller+"="+recc, envMediaDir+"="+media, envLabLAN+"=127.0.0.1", envMI5Timing+"="+mi5ShortTiming)
	if p := r.run(clientsInput(), nil, "setup", "--terminal"); p.code != 0 {
		t.Fatalf("setup: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := r.start([]string{envSignalFile + "=" + signalFile}, "mi5", "--block", "10m", "--segment-secs", "30", "--part-size", "512KiB",
		"--cameras", "2", "--fakes", f.base)
	d := &mi6Device{r: r, fwLog: log}
	d.waitBlock(t, m, 0)
	out := m.stdout.String()
	if n := strings.Count(out, strings.SplitN(msgMI5DeviceLogin, "%", 2)[0]); n != 1 || !strings.Contains(out, fmt.Sprintf(msgMI5LoginDone, "dst-1")) {
		t.Errorf("mi5 asked %d device logins; want one, for dst-1, done:\n%s", n, out)
	}
	st := r.state()
	if st.Handoffs["acct-1/google:tv"] != 1 || len(st.Handoffs) != 1 {
		t.Errorf("handoffs %v; want acct-1's TV client once", st.Handoffs)
	}
	o := d.openMI5(t)
	if len(o.Dests) == 0 || o.Dests[0].ID != "dst-1" || o.Dests[0].TokenRef != "dst-1" || len(o.Logins) != 1 || o.Logins[0].TokenRef != "dst-1" ||
		o.Logins[0].IssuedAtMs <= 0 {
		t.Errorf("the open run's destinations %+v and logins %+v; want dst-1 on its own login", o.Dests, o.Logins)
	}
	stopHolder(t, m, signalFile)
}

// TestMI5_HubCrashDuringBlock_RestartedAndRecorded (Task 9's hub
// lifetime under mi5): an ipcam-hub that ends on its own during mi5's
// block (SIGKILL, as hub.pid names it) is noticed at once: the owner's
// «Hub-ul s-a oprit singur; îl repornesc.», the hub restarted with MI5's
// composition, and the run goes on to its normal end. Its envelope is
// valid and records the crash: hubCrashes 1 and a hub-crash gap in the
// run's gaps (G14 then gives no verdict on it). Unnoticed, the run waited
// out the restart's bound (8 min here, 12 h for the owner) for an upload
// after the unblock, and ended invalid with no data.
func TestMI5_HubCrashDuringBlock_RestartedAndRecorded(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the lab's run is Linux's")
	}
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	r, f := realRigWith(t, []string{"-throttle", "262144"})
	recc := binary(t, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")
	log, seam := r.firewallLog()
	p := r.start([]string{seam, envReccontroller + "=" + recc, envMediaDir + "=" + media, envLabLAN + "=127.0.0.1", envMI5Timing + "=" + mi5ShortTiming},
		"mi5", "--lab", "--block", "40s", "--segment-secs", "30", "--part-size", "512KiB", "--cameras", "2", "--fakes", f.base)
	d := &mi6Device{r: r, fwLog: log}
	d.waitBlock(t, p, 0)
	crashed := r.hubPID()
	if err := killPID(crashed); err != nil {
		t.Fatal(err)
	}
	until(t, time.Minute, "the crash's line and a new hub", func() bool {
		return strings.Contains(p.stdout.String(), msgHubCrashed) && r.hubPID() != crashed && alive(r.hubPID())
	})
	if !p.wait(15 * time.Minute) {
		t.Fatalf("mi5 did not end\nstdout:\n%s\nstderr:\n%s", p.stdout, tail(p.stderr.String(), 40))
	}
	if p.code != 0 {
		t.Fatalf("mi5: exit %d (%s)\nstdout:\n%s\nstderr:\n%s", p.code, p.errorLine(), p.stdout, tail(p.stderr.String(), 40))
	}
	files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI5", "lab-linux", "*.json"))
	if len(files) != 1 {
		t.Fatalf("envelopes %v", files)
	}
	var e results.Envelope
	if err := json.Unmarshal([]byte(mustRead(t, files[0])), &e); err != nil || e.MI5 == nil || len(e.Runs) != 1 {
		t.Fatalf("the envelope: %v", err)
	}
	if !e.Runs[0].Valid || e.MI5.HubCrashes != 1 {
		t.Errorf("the run %+v, hubCrashes %d; want valid with the crash counted", e.Runs[0], e.MI5.HubCrashes)
	}
	crashGaps := 0
	for _, g := range e.Runs[0].Gaps {
		if g.Reason == results.GapHubCrash {
			crashGaps++
		}
	}
	if crashGaps != 1 {
		t.Errorf("the run's gaps %+v; want one hub-crash gap", e.Runs[0].Gaps)
	}
	if dst := e.MI5.Dests[0]; dst.Dest != "dst-1" || dst.Segments == 0 || dst.Verified != dst.Segments || dst.Missing != 0 || dst.Duplicates != 0 {
		t.Errorf("dst-1 %+v", dst)
	}
}

// mustRead is a file's text.
func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
