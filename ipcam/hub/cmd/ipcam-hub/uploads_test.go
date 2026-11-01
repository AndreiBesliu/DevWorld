package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/rtsptest"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	fakegraph "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/graph"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
)

// The upload tests' stream: one clear job, which the fake MediaMTX in mode
// "sleep" records nothing of, so its segments are the ones a test places
// under rec/.
const (
	testCam     = "cam1"
	testProfile = "sub"
)

// localDest is a LOCAL destination dst-1 whose folder is root.
func localDest(root string) map[string]any {
	return map[string]any{"id": "dst-1", "kind": "LOCAL", "folder": root, "tokenRef": "", "baseUrl": "", "uploadBaseUrl": "", "partSize": 0}
}

// uploadsTo is an uploads section that sends cam1/sub's continuous segments
// to dest, from fromMs on (0: always).
func uploadsTo(dest map[string]any, fromMs int64) map[string]any {
	return map[string]any{
		"hubName":      "hub",
		"destinations": []any{dest},
		"routes": []any{map[string]any{"cam": testCam, "profile": testProfile, "class": catalog.ClassContinuous,
			"destinations": []string{dest["id"].(string)}, "fromMs": fromMs, "untilMs": 0}},
	}
}

// uploadConfig writes the run.json of a hub on dataDir that records cam1/sub
// through the fake MediaMTX in mode "sleep", with uploads when it is not nil,
// beside dataDir, and returns its path.
func uploadConfig(t *testing.T, dataDir string, uploads map[string]any) string {
	t.Helper()
	t.Setenv(fakeMediaMTXEnv, "sleep")
	cfg := map[string]any{"dataDir": dataDir, "mediamtx": os.Args[0], "jobs": []map[string]any{
		{"cam": testCam, "profile": testProfile, "restreamUrl": "rtsp://127.0.0.1:1/" + testCam + "/" + testProfile, "recipients": nil},
	}}
	if uploads != nil {
		cfg["uploads"] = uploads
	}
	config := filepath.Join(filepath.Dir(dataDir), "run.json")
	writeJSON(t, config, cfg)
	return config
}

// placeTornSegment writes, under dataDir's rec/, the .part of a segment of
// cam/profile that starts at start, with n whole parts and a torn last one,
// as a killed run leaves it, and returns the path of the segment that the
// startup reconcile finalises.
func placeTornSegment(t *testing.T, dataDir, cam, profile string, start time.Time, n int) string {
	t.Helper()
	init, parts, err := fakeRecording(n + 1)
	if err != nil {
		t.Fatal(err)
	}
	start = start.UTC().Truncate(time.Second)
	dir := filepath.Join(dataDir, "rec", cam, profile, start.Format(time.DateOnly))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(dir, start.Format("150405")+"_000000.mp4.part")
	data := slices.Concat(append(append([][]byte{init}, parts[:n]...), parts[n][:20])...)
	if err := os.WriteFile(part, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(part, ".part")
}

// stopAndDrain stops h and returns the events that it emits until it ends,
// failing the test unless it exits 0. Its stderr is complete afterwards.
func stopAndDrain(t *testing.T, h *hub) []map[string]any {
	t.Helper()
	if err := stopHub(h); err != nil {
		t.Fatalf("stop ipcam-hub: %v", err)
	}
	var after []map[string]any
	for e := range h.events {
		after = append(after, e)
	}
	<-h.done
	if err := h.cmd.Wait(); err != nil {
		t.Errorf("ipcam-hub after the stop: %v, want exit 0", err)
	}
	return after
}

// uploadEvent reports whether e is one of the uploads' events.
func uploadEvent(e map[string]any) bool {
	typ, _ := e["type"].(string)
	return strings.HasPrefix(typ, "upload_") || typ == "dest_state"
}

func sha256File(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a file of the test
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// objectFile is the file under root, a LOCAL destination's, of the copy of
// cam1/sub's clear segment that starts at startMs, in class CONTINUOUS
// (outbox.KeyFor).
func objectFile(t *testing.T, root, hubID string, startMs int64) string {
	t.Helper()
	s := catalog.Segment{Cam: testCam, Profile: testProfile, StartMs: startMs}
	return filepath.Join(root, filepath.FromSlash(string(outbox.KeyFor(hubID, "hub", s, catalog.ClassContinuous))))
}

func readHubIDFile(t *testing.T, dataDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, "state", "hub_id")) //nolint:gosec // a file of the test
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// TestRun_UploadsSection_Validates (P01): run.json's uploads section is
// refused, exit 1 with a message that names what is wrong, for an unknown
// field, an unknown kind, a route to an unknown destination, a duplicate
// destination id, a partSize without baseUrl, a partSize off the driver's
// alignment or over OneDrive's largest request, a LOCAL destination with a
// partSize, a baseUrl, an uploadBaseUrl or a tokenRef, or a folder that is
// not absolute, a OneDrive destination with an uploadBaseUrl or a folder
// (fix round 1's concern 2: each kind takes only its own fields), a cloud
// destination with no tokenRef, a route of an unknown class and a
// route whose non-zero untilMs is not after its fromMs. diag uploads, which
// reads the same section, refuses it alike, and neither makes the data
// directory or the LOCAL folder. A route from fromMs on with no end is valid.
func TestRun_UploadsSection_Validates(t *testing.T) {
	dir := t.TempDir()
	drive := func(change func(map[string]any)) map[string]any {
		d := map[string]any{"id": "dst-1", "kind": "GOOGLE_DRIVE", "folder": "ipcam", "tokenRef": "dst-1", "baseUrl": "", "uploadBaseUrl": "", "partSize": 0}
		if change != nil {
			change(d)
		}
		return d
	}
	localDest := func(change func(map[string]any)) map[string]any {
		d := map[string]any{"id": "dst-1", "kind": "LOCAL", "folder": filepath.Join(dir, "local"), "tokenRef": "", "baseUrl": "", "uploadBaseUrl": "", "partSize": 0}
		if change != nil {
			change(d)
		}
		return d
	}
	route := func(change func(map[string]any)) map[string]any {
		r := map[string]any{"cam": "cam1", "profile": "sub", "class": "EVENT_CLIP", "destinations": []string{"dst-1"}, "fromMs": 0, "untilMs": 0}
		if change != nil {
			change(r)
		}
		return r
	}
	uploads := func(dests []any, routes ...any) map[string]any {
		return map[string]any{"hubName": "hub", "destinations": dests, "routes": routes}
	}
	for _, tc := range []struct {
		name, want string
		uploads    map[string]any
	}{
		{"an unknown field", `unknown field "bogus"`, uploads([]any{drive(func(d map[string]any) { d["bogus"] = 1 })}, route(nil))},
		{"an unknown kind", `unknown kind "FTP"`, uploads([]any{drive(func(d map[string]any) { d["kind"] = "FTP" })}, route(nil))},
		{"a route to an unknown destination", `unknown destination "dst-9"`, uploads([]any{drive(nil)}, route(func(r map[string]any) { r["destinations"] = []string{"dst-9"} }))},
		{"a duplicate destination id", `duplicate destination id "dst-1"`, uploads([]any{drive(nil), drive(nil)}, route(nil))},
		{"a partSize without baseUrl", "partSize needs baseUrl", uploads([]any{drive(func(d map[string]any) { d["partSize"] = 512 << 10 })}, route(nil))},
		{"a partSize off the alignment", "partSize 1000 is not a positive multiple of 262144", uploads([]any{drive(func(d map[string]any) {
			d["baseUrl"], d["partSize"] = "http://127.0.0.1:1", 1000
		})}, route(nil))},
		{"a OneDrive partSize off the alignment", "partSize 524288 is not a positive multiple of 327680", uploads([]any{drive(func(d map[string]any) {
			d["kind"], d["folder"], d["baseUrl"], d["partSize"] = "ONEDRIVE_PERSONAL", "", "http://127.0.0.1:1", 512<<10
		})}, route(nil))},
		{"a OneDrive partSize over its largest request", "partSize 62914560 is over the driver's largest request, 62914559 bytes", uploads([]any{drive(func(d map[string]any) {
			d["kind"], d["folder"], d["baseUrl"], d["partSize"] = "ONEDRIVE_PERSONAL", "", "http://127.0.0.1:1", 192*(320<<10)
		})}, route(nil))},
		{"a LOCAL partSize", "a LOCAL destination takes no partSize", uploads([]any{localDest(func(d map[string]any) {
			d["baseUrl"], d["partSize"] = "http://127.0.0.1:1", 512<<10
		})}, route(nil))},
		{"a LOCAL baseUrl", "a LOCAL destination takes no baseUrl", uploads([]any{localDest(func(d map[string]any) {
			d["baseUrl"] = "http://127.0.0.1:1"
		})}, route(nil))},
		{"a LOCAL uploadBaseUrl", "a LOCAL destination takes no uploadBaseUrl", uploads([]any{localDest(func(d map[string]any) {
			d["uploadBaseUrl"] = "http://127.0.0.1:1"
		})}, route(nil))},
		{"a LOCAL folder that is not absolute", `folder "rel/dir" is not an absolute directory`, uploads([]any{localDest(func(d map[string]any) {
			d["folder"] = "rel/dir"
		})}, route(nil))},
		{"a cloud destination with no tokenRef", "no tokenRef", uploads([]any{drive(func(d map[string]any) { d["tokenRef"] = "" })}, route(nil))},
		{"a LOCAL tokenRef", "a LOCAL destination takes no tokenRef", uploads([]any{localDest(func(d map[string]any) {
			d["tokenRef"] = "dst-1"
		})}, route(nil))},
		{"a OneDrive uploadBaseUrl", "a ONEDRIVE_PERSONAL destination takes no uploadBaseUrl", uploads([]any{drive(func(d map[string]any) {
			d["kind"], d["folder"], d["baseUrl"], d["uploadBaseUrl"] = "ONEDRIVE_PERSONAL", "", "http://127.0.0.1:1", "http://127.0.0.1:2"
		})}, route(nil))},
		{"a OneDrive folder", "a ONEDRIVE_PERSONAL destination takes no folder", uploads([]any{drive(func(d map[string]any) {
			d["kind"] = "ONEDRIVE_PERSONAL"
		})}, route(nil))},
		{"a route of an unknown class", `unknown class "BOGUS"`, uploads([]any{drive(nil)}, route(func(r map[string]any) { r["class"] = "BOGUS" }))},
		{"a route whose untilMs is not after its fromMs", "untilMs 5000 is not after its fromMs 5000", uploads([]any{drive(nil)}, route(func(r map[string]any) {
			r["fromMs"], r["untilMs"] = 5000, 5000
		}))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "run.json")
			writeJSON(t, config, map[string]any{"dataDir": filepath.Join(dir, "data"), "jobs": []any{}, "uploads": tc.uploads})
			var stdout, stderr bytes.Buffer
			if code := run([]string{"run", "--config", config}, strings.NewReader(""), &stdout, &stderr); code != exitFatal {
				t.Errorf("run = %d, want %d", code, exitFatal)
			}
			if !strings.Contains(stderr.String(), tc.want) || !strings.Contains(stdout.String(), `"type":"error"`) {
				t.Errorf("stdout %q, stderr %q; want an error event and %q", stdout.String(), stderr.String(), tc.want)
			}
			code, lines, diagErr := runDiag(t, "uploads", "--config", config)
			if code != exitFatal || len(lines) != 0 || !strings.Contains(diagErr, tc.want) {
				t.Errorf("diag uploads = %d, %v, stderr %q; want exit %d, no line and %q", code, lines, diagErr, exitFatal, tc.want)
			}
			for _, p := range []string{filepath.Join(dir, "data"), filepath.Join(dir, "local")} {
				if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("the refused configuration made %s (%v)", p, err)
				}
			}
		})
	}
	t.Run("a route from fromMs on", func(t *testing.T) {
		config := filepath.Join(t.TempDir(), "run.json")
		writeJSON(t, config, map[string]any{"dataDir": filepath.Join(dir, "data"), "jobs": []any{},
			"uploads": uploads([]any{drive(nil)}, route(func(r map[string]any) { r["fromMs"] = 5000 }))})
		if _, _, _, err := loadRunConfig(config); err != nil {
			t.Errorf("loadRunConfig: %v", err)
		}
	})
	t.Run("a LOCAL, a Drive and a OneDrive destination at their limits", func(t *testing.T) {
		config := filepath.Join(t.TempDir(), "run.json")
		writeJSON(t, config, map[string]any{"dataDir": filepath.Join(dir, "data"), "jobs": []any{},
			"uploads": uploads([]any{
				localDest(func(d map[string]any) { d["id"] = "dst-l" }),
				drive(func(d map[string]any) {
					d["id"], d["baseUrl"], d["uploadBaseUrl"], d["partSize"] = "dst-g", "http://127.0.0.1:1", "http://127.0.0.1:2", 256<<10
				}),
				drive(func(d map[string]any) {
					d["id"], d["kind"], d["folder"], d["baseUrl"], d["partSize"] = "dst-o", "ONEDRIVE_PERSONAL", "", "http://127.0.0.1:1", 191*(320<<10)
				}),
			}, route(func(r map[string]any) { r["destinations"] = []string{"dst-l", "dst-g", "dst-o"} }))})
		if _, _, _, err := loadRunConfig(config); err != nil {
			t.Errorf("loadRunConfig: %v", err)
		}
	})
}

// TestRun_RouteWindow (P01, D1-B): a route's window bounds the segments it
// places by their start: with fromMs = T0 and untilMs = T1, a segment that
// starts before T0, or at T1 or later, gets no placement, and one that starts
// in [T0, T1) gets one, whether its row comes from PutSegment (the hook, the
// rewrite, the live repair) or from a ReconcileFiles finalisation (the
// startup reconcile).
func TestRun_RouteWindow(t *testing.T) {
	ctx := t.Context()
	const t0, t1 = int64(1_000_000), int64(2_000_000)
	cfg := &uploadsConfig{HubName: "hub",
		Destinations: []destDef{{ID: "dst-1", Kind: storage.KindLocal, Folder: t.TempDir()}},
		Routes:       []routeDef{{Cam: testCam, Profile: testProfile, Class: catalog.ClassContinuous, Destinations: []string{"dst-1"}, FromMs: t0, UntilMs: t1}},
	}
	u := &uploads{cfg: cfg}
	cat, err := catalog.OpenWith(ctx, filepath.Join(t.TempDir(), "catalog.db"), catalog.Options{OnSegmentComplete: u.enqueue})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	dst, err := local.New(cfg.Destinations[0].Folder)
	if err != nil {
		t.Fatal(err)
	}
	u.q = outbox.New(cat, map[string]storage.Destination{"dst-1": dst}, outbox.Options{RecDir: t.TempDir(), HubID: "H", HubName: "hub"})
	segment := func(name string, startMs int64) catalog.Segment {
		return catalog.Segment{Cam: testCam, Profile: testProfile, Path: testCam + "/" + testProfile + "/d/" + name + ".mp4", StartMs: startMs, EndMs: startMs + 10, Bytes: 10}
	}
	placed := map[string]bool{"t0": true, "before-t1": true, "reconciled-in": true}
	other := segment("other", t0)
	other.Cam = "cam2"
	for _, s := range []catalog.Segment{segment("before-t0", t0-1), segment("t0", t0), segment("before-t1", t1-1), segment("t1", t1), other} {
		if err := cat.PutSegment(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	var facts []catalog.FileFact
	segs, err := cat.Segments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range segs {
		facts = append(facts, catalog.FileFact{Cam: s.Cam, Profile: s.Profile, Path: s.Path, Bytes: s.Bytes})
	}
	for name, start := range map[string]int64{"reconciled-before": t0 - 1000, "reconciled-in": t0 + 500, "reconciled-after": t1} {
		s := segment(name, start)
		facts = append(facts, catalog.FileFact{Cam: s.Cam, Profile: s.Profile, Path: s.Path, StartMs: s.StartMs, EndMs: s.EndMs, Bytes: s.Bytes})
	}
	if _, err := cat.ReconcileFiles(ctx, facts); err != nil {
		t.Fatal(err)
	}
	ps, err := cat.Placements(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]catalog.Segment{}
	if segs, err = cat.Segments(ctx); err != nil {
		t.Fatal(err)
	}
	for _, s := range segs {
		byID[s.ID] = s
	}
	got := map[string]bool{}
	for _, p := range ps {
		name := strings.TrimSuffix(filepath.Base(byID[p.ObjectID].Path), ".mp4")
		got[name] = true
		if p.DestinationID != "dst-1" || p.Class != catalog.ClassContinuous || p.State != catalog.PlacementPending {
			t.Errorf("placement %+v, want a pending continuous one on dst-1", p)
		}
	}
	if !sameKeys(got, placed) {
		t.Errorf("placements for %v, want for %v only", got, placed)
	}
}

// sameKeys reports whether a and b hold the same keys.
func sameKeys(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// TestRun_TornSegmentFinalisedAtStartupIsUploaded (P03, P16): a torn .part
// that a killed run left is finalised by the startup reconcile, reaches the
// callback through ReconcileFiles' Added row, is enqueued in its row's
// transaction and uploaded to the LOCAL destination, where the copy is the
// finalised file, its SHA-256 verified. A clear job: C1-R9 rewrites only
// encrypted files.
func TestRun_TornSegmentFinalisedAtStartupIsUploaded(t *testing.T) {
	requireLocalRoom(t)
	dir := t.TempDir()
	dataDir, root := filepath.Join(dir, "data"), filepath.Join(dir, "local")
	start := time.Now().Add(-time.Hour)
	final := placeTornSegment(t, dataDir, testCam, testProfile, start, 3)
	h := startHub(t, "run", "--config", uploadConfig(t, dataDir, uploadsTo(localDest(root), 0)))
	queued := h.next(t, 30*time.Second, "upload_queued event", ofType("upload_queued"))
	startMs := start.UTC().Truncate(time.Second).UnixMilli()
	if queued["cam"] != testCam || queued["profile"] != testProfile || queued["startMs"] != float64(startMs) ||
		queued["dest"] != "dst-1" || queued["class"] != catalog.ClassContinuous {
		t.Errorf("upload_queued %v, want cam1/sub's segment of %d on dst-1", queued, startMs)
	}
	verified := h.next(t, 30*time.Second, "upload_verified event", ofType("upload_verified"))
	if verified["placementId"] != queued["placementId"] || verified["hashKind"] != string(storage.HashSHA256) {
		t.Errorf("upload_verified %v, want the queued placement's, with SHA256", verified)
	}
	stopAndDrain(t, h)
	copyPath := objectFile(t, root, readHubIDFile(t, dataDir), startMs)
	if sha256File(t, copyPath) != sha256File(t, final) {
		t.Errorf("the destination's copy %s differs from the finalised segment %s", copyPath, final)
	}
	sum := sha256File(t, final)
	if verified["hash"] != fmt.Sprintf("%x", sum[:]) {
		t.Errorf("upload_verified's hash %v, want the segment's SHA-256", verified["hash"])
	}
}

// TestRun_HubIDStableAcrossRestarts (§9.10): the hub's ID, a UUIDv7 in
// state/hub_id (0600), is written by the first run with uploads and kept by
// every later run.
func TestRun_HubIDStableAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	config := uploadConfig(t, dataDir, uploadsTo(localDest(filepath.Join(dir, "local")), 0))
	var ids []string
	for range 2 {
		h := startHub(t, "run", "--config", config)
		h.next(t, 30*time.Second, "ready event", ofType("ready"))
		stopAndDrain(t, h)
		ids = append(ids, readHubIDFile(t, dataDir))
	}
	id, err := uuid.Parse(ids[0])
	if err != nil || id.Version() != 7 || ids[1] != ids[0] {
		t.Errorf("hub IDs %q, want one UUIDv7 kept across the restart (%v)", ids, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(dataDir, "state", "hub_id")); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("state/hub_id: %v, %v; want mode 0600", info, err)
		}
	}
}

// TestRun_NoUploadsSectionKeepsC1Behaviour (g2-01): without an uploads
// section, run is c1's: it writes no state/hub_id and no hub lock, and emits
// no upload_* or dest_state event, a segment finalised at the start
// included.
func TestRun_NoUploadsSectionKeepsC1Behaviour(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	placeTornSegment(t, dataDir, testCam, testProfile, time.Now().Add(-time.Hour), 2)
	h := startHub(t, "run", "--config", uploadConfig(t, dataDir, nil))
	var seen []map[string]any
	for len(seen) == 0 || seen[len(seen)-1]["type"] != "status" {
		seen = append(seen, h.next(t, 30*time.Second, "status event", func(map[string]any) bool { return true }))
	}
	seen = append(seen, stopAndDrain(t, h)...)
	for _, e := range seen {
		if uploadEvent(e) {
			t.Errorf("event %v without an uploads section", e)
		}
	}
	for _, name := range []string{"hub_id", "hub.lock"} {
		if _, err := os.Stat(filepath.Join(dataDir, "state", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("state/%s is there (%v)", name, err)
		}
	}
}

// TestRun_SdGateBeforeUploads (HUB-17, g2-01): with an uploads section, an
// unconfirmed SD card still exits 4 with sd_card_unconfirmed alone, before
// anything changes: no data directory, and no LOCAL folder.
func TestRun_SdGateBeforeUploads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the SD-card check reads Linux's mount table (T40); Windows has none, and its gate never closes")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dataDir, root := filepath.Join(dir, "hub data"), filepath.Join(dir, "local")
	mountinfo := filepath.Join(dir, "mountinfo")
	table := mountInfoLine(21, "259:2", "/", "/dev/nvme0n1p2") + mountInfoLine(22, "179:2", dataDir, "/dev/mmcblk0p2")
	if err := os.WriteFile(mountinfo, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mountInfoEnv, mountinfo)
	h := startHub(t, "run", "--config", uploadConfig(t, dataDir, uploadsTo(localDest(root), 0)))
	var types []any
	for e := range h.events {
		types = append(types, e["type"])
	}
	<-h.done
	var exit *exec.ExitError
	if err := h.cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != exitSdCardUnconfirmed {
		t.Errorf("ipcam-hub ended with %v, want exit code 4", err)
	}
	if !slices.Equal(types, []any{"sd_card_unconfirmed"}) {
		t.Errorf("events %v, want sd_card_unconfirmed alone", types)
	}
	for _, path := range []string{dataDir, root} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is there (%v): ipcam-hub changed it before the SD-card check", path, err)
		}
	}
}

// testTokenFromParts is the bearer token of the process tests, assembled at
// run time so that no committed string looks like a token (P46).
func testTokenFromParts() string { return "SENT" + "INEL-" + "token-7b" }

// TestRun_UploadEventsCarryNoSessionURL (Review Focus 5, PH0-22, g2-03):
// with fakegraph and a static token, the uploads' events, the error events
// and stderr hold neither the fake's uploadUrl nor its address nor the
// token, also when the fake drops the connection in the middle of a
// fragment once and answers 500 once.
func TestRun_UploadEventsCarryNoSessionURL(t *testing.T) {
	fake := fakegraph.New(fakegraph.Options{})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	token := testTokenFromParts()
	fake.AcceptToken(func(got string) bool { return got == token })
	fake.FailNext("upload.put", fakegraph.Reset, "", 0)
	fake.FailNext("upload.put", http.StatusInternalServerError, "generalException", 0)
	t.Setenv(testTokenEnv, token)
	dataDir := filepath.Join(t.TempDir(), "data")
	placeTornSegment(t, dataDir, testCam, testProfile, time.Now().Add(-time.Hour), 3)
	dest := map[string]any{"id": "dst-od", "kind": "ONEDRIVE_PERSONAL", "folder": "", "tokenRef": "dst-od", "baseUrl": srv.URL, "uploadBaseUrl": "", "partSize": 320 << 10}
	h := startHub(t, "run", "--config", uploadConfig(t, dataDir, uploadsTo(dest, 0)))
	var c cardEvents
	c.until(t, h, 60*time.Second, "upload_verified event", func(all []map[string]any) bool {
		return len(ofTypeIn(all, "upload_verified", "", nil)) == 1
	})
	all := append(c.all, stopAndDrain(t, h)...)
	if fails := ofTypeIn(all, "upload_failed", "", nil); len(fails) != 2 || fails[0]["reason"] != "transient" || fails[1]["reason"] != "transient" {
		t.Errorf("upload_failed events %v, want two transient ones", fails)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	secrets := []string{token, "/up/", host}
	for _, e := range all {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(line), secret) {
				t.Errorf("event %s holds %q", line, secret)
			}
		}
	}
	for _, secret := range secrets {
		if strings.Contains(h.stderr.String(), secret) {
			t.Errorf("stderr holds %q:\n%s", secret, h.stderr.String())
		}
	}
	if n := len(fake.Objects()); n == 0 {
		t.Error("the fake holds no object")
	}
}

// holdingServer serves fake before the test's eyes: the second chunk PUT of
// an upload session that it sees (Drive's upload.put) is held, its arrival
// sent on held, until the client gives up or release is closed.
type holdingServer struct {
	fake    http.Handler
	held    chan struct{}
	release chan struct{}
	puts    atomic.Int32
	hold    atomic.Bool
}

func (s *holdingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/upload/") && s.hold.Load() && s.puts.Add(1) == 2 {
		_, _ = io.Copy(io.Discard, r.Body)
		s.held <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-s.release:
		}
		return
	}
	s.fake.ServeHTTP(w, r)
}

// TestRun_StopDuringUploadEmitsNoError (g2-02): a stop while the provider
// holds a chunk PUT is no failure: ipcam-hub exits 0 with no error or
// upload_failed event after the stop, and the placement stays uploading;
// after a restart it resumes from the confirmed chunk and is verified.
func TestRun_StopDuringUploadEmitsNoError(t *testing.T) {
	fake := fakegdrive.New(fakegdrive.Options{})
	hold := &holdingServer{fake: fake.Handler(), held: make(chan struct{}, 1), release: make(chan struct{})}
	hold.hold.Store(true)
	srv := httptest.NewServer(hold)
	t.Cleanup(func() {
		close(hold.release)
		srv.Close()
	})
	t.Setenv(testTokenEnv, testTokenFromParts())
	dataDir := filepath.Join(t.TempDir(), "data")
	placeTornSegment(t, dataDir, testCam, testProfile, time.Now().Add(-time.Hour), 120) // about 600 KB: three chunks of 256 KiB
	dest := map[string]any{"id": "dst-1", "kind": "GOOGLE_DRIVE", "folder": "ipcam", "tokenRef": "dst-1", "baseUrl": srv.URL, "uploadBaseUrl": "", "partSize": 256 << 10}
	config := uploadConfig(t, dataDir, uploadsTo(dest, 0))
	h := startHub(t, "run", "--config", config)
	select {
	case <-hold.held:
	case <-time.After(60 * time.Second):
		t.Fatal("no second chunk PUT after 60 s")
	}
	// The events since the start, Drive's create limit on the day's first
	// folders included, wait in the channel: those that the hub emitted
	// from the stop on are told apart by their time, on the same clock.
	stoppedAtMs := float64(time.Now().UnixMilli())
	for _, e := range stopAndDrain(t, h) {
		if (e["type"] == "error" || e["type"] == "upload_failed") && e["atMs"].(float64) >= stoppedAtMs {
			t.Errorf("event %v after the stop", e)
		}
	}
	hold.hold.Store(false)
	h = startHub(t, "run", "--config", config)
	resumed := h.next(t, 60*time.Second, "upload_resumed event", ofType("upload_resumed"))
	if resumed["offset"] != float64(256<<10) {
		t.Errorf("upload_resumed %v, want offset %d, the chunk confirmed before the stop", resumed, 256<<10)
	}
	h.next(t, 60*time.Second, "upload_verified event", ofType("upload_verified"))
	stopAndDrain(t, h)
	if files := len(fake.Objects()); files != 5 { // ipcam, hub, cam1, the day and the segment
		t.Errorf("the fake holds %d objects, want the 4 folders and one file", files)
	}
}

// TestRun_CardFailedStopsUploads (g2-04, T40): while rec/ is on an SD card,
// the uploads' local errors reach the card monitor, a repeated identical one
// as one error event; once the card sampler reaches FAILED, the uploads stop
// with the jobs, one error event says so, and no upload_* event and no
// write of a placement follows. The catalog's writes of placements fail
// with an I/O error, as on a dying card, until FAILED; then they would
// succeed.
func TestRun_CardFailedStopsUploads(t *testing.T) {
	requireLocalRoom(t)
	var catalogPath, fakeOut string
	h, _ := sdCardHubWith(t, "eio-on-signal:3", false, func(dataDir, out string, config map[string]any) {
		fakeOut, catalogPath = out, filepath.Join(dataDir, "state", "catalog.db")
		placeTornSegment(t, dataDir, "cam1", "main", time.Now().Add(-time.Hour), 2)
		config["uploads"] = map[string]any{"hubName": "hub",
			"destinations": []any{localDest(filepath.Join(filepath.Dir(dataDir), "local"))},
			"routes": []any{map[string]any{"cam": "cam1", "profile": "main", "class": catalog.ClassContinuous,
				"destinations": []string{"dst-1"}, "fromMs": 0, "untilMs": 0}}}
		db := openCatalog(t, catalogPath)
		if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER failing_card BEFORE UPDATE ON placements
			BEGIN SELECT RAISE(FAIL, 'input/output error'); END`); err != nil {
			t.Fatal(err)
		}
	}, "cam1/main")
	var c cardEvents
	catalogErrors := func(all []map[string]any) []map[string]any {
		var errs []map[string]any
		for _, e := range ofTypeIn(all, "error", "", nil) {
			if strings.Contains(e["message"].(string), "input/output error") {
				errs = append(errs, e)
			}
		}
		return errs
	}
	c.until(t, h, 30*time.Second, "the catalog's error event", func(all []map[string]any) bool { return len(catalogErrors(all)) > 0 })
	// The queue polls every second: let it fail again twice.
	deadline := time.Now().Add(2500 * time.Millisecond)
	c.until(t, h, 5*time.Second, "2.5 s of events", func([]map[string]any) bool { return time.Now().After(deadline) })
	if err := os.WriteFile(fakeSignal(fakeOut, signalEIO), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.until(t, h, 60*time.Second, "the uploads' stop", func(all []map[string]any) bool {
		return len(ofTypeIn(all, "error", "message", uploadsStoppedMessage)) == 1
	})
	failedAt := len(c.all)
	for i, e := range c.all {
		if e["type"] == "card" && e["state"] == "failed" {
			failedAt = i
		}
	}
	db := openCatalog(t, catalogPath)
	if _, err := db.ExecContext(t.Context(), "DROP TRIGGER failing_card"); err != nil {
		t.Fatal(err)
	}
	before := placementRows(t, db)
	deadline = time.Now().Add(2500 * time.Millisecond)
	c.until(t, h, 5*time.Second, "2.5 s of events", func([]map[string]any) bool { return time.Now().After(deadline) })
	all := append(c.all, stopAndDrain(t, h)...)
	for _, e := range all[failedAt:] {
		if uploadEvent(e) {
			t.Errorf("event %v after the card failed", e)
		}
	}
	if errs := catalogErrors(all); len(errs) != 1 {
		t.Errorf("the catalog's error events %v, want one while it lasts", errs)
	}
	if stops := ofTypeIn(all, "error", "message", uploadsStoppedMessage); len(stops) != 1 {
		t.Errorf("%d events say the uploads stopped, want one", len(stops))
	}
	if after := placementRows(t, db); !slices.Equal(after, before) || len(after) != 1 || !strings.Contains(after[0], " pending ") {
		t.Errorf("placements %q after the card failed, want them as they were, %q: one pending", after, before)
	}
}

// openCatalog opens the catalog at path as a database of its own, its
// schema migrated, for a test to read or rig.
func openCatalog(t *testing.T, path string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// placementRows are the placements of db, one line each.
func placementRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT id || ' ' || state || ' ' || attempts || ' ' || next_attempt_at_ms FROM placements ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

// TestRun_SegmentClosedIsUploadedToLocalDest (STO-02, P14): with the real
// MediaMTX and 10-s segments, every segment that closes before the test asks
// for the stop has exactly one upload_queued, upload_progress and an
// upload_verified with SHA256, matched by cam, profile and startMs, in any
// order against its segment_closed, and the destination's copy holds the
// segment's bytes. The test waits for the first two segments' upload_verified,
// then stops; the segments that close during the stop do not count.
func TestRun_SegmentClosedIsUploadedToLocalDest(t *testing.T) {
	requireLocalRoom(t)
	bin := rtsptest.MediaMTX(t)
	srv := rtsptest.Start(t, rtsptest.MainH264(t))
	dir := t.TempDir()
	dataDir, root := filepath.Join(dir, "data"), filepath.Join(dir, "local")
	config := filepath.Join(dir, "run.json")
	writeJSON(t, config, map[string]any{"dataDir": dataDir, "mediamtx": bin,
		"jobs":    []map[string]any{{"cam": testCam, "profile": testProfile, "restreamUrl": srv.URL(testCam + "/" + testProfile), "segmentSecs": 10, "recipients": nil}},
		"uploads": uploadsTo(localDest(root), 0)})
	h := startHub(t, "run", "--config", config)
	closed := map[int64]string{}         // startMs → path
	queued := map[int64]map[string]any{} // startMs → upload_queued
	byPlacement := map[string]map[string]int{}
	verified := func(startMs int64) bool {
		q := queued[startMs]
		return q != nil && byPlacement[q["placementId"].(string)]["upload_verified"] > 0
	}
	note := func(e map[string]any) {
		switch e["type"] {
		case "segment_closed":
			closed[int64(e["startMs"].(float64))] = e["path"].(string)
		case "upload_queued":
			startMs := int64(e["startMs"].(float64))
			if queued[startMs] != nil {
				t.Errorf("a second upload_queued %v of the segment of %d", e, startMs)
			}
			queued[startMs] = e
		}
		if id, ok := e["placementId"].(string); ok {
			if byPlacement[id] == nil {
				byPlacement[id] = map[string]int{}
			}
			byPlacement[id][e["type"].(string)]++
			if e["type"] == "upload_verified" && e["hashKind"] != string(storage.HashSHA256) {
				t.Errorf("upload_verified %v, want SHA256", e)
			}
		}
	}
	allVerified := func(n int) bool {
		if len(closed) < n {
			return false
		}
		for startMs := range closed {
			if !verified(startMs) {
				return false
			}
		}
		return true
	}
	deadline := time.Now().Add(90 * time.Second)
	for !allVerified(2) {
		note(h.next(t, time.Until(deadline), "the first two segments' upload_verified", func(map[string]any) bool { return true }))
	}
	stopAndDrain(t, h)
	id := readHubIDFile(t, dataDir)
	for startMs, path := range closed {
		c := byPlacement[queued[startMs]["placementId"].(string)]
		if c["upload_queued"] != 1 || c["upload_progress"] < 1 || c["upload_verified"] != 1 {
			t.Errorf("the segment of %d has events %v, want one upload_queued, upload_progress and one upload_verified", startMs, c)
		}
		if sha256File(t, objectFile(t, root, id, startMs)) != sha256File(t, path) {
			t.Errorf("the copy of the segment of %d differs from %s", startMs, path)
		}
	}
}

// TestHTTPClient_Redirects (Task 6 hand-off, Task 7 review M5): ipcam-hub's
// plain client follows a redirect (OneDrive's /content answers 302), strips
// Authorization when the redirect leaves the first host:port, keeps it on
// the same host:port, and refuses a redirect from https to http.
func TestHTTPClient_Redirects(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{} // the path of each request → its Authorization
	record := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}
	other := httptest.NewServer(http.HandlerFunc(record))
	t.Cleanup(other.Close)
	otherURL, err := url.Parse(other.URL)
	if err != nil {
		t.Fatal(err)
	}
	var first *httptest.Server
	first = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, first.URL+"/landed-same", http.StatusFound)
		case "/cross":
			// The same IP on another port: another host:port, which Go's own
			// rule, by host alone, would still send the header to.
			http.Redirect(w, r, "http://"+otherURL.Host+"/landed-cross", http.StatusFound)
		default:
			record(w, r)
		}
	}))
	t.Cleanup(first.Close)
	client := newHTTPClient(http.DefaultTransport)
	for _, path := range []string{"/same", "/cross"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, first.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+testTokenFromParts())
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if got := seen["/landed-same"]; got != "Bearer "+testTokenFromParts() {
		t.Errorf("the redirect to the same host:port carried Authorization %q, want the token", got)
	}
	if got, ok := seen["/landed-cross"]; !ok || got != "" {
		t.Errorf("the redirect to another host:port carried Authorization %q (reached: %v), want none", got, ok)
	}

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/downgraded", http.StatusFound)
	}))
	t.Cleanup(tlsSrv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tlsSrv.URL+"/content", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := newHTTPClient(tlsSrv.Client().Transport).Do(req) // the transport that trusts the test server
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the client followed a redirect from https to http")
	}
	if !strings.Contains(err.Error(), "from https to http") {
		t.Errorf("the refusal %v does not say why", err)
	}
	if _, ok := seen["/downgraded"]; ok {
		t.Error("the http URL was requested")
	}
}

// TestUploadWorkers (C2-R8): 2 uploads per destination, 1 on a host with
// less than 2 GB of RAM.
func TestUploadWorkers(t *testing.T) {
	for _, tc := range []struct{ ramMb, want int }{{0, 2}, {512, 1}, {924, 1}, {1850, 2}, {16000, 2}} {
		if got := uploadWorkers(tc.ramMb); got != tc.want {
			t.Errorf("uploadWorkers(%d) = %d, want %d", tc.ramMb, got, tc.want)
		}
	}
}
