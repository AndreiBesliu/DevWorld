package onedrive

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/rest"
)

const (
	// metadataTimeout is the deadline of each metadata call, and
	// transferTimeout that of each fragment PUT and content read (P26):
	// with ipcam-hub's dial, TLS and response-header timeouts, a route that
	// drops ends a call as ErrTransient instead of holding a worker.
	metadataTimeout = 30 * time.Second
	transferTimeout = 10 * time.Minute
)

// The API methods, by the names that the errors give them: Graph's, and
// three of an upload session's uploadUrl.
const (
	methodCreateSession = "createUploadSession"
	methodUploadPut     = "upload.put"    // a fragment PUT to the uploadUrl
	methodUploadStatus  = "upload.status" // a GET of the uploadUrl: the next expected range
	methodUploadCancel  = "upload.cancel" // a DELETE of the uploadUrl
	methodGetByPath     = "items.getByPath"
	methodGet           = "items.get"
	methodContent       = "items.content" // /content, which redirects to a pre-authenticated download URL
	methodDelete        = "items.delete"
	methodDrive         = "drive.get"
)

// call is one request of the API.
type call struct {
	method string // the API method: the errors' name
	verb   string // the HTTP method
	url    string
	header map[string]string
	body   io.Reader
	length int64 // the body's length
	// upload marks a request to an upload session's uploadUrl. That URL is
	// pre-authenticated, so the request gets no token and carries no
	// Authorization (STO-08: Graph may answer 401 to one that does), and a
	// 404 there means that the session is gone.
	upload  bool
	timeout time.Duration      // metadataTimeout when 0
	calls   storage.CallCounts // the counts of the upload that the call serves, or nil
}

// answer is a response that send accepted. Close releases it, and Decode
// reads its JSON.
type answer = rest.Answer

// send makes c's request on the plain client, with the bearer token that
// Tokens gives now unless c is an uploadUrl's (P27), and returns the answer
// when its status is one of want. Any other status is an error as
// statusError maps it, and a request that gets no answer is ErrTransient;
// neither text quotes the request (g2-03). A request that an upload serves
// is counted in its calls.
func (o *OneDrive) send(ctx context.Context, c call, want ...int) (*answer, error) {
	var token string
	if !c.upload {
		tok, err := o.tokens.Token()
		if err != nil {
			return nil, rest.TokenError(driverName, c.method, err)
		}
		token = tok.AccessToken
	}
	timeout := c.timeout
	if timeout == 0 {
		timeout = metadataTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, c.verb, c.url, c.body)
	if err != nil {
		cancel()
		return nil, rest.RequestError(driverName, c.method)
	}
	if c.body != nil {
		req.ContentLength = c.length
	}
	if !c.upload {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	if c.calls != nil {
		c.calls[c.method]++
	}
	resp, err := o.client.Do(req)
	if err != nil {
		cancel()
		return nil, rest.NetworkError(driverName, c.method, err)
	}
	a := rest.NewAnswer(driverName, resp, cancel)
	if !slices.Contains(want, resp.StatusCode) {
		err := statusError(&c, resp, o.clock())
		a.Close()
		return nil, err
	}
	return a, nil
}

// driveItem is an item's resource, with the properties that the driver
// asks for.
type driveItem struct {
	ID   string `json:"id"`
	Size int64  `json:"size"`
	File *struct {
		Hashes struct {
			QuickXor string `json:"quickXorHash"`
		} `json:"hashes"`
	} `json:"file"`
	Modified    time.Time `json:"lastModifiedDateTime"`
	DownloadURL string    `json:"@microsoft.graph.downloadUrl"`
}

// hash is the item's quickXorHash, "" when Graph gave none.
func (it *driveItem) hash() string {
	if it.File == nil {
		return ""
	}
	return it.File.Hashes.QuickXor
}

// ref is it as the ObjectRef of key.
func (it *driveItem) ref(key storage.ObjectKey) storage.ObjectRef {
	ref := storage.ObjectRef{ID: it.ID, Key: key, Size: it.Size, Hash: it.hash()}
	if ref.Hash != "" {
		ref.HashKind = storage.HashQuickXor
	}
	return ref
}

// sessionAnswer is an upload session's resource: createUploadSession's
// answer, a fragment's 202 and a status query's 200.
type sessionAnswer struct {
	UploadURL          string   `json:"uploadUrl"`
	NextExpectedRanges []string `json:"nextExpectedRanges"`
}

// next is the offset from which the session expects the upload's next
// bytes: the start of the first of nextExpectedRanges ("a-" or "a-b"), as
// the fragments go in order.
func (s *sessionAnswer) next(size int64) (int64, bool) {
	if len(s.NextExpectedRanges) == 0 {
		return 0, false
	}
	start, _, _ := strings.Cut(s.NextExpectedRanges[0], "-")
	n, err := strconv.ParseInt(start, 10, 64)
	return n, err == nil && n >= 0 && n < size
}

// pathURL is the URL of key's item, by its path under the app folder, each
// name escaped.
func (o *OneDrive) pathURL(key storage.ObjectKey) string {
	names := strings.Split(string(key), "/")
	for i, name := range names {
		names[i] = url.PathEscape(name)
	}
	return o.base + "/v1.0/me/drive/special/approot:/" + strings.Join(names, "/")
}

// itemURL is the URL of the item id.
func (o *OneDrive) itemURL(id string) string {
	return o.base + "/v1.0/me/drive/items/" + url.PathEscape(id)
}

// preauthURL reports whether u is a pre-authenticated URL that the driver
// may use, as an uploadUrl or a download URL: an absolute https URL with no
// user info, so that a session in the catalog that is not one is never sent
// to. An http one is taken only with Options.BaseURL set, for the fakes,
// which serve plain HTTP; the live API's are https (Task 6 review minor 6).
func (o *OneDrive) preauthURL(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http" && o.plainHTTP) && parsed.Host != "" && parsed.User == nil
}
