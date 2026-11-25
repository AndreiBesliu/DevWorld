package events

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/kerberos-io/onvif/event"
)

// The PullPoint's actions (ONVIF Core §9.1, WS-BaseNotification).
const (
	actionCreatePullPoint = nsTEV + "/EventPortType/CreatePullPointSubscriptionRequest"
	actionPullMessages    = nsTEV + "/PullPointSubscription/PullMessagesRequest"
	actionRenew           = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest"
	actionUnsubscribe     = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest"
)

// eventMessage is a PullMessages notification.
type eventMessage = event.NotificationMessage

// pullMessageLimit is how many messages one PullMessages takes at most.
const pullMessageLimit = 64

// pullPoint is a PullPoint subscription: its address, and the reference
// parameters that every request to it carries in its header.
type pullPoint struct {
	addr      string
	refParams string
}

// createResponse is CreatePullPointSubscription's response, decoded by this
// package for the reference parameters' XML, which the ONVIF module's type
// drops.
type createResponse struct {
	Ref struct {
		Address   string `xml:"Address"`
		RefParams struct {
			Inner string `xml:",innerxml"`
		} `xml:"ReferenceParameters"`
	} `xml:"SubscriptionReference"`
}

// xsdDuration is d as an xsd:duration in seconds, "PT600S".
func xsdDuration(d time.Duration) string {
	return "PT" + strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "S"
}

// header is the WS-Addressing header of a request to the PullPoint: its
// action, its address and the reference parameters.
func (p *pullPoint) header(action string) string {
	return `<wsa:Action>` + action + `</wsa:Action><wsa:To>` + escapeText(p.addr) + `</wsa:To>` + p.refParams
}

// createPullPoint creates a PullPoint subscription on the events service
// with a term of term.
func (c *client) createPullPoint(ctx context.Context, events string, term time.Duration) (*pullPoint, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp createResponse
	body := `<tev:CreatePullPointSubscription><tev:InitialTerminationTime>` + xsdDuration(term) +
		`</tev:InitialTerminationTime></tev:CreatePullPointSubscription>`
	if err := c.call(ctx, events, actionCreatePullPoint, "", body, &resp); err != nil {
		return nil, err
	}
	addr := rebase(resp.Ref.Address, c.cam.XAddr)
	if addr == "" {
		return nil, ErrNoPullPoint
	}
	return &pullPoint{addr: addr, refParams: strings.TrimSpace(resp.Ref.RefParams.Inner)}, nil
}

// pull is PullMessages with the timeout wait: the messages that arrived,
// none when the camera's timeout passed without one. The request's own
// deadline is wait plus the client's call timeout.
func (c *client) pull(ctx context.Context, p *pullPoint, wait time.Duration) ([]eventMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, wait+c.opts.callTimeout())
	defer cancel()
	var resp event.PullMessagesResponse
	body := `<tev:PullMessages><tev:Timeout>` + xsdDuration(wait) + `</tev:Timeout><tev:MessageLimit>` +
		strconv.Itoa(pullMessageLimit) + `</tev:MessageLimit></tev:PullMessages>`
	if err := c.call(ctx, p.addr, actionPullMessages, p.header(actionPullMessages), body, &resp); err != nil {
		return nil, err
	}
	return resp.NotificationMessage, nil
}

// renew extends the subscription's term to term from now.
func (c *client) renew(ctx context.Context, p *pullPoint, term time.Duration) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resp event.RenewResponse
	body := `<wsnt:Renew><wsnt:TerminationTime>` + xsdDuration(term) + `</wsnt:TerminationTime></wsnt:Renew>`
	return c.call(ctx, p.addr, actionRenew, p.header(actionRenew), body, &resp)
}

// unsubscribe ends the subscription.
func (c *client) unsubscribe(ctx context.Context, p *pullPoint) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.call(ctx, p.addr, actionUnsubscribe, p.header(actionUnsubscribe), `<wsnt:Unsubscribe/>`, nil)
}

// motionItems are the data items whose true value is motion: IsMotion of
// the cell motion detector, State of a motion alarm, and their kin.
var motionItems = []string{"IsMotion", "State", "Motion", "MotionActive", "IsInside"}

// isMotion reports whether a notification says that motion is on: a data
// item of motionItems that is true, in a motion topic or named IsMotion.
func isMotion(topic string, m eventMessage) bool {
	motionTopic := strings.Contains(strings.ToLower(topic), "motion")
	for _, it := range m.Message.Message.Data.SimpleItem {
		name := string(it.Name)
		val := strings.ToLower(strings.TrimSpace(string(it.Value)))
		if val != "true" && val != "1" {
			continue
		}
		if name == "IsMotion" || (motionTopic && containsFold(motionItems, name)) {
			return true
		}
	}
	return false
}

// containsFold reports whether list holds s, ignoring case.
func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
