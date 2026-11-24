// Package onviftest is a simulated ONVIF camera for the events package's
// tests and ipcam-hub diag onvif's: an httptest server with a device, media
// and events service, PullPoint subscriptions that expire at their term
// (MaxPullPoints, 5 by default), and modes that renew, fall silent, expire
// every subscription, skew the camera's clock or take HTTP digest only. It
// records the login method of every request.
package onviftest

import (
	"bytes"
	"crypto/md5" //nolint:gosec // G501: HTTP digest's MD5, as cameras offer it
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: WS-UsernameToken's PasswordDigest is SHA-1
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"hash"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The login methods that Request.Auth records.
const (
	AuthNone       = "none"
	AuthWSSEDigest = "wsse-digest"
	AuthWSSEText   = "wsse-text"
	AuthHTTPDigest = "http-digest"
	AuthBasic      = "basic"
)

// DevicePath is the device service's path in XAddr.
const DevicePath = "/onvif/device_service"

// The services' paths.
const (
	eventsPath       = "/onvif/events_service"
	mediaPath        = "/onvif/media_service"
	subscriptionPath = "/onvif/subscription"
)

// createdSlack is how far a WS-UsernameToken's Created may be from the
// camera's clock.
const createdSlack = 5 * time.Second

// Config is a simulated camera.
type Config struct {
	// User and Pass are the camera's ONVIF account.
	User, Pass string
	// Brand, Model and Serial are GetDeviceInformation's Manufacturer,
	// Model and SerialNumber.
	Brand, Model, Serial string
	// MaxPullPoints is how many subscriptions the camera holds at once; 5
	// by default.
	MaxPullPoints int
	// ClockSkew is the camera's clock minus the real one.
	ClockSkew time.Duration
	// WSSEOnly takes the WS-UsernameToken only: no HTTP challenge, and a
	// refused login is a NotAuthorized fault.
	WSSEOnly bool
	// DigestOnly refuses the WS-UsernameToken and takes HTTP digest only.
	DigestOnly bool
	// Basic offers HTTP basic too.
	Basic bool
	// NoPullPoint is a camera whose events service has no PullPoint.
	NoPullPoint bool
	// StreamURIs are the RTSP URIs of the "main" and "sub" profiles, and
	// SnapshotURI the snapshot URI of both.
	StreamURIs  map[string]string
	SnapshotURI string
}

// Request is a request the camera received: its operation, the login method
// it carried, and whether the camera accepted it.
type Request struct {
	Op       string
	Auth     string
	Accepted bool
	raw      []byte
}

// Credentialed reports whether the request carried a login.
func (r Request) Credentialed() bool { return r.Auth != AuthNone }

// Contains reports whether s appears in clear in the request's header lines
// or body.
func (r Request) Contains(s string) bool { return bytes.Contains(r.raw, []byte(s)) }

// Counts are the subscription requests that the camera answered, and the
// subscriptions that lapsed or were dropped.
type Counts struct {
	Creates, Renews, Pulls, Unsubscribes, Expired int
}

// Sim is a simulated camera.
type Sim struct {
	cfg  Config
	srv  *httptest.Server
	done chan struct{}

	mu        sync.Mutex
	silent    bool
	subs      map[int]*pullSub
	next      int
	counts    Counts
	requests  []Request
	nonces    map[string]bool
	renewGaps []time.Duration
	closeOnce sync.Once
}

// pullSub is a subscription: its term's end on the camera's clock, the
// real time its term began, and its queued messages.
type pullSub struct {
	expires   time.Time
	termStart time.Time
	queue     []string
	wake      chan struct{}
}

// New starts a simulated camera.
func New(cfg Config) *Sim {
	if cfg.MaxPullPoints <= 0 {
		cfg.MaxPullPoints = 5
	}
	s := &Sim{cfg: cfg, done: make(chan struct{}), subs: map[int]*pullSub{}, nonces: map[string]bool{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// XAddr is the device service's address.
func (s *Sim) XAddr() string { return s.srv.URL + DevicePath }

// Close stops the camera, ending the requests it holds.
func (s *Sim) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.srv.Close()
	})
}

// SetSilent makes the camera stop answering any request, or answer again.
func (s *Sim) SetSilent(silent bool) {
	s.mu.Lock()
	s.silent = silent
	s.mu.Unlock()
}

// ExpireAll drops every subscription, as a camera's reboot does.
func (s *Sim) ExpireAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts.Expired += len(s.subs)
	clear(s.subs)
}

// Emit queues a notification of topic, with IsMotion set to motion, on
// every subscription.
func (s *Sim) Emit(topic string, motion bool) {
	msg := notification(topic, motion, s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		sub.queue = append(sub.queue, msg)
		select {
		case sub.wake <- struct{}{}:
		default:
		}
	}
}

// Subscriptions is how many subscriptions the camera holds.
func (s *Sim) Subscriptions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	return len(s.subs)
}

// Counts returns the subscription requests' counts.
func (s *Sim) Counts() Counts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts
}

// RenewOffsets are, for each Renew, the real time since its subscription's
// term began (its creation or its previous Renew).
func (s *Sim) RenewOffsets() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.renewGaps)
}

// Requests returns the requests the camera received.
func (s *Sim) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Refused is how many requests with a login the camera refused.
func (s *Sim) Refused() int {
	n := 0
	for _, r := range s.Requests() {
		if r.Credentialed() && !r.Accepted {
			n++
		}
	}
	return n
}

// now is the camera's clock.
func (s *Sim) now() time.Time { return time.Now().Add(s.cfg.ClockSkew).UTC() }

// sweep drops the subscriptions whose term ended. s.mu is held.
func (s *Sim) sweep() {
	now := s.now()
	for i, sub := range s.subs {
		if now.After(sub.expires) {
			delete(s.subs, i)
			s.counts.Expired++
		}
	}
}

// envelopeIn is a request's envelope: its WS-UsernameToken and its body's
// first element.
type envelopeIn struct {
	Token struct {
		Username string `xml:"Username"`
		Password struct {
			Type  string `xml:"Type,attr"`
			Value string `xml:",chardata"`
		} `xml:"Password"`
		Nonce   string `xml:"Nonce"`
		Created string `xml:"Created"`
	} `xml:"Header>Security>UsernameToken"`
	Body struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"Body"`
}

// opOf is the local name of the body's first element.
func opOf(body []byte) string {
	d := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

// serve answers a request.
func (s *Sim) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return
	}
	var raw bytes.Buffer
	for k, vs := range r.Header {
		for _, v := range vs {
			fmt.Fprintf(&raw, "%s: %s\n", k, v)
		}
	}
	raw.WriteString(r.URL.String() + "\n")
	raw.Write(body)
	var env envelopeIn
	if err := xml.Unmarshal(body, &env); err != nil {
		http.Error(w, "not SOAP", http.StatusBadRequest)
		return
	}
	op := opOf(env.Body.Inner)
	s.mu.Lock()
	silent := s.silent
	s.mu.Unlock()
	if silent {
		select {
		case <-r.Context().Done():
		case <-s.done:
		}
		return
	}
	method, ok := s.authenticate(r, &env)
	anonymous := op == "GetSystemDateAndTime" && method == AuthNone
	s.mu.Lock()
	s.requests = append(s.requests, Request{Op: op, Auth: method, Accepted: ok || anonymous, raw: raw.Bytes()})
	s.mu.Unlock()
	if !ok && !anonymous {
		s.refuse(w)
		return
	}
	s.dispatch(w, r, op, env.Body.Inner)
}

// refuse answers a request whose login failed: a 401 with the HTTP
// challenges, or a NotAuthorized fault on a WSSEOnly camera.
func (s *Sim) refuse(w http.ResponseWriter) {
	if s.cfg.WSSEOnly {
		fault(w, http.StatusBadRequest, "s:Sender", "ter:NotAuthorized")
		return
	}
	nonce := randomHex(16)
	s.mu.Lock()
	s.nonces[nonce] = true
	s.mu.Unlock()
	w.Header().Add("WWW-Authenticate", `Digest realm="onviftest", qop="auth", nonce="`+nonce+`", algorithm=MD5`)
	w.Header().Add("WWW-Authenticate", `Digest realm="onviftest", qop="auth", nonce="`+nonce+`", algorithm=SHA-256`)
	if s.cfg.Basic {
		w.Header().Add("WWW-Authenticate", `Basic realm="onviftest"`)
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// authenticate checks the request's login: the method it carried, and
// whether the camera accepts it.
func (s *Sim) authenticate(r *http.Request, env *envelopeIn) (string, bool) {
	authz := r.Header.Get("Authorization")
	switch {
	case strings.HasPrefix(authz, "Digest "):
		return AuthHTTPDigest, !s.cfg.WSSEOnly && s.checkDigest(r, strings.TrimPrefix(authz, "Digest "))
	case strings.HasPrefix(authz, "Basic "):
		user, pass, _ := r.BasicAuth()
		return AuthBasic, s.cfg.Basic && user == s.cfg.User && pass == s.cfg.Pass
	case env.Token.Username != "":
		if !strings.HasSuffix(env.Token.Password.Type, "#PasswordDigest") {
			return AuthWSSEText, false
		}
		return AuthWSSEDigest, !s.cfg.DigestOnly && s.checkWSSE(env)
	}
	return AuthNone, false
}

// checkWSSE checks a WS-UsernameToken PasswordDigest, and that its Created
// is within createdSlack of the camera's clock.
func (s *Sim) checkWSSE(env *envelopeIn) bool {
	t := env.Token
	if t.Username != s.cfg.User {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(t.Created))
	if err != nil || created.Sub(s.now()).Abs() > createdSlack {
		return false
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(t.Nonce))
	if err != nil {
		return false
	}
	h := sha1.New() //nolint:gosec // G401: see the import
	h.Write(nonce)
	h.Write([]byte(strings.TrimSpace(t.Created)))
	h.Write([]byte(s.cfg.Pass))
	return base64.StdEncoding.EncodeToString(h.Sum(nil)) == strings.TrimSpace(t.Password.Value)
}

// digestParam matches a k=v or k="v" pair of an Authorization header.
var digestParam = regexp.MustCompile(`(\w+)=(?:"((?:[^"\\]|\\.)*)"|([^,\s]*))`)

// checkDigest checks an HTTP digest Authorization (RFC 7616) against the
// camera's nonces.
func (s *Sim) checkDigest(r *http.Request, params string) bool {
	p := map[string]string{}
	for _, m := range digestParam.FindAllStringSubmatch(params, -1) {
		v := m[3]
		if m[2] != "" || strings.Contains(m[0], `"`) {
			v = strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(m[2])
		}
		p[strings.ToLower(m[1])] = v
	}
	s.mu.Lock()
	known := s.nonces[p["nonce"]]
	s.mu.Unlock()
	if !known || p["username"] != s.cfg.User || p["uri"] != r.URL.RequestURI() {
		return false
	}
	var newHash func() hash.Hash
	switch strings.ToUpper(p["algorithm"]) {
	case "", "MD5":
		newHash = md5.New
	case "SHA-256":
		newHash = sha256.New
	default:
		return false
	}
	hx := func(parts ...string) string {
		h := newHash()
		h.Write([]byte(strings.Join(parts, ":")))
		return hex.EncodeToString(h.Sum(nil))
	}
	ha1 := hx(s.cfg.User, p["realm"], s.cfg.Pass)
	ha2 := hx(r.Method, p["uri"])
	want := hx(ha1, p["nonce"], ha2)
	if p["qop"] == "auth" {
		want = hx(ha1, p["nonce"], p["nc"], p["cnonce"], "auth", ha2)
	}
	return p["response"] == want
}

// dispatch answers an authenticated request.
func (s *Sim) dispatch(w http.ResponseWriter, r *http.Request, op string, body []byte) {
	base := s.srv.URL
	switch op {
	case "GetSystemDateAndTime":
		now := s.now()
		reply(w, fmt.Sprintf(`<tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime><tt:DateTimeType>NTP</tt:DateTimeType>`+
			`<tt:DaylightSavings>false</tt:DaylightSavings><tt:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute>`+
			`<tt:Second>%d</tt:Second></tt:Time><tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>`+
			`</tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			now.Hour(), now.Minute(), now.Second(), now.Year(), int(now.Month()), now.Day()))
	case "GetDeviceInformation":
		reply(w, `<tds:GetDeviceInformationResponse><tds:Manufacturer>`+esc(s.cfg.Brand)+`</tds:Manufacturer><tds:Model>`+
			esc(s.cfg.Model)+`</tds:Model><tds:FirmwareVersion>1.0</tds:FirmwareVersion><tds:SerialNumber>`+esc(s.cfg.Serial)+
			`</tds:SerialNumber><tds:HardwareId>sim</tds:HardwareId></tds:GetDeviceInformationResponse>`)
	case "GetCapabilities":
		reply(w, `<tds:GetCapabilitiesResponse><tds:Capabilities><tt:Device><tt:XAddr>`+base+DevicePath+`</tt:XAddr></tt:Device>`+
			`<tt:Events><tt:XAddr>`+base+eventsPath+`</tt:XAddr><tt:WSSubscriptionPolicySupport>false</tt:WSSubscriptionPolicySupport>`+
			`<tt:WSPullPointSupport>`+strconv.FormatBool(!s.cfg.NoPullPoint)+`</tt:WSPullPointSupport>`+
			`<tt:WSPausableSubscriptionManagerInterfaceSupport>false</tt:WSPausableSubscriptionManagerInterfaceSupport></tt:Events>`+
			`<tt:Media><tt:XAddr>`+base+mediaPath+`</tt:XAddr></tt:Media></tds:Capabilities></tds:GetCapabilitiesResponse>`)
	case "GetServiceCapabilities":
		reply(w, `<tev:GetServiceCapabilitiesResponse><tev:Capabilities WSPullPointSupport="`+
			strconv.FormatBool(!s.cfg.NoPullPoint)+`" MaxPullPoints="`+strconv.Itoa(s.cfg.MaxPullPoints)+
			`"/></tev:GetServiceCapabilitiesResponse>`)
	case "GetProfiles":
		reply(w, `<trt:GetProfilesResponse>`+profile("Profile_1", 1920, 1080)+profile("Profile_2", 640, 360)+`</trt:GetProfilesResponse>`)
	case "GetStreamUri":
		reply(w, `<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>`+esc(s.cfg.StreamURIs[roleOf(body)])+
			`</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`)
	case "GetSnapshotUri":
		reply(w, `<trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>`+esc(s.cfg.SnapshotURI)+
			`</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse>`)
	case "CreatePullPointSubscription":
		s.create(w, body)
	case "PullMessages":
		s.pull(w, r, body)
	case "Renew":
		s.renew(w, r, body)
	case "Unsubscribe":
		s.unsubscribe(w, r)
	default:
		fault(w, http.StatusBadRequest, "s:Sender", "ter:ActionNotSupported")
	}
}

// profile is a GetProfiles profile.
func profile(token string, width, height int) string {
	return fmt.Sprintf(`<trt:Profiles token="%s" fixed="true"><tt:Name>%s</tt:Name><tt:VideoEncoderConfiguration token="enc-%s">`+
		`<tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution>`+
		`</tt:VideoEncoderConfiguration></trt:Profiles>`, token, token, token, width, height)
}

// roleOf is the profile role, "main" or "sub", of a media request's token.
func roleOf(body []byte) string {
	var req struct {
		Token string `xml:"ProfileToken"`
	}
	_ = xml.Unmarshal(body, &req)
	if req.Token == "Profile_2" {
		return "sub"
	}
	return "main"
}

// termOf parses an xsd:duration (PT…H…M…S) or an absolute dateTime into
// the term's end from now, on the camera's clock.
func (s *Sim) termOf(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, true
	}
	m := durationRe.FindStringSubmatch(v)
	if m == nil {
		return time.Time{}, false
	}
	var d time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		f, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			return time.Time{}, false
		}
		d += time.Duration(f * float64(unit))
	}
	return s.now().Add(d), true
}

// durationRe is an xsd:duration of hours, minutes and seconds.
var durationRe = regexp.MustCompile(`^PT(?:([\d.]+)H)?(?:([\d.]+)M)?(?:([\d.]+)S)?$`)

// create answers CreatePullPointSubscription.
func (s *Sim) create(w http.ResponseWriter, body []byte) {
	if s.cfg.NoPullPoint {
		fault(w, http.StatusBadRequest, "s:Sender", "ter:ActionNotSupported")
		return
	}
	var req struct {
		Term string `xml:"InitialTerminationTime"`
	}
	_ = xml.Unmarshal(body, &req)
	expires, ok := s.termOf(req.Term)
	if !ok {
		expires = s.now().Add(time.Minute)
	}
	s.mu.Lock()
	s.sweep()
	if len(s.subs) >= s.cfg.MaxPullPoints {
		s.mu.Unlock()
		fault(w, http.StatusInternalServerError, "s:Receiver", "ter:Action")
		return
	}
	s.next++
	idx := s.next
	s.subs[idx] = &pullSub{expires: expires, termStart: time.Now(), wake: make(chan struct{}, 1)}
	s.counts.Creates++
	s.mu.Unlock()
	reply(w, `<tev:CreatePullPointSubscriptionResponse><tev:SubscriptionReference><wsa:Address>`+s.srv.URL+subscriptionPath+
		`?Idx=`+strconv.Itoa(idx)+`</wsa:Address></tev:SubscriptionReference><wsnt:CurrentTime>`+
		s.now().Format(time.RFC3339)+`</wsnt:CurrentTime><wsnt:TerminationTime>`+expires.Format(time.RFC3339)+
		`</wsnt:TerminationTime></tev:CreatePullPointSubscriptionResponse>`)
}

// subOf is the live subscription that the request's address names. s.mu is
// held.
func (s *Sim) subOf(r *http.Request) (*pullSub, int, bool) {
	s.sweep()
	idx, err := strconv.Atoi(r.URL.Query().Get("Idx"))
	if err != nil {
		return nil, 0, false
	}
	sub, ok := s.subs[idx]
	return sub, idx, ok
}

// pull answers PullMessages: the queued messages, or after the request's
// timeout none.
func (s *Sim) pull(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		Timeout string `xml:"Timeout"`
	}
	_ = xml.Unmarshal(body, &req)
	wait := time.Second
	if until, ok := s.termOf(req.Timeout); ok {
		wait = until.Sub(s.now())
	}
	s.mu.Lock()
	sub, _, ok := s.subOf(r)
	if !ok {
		s.mu.Unlock()
		fault(w, http.StatusBadRequest, "s:Sender", "wsrf-rw:ResourceUnknownFault")
		return
	}
	s.counts.Pulls++
	s.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		msgs := sub.queue
		sub.queue = nil
		s.mu.Unlock()
		if len(msgs) > 0 {
			s.pullReply(w, sub, msgs)
			return
		}
		select {
		case <-sub.wake:
		case <-timer.C:
			s.pullReply(w, sub, nil)
			return
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		}
	}
}

// pullReply is PullMessages' response with msgs.
func (s *Sim) pullReply(w http.ResponseWriter, sub *pullSub, msgs []string) {
	s.mu.Lock()
	expires := sub.expires
	s.mu.Unlock()
	reply(w, `<tev:PullMessagesResponse><tev:CurrentTime>`+s.now().Format(time.RFC3339)+`</tev:CurrentTime><tev:TerminationTime>`+
		expires.Format(time.RFC3339)+`</tev:TerminationTime>`+strings.Join(msgs, "")+`</tev:PullMessagesResponse>`)
}

// renew answers Renew.
func (s *Sim) renew(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		Term string `xml:"TerminationTime"`
	}
	_ = xml.Unmarshal(body, &req)
	expires, ok := s.termOf(req.Term)
	if !ok {
		fault(w, http.StatusBadRequest, "s:Sender", "wsntw:UnacceptableTerminationTimeFault")
		return
	}
	s.mu.Lock()
	sub, _, found := s.subOf(r)
	if !found {
		s.mu.Unlock()
		fault(w, http.StatusBadRequest, "s:Sender", "wsrf-rw:ResourceUnknownFault")
		return
	}
	now := time.Now()
	s.renewGaps = append(s.renewGaps, now.Sub(sub.termStart))
	sub.termStart, sub.expires = now, expires
	s.counts.Renews++
	s.mu.Unlock()
	reply(w, `<wsnt:RenewResponse><wsnt:TerminationTime>`+expires.Format(time.RFC3339)+`</wsnt:TerminationTime><wsnt:CurrentTime>`+
		s.now().Format(time.RFC3339)+`</wsnt:CurrentTime></wsnt:RenewResponse>`)
}

// unsubscribe answers Unsubscribe.
func (s *Sim) unsubscribe(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	_, idx, found := s.subOf(r)
	if found {
		delete(s.subs, idx)
		s.counts.Unsubscribes++
	}
	s.mu.Unlock()
	if !found {
		fault(w, http.StatusBadRequest, "s:Sender", "wsrf-rw:ResourceUnknownFault")
		return
	}
	reply(w, `<wsnt:UnsubscribeResponse/>`)
}

// notification is a NotificationMessage of topic with IsMotion = motion.
func notification(topic string, motion bool, at time.Time) string {
	return `<wsnt:NotificationMessage><wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">` +
		esc(topic) + `</wsnt:Topic><wsnt:Message><tt:Message UtcTime="` + at.Format(time.RFC3339) +
		`" PropertyOperation="Changed"><tt:Source><tt:SimpleItem Name="VideoSourceConfigurationToken" Value="1"/></tt:Source>` +
		`<tt:Data><tt:SimpleItem Name="IsMotion" Value="` + strconv.FormatBool(motion) + `"/></tt:Data></tt:Message>` +
		`</wsnt:Message></wsnt:NotificationMessage>`
}

// namespaces are the response envelope's declarations.
var namespaces = map[string]string{
	"s":       "http://www.w3.org/2003/05/soap-envelope",
	"tds":     "http://www.onvif.org/ver10/device/wsdl",
	"trt":     "http://www.onvif.org/ver10/media/wsdl",
	"tev":     "http://www.onvif.org/ver10/events/wsdl",
	"tt":      "http://www.onvif.org/ver10/schema",
	"wsnt":    "http://docs.oasis-open.org/wsn/b-2",
	"wsntw":   "http://docs.oasis-open.org/wsn/bw-2",
	"wsa":     "http://www.w3.org/2005/08/addressing",
	"ter":     "http://www.onvif.org/ver10/error",
	"wsrf-rw": "http://docs.oasis-open.org/wsrf/rw-2",
}

// writeEnvelope writes a SOAP envelope with body and status.
func writeEnvelope(w http.ResponseWriter, status int, body string) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><s:Envelope`)
	for _, k := range slices.Sorted(maps.Keys(namespaces)) {
		b.WriteString(` xmlns:` + k + `="` + namespaces[k] + `"`)
	}
	b.WriteString(`><s:Body>` + body + `</s:Body></s:Envelope>`)
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, b.String())
}

// reply writes a 200 response with body.
func reply(w http.ResponseWriter, body string) { writeEnvelope(w, http.StatusOK, body) }

// fault writes a SOAP fault with code and subcode.
func fault(w http.ResponseWriter, status int, code, subcode string) {
	writeEnvelope(w, status, `<s:Fault><s:Code><s:Value>`+code+`</s:Value><s:Subcode><s:Value>`+subcode+
		`</s:Value></s:Subcode></s:Code><s:Reason><s:Text xml:lang="en">`+subcode+`</s:Text></s:Reason></s:Fault>`)
}

// esc is s escaped for XML.
func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// randomHex is n random bytes in hex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
