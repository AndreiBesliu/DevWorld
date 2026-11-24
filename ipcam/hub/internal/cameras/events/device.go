package events

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kerberos-io/onvif/device"
	"github.com/kerberos-io/onvif/event"
)

// The device service's actions.
const (
	actionGetSystemDateAndTime = nsTDS + "/GetSystemDateAndTime"
	actionGetDeviceInformation = nsTDS + "/GetDeviceInformation"
	actionGetCapabilities      = nsTDS + "/GetCapabilities"
	actionGetEventCapabilities = nsTEV + "/EventPortType/GetServiceCapabilitiesRequest"
)

// ProbeResult is what Probe learned of a camera: whether it has an events
// service and PullPoint support, its clock's offset from the hub's, the
// login method that worked (AuthWSSEDigest, AuthHTTPDigest, AuthBasic or
// AuthNone), and its brand and model. Never its serial number.
type ProbeResult struct {
	EventsService, PullPoint bool
	ClockOffsetMs            int64
	Auth                     string
	Brand, Model             string
}

// services are the XAddrs of a camera's services, rebased on the device
// XAddr: "" for a service the camera does not have.
type services struct {
	events, media string
	pullPoint     bool
}

// withTimeout is ctx bounded by the client's call timeout.
func (c *client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.opts.callTimeout())
}

// syncClock reads the camera's clock with GetSystemDateAndTime, which ONVIF
// answers without a login (with one when the camera asks), and keeps its
// offset from the hub's clock for the WS-UsernameToken's Created (§8.2).
func (c *client) syncClock(ctx context.Context) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp device.GetSystemDateAndTimeResponse
	body := `<tds:GetSystemDateAndTime/>`
	res, err := c.anon(ctx, c.cam.XAddr, actionGetSystemDateAndTime, body, &resp)
	if err != nil {
		return err
	}
	if res.refused() {
		c.mu.Lock()
		c.noteChallenges(res.header)
		c.mu.Unlock()
		if err := c.call(ctx, c.cam.XAddr, actionGetSystemDateAndTime, "", body, &resp); err != nil {
			return err
		}
	} else if res.err != nil {
		return res.err
	}
	at := c.now()
	u := resp.SystemDateAndTime.UTCDateTime
	if u.Date.Year == 0 {
		// A camera that gives no UTC time: its WS-UsernameToken is
		// checked against a clock taken as the hub's.
		return nil
	}
	cam := time.Date(int(u.Date.Year), time.Month(u.Date.Month), int(u.Date.Day),
		int(u.Time.Hour), int(u.Time.Minute), int(u.Time.Second), 0, time.UTC)
	c.mu.Lock()
	c.offset = cam.Sub(at).Round(time.Second)
	c.mu.Unlock()
	return nil
}

// deviceInformation is GetDeviceInformation's manufacturer and model. The
// response's serial number is decoded with them and dropped here.
func (c *client) deviceInformation(ctx context.Context) (brand, model string, err error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp device.GetDeviceInformationResponse
	if err := c.call(ctx, c.cam.XAddr, actionGetDeviceInformation, "", `<tds:GetDeviceInformation/>`, &resp); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(resp.Manufacturer), strings.TrimSpace(resp.Model), nil
}

// services reads the XAddrs of the camera's events and media services
// with GetCapabilities, and whether its events service has PullPoint
// support: the capability's flag, or, when it is false, the events
// service's own GetServiceCapabilities (some cameras report only there).
func (c *client) services(ctx context.Context) (services, error) {
	cctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp device.GetCapabilitiesResponse
	body := `<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>`
	if err := c.call(cctx, c.cam.XAddr, actionGetCapabilities, "", body, &resp); err != nil {
		return services{}, err
	}
	caps := resp.Capabilities
	s := services{
		events:    rebase(string(caps.Events.XAddr), c.cam.XAddr),
		media:     rebase(string(caps.Media.XAddr), c.cam.XAddr),
		pullPoint: bool(caps.Events.WSPullPointSupport),
	}
	if s.events != "" && !s.pullPoint {
		ectx, cancel := c.withTimeout(ctx)
		defer cancel()
		var ev event.GetServiceCapabilitiesResponse
		err := c.call(ectx, s.events, actionGetEventCapabilities, "", `<tev:GetServiceCapabilities/>`, &ev)
		if err != nil && Reason(err) == ReasonAuth {
			return services{}, err
		}
		s.pullPoint = err == nil && bool(ev.Capabilities.WSPullPointSupport)
	}
	return s, nil
}

// Probe reads what the MI8 spike needs of a camera before it subscribes
// (P42): its clock's offset, the login method that works, its brand and
// model (GetDeviceInformation's Manufacturer and Model only, never the
// serial), and whether it has an events service with PullPoint support. Its
// error's Reason is ReasonOnvifPort when nothing answers at the camera's
// port.
func Probe(ctx context.Context, cam CameraRef, opts Options) (ProbeResult, error) {
	c := newClient(cam, opts)
	if err := c.syncClock(ctx); err != nil {
		return ProbeResult{}, err
	}
	brand, model, err := c.deviceInformation(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	s, err := c.services(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{
		EventsService: s.events != "",
		PullPoint:     s.events != "" && s.pullPoint,
		ClockOffsetMs: c.clockOffset().Milliseconds(),
		Auth:          c.authMethod(),
		Brand:         brand,
		Model:         model,
	}, nil
}

// errNoMedia is a camera without a media service.
var errNoMedia = errors.New("onvif: no media service")
