package outbox

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// oauthDrive is a Drive destination built as ipcam-hub builds it (P27): the
// gdrive driver on a plain client, its tokens from an oauth.Store on the
// harness's catalog, logged in through fakeoauth, against a fakegdrive that
// takes only fakeoauth's live access tokens. The token endpoint drops every
// connection while down() says so.
type oauthDrive struct {
	login *fakeoauth.Fake
	drive *fakegdrive.Fake
	dest  *gdrive.Drive
	store *oauth.Store
}

func newOAuthDrive(t *testing.T, h *harness, down func() bool) *oauthDrive {
	t.Helper()
	const secret = "SENT" + "INEL-desktop-secret"
	o := &oauthDrive{login: fakeoauth.New(fakeoauth.Options{Clock: h.clk.Now}), drive: fakegdrive.New(fakegdrive.Options{Clock: h.clk.Now})}
	o.login.AddClient(fakeoauth.Client{Provider: fakeoauth.Google, Type: fakeoauth.TypeDesktop, ID: "desktop-client", Secret: secret, Project: "p1"})
	o.drive.IdentifyTokens(func(token string) (fakegdrive.Identity, bool) {
		tok, ok := o.login.Lookup(token)
		return fakegdrive.Identity{Client: tok.ClientID, Project: tok.Project, Account: tok.Account}, ok && !tok.Expired
	})
	handler := o.login.Handler()
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down() {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				if tcp, ok := conn.(*net.TCPConn); ok {
					_ = tcp.SetLinger(0)
				}
				_ = conn.Close()
			}
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(tokenSrv.Close)
	driveSrv := httptest.NewServer(o.drive.Handler())
	t.Cleanup(driveSrv.Close)
	sealer, err := crypto.NewSealer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o.store, err = oauth.New(oauth.Config{
		Clients:   oauth.Clients{Google: map[oauth.ClientType]oauth.Client{oauth.ClientDesktop: {ID: "desktop-client", Secret: secret}}},
		Endpoints: oauth.Endpoints(fakeoauth.URLsAt(tokenSrv.URL)), Clock: h.clk.Now, Sealer: sealer, HTTP: &http.Client{}, Secrets: h.cat,
	})
	if err != nil {
		t.Fatal(err)
	}
	begin, err := o.store.Begin(t.Context(), oauth.BeginRequest{DestID: "dst-a", Provider: storage.KindGoogleDrive, Platform: oauth.PlatformWindows, LoopbackPort: 53123})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := o.login.Authorize(begin.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.store.Complete(t.Context(), begin.State, u.Query().Get("code")); err != nil {
		t.Fatal(err)
	}
	o.dest, err = gdrive.New(gdrive.Options{
		DestID: "dst-a", HTTPClient: &http.Client{}, Tokens: o.store.TokenSource(t.Context(), "dst-a"), BaseURL: driveSrv.URL,
		PartSize: 256 << 10, Clock: h.clk.Now, Folders: h.cat, HubID: "H",
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// TestOutbox_TokenEndpointOutageRecoversWithoutAction (P26, P27, MI5): the
// Drive destination's access token expires while the token endpoint is
// unreachable for 6 h, as under MI5's WAN block. Each refresh is
// ErrTransient, so the uploads back off and the destination turns
// UNREACHABLE, never BLOCKED_AUTH; once the endpoint answers again, the
// refresh succeeds, every placement is verified and the destination is OK,
// with nobody calling Unblock.
func TestOutbox_TokenEndpointOutageRecoversWithoutAction(t *testing.T) {
	h := newHarness(t)
	outageEnd := h.start.Add(6 * time.Hour)
	var outage atomic.Bool // from the login's end
	o := newOAuthDrive(t, h, func() bool { return outage.Load() && h.clk.Now().Before(outageEnd) })
	outage.Store(true)
	h.clk.Advance(61 * time.Minute) // the login's access token has expired
	var ps []catalog.Placement
	for i := range 3 {
		ps = append(ps, h.enqueue(h.segment(fmt.Sprintf("s%d", i), 70*time.Minute+time.Duration(i)*time.Minute,
			randomBytes(300<<10+i, byte(i))), "dst-a", catalog.ClassContinuous))
	}
	q := h.newQueue(map[string]storage.Destination{"dst-a": o.dest}, func(opts *Options) {
		opts.Rand = func() float64 { return 0.5 }
		opts.Workers = 1
	})
	h.run(q, time.Minute, 8*time.Hour, h.verified(ps...))

	states := h.destStates("dst-a")
	if slices.Contains(states, StateBlockedAuth) || !slices.Contains(states, StateUnreachable) || q.State("dst-a") != StateOK {
		t.Errorf("DestState %v, now %s; want UNREACHABLE, then OK, and never BLOCKED_AUTH", states, q.State("dst-a"))
	}
	for _, e := range h.obs.of("Failed") {
		if e.Reason != "transient" && e.Reason != "rate_limited" {
			t.Errorf("an upload failed with %s, want transient during the outage", e.Reason)
		}
	}
	if verified := h.obs.of("Verified"); len(verified) != len(ps) || verified[0].At.Before(outageEnd) {
		t.Errorf("%d placements verified, the first at %v; want all %d, after the outage's end", len(verified), verified, len(ps))
	}
	if n := o.login.Counts()["refresh_token"]; n == 0 {
		t.Error("no refresh reached the token endpoint after the outage")
	}
}

// TestARH08_NoTokenBytesInUploadedObjects (P45, ARH-08): after uploads
// through fakegdrive with the store's tokens, refreshed once on the way, no
// object that the provider stores holds an access or a refresh token.
func TestARH08_NoTokenBytesInUploadedObjects(t *testing.T) {
	h := newHarness(t)
	o := newOAuthDrive(t, h, func() bool { return false })
	var ps []catalog.Placement
	for i := range 2 {
		ps = append(ps, h.enqueue(h.segment(fmt.Sprintf("s%d", i), time.Duration(i)*time.Minute, randomBytes(300<<10+i, byte(i))),
			"dst-a", catalog.ClassContinuous))
	}
	q := h.newQueue(map[string]storage.Destination{"dst-a": o.dest}, func(opts *Options) { opts.Rand = func() float64 { return 0.5 } })
	h.run(q, time.Minute, time.Hour, h.verified(ps...))
	if _, err := o.store.Refresh(t.Context(), "dst-a"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Second) // a create slot of the driver's 2 a second
	if _, err := o.dest.PutSmall(t.Context(), "README.txt", []byte("after the refresh")); err != nil {
		t.Fatal(err)
	}
	tokens := append(o.login.RefreshTokens(), o.login.IssuedAccessTokens()...)
	if len(tokens) < 3 {
		t.Fatalf("the fake issued %d tokens, want a login's two and a refresh's", len(tokens))
	}
	objects := o.drive.Objects()
	if len(objects) < 3 {
		t.Fatalf("the fake holds %d objects", len(objects))
	}
	for _, obj := range objects {
		for _, tok := range tokens {
			if bytes.Contains(obj.Data, []byte(tok)) || bytes.Contains([]byte(obj.Name), []byte(tok)) {
				t.Errorf("the object %s holds a token", obj.Name)
			}
		}
	}
}
