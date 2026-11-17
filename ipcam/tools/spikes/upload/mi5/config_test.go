package mi5

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

func owner(drive string, oneDrive bool) Mi5Config {
	return Mi5Config{Cameras: 4, SegmentSecs: 300, Drive: drive, OneDrive: oneDrive, BlockFor: OwnerBlock, ReadOpenCalls: 20}
}

// TestMI5_DriveSetting (C2-R1): each --drive gives exactly its
// destinations; test+personal gives dst-2 cam02 for the run's first 2 h,
// and every route of every setting starts at the run's start.
func TestMI5_DriveSetting(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC).UnixMilli()
	for _, tc := range []struct {
		drive string
		want  []string
	}{
		{DriveTest, []string{"dst-1 acct-1 test cam01,cam02,cam03,cam04 0s"}},
		{DriveTestPersonal, []string{"dst-1 acct-1 test cam01,cam02,cam03,cam04 0s", "dst-2 acct-2 personal cam02 2h0m0s"}},
		{DrivePersonal, []string{"dst-1 acct-1 personal cam01,cam02,cam03,cam04 0s"}},
	} {
		cfg := owner(tc.drive, false)
		dests := Destinations(cfg)
		var got []string
		for _, d := range dests {
			if d.Kind != KindDrive {
				t.Errorf("%s: %s is %s", tc.drive, d.ID, d.Kind)
			}
			got = append(got, strings.Join([]string{d.ID, d.Account, d.AccountKind, strings.Join(d.Cameras, ","), d.Window.String()}, " "))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("--drive %s: %q; want %q", tc.drive, got, tc.want)
		}
		for _, r := range Routes(cfg, dests, start) {
			if r.FromMs != start || r.Profile != "sub" {
				t.Errorf("--drive %s: route %+v does not start at the run's start", tc.drive, r)
			}
			wantUntil := int64(0)
			if r.Destinations[0] == "dst-2" {
				wantUntil = start + (2 * time.Hour).Milliseconds()
			}
			if r.UntilMs != wantUntil {
				t.Errorf("--drive %s: route %+v; want untilMs %d", tc.drive, r, wantUntil)
			}
			if want := map[bool]string{true: "EVENT_CLIP", false: "CONTINUOUS"}[r.Cam == "cam01"]; r.Class != want {
				t.Errorf("route %+v; want class %s", r, want)
			}
		}
	}
}

func TestMI5_OneDriveGetsOneCamera(t *testing.T) {
	dests := Destinations(owner(DriveTest, true))
	od := dests[len(dests)-1]
	if od.ID != "dst-od" || od.Kind != KindOneDrive || !slices.Equal(od.Cameras, []string{"cam01"}) {
		t.Errorf("OneDrive's destination: %+v; want dst-od with cam01 only", od)
	}
	if b := Backlog(od, OwnerBlock); b > 2_000_000_000 {
		t.Errorf("dst-od's backlog %d > 2 GB", b)
	}
	if DeferredDests(owner(DriveTest, true)) != nil {
		t.Error("with A5, OneDrive is deferred")
	}
	if hosts := ProviderHostsFor(owner(DriveTest, true)); !slices.Contains(hosts, "graph.microsoft.com") {
		t.Errorf("hosts %v without Graph", hosts)
	}
}

func TestMI5_OneDriveDeferredWithoutA5(t *testing.T) {
	for _, d := range Destinations(owner(DriveTest, false)) {
		if d.Kind == KindOneDrive {
			t.Errorf("without A5: %+v", d)
		}
	}
	got := DeferredDests(owner(DriveTest, false))
	if len(got) != 1 || got[0].Dest != "dst-od" ||
		got[0].Deferred != "amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5)" {
		t.Errorf("deferred: %+v", got)
	}
}

// TestMI5_RefusesWhenQuotaOrSpoolTooSmall (P38).
func TestMI5_RefusesWhenQuotaOrSpoolTooSmall(t *testing.T) {
	cfg := owner(DriveTest, false)
	dests := Destinations(cfg)
	backlog := Backlog(dests[0], cfg.BlockFor)
	if want := int64(4 * 64_000 * 6 * 3600); backlog != want {
		t.Fatalf("dst-1's backlog %d; want %d", backlog, want)
	}
	plenty := int64(100e9)
	if err := PreCheck(cfg, dests, map[string]int64{"dst-1": 2 * backlog}, plenty); err != nil {
		t.Errorf("exactly 2× the backlog: %v", err)
	}
	err := PreCheck(cfg, dests, map[string]int64{"dst-1": 2*backlog - 1}, plenty)
	if err == nil || !strings.Contains(err.Error(), "Destinația dst-1 are") {
		t.Errorf("below 2× the backlog: %v", err)
	}
	err = PreCheck(cfg, dests, map[string]int64{"dst-1": plenty}, backlog*3/2-1)
	if err == nil || !strings.Contains(err.Error(), "Discul înregistrărilor are") {
		t.Errorf("a spool below 1.5× the backlog: %v", err)
	}
	personal := owner(DrivePersonal, false)
	err = PreCheck(personal, Destinations(personal), map[string]int64{"dst-1": 10_900_000_000}, plenty)
	if err == nil || !strings.Contains(err.Error(), "11 GB") {
		t.Errorf("--drive personal with 10.9 GB free: %v", err)
	}
	if err := PreCheck(personal, Destinations(personal), map[string]int64{"dst-1": 12_000_000_000}, plenty); err != nil {
		t.Errorf("--drive personal with 12 GB free: %v", err)
	}
}

// TestMI5_BlockAvoidsDueRefresh (P53): a daily refresh's first attempt
// 3 h into the block, of an MI6 pair's token or of an MI5 login on the
// same hub, refuses the start and names the next allowed one, after which
// no first attempt falls in the block's window.
func TestMI5_BlockAvoidsDueRefresh(t *testing.T) {
	start := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	for _, name := range []string{"g-windows", "dst-2"} {
		tokens := []Token{{Name: "g-android", IssuedAt: start.Add(-2*day + 10*time.Hour)}, {Name: name, IssuedAt: start.Add(-day + 3*time.Hour)}}
		c, at, next := RefreshConflict(start, OwnerBlock, tokens)
		if c == nil || c.Name != name || !at.Equal(start.Add(3*time.Hour)) {
			t.Fatalf("%s: conflict %v at %v", name, c, at)
		}
		if c2, _, _ := RefreshConflict(next, OwnerBlock, tokens); c2 != nil || !next.After(at) {
			t.Errorf("%s: the next start %v still covers %v's refresh", name, next, c2)
		}
		err := CheckRefreshes(start, OwnerBlock, tokens)
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), next.Local().Format("02.01.2006 15:04")) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A refresh just after the block's window (6 h + 30 min) is no conflict.
	ok := []Token{{Name: "g-web", IssuedAt: start.Add(-day + OwnerBlock + CleanupAfter + time.Minute)}}
	if err := CheckRefreshes(start, OwnerBlock, ok); err != nil {
		t.Errorf("a refresh after the window: %v", err)
	}
}

// TestMI5_PiCameraCount (P17, P38).
func TestMI5_PiCameraCount(t *testing.T) {
	for _, tc := range []struct {
		name string
		max  int
		want int
	}{{"Pi 5", 8, 4}, {"Pi 4", 4, 4}, {"Pi 3", 2, 2}, {"Zero 2 W", 2, 2}} {
		n, err := PiCameras(hostinfo.Host{PiModel: &results.PiModelInfo{Raw: tc.name, Known: true, Supported: true, RecommendedMax: tc.max}})
		if err != nil || n != tc.want {
			t.Errorf("%s: %d, %v; want %d", tc.name, n, err, tc.want)
		}
	}
	if n, err := PiCameras(hostinfo.Host{}); err != nil || n != 4 {
		t.Errorf("the PC: %d, %v", n, err)
	}
	for _, m := range []*results.PiModelInfo{{Raw: "Raspberry Pi ?", Tier: "unknown"}, {Raw: "Raspberry Pi Model B", Tier: "unsupported", Known: true}} {
		if _, err := PiCameras(hostinfo.Host{PiModel: m}); err == nil || err.Error() != MsgPiUnknown {
			t.Errorf("%s: %v", m.Raw, err)
		}
	}
}

// TestLabConfig_AtLeastTwoChunksPerSegment (P09, P16).
func TestLabConfig_AtLeastTwoChunksPerSegment(t *testing.T) {
	if n := Chunks(30, LabPartSize); n < 2 {
		t.Errorf("30 s of camNN_sub at 512 KiB: %d parts", n)
	}
	if err := CheckPartSize(LabPartSize, ""); err == nil || err.Error() != MsgPartSizeNeedsFakes {
		t.Errorf("--part-size without --fakes: %v", err)
	}
	if err := CheckPartSize(LabPartSize, "http://198.51.100.1:18080"); err != nil {
		t.Errorf("--part-size with --fakes: %v", err)
	}
}

// TestMI5_LanProbeIsDefaultGateway (P56, C2-R18): the probe's target is
// the routing table's default gateway, on 80 or else 53; a gateway that
// answers neither refuses the run, whose error names no address.
func TestMI5_LanProbeIsDefaultGateway(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("the Linux routing table")
	}
	table := filepath.Join(t.TempDir(), "route")
	// Default route via 192.168.1.1 (0101A8C0), flags UG (0003).
	content := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth0\t0001A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n"
	if err := os.WriteFile(table, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTable, oldDial := RouteTable, Dialer
	t.Cleanup(func() { RouteTable, Dialer = oldTable, oldDial })
	RouteTable = table
	var dialled []string
	answer := map[string]bool{}
	Dialer = func(_ context.Context, addr string) error {
		dialled = append(dialled, addr)
		if answer[addr] {
			return nil
		}
		return errors.New("refused")
	}
	if gw, err := linuxGateway(table); err != nil || gw.String() != "192.168.1.1" {
		t.Fatalf("the gateway: %v, %v", gw, err)
	}
	answer["192.168.1.1:53"] = true
	if got, err := FindLanProbe(t.Context()); err != nil || got != "192.168.1.1:53" || !slices.Equal(dialled, []string{"192.168.1.1:80", "192.168.1.1:53"}) {
		t.Errorf("probe %q, %v after %v; want 80 tried, then 53", got, err, dialled)
	}
	answer = map[string]bool{}
	_, err := FindLanProbe(t.Context())
	if !errors.Is(err, ErrLanSilent) || strings.Contains(err.Error(), "192.168") {
		t.Errorf("a silent gateway: %v", err)
	}
}
