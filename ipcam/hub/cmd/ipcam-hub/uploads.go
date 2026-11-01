package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/onedrive"
)

// The chunk alignment of the cloud destinations' partSize (§9.2), and
// OneDrive's largest request, under 60 MiB (the onedrive driver's
// maxRequest): the drivers refuse another partSize, so the validator refuses
// it first.
const (
	driveAlign      = 256 << 10
	oneDriveAlign   = 320 << 10
	oneDriveMaxPart = 60<<20 - 1
)

// uploadsStoppedMessage is the error event of the uploads' stop when the SD
// card fails (g2-04).
const uploadsStoppedMessage = "ipcam-hub: the SD card failed: the uploads stopped"

// testToken stands in for the token store in the tests of ipcam-hub's
// process: their TestMain reads it from IPCAM_HUB_TEST_TOKEN, and the
// shipped binary never sets it, so its destinations take their tokens from
// the sealed token store (oauth.Store, Task 9).
var testToken string

// uploadsConfig is run.json's uploads section (P01): the hub's name, which
// names its clear copies (§9.10), its destinations, and the routes that send
// a stream's segments to them. A segment of a stream that no route names is
// not uploaded.
type uploadsConfig struct {
	HubName      string     `json:"hubName"`
	Destinations []destDef  `json:"destinations"`
	Routes       []routeDef `json:"routes"`
}

// destDef is a destination of the uploads section. Folder is LOCAL's root, an
// absolute directory, or Drive's folder in My Drive ("ipcam" when empty);
// TokenRef names its sealed token row. BaseURL and UploadBaseURL override
// the provider's endpoints, for the fakes only, and PartSize (0: the
// driver's own) is accepted only with BaseURL. Each kind takes only its own
// fields: a LOCAL destination takes none of the three nor a tokenRef, and a
// OneDrive one neither an uploadBaseUrl nor a folder.
type destDef struct {
	ID            string       `json:"id"`
	Kind          storage.Kind `json:"kind"`
	Folder        string       `json:"folder"`
	TokenRef      string       `json:"tokenRef"`
	BaseURL       string       `json:"baseUrl"`
	UploadBaseURL string       `json:"uploadBaseUrl"`
	PartSize      int64        `json:"partSize"`
}

// routeDef sends the segments of a stream to destinations, in a class. Its
// window, FromMs and UntilMs (0: open), bounds the segments that it places
// by their StartMs: FromMs ≤ StartMs < UntilMs.
type routeDef struct {
	Cam          string   `json:"cam"`
	Profile      string   `json:"profile"`
	Class        string   `json:"class"`
	Destinations []string `json:"destinations"`
	FromMs       int64    `json:"fromMs"`
	UntilMs      int64    `json:"untilMs"`
}

// holds reports whether the route places s: its stream, and its start in
// the window.
func (r routeDef) holds(s catalog.Segment) bool {
	return r.Cam == s.Cam && r.Profile == s.Profile && s.StartMs >= r.FromMs && (r.UntilMs == 0 || s.StartMs < r.UntilMs)
}

// validate checks the section as loadRunConfig reads it: it touches no disk
// and builds no driver. Its errors name what is wrong.
func (u *uploadsConfig) validate() error {
	if u.HubName == "" {
		return errors.New("uploads: no hubName")
	}
	known := map[string]bool{}
	for i, d := range u.Destinations {
		if err := d.validate(); err != nil {
			return fmt.Errorf("uploads: destination %d (%q): %w", i+1, d.ID, err)
		}
		if known[d.ID] {
			return fmt.Errorf("uploads: duplicate destination id %q", d.ID)
		}
		known[d.ID] = true
	}
	for i, r := range u.Routes {
		switch {
		case r.Cam == "" || r.Profile == "":
			return fmt.Errorf("uploads: route %d names no cam and profile", i+1)
		case !validClass(r.Class):
			return fmt.Errorf("uploads: route %d: unknown class %q", i+1, r.Class)
		case len(r.Destinations) == 0:
			return fmt.Errorf("uploads: route %d names no destination", i+1)
		case r.FromMs < 0 || r.UntilMs < 0:
			return fmt.Errorf("uploads: route %d: a negative fromMs or untilMs", i+1)
		case r.UntilMs != 0 && r.UntilMs <= r.FromMs:
			return fmt.Errorf("uploads: route %d: untilMs %d is not after its fromMs %d", i+1, r.UntilMs, r.FromMs)
		}
		for _, id := range r.Destinations {
			if !known[id] {
				return fmt.Errorf("uploads: route %d: unknown destination %q", i+1, id)
			}
		}
	}
	return nil
}

func (d destDef) validate() error {
	if d.ID == "" {
		return errors.New("no id")
	}
	switch d.Kind {
	case storage.KindLocal:
		return d.validateLocal()
	case storage.KindGoogleDrive:
		return d.validateCloud(driveAlign, 0)
	case storage.KindOneDrivePersonal:
		return d.validateOneDrive()
	}
	return fmt.Errorf("unknown kind %q", d.Kind)
}

// validateLocal checks a LOCAL destination: an absolute folder, and none of
// the cloud drivers' fields, which local.New would ignore.
func (d destDef) validateLocal() error {
	switch {
	case !filepath.IsAbs(d.Folder):
		return fmt.Errorf("folder %q is not an absolute directory", d.Folder)
	case d.PartSize != 0:
		return errors.New("a LOCAL destination takes no partSize")
	case d.BaseURL != "":
		return errors.New("a LOCAL destination takes no baseUrl")
	case d.UploadBaseURL != "":
		return errors.New("a LOCAL destination takes no uploadBaseUrl")
	case d.TokenRef != "":
		return errors.New("a LOCAL destination takes no tokenRef")
	}
	return nil
}

// validateOneDrive checks a OneDrive destination: the cloud fields, and
// neither uploadBaseUrl, since its upload URLs come from its sessions, nor
// folder, since its root is the app folder; onedrive.New has neither.
func (d destDef) validateOneDrive() error {
	switch {
	case d.UploadBaseURL != "":
		return fmt.Errorf("a %s destination takes no uploadBaseUrl", storage.KindOneDrivePersonal)
	case d.Folder != "":
		return fmt.Errorf("a %s destination takes no folder: its root is the app folder", storage.KindOneDrivePersonal)
	}
	return d.validateCloud(oneDriveAlign, oneDriveMaxPart)
}

// validateCloud checks a cloud destination whose driver sends parts that are
// multiples of align (never 0) and, when maxPart is not 0, at most maxPart
// bytes: a tokenRef, and a partSize only with baseUrl, the fakes'.
func (d destDef) validateCloud(align, maxPart int64) error {
	switch {
	case d.TokenRef == "":
		return errors.New("no tokenRef")
	case d.PartSize == 0:
		return nil
	case d.BaseURL == "":
		return errors.New("partSize needs baseUrl: the providers take their own part size")
	case d.PartSize < 0 || d.PartSize%align != 0:
		return fmt.Errorf("partSize %d is not a positive multiple of %d", d.PartSize, align)
	case maxPart != 0 && d.PartSize > maxPart:
		return fmt.Errorf("partSize %d is over the driver's largest request, %d bytes", d.PartSize, maxPart)
	}
	return nil
}

func validClass(class string) bool {
	switch class {
	case catalog.ClassEventClip, catalog.ClassThumbnail, catalog.ClassContinuous, catalog.ClassExport:
		return true
	}
	return false
}

// placementsFor returns s's placements: one on each destination of each
// route that holds s, the first route's class for a destination that two
// routes name.
func (u *uploadsConfig) placementsFor(s catalog.Segment) []catalog.Placement {
	var ps []catalog.Placement
	for _, r := range u.Routes {
		if !r.holds(s) {
			continue
		}
		for _, dest := range r.Destinations {
			if !slices.ContainsFunc(ps, func(p catalog.Placement) bool { return p.DestinationID == dest }) {
				ps = append(ps, outbox.PlacementFor(s, dest, r.Class))
			}
		}
	}
	return ps
}

// uploads are ipcam-hub's uploads (P01, Task 7b): the outbox queue on the
// catalog, which uploads its destinations' placements, and the events that
// it emits. run creates them only with an uploads section, after the SD
// gate: without one, run is c1's (g2-01).
type uploads struct {
	cfg    *uploadsConfig
	hubID  string
	q      *outbox.Queue
	events *uploadEvents
	unlock func() // releases the hub lock

	cancel context.CancelFunc // stops q.Run; nil until start
	done   chan struct{}      // closed once q.Run has returned
	halt   sync.Once
}

// prepareUploads makes what the uploads need on the disk, after the SD gate:
// it takes the hub lock (C2-R11), reads or creates state/hub_id, and makes
// each LOCAL destination's folder, never its parents: a missing parent is a
// disk that is not mounted.
func prepareUploads(cfg *uploadsConfig, stateDir string) (*uploads, error) {
	unlock, err := lockHub(stateDir)
	if err != nil {
		return nil, err
	}
	hubID, err := readOrCreateHubID(stateDir)
	if err != nil {
		unlock()
		return nil, err
	}
	for _, d := range cfg.Destinations {
		if d.Kind != storage.KindLocal {
			continue
		}
		if err := os.Mkdir(d.Folder, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			unlock()
			return nil, fmt.Errorf("uploads: destination %s: %w", d.ID, err)
		}
	}
	return &uploads{cfg: cfg, hubID: hubID, unlock: unlock}, nil
}

// enqueue is the catalog's OnSegmentComplete: it enqueues s's placements in
// s's transaction (S3, P03). run builds the queue before anything can write
// a segment: recorder.New comes after it.
func (u *uploads) enqueue(ctx context.Context, tx *sql.Tx, s catalog.Segment) error {
	for _, p := range u.cfg.placementsFor(s) {
		if err := u.q.Enqueue(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}

// build makes each destination's driver on client, the plain HTTP client,
// with the bearer tokens of its tokenRef from tokens and, for Drive, the
// catalog as its folder store, and the queue that uploads to them from
// recDir, its events on ev and its local errors through obs.
func (u *uploads) build(cat *catalog.DB, ev *events, obs *observer, recDir string, client *http.Client, tokens tokenSourceFunc) error {
	dests := map[string]storage.Destination{}
	for _, def := range u.cfg.Destinations {
		d, err := newDestination(def, u.hubID, cat, client, tokens)
		if err != nil {
			return fmt.Errorf("uploads: destination %s: %w", def.ID, err)
		}
		dests[def.ID] = d
	}
	ram, err := health.RAMMb(health.SystemRoots)
	if err != nil {
		ram = 0 // unknown: the default
	}
	u.events = &uploadEvents{ev: ev, obs: obs}
	u.q = outbox.New(cat, dests, outbox.Options{
		Workers: uploadWorkers(ram), RecDir: recDir, HubID: u.hubID, HubName: u.cfg.HubName, Observer: u.events,
	})
	return nil
}

// uploadWorkers is how many uploads each destination runs at a time (C2-R8):
// 2, and 1 on a host with less than 1.5 GB of RAM, a board sold with less
// than 2 GB, as a 1 GB Raspberry Pi (a 2 GB one reports about 1.8 GB). An
// upload holds two buffers of its destination's PartSize, so the peak is
// about the sum over the destinations of Workers × 2 × PartSize: with 2
// workers, 64 MiB for a Drive destination (16 MiB parts), 40 MiB for
// OneDrive (10 MiB) and 32 MiB for LOCAL (8 MiB); with 1, half of each.
func uploadWorkers(ramMb int) int {
	if ramMb > 0 && ramMb < 1536 {
		return 1
	}
	return 2
}

// start runs the queue on a context of its own, after the ready event, so
// the exit-3 and Apply-failure paths never start it. With card set, the
// card's failure stops it (g2-04), also when the card failed already.
func (u *uploads) start(card *cardSampler) {
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel, u.done = cancel, make(chan struct{})
	var failed <-chan struct{}
	if card != nil {
		card.onFailedRun(u.events.stop) // before the card's failed event (Task 7b review M2)
		failed = card.failedC()
	}
	go func() {
		defer close(u.done)
		select {
		case <-failed:
			u.cardFailed()
			return
		default:
		}
		_ = u.q.Run(ctx)
	}()
	if failed != nil {
		go func() {
			select {
			case <-failed:
				u.cardFailed()
			case <-u.done:
			}
		}()
	}
}

// cardFailed stops the uploads once the SD card has failed: every job stops,
// and nothing of the uploads writes the catalog in a loop or emits more
// (g2-04). One error event says so.
func (u *uploads) cardFailed() {
	u.halt.Do(func() {
		u.events.stop()
		u.cancel()
		u.events.ev.emit("error", map[string]any{"message": uploadsStoppedMessage})
	})
}

// stop cancels the queue and waits for it for at most shutdownWait, so its
// uploads end before the catalog closes. A context that the stop cancelled
// is no upload failure (g2-02).
func (u *uploads) stop() {
	if u.cancel == nil {
		return
	}
	u.cancel()
	select {
	case <-u.done:
	case <-time.After(shutdownWait):
	}
}

// release releases the hub lock.
func (u *uploads) release() { u.unlock() }

// unblock makes every BLOCKED_AUTH destination whose tokenRef is ref OK
// again: a new login of ref completed (Task 9).
func (u *uploads) unblock(ref string) {
	if u.q == nil {
		return
	}
	for _, d := range u.cfg.Destinations {
		if d.TokenRef == ref {
			u.q.Unblock(d.ID)
		}
	}
}

// newDestination builds def's driver on client, with the bearer tokens of
// its tokenRef from tokens: Drive's folder store is folders, the catalog.
func newDestination(def destDef, hubID string, folders gdrive.FolderStore, client *http.Client, tokens tokenSourceFunc) (storage.Destination, error) {
	switch def.Kind {
	case storage.KindLocal:
		return local.New(def.Folder)
	case storage.KindGoogleDrive:
		ts, err := tokens(def.TokenRef)
		if err != nil {
			return nil, err
		}
		return gdrive.New(gdrive.Options{
			DestID: def.ID, HTTPClient: client, Tokens: ts, BaseURL: def.BaseURL, UploadBaseURL: def.UploadBaseURL,
			PartSize: def.PartSize, Folders: folders, HubID: hubID, RootName: def.Folder,
		})
	case storage.KindOneDrivePersonal:
		ts, err := tokens(def.TokenRef)
		if err != nil {
			return nil, err
		}
		return onedrive.New(onedrive.Options{
			DestID: def.ID, HubID: hubID, HTTPClient: client, Tokens: ts, BaseURL: def.BaseURL, PartSize: def.PartSize,
		})
	}
	return nil, fmt.Errorf("unknown kind %q", def.Kind)
}

// tokenSourceFunc gives a destination the bearer tokens of its tokenRef.
type tokenSourceFunc func(tokenRef string) (oauth2.TokenSource, error)

// storeTokens takes a tokenRef's tokens from store (P27), each refresh
// within ctx; several destinations that name one tokenRef share its source.
// The tests' token, when they set one, stands in for the store.
func storeTokens(ctx context.Context, store *oauth.Store) tokenSourceFunc {
	return func(tokenRef string) (oauth2.TokenSource, error) {
		switch {
		case testToken != "":
			return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: testToken, TokenType: "Bearer"}), nil
		case store == nil:
			return nil, fmt.Errorf("no token store for tokenRef %s", tokenRef)
		}
		return store.TokenSource(ctx, tokenRef), nil
	}
}

// The timeouts of ipcam-hub's plain HTTP client, for the drivers' calls
// (P26): a route that drops ends a call as ErrTransient instead of holding a
// worker; the drivers bound each call besides.
const (
	dialTimeout           = 30 * time.Second
	tlsHandshakeTimeout   = 15 * time.Second
	responseHeaderTimeout = 2 * time.Minute
	maxRedirects          = 10
)

// hubTransport is http.DefaultTransport's, proxy from the environment
// included, with the dial, TLS and response-header timeouts, its
// connections dialled through conns.
func hubTransport(conns *connTracker) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // the standard library's own
	t.DialContext = conns.dial
	t.TLSHandshakeTimeout = tlsHandshakeTimeout
	t.ResponseHeaderTimeout = responseHeaderTimeout
	return t
}

// newHubClient is the drivers' and the token store's plain HTTP client, and
// the tracker of its connections, which reset-connections closes.
func newHubClient() (*http.Client, *connTracker) {
	conns := &connTracker{conns: map[*trackedConn]bool{}}
	return newHTTPClient(hubTransport(conns)), conns
}

// connTracker dials the hub client's connections and keeps every open one,
// so that reset closes them all: an idle one, and one that a request is on,
// which that request then sees fail as a network error, ErrTransient, and is
// retried on a new connection (Task 9, MI5's block: Windows Firewall
// authorizes at connect, so a rule added under an established flow leaves
// it open).
type connTracker struct {
	mu    sync.Mutex
	conns map[*trackedConn]bool
}

func (t *connTracker) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tc := &trackedConn{Conn: c, t: t}
	t.mu.Lock()
	t.conns[tc] = true
	t.mu.Unlock()
	return tc, nil
}

// reset closes every open connection and returns how many.
func (t *connTracker) reset() int {
	t.mu.Lock()
	open := make([]*trackedConn, 0, len(t.conns))
	for c := range t.conns {
		open = append(open, c)
	}
	t.mu.Unlock()
	for _, c := range open {
		_ = c.Close()
	}
	return len(open)
}

// trackedConn leaves its tracker once closed.
type trackedConn struct {
	net.Conn
	t    *connTracker
	once sync.Once
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.t.mu.Lock()
		delete(c.t.conns, c)
		c.t.mu.Unlock()
	})
	return c.Conn.Close()
}

// newHTTPClient is the drivers' plain client on rt (P27): the drivers set
// the bearer token on each request. It follows redirects, as OneDrive's
// /content answers 302, strips Authorization when a redirect leaves the
// first request's host and port (Go's own rule compares hosts alone), and
// refuses a redirect from https to http (Task 7 review M5).
func newHTTPClient(rt http.RoundTripper) *http.Client {
	return &http.Client{Transport: rt, CheckRedirect: checkRedirect}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("ipcam-hub: stopped after %d redirects", maxRedirects)
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("ipcam-hub: refused a redirect from https to http")
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	return nil
}
