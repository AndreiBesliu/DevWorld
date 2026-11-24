package events

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// The media service's actions. The ONVIF module's media package is not in
// the allowlist (P06), so these requests and responses are this package's.
const (
	actionGetProfiles    = nsTRT + "/GetProfiles"
	actionGetStreamURI   = nsTRT + "/GetStreamUri"
	actionGetSnapshotURI = nsTRT + "/GetSnapshotUri"
)

// The profiles that StreamURI and SnapshotURI name by role.
const (
	ProfileMain = "main" // the profile of the largest resolution
	ProfileSub  = "sub"  // the profile of the smallest resolution
)

// mediaProfile is a GetProfiles profile: its token and its video encoder's
// resolution.
type mediaProfile struct {
	Token   string `xml:"token,attr"`
	Encoder struct {
		Resolution struct {
			Width  int `xml:"Width"`
			Height int `xml:"Height"`
		} `xml:"Resolution"`
	} `xml:"VideoEncoderConfiguration"`
}

// pixels is the profile's resolution's area.
func (p mediaProfile) pixels() int { return p.Encoder.Resolution.Width * p.Encoder.Resolution.Height }

// getProfilesResponse is GetProfiles' response.
type getProfilesResponse struct {
	Profiles []mediaProfile `xml:"Profiles"`
}

// mediaURIResponse is GetStreamUri's and GetSnapshotUri's response.
type mediaURIResponse struct {
	MediaURI struct {
		URI string `xml:"Uri"`
	} `xml:"MediaUri"`
}

// mediaCall is a camera's media service, reached with a synced clock.
func mediaCall(ctx context.Context, cam CameraRef, opts Options) (*client, string, error) {
	c := newClient(cam, opts)
	if err := c.syncClock(ctx); err != nil {
		return nil, "", err
	}
	s, err := c.services(ctx)
	if err != nil {
		return nil, "", err
	}
	if s.media == "" {
		return nil, "", errNoMedia
	}
	return c, s.media, nil
}

// profileToken picks profile among the camera's profiles: ProfileMain or
// ProfileSub by resolution, or a profile's own token.
func (c *client) profileToken(ctx context.Context, media, profile string) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp getProfilesResponse
	if err := c.call(ctx, media, actionGetProfiles, "", `<trt:GetProfiles/>`, &resp); err != nil {
		return "", err
	}
	var video []mediaProfile
	for _, p := range resp.Profiles {
		if p.Token == profile {
			return p.Token, nil
		}
		if p.pixels() > 0 {
			video = append(video, p)
		}
	}
	if len(video) == 0 {
		video = resp.Profiles
	}
	if len(video) == 0 {
		return "", fmt.Errorf("%w: no media profile", errFault)
	}
	// A stable sort keeps the camera's order between profiles of one
	// resolution: its first is its main stream.
	slices.SortStableFunc(video, func(a, b mediaProfile) int { return b.pixels() - a.pixels() })
	switch profile {
	case ProfileMain:
		return video[0].Token, nil
	case ProfileSub:
		return video[len(video)-1].Token, nil
	}
	return "", fmt.Errorf("%w: no profile %q", errFault, profile)
}

// StreamURI is the RTSP URI of the camera's profile (ProfileMain,
// ProfileSub or a profile token), as the camera returns it, without the
// credentials. The caller keeps it in RAM and never logs it.
func StreamURI(ctx context.Context, cam CameraRef, profile string, opts Options) (string, error) {
	stream, _, err := mediaURIs(ctx, cam, profile, opts, true, false)
	return stream, err
}

// SnapshotURI is the JPEG snapshot URI of the camera's profile, as
// StreamURI's.
func SnapshotURI(ctx context.Context, cam CameraRef, profile string, opts Options) (string, error) {
	_, snapshot, err := mediaURIs(ctx, cam, profile, opts, false, true)
	return snapshot, err
}

// MediaURIs are StreamURI and SnapshotURI read with one login: the camera
// sees one login method tried, not two. Each URI has its own error, and err
// is the error of the steps they share.
func MediaURIs(ctx context.Context, cam CameraRef, profile string, opts Options) (stream, snapshot string, streamErr, snapshotErr error) {
	c, media, err := mediaCall(ctx, cam, opts)
	if err != nil {
		return "", "", err, err
	}
	token, err := c.profileToken(ctx, media, profile)
	if err != nil {
		return "", "", err, err
	}
	stream, streamErr = c.streamURI(ctx, media, token)
	snapshot, snapshotErr = c.snapshotURI(ctx, media, token)
	return stream, snapshot, streamErr, snapshotErr
}

// mediaURIs reads the stream URI, the snapshot URI or both.
func mediaURIs(ctx context.Context, cam CameraRef, profile string, opts Options, wantStream, wantSnapshot bool) (stream, snapshot string, err error) {
	c, media, err := mediaCall(ctx, cam, opts)
	if err != nil {
		return "", "", err
	}
	token, err := c.profileToken(ctx, media, profile)
	if err != nil {
		return "", "", err
	}
	if wantStream {
		if stream, err = c.streamURI(ctx, media, token); err != nil {
			return "", "", err
		}
	}
	if wantSnapshot {
		if snapshot, err = c.snapshotURI(ctx, media, token); err != nil {
			return "", "", err
		}
	}
	return stream, snapshot, nil
}

// streamURI is GetStreamUri's RTSP URI of the profile token.
func (c *client) streamURI(ctx context.Context, media, token string) (string, error) {
	body := `<trt:GetStreamUri><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream>` +
		`<tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup>` +
		`<trt:ProfileToken>` + escapeText(token) + `</trt:ProfileToken></trt:GetStreamUri>`
	return c.mediaURI(ctx, media, actionGetStreamURI, body)
}

// snapshotURI is GetSnapshotUri's URI of the profile token.
func (c *client) snapshotURI(ctx context.Context, media, token string) (string, error) {
	body := `<trt:GetSnapshotUri><trt:ProfileToken>` + escapeText(token) + `</trt:ProfileToken></trt:GetSnapshotUri>`
	return c.mediaURI(ctx, media, actionGetSnapshotURI, body)
}

// mediaURI sends a GetStreamUri or GetSnapshotUri request and returns its
// URI.
func (c *client) mediaURI(ctx context.Context, media, action, body string) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp mediaURIResponse
	if err := c.call(ctx, media, action, "", body, &resp); err != nil {
		return "", err
	}
	uri := strings.TrimSpace(resp.MediaURI.URI)
	if uri == "" {
		return "", fmt.Errorf("%w: no URI", errFault)
	}
	return uri, nil
}
