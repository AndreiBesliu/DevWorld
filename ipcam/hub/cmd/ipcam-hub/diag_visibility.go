package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// visibilitySchema names the output of diag visibility.
const visibilitySchema = "ipcam.diag.visibility/1"

// visibilityProbe is the probe file's 16 fixed bytes.
const visibilityProbe = "ipcam-visibility"

// The Drive API's base, and the bound of each of diag visibility's calls.
const (
	driveAPI       = "https://www.googleapis.com"
	visibilityCall = 30 * time.Second
	maxListAnswer  = 4 << 20
)

// visibilityLine is a cell of the cross-client visibility matrix (P21): the
// creator's and the reader's client types, whether they are one client,
// and whether the reader lists and reads the creator's probe file; or,
// for two accounts, Error "accounts-differ".
type visibilityLine struct {
	Schema   string       `json:"schema"`
	Provider storage.Kind `json:"provider"`
	Creator  string       `json:"creator"`
	Reader   string       `json:"reader"`
	Self     *bool        `json:"self,omitempty"`
	Listed   *bool        `json:"listed,omitempty"`
	Read     *bool        `json:"read,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// diagVisibilityCommand is "ipcam-hub diag visibility --config <run.json>
// --creator <destId> --reader <destId>" (P21): a cell of MI6's matrix, with
// the hub's own tokens, so no token leaves the hub. Both destinations'
// logins must be of one account: it compares about.user.permissionId of
// their tokens in RAM and, on a mismatch, prints the accounts-differ error
// and exits 5, never the ID. The creator ensures vis-<creatorType>.txt
// (16 fixed bytes) in its own root folder through PutSmall, which keeps the
// file's ID in the catalog; the reader makes one files.list of that folder
// (the reconciliation path) and, when it lists the file, one ranged read of
// its 16 bytes.
//
// It opens the catalog with catalog.OpenExisting, beside a running hub: it
// migrates nothing and takes no hub lock, and it writes only what the
// cell needs, in short transactions, the refresh tokens that a refresh
// rotated (SwapSecret: a rotating provider revokes the old one, so a
// read-only diag would leave the hub a revoked token) and the probe's
// remote IDs. It creates no file under state/ but SQLite's own -wal and
// -shm, and makes no sealer key, since the logins' rows exist.
func diagVisibilityCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag visibility", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "the run configuration (run.json)")
	creatorID := flags.String("creator", "", "the destination whose login creates the probe file")
	readerID := flags.String("reader", "", "the destination whose login lists and reads it")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *config == "" || *creatorID == "" || *readerID == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	failed := func(err error) int {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag visibility:", err)
		return exitFatal
	}
	cfg, hubID, err := uploadsOf(*config)
	if err != nil {
		return failed(err)
	}
	creator, ok := cfg.Uploads.destByID(*creatorID)
	reader, ok2 := cfg.Uploads.destByID(*readerID)
	switch {
	case !ok || !ok2:
		return failed(errors.New("the creator and the reader must be destinations of the uploads section"))
	case creator.Kind != storage.KindGoogleDrive || reader.Kind != storage.KindGoogleDrive:
		// OneDrive's approot across the Entra client's platforms is Faza 0-bis
		// unless A5 passed (C2-R4), and fakegraph lists no folder.
		return failed(fmt.Errorf("visibility is measured on %s only", storage.KindGoogleDrive))
	}
	ctx := context.Background()
	cat, err := catalog.OpenExisting(ctx, catalogPath(cfg.DataDir))
	if err != nil {
		return failed(err)
	}
	defer func() { _ = cat.Close() }()
	client, _ := newHubClient()
	store, err := newOAuthStore(cfg, cat, client)
	if err != nil {
		return failed(err)
	}
	cell, err := visibilityCell(ctx, store, cat, client, creator, reader, hubID)
	if err != nil {
		return failed(err)
	}
	if code := printJSONLine(stdout, stderr, cell); code != exitOK || cell.Error == "" {
		return code
	}
	return exitAccountsDiffer
}

// visibilityCell measures creator → reader on Drive.
func visibilityCell(ctx context.Context, store *oauth.Store, cat *catalog.DB, client *http.Client, creator, reader destDef,
	hubID string) (visibilityLine, error) {
	cl, err := loginOf(ctx, store, creator.TokenRef)
	if err != nil {
		return visibilityLine{}, err
	}
	rl, err := loginOf(ctx, store, reader.TokenRef)
	if err != nil {
		return visibilityLine{}, err
	}
	line := visibilityLine{Schema: visibilitySchema, Provider: storage.KindGoogleDrive, Creator: string(cl.ClientType), Reader: string(rl.ClientType)}
	creatorTokens, readerTokens := store.TokenSource(ctx, creator.TokenRef), store.TokenSource(ctx, reader.TokenRef)
	creatorAPI, readerAPI := drive{client: client, base: apiBase(creator), tokens: creatorTokens}, drive{client: client, base: apiBase(reader), tokens: readerTokens}
	a, err := creatorAPI.permissionID(ctx)
	if err != nil {
		return visibilityLine{}, fmt.Errorf("the creator's account: %w", err)
	}
	b, err := readerAPI.permissionID(ctx)
	if err != nil {
		return visibilityLine{}, fmt.Errorf("the reader's account: %w", err)
	}
	if a != b {
		line.Error = "accounts-differ"
		return line, nil
	}
	d, err := gdrive.New(gdrive.Options{
		DestID: creator.ID, HTTPClient: client, Tokens: creatorTokens, BaseURL: creator.BaseURL, UploadBaseURL: creator.UploadBaseURL,
		Folders: cat, HubID: hubID, RootName: creator.Folder,
	})
	if err != nil {
		return visibilityLine{}, err
	}
	probe, err := d.PutSmall(ctx, storage.ObjectKey("vis-"+string(cl.ClientType)+".txt"), []byte(visibilityProbe))
	if err != nil {
		return visibilityLine{}, fmt.Errorf("the creator's probe file: %w", err)
	}
	root, ok, err := cat.Folder(ctx, creator.ID, driveRoot(creator))
	if err != nil || !ok {
		return visibilityLine{}, fmt.Errorf("the creator's root folder is not in the catalog (%w)", err)
	}
	ids, err := readerAPI.children(ctx, root)
	if err != nil {
		return visibilityLine{}, fmt.Errorf("the reader's list: %w", err)
	}
	self := cl.ClientType == rl.ClientType
	listed := slices.Contains(ids, probe.ID)
	read := false
	if listed {
		if read, err = readerAPI.readsProbe(ctx, probe.ID); err != nil {
			return visibilityLine{}, fmt.Errorf("the reader's read: %w", err)
		}
	}
	line.Self, line.Listed, line.Read = &self, &listed, &read
	return line, nil
}

// loginOf is the login of ref, which must exist.
func loginOf(ctx context.Context, store *oauth.Store, ref string) (oauth.Completion, error) {
	l, ok, err := store.Login(ctx, ref)
	switch {
	case err != nil:
		return oauth.Completion{}, err
	case !ok:
		return oauth.Completion{}, fmt.Errorf("no login for tokenRef %s", ref)
	}
	return l, nil
}

// apiBase is d's Drive API base: its fake's, or the live one.
func apiBase(d destDef) string {
	if d.BaseURL != "" {
		return strings.TrimSuffix(d.BaseURL, "/")
	}
	return driveAPI
}

// drive makes diag visibility's own Drive calls with one login's tokens;
// none of its errors quotes a URL, a token or an answer.
type drive struct {
	client *http.Client
	base   string
	tokens oauth2.TokenSource
}

// do sends a GET of path with q and header, and returns the answer's
// status and body, at most limit bytes.
func (d drive) do(ctx context.Context, path string, q url.Values, header http.Header, limit int64) (int, []byte, error) {
	tok, err := d.tokens.Token()
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, visibilityCall)
	defer cancel()
	u := d.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, errors.New("a Drive request that is not a URL")
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("the Drive API did not answer: %w", storage.ErrTransient)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return 0, nil, fmt.Errorf("the Drive API's answer was cut: %w", storage.ErrTransient)
	}
	return resp.StatusCode, body, nil
}

// permissionID is about.user.permissionId of the login's account, kept in
// RAM only (P45).
func (d drive) permissionID(ctx context.Context) (string, error) {
	status, body, err := d.do(ctx, "/drive/v3/about", url.Values{"fields": {"user(permissionId)"}}, nil, maxListAnswer)
	if err != nil {
		return "", err
	}
	var about struct {
		User struct {
			PermissionID string `json:"permissionId"`
		} `json:"user"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &about) != nil || about.User.PermissionID == "" {
		return "", fmt.Errorf("about.get answered HTTP %d without a permission ID", status)
	}
	return about.User.PermissionID, nil
}

// children is one files.list of the folder's children that the login sees:
// their IDs.
func (d drive) children(ctx context.Context, folder string) ([]string, error) {
	q := url.Values{"q": {"'" + folder + "' in parents and trashed = false"}, "fields": {"files(id)"}, "pageSize": {"1000"}}
	status, body, err := d.do(ctx, "/drive/v3/files", q, nil, maxListAnswer)
	if err != nil {
		return nil, err
	}
	var list struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	switch {
	case status == http.StatusNotFound:
		return nil, nil
	case status != http.StatusOK || json.Unmarshal(body, &list) != nil:
		return nil, fmt.Errorf("files.list answered HTTP %d", status)
	}
	ids := make([]string, 0, len(list.Files))
	for _, f := range list.Files {
		ids = append(ids, f.ID)
	}
	return ids, nil
}

// readsProbe reports whether a ranged files.get?alt=media of the file id,
// bytes 0-15, gives the probe's 16 bytes.
func (d drive) readsProbe(ctx context.Context, id string) (bool, error) {
	status, body, err := d.do(ctx, "/drive/v3/files/"+url.PathEscape(id), url.Values{"alt": {"media"}},
		http.Header{"Range": {"bytes=0-15"}}, int64(len(visibilityProbe)+1))
	if err != nil {
		return false, err
	}
	return (status == http.StatusPartialContent || status == http.StatusOK) && bytes.Equal(body, []byte(visibilityProbe)), nil
}
