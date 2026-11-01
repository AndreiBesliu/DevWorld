package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// The OAuth clients of ipcam-hub's tests, their IDs and secrets assembled
// at run time, in no provider's shape (P46).
var (
	clientAndroid, clientDesktop, clientTV, clientWeb = "android-client", "desktop-client", "tv-client", "web-client"
	secretDesktop                                     = "SENT" + "INEL-desktop-secret"
	secretTV                                          = "SENT" + "INEL-tv-secret"
)

// testClients are the hub's clients: the Web client's secret never reaches
// the hub.
func testClients() oauth.Clients {
	return oauth.Clients{Google: map[oauth.ClientType]oauth.Client{
		oauth.ClientAndroid: {ID: clientAndroid}, oauth.ClientDesktop: {ID: clientDesktop, Secret: secretDesktop},
		oauth.ClientTV: {ID: clientTV, Secret: secretTV}, oauth.ClientWeb: {ID: clientWeb},
	}}
}

// oauthFixture is a hub's data directory with a catalog, state/hub_id and
// sealed clients, and fakeoauth and fakegdrive servers, which fakegdrive's
// drive.file scoping ties to fakeoauth's tokens; run.json names them, with
// one Drive destination per MI6 pair, its tokenRef its own id.
type oauthFixture struct {
	dataDir, config string
	cat             *catalog.DB
	store           *oauth.Store
	login           *fakeoauth.Fake
	drive           *fakegdrive.Fake
	loginURL        string
	driveURL        string
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	f := &oauthFixture{login: fakeoauth.New(fakeoauth.Options{}), drive: fakegdrive.New(fakegdrive.Options{})}
	for _, c := range []fakeoauth.Client{
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeAndroid, ID: clientAndroid, Project: "p1", RedirectURIs: []string{"ro.ipcam.spike:/oauth2redirect"}},
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeDesktop, ID: clientDesktop, Secret: secretDesktop, Project: "p1"},
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeTV, ID: clientTV, Secret: secretTV, Project: "p1"},
	} {
		f.login.AddClient(c)
	}
	f.drive.IdentifyTokens(func(token string) (fakegdrive.Identity, bool) {
		tok, ok := f.login.Lookup(token)
		return fakegdrive.Identity{Client: tok.ClientID, Project: tok.Project, Account: tok.Account}, ok && !tok.Expired
	})
	loginSrv := httptest.NewServer(f.login.Handler())
	t.Cleanup(loginSrv.Close)
	driveSrv := httptest.NewServer(f.drive.Handler())
	t.Cleanup(driveSrv.Close)
	f.loginURL, f.driveURL = loginSrv.URL, driveSrv.URL
	f.dataDir = filepath.Join(t.TempDir(), "data")
	stateDir := filepath.Join(f.dataDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "hub_id"), []byte(testHubID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(t.Context(), filepath.Join(stateDir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	f.cat = cat
	sealer, err := crypto.NewSealer(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	endpoints := oauth.Endpoints(fakeoauth.URLsAt(loginSrv.URL))
	if f.store, err = oauth.New(oauth.Config{Endpoints: endpoints, Sealer: sealer, HTTP: http.DefaultClient, Secrets: cat}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetClients(t.Context(), testClients()); err != nil {
		t.Fatal(err)
	}
	var dests []any
	for _, id := range []string{"g-android", "g-windows", "g-web"} {
		dests = append(dests, map[string]any{"id": id, "kind": "GOOGLE_DRIVE", "folder": "", "tokenRef": id,
			"baseUrl": driveSrv.URL, "uploadBaseUrl": "", "partSize": 0})
	}
	f.config = filepath.Join(filepath.Dir(f.dataDir), "run.json")
	writeJSON(t, f.config, map[string]any{"dataDir": f.dataDir, "jobs": []any{},
		"uploads": map[string]any{"hubName": "hub", "destinations": dests, "routes": []any{}},
		"oauth":   map[string]any{"endpoints": endpoints, "testingExpiryDays": 7}})
	return f
}

// logIn runs the login of destID on platform through the fixture's store,
// the fake's consent given by account.
func (f *oauthFixture) logIn(t *testing.T, destID string, platform oauth.Platform, account string) oauth.Completion {
	t.Helper()
	ctx := t.Context()
	begin, err := f.store.Begin(ctx, oauth.BeginRequest{DestID: destID, Provider: storage.KindGoogleDrive, Platform: platform, LoopbackPort: 53123})
	if err != nil {
		t.Fatal(err)
	}
	if begin.Plan == oauth.PlanB1 {
		if err := f.login.ApproveDevice(begin.UserCode, account); err != nil {
			t.Fatal(err)
		}
		c, err := f.store.DeviceStatus(ctx, begin.State)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	redirect, err := f.login.AuthorizeAs(begin.AuthURL, account)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.Complete(ctx, begin.State, u.Query().Get("code"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
