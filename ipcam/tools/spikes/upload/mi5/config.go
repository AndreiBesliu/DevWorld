// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package mi5 is MI5, the upload during a 6-hour WAN outage (G14, P09,
// P14–P19, P38, P53, P56): its destinations and pre-checks, the host
// firewall's block of the hub's WAN and its checks, the run's order (the
// encrypted pre-run, the block, the restart in the middle of an upload,
// the drain, the audit), and the G14 judge. It drives ipcam-hub only
// across the process boundary (P01), through the device's one hub that
// uploadspike holds (Task 9).
package mi5

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The providers as run.json names them.
const (
	KindDrive    = "GOOGLE_DRIVE"
	KindOneDrive = "ONEDRIVE_PERSONAL"
)

// The D1 settings of --drive (C2-R1).
const (
	DriveTest         = "test"
	DriveTestPersonal = "test+personal"
	DrivePersonal     = "personal"
)

// The run's figures (P14, P38).
const (
	// SubBitrate is a camNN_sub stream's bitrate, 512 kbps, in bytes a
	// second.
	SubBitrate = 512_000 / 8
	// MaxCameras is the spec's camera count, the PC's and a strong Pi's.
	MaxCameras = 4
	// PersonalMinFree is what --drive personal needs free (D1-C).
	PersonalMinFree = 11_000_000_000
	// LabPartSize is the lab's part size, accepted only with the fakes.
	LabPartSize = 512 << 10
	// SecondDestWindow is dst-2's route window under test+personal.
	SecondDestWindow = 2 * time.Hour
	// EventCamera is the camera whose route is EVENT_CLIP, the stand-in
	// for event clips; the others' are CONTINUOUS.
	EventCamera = "cam01"
	// ReadOpenCalls is how many open-segment reads each series makes.
	ReadOpenCalls = 20
)

// DestConfig is one of MI5's destinations: its kind as in run.json, its
// account's pseudonym, its token row, its cameras, and its route window:
// every route's fromMs is the run's start; Window 0 is untilMs 0 (to the
// run's end), else untilMs is the run's start + Window (Task 7b).
type DestConfig struct {
	ID, Kind, Account, TokenRef string
	Cameras                     []string
	Window                      time.Duration
	// AccountKind is the account's kind, "test" or "personal" (D1).
	AccountKind string
}

// Mi5Config is one MI5 run's settings (Task 8's interface names it so).
type Mi5Config struct { //nolint:revive // the plan's interface name
	HubBin, MediaMTXBin, ReccontrollerBin, MediaDir, DataDir string
	// Cameras is 4 on the PC; min(4, diag host's RecommendedMax) on a Pi
	// (PiCameras).
	Cameras int
	// SegmentSecs is 300 for the owner, 30 in the lab.
	SegmentSecs int
	// PartSize is 0 for the owner (the driver's own); 512 KiB in the lab,
	// accepted only with the fakes.
	PartSize int64
	// EventCamera is "cam01": class EVENT_CLIP.
	EventCamera string
	// Drive is D1's setting: "test" (default), "test+personal" or
	// "personal".
	Drive string
	// OneDrive is set only when A5 passed (C2-R4).
	OneDrive bool
	// BlockFor is 6 h for the owner, 15 min in the lab (P09).
	BlockFor time.Duration
	// ProviderHosts are host[:port], 443 by default.
	ProviderHosts []string
	// LanProbe is the owner's home router (the routing table's default
	// gateway) on TCP 80, else 53, kept in RAM and never written; in the
	// lab a listener on the runner's RFC 1918 address.
	LanProbe string
	// ReadOpenCalls is at least 20 on a clear camera.
	ReadOpenCalls int
	Lab           bool
	// Fakes is the fakeproviders' base URL in the lab; "" for the owner.
	Fakes string
}

// The provider hosts of the block's checks.
var (
	GoogleHosts    = []string{"www.googleapis.com", "oauth2.googleapis.com", "accounts.google.com"}
	MicrosoftHosts = []string{"graph.microsoft.com", "login.microsoftonline.com"}
	// LabHost is the lab's fakeproviders (TEST-NET-2, P56).
	LabHost = "198.51.100.1:18080"
)

// ProviderHostsFor are the hosts that the block must cut for cfg.
func ProviderHostsFor(cfg Mi5Config) []string {
	if cfg.Lab {
		if u, err := url.Parse(cfg.Fakes); err == nil && u.Host != "" {
			return []string{u.Host} // the fakes that the lab serves (198.51.100.1:18080)
		}
		return []string{LabHost}
	}
	hosts := slices.Clone(GoogleHosts)
	if cfg.OneDrive {
		hosts = append(hosts, MicrosoftHosts...)
	}
	return hosts
}

// CameraNames are cam01 … camNN.
func CameraNames(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("cam%02d", i))
	}
	return out
}

// Destinations are cfg's destinations (C2-R1, P38): test → dst-1 on the
// test account, every camera; test+personal → also dst-2 on the personal
// Drive (acct-2), cam02 for the first 2 h; personal → dst-1 on the
// personal Drive; with OneDrive, dst-od gets cam01 only, since its bin
// keeps counting for 30 days. Each token row is the destination's own
// until the caller points it at an MI6 row of the same account.
func Destinations(cfg Mi5Config) []DestConfig {
	cams := CameraNames(cfg.Cameras)
	kind := "test"
	if cfg.Drive == DrivePersonal {
		kind = "personal"
	}
	out := []DestConfig{{ID: "dst-1", Kind: KindDrive, Account: "acct-1", AccountKind: kind, TokenRef: "dst-1", Cameras: cams}}
	if cfg.Drive == DriveTestPersonal {
		out = append(out, DestConfig{ID: "dst-2", Kind: KindDrive, Account: "acct-2", AccountKind: "personal", TokenRef: "dst-2",
			Cameras: []string{"cam02"}, Window: SecondDestWindow})
	}
	if cfg.OneDrive {
		out = append(out, DestConfig{ID: "dst-od", Kind: KindOneDrive, Account: "acct-1", AccountKind: kind, TokenRef: "dst-od",
			Cameras: []string{cfg.eventCamera()}})
	}
	return out
}

// DeferredDests are the destinations that this run leaves out, with why:
// OneDrive without A5 (C2-R4).
func DeferredDests(cfg Mi5Config) []results.Mi5Dest {
	if cfg.OneDrive {
		return nil
	}
	return []results.Mi5Dest{{Dest: "dst-od", Kind: KindOneDrive, Account: "acct-1", Deferred: MsgOneDriveDeferred}}
}

// eventCamera is cfg's event camera, cam01 by default.
func (cfg Mi5Config) eventCamera() string {
	if cfg.EventCamera == "" {
		return EventCamera
	}
	return cfg.EventCamera
}

// ClassOf is a camera's route class: EVENT_CLIP for the event camera,
// CONTINUOUS for the others.
func (cfg Mi5Config) ClassOf(cam string) string {
	if cam == cfg.eventCamera() {
		return "EVENT_CLIP"
	}
	return "CONTINUOUS"
}

// Route is one of run.json's routes, as Task 7b reads it.
type Route struct {
	Cam          string   `json:"cam"`
	Profile      string   `json:"profile"`
	Class        string   `json:"class"`
	Destinations []string `json:"destinations"`
	FromMs       int64    `json:"fromMs"`
	UntilMs      int64    `json:"untilMs"`
}

// Routes are the destinations' routes for a run that started at startMs:
// one per camera and destination, its fromMs the run's start and its
// untilMs the start + the destination's window (0 without one).
func Routes(cfg Mi5Config, dests []DestConfig, startMs int64) []Route {
	var out []Route
	for _, d := range dests {
		until := int64(0)
		if d.Window > 0 {
			until = startMs + d.Window.Milliseconds()
		}
		for _, cam := range d.Cameras {
			out = append(out, Route{Cam: cam, Profile: "sub", Class: cfg.ClassOf(cam), Destinations: []string{d.ID}, FromMs: startMs, UntilMs: until})
		}
	}
	return out
}

// Backlog is what a destination's cameras record during the block, in
// bytes: its cameras × 512 kbps × the block, within its window.
func Backlog(d DestConfig, blockFor time.Duration) int64 {
	span := blockFor
	if d.Window > 0 && d.Window < span {
		span = d.Window
	}
	return int64(len(d.Cameras)) * SubBitrate * int64(span/time.Second)
}

// PreCheck refuses a run whose destinations or spool cannot hold the
// block's backlog (P38): a destination's free quota below 2 × its
// backlog, the spool's free space below 1.5 × every camera's backlog, or
// --drive personal with less than 11 GB free on the personal Drive. free
// is each destination's Quota().Free (diag dest-probe).
func PreCheck(cfg Mi5Config, dests []DestConfig, free map[string]int64, spoolFree int64) error {
	for _, d := range dests {
		f, ok := free[d.ID]
		if !ok {
			continue
		}
		if cfg.Drive == DrivePersonal && d.ID == "dst-1" && f < PersonalMinFree {
			return ownerError(fmt.Sprintf(MsgPersonalLow, gb(f)))
		}
		if need := 2 * Backlog(d, cfg.BlockFor); f < need {
			return ownerError(fmt.Sprintf(MsgQuotaLow, d.ID, gb(f), gb(need), span(cfg.BlockFor)))
		}
	}
	total := int64(cfg.Cameras) * SubBitrate * int64(cfg.BlockFor/time.Second)
	if need := total * 3 / 2; spoolFree < need {
		return ownerError(fmt.Sprintf(MsgSpoolLow, gb(spoolFree), gb(need)))
	}
	return nil
}

// span is a block's length for the owner: "6 h", "15 min".
func span(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d h", d/time.Hour)
	}
	return fmt.Sprintf("%d min", d/time.Minute)
}

// gb is bytes in GB, one decimal, Romanian style.
func gb(n int64) string {
	return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/1e9), ".", ",", 1)
}

// PiCameras is how many cameras MI5 records on host (P17, P38): 4 on the
// PC; on a Pi min(4, its model's RecommendedMax), and a refusal for a Pi
// whose model is unknown or unsupported (RecommendedMax 0), which starts
// no hub.
func PiCameras(host hostinfo.Host) (int, error) {
	if host.PiModel == nil {
		return MaxCameras, nil
	}
	if host.PiModel.RecommendedMax <= 0 {
		return 0, ownerError(MsgPiUnknown)
	}
	return min(MaxCameras, host.PiModel.RecommendedMax), nil
}

// CheckPartSize refuses --part-size without the fakes (Task 7b accepts a
// part size only with BaseURL).
func CheckPartSize(partSize int64, fakes string) error {
	if partSize != 0 && fakes == "" {
		return ownerError(MsgPartSizeNeedsFakes)
	}
	return nil
}

// Chunks is how many parts a segment of segmentSecs of a camNN_sub stream
// takes at partSize.
func Chunks(segmentSecs int, partSize int64) int64 {
	size := int64(segmentSecs) * SubBitrate
	return (size + partSize - 1) / partSize
}

// The daily refresh's schedule, as the hub's RunDaily keeps it (P53):
// day N's first attempt is at IssuedAt + N·24 h, for days 1 to 7.
const (
	refreshDays = 7
	day         = 24 * time.Hour
	// refreshMargin is how long after a day's first attempt the block may
	// start: one retry's distance.
	refreshMargin = 15 * time.Minute
)

// Token is a token row of the device's one hub whose daily refresh the
// block must not cover: its name for the owner and its issuance.
type Token struct {
	Name     string
	IssuedAt time.Time
}

// BlockWindow is what the block covers when it starts at start: [start,
// start + blockFor + 30 min], the cleanup's margin included.
func BlockWindow(start time.Time, blockFor time.Duration) (time.Time, time.Time) {
	return start, start.Add(blockFor + CleanupAfter)
}

// RefreshConflict reports the first token whose daily refresh has its
// first attempt inside the block's window when it starts at start (P53),
// and the next start that no such attempt falls into.
func RefreshConflict(start time.Time, blockFor time.Duration, tokens []Token) (conflict *Token, firstAt, next time.Time) {
	clash := func(s time.Time) (*Token, time.Time) {
		from, to := BlockWindow(s, blockFor)
		for i, t := range tokens {
			for n := 1; n <= refreshDays; n++ {
				at := t.IssuedAt.Add(time.Duration(n) * day)
				if !at.Before(from) && !at.After(to) {
					return &tokens[i], at
				}
			}
		}
		return nil, time.Time{}
	}
	conflict, firstAt = clash(start)
	if conflict == nil {
		return nil, time.Time{}, start
	}
	next = firstAt.Add(refreshMargin)
	for range 64 {
		c, at := clash(next)
		if c == nil {
			break
		}
		next = at.Add(refreshMargin)
	}
	return conflict, firstAt, next
}

// CheckRefreshes refuses a block that would cover a daily refresh's first
// attempt, with the next allowed start.
func CheckRefreshes(start time.Time, blockFor time.Duration, tokens []Token) error {
	t, at, next := RefreshConflict(start, blockFor, tokens)
	if t == nil {
		return nil
	}
	return ownerError(fmt.Sprintf(MsgRefreshInBlock, t.Name, at.Local().Format("02.01.2006 15:04"), next.Local().Format("02.01.2006 15:04")))
}

// RouteTable is where the routing table is read: /proc/net/route on
// Linux; tests give their own file.
var RouteTable = "/proc/net/route"

// errNoGateway is a routing table without a default IPv4 gateway.
var errNoGateway = ownerError(MsgNoGateway)

// DefaultGateway is the routing table's default IPv4 gateway (the home
// router), which the LAN probe dials: it stays in RAM, and no output
// names it.
func DefaultGateway(ctx context.Context) (netip.Addr, error) {
	if runtime.GOOS == "windows" {
		out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1).NextHop").Output()
		if err != nil {
			return netip.Addr{}, errNoGateway
		}
		a, err := netip.ParseAddr(strings.TrimSpace(string(out)))
		if err != nil || !a.Is4() || a.IsUnspecified() {
			return netip.Addr{}, errNoGateway
		}
		return a, nil
	}
	return linuxGateway(RouteTable)
}

// linuxGateway reads the default route's gateway from a /proc/net/route
// file: destination 00000000, the RTF_GATEWAY flag, the gateway in
// little-endian hex.
func linuxGateway(path string) (netip.Addr, error) {
	f, err := os.Open(path) //nolint:gosec // the routing table, or a test's file
	if err != nil {
		return netip.Addr{}, errNoGateway
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		raw, err := hex.DecodeString(fields[2])
		if err != nil || len(raw) != 4 {
			continue
		}
		var flags uint64
		_, _ = fmt.Sscanf(fields[3], "%x", &flags)
		if flags&0x2 == 0 { // RTF_GATEWAY
			continue
		}
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], binary.LittleEndian.Uint32(raw))
		if a := netip.AddrFrom4(b); !a.IsUnspecified() {
			return a, nil
		}
	}
	return netip.Addr{}, errNoGateway
}

// dialTimeout bounds each LAN probe's dial.
const dialTimeout = 3 * time.Second

// Dialer dials the LAN probe; tests replace it.
var Dialer = func(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp4", addr)
	if err != nil {
		return err
	}
	return c.Close()
}

// ErrLanSilent is a router that answers neither TCP 80 nor 53.
var ErrLanSilent = ownerError(MsgLanSilent)

// FindLanProbe is the owner's LAN probe: the default gateway on TCP 80,
// else 53; a gateway that answers neither refuses the run. The address
// stays in RAM: the caller never prints or writes it.
func FindLanProbe(ctx context.Context) (string, error) {
	gw, err := DefaultGateway(ctx)
	if err != nil {
		return "", err
	}
	for _, port := range []string{"80", "53"} {
		addr := net.JoinHostPort(gw.String(), port)
		if Dialer(ctx, addr) == nil {
			return addr, nil
		}
	}
	return "", ErrLanSilent
}

// errBadConfig is a configuration that no command builds.
var errBadConfig = errors.New("mi5: bad configuration")

// Validate checks cfg's settings before a run.
func (cfg Mi5Config) Validate() error {
	switch {
	case cfg.Cameras < 1 || cfg.Cameras > MaxCameras:
		return fmt.Errorf("%w: %d cameras", errBadConfig, cfg.Cameras)
	case cfg.SegmentSecs <= 0 || cfg.BlockFor <= 0:
		return fmt.Errorf("%w: segment %d s, block %v", errBadConfig, cfg.SegmentSecs, cfg.BlockFor)
	case !slices.Contains([]string{DriveTest, DriveTestPersonal, DrivePersonal}, cfg.Drive):
		return fmt.Errorf("%w: --drive %q", errBadConfig, cfg.Drive)
	}
	return CheckPartSize(cfg.PartSize, cfg.Fakes)
}
