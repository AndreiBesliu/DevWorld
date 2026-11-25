package events

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// The namespaces of the requests.
const (
	nsSOAP = "http://www.w3.org/2003/05/soap-envelope"
	nsTDS  = "http://www.onvif.org/ver10/device/wsdl"
	nsTRT  = "http://www.onvif.org/ver10/media/wsdl"
	nsTEV  = "http://www.onvif.org/ver10/events/wsdl"
	nsWSNT = "http://docs.oasis-open.org/wsn/b-2"
	nsWSA  = "http://www.w3.org/2005/08/addressing"
	nsTT   = "http://www.onvif.org/ver10/schema"
	nsWSSE = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	nsWSU  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
)

// maxResponse bounds a response body that the client reads.
const maxResponse = 4 << 20

// The errors of a call. None names an address, a URL or a credential.
var (
	// ErrAuth is a camera that refused every login method.
	ErrAuth = errors.New("onvif: the camera refused the login")
	// ErrAuthPaused is a call not sent: after 3 refused logins the client
	// waits Options.AuthPause before it tries again (§8.2).
	ErrAuthPaused = errors.New("onvif: login paused after 3 refusals")
	// ErrOnvifPort is a camera whose ONVIF port does not answer: the TCP
	// connection was refused or timed out, or the name did not resolve.
	ErrOnvifPort = errors.New("onvif: nothing answers at the ONVIF port")
	// ErrUnreachable is a camera that stopped answering a connection that
	// it accepted, or answered with something that is not ONVIF.
	ErrUnreachable = errors.New("onvif: the camera did not answer")
	// ErrNoPullPoint is a camera without an events service with PullPoint
	// support.
	ErrNoPullPoint = errors.New("onvif: no events service with PullPoint support")
	// errGone is a subscription that the camera no longer knows.
	errGone = errors.New("onvif: the subscription is unknown to the camera")
	// errFault is any other SOAP fault.
	errFault = errors.New("onvif: SOAP fault")
)

// The reasons of Reason.
const (
	ReasonAuth        = "auth"
	ReasonNoPullPoint = "no-pullpoint"
	ReasonOnvifPort   = "onvif-port"
	ReasonUnreachable = "unreachable"
)

// Reason classifies an error of this package: ReasonAuth for a refused or
// paused login, ReasonNoPullPoint, ReasonOnvifPort when nothing answered at
// the camera's port, ReasonUnreachable otherwise; "" for nil.
func Reason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrAuth), errors.Is(err, ErrAuthPaused):
		return ReasonAuth
	case errors.Is(err, ErrNoPullPoint):
		return ReasonNoPullPoint
	case errors.Is(err, ErrOnvifPort):
		return ReasonOnvifPort
	}
	return ReasonUnreachable
}

// transportError replaces err, an error of an HTTP request, by one that names
// no URL or address: net/http's errors quote the URL, net's the address.
func transportError(ctx context.Context, err error) error {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return fmt.Errorf("%w: %w", ErrUnreachable, ctx.Err())
	case errors.As(err, &dnsErr):
		return fmt.Errorf("%w: the name did not resolve", ErrOnvifPort)
	case errors.As(err, &opErr) && opErr.Op == "dial":
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			return fmt.Errorf("%w: connection refused", ErrOnvifPort)
		case errors.Is(err, os.ErrDeadlineExceeded) || opErr.Timeout():
			return fmt.Errorf("%w: connection timed out", ErrOnvifPort)
		}
		return fmt.Errorf("%w: connection failed", ErrOnvifPort)
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: timed out", ErrUnreachable)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET):
		return fmt.Errorf("%w: connection closed", ErrUnreachable)
	}
	return fmt.Errorf("%w: request failed", ErrUnreachable)
}

// envelope is a SOAP 1.2 request with header and body, each XML text.
func envelope(header, body string) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<s:Envelope xmlns:s="` + nsSOAP + `" xmlns:tds="` + nsTDS + `" xmlns:trt="` + nsTRT +
		`" xmlns:tev="` + nsTEV + `" xmlns:wsnt="` + nsWSNT + `" xmlns:wsa="` + nsWSA + `" xmlns:tt="` + nsTT + `">`)
	if header != "" {
		b.WriteString("<s:Header>" + header + "</s:Header>")
	}
	b.WriteString("<s:Body>" + body + "</s:Body></s:Envelope>")
	return b.Bytes()
}

// escapeText is s escaped for XML character data.
func escapeText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// soapFault is a SOAP 1.2 fault, or a 1.1 one's faultcode.
type soapFault struct {
	Code struct {
		Value   string `xml:"Value"`
		Subcode struct {
			Value   string `xml:"Value"`
			Subcode struct {
				Value string `xml:"Value"`
			} `xml:"Subcode"`
		} `xml:"Subcode"`
	} `xml:"Code"`
	FaultCode string `xml:"faultcode"`
	Detail    struct {
		Inner string `xml:",innerxml"`
	} `xml:"Detail"`
}

// codes are the fault's codes, joined.
func (f *soapFault) codes() string {
	return strings.Join([]string{f.Code.Value, f.Code.Subcode.Value, f.Code.Subcode.Subcode.Value, f.FaultCode, f.Detail.Inner}, " ")
}

// err classifies the fault: an authentication fault, a subscription the
// camera does not know, or any other.
func (f *soapFault) err() error {
	codes := f.codes()
	switch {
	case strings.Contains(codes, "NotAuthorized"), strings.Contains(codes, "FailedAuthentication"),
		strings.Contains(codes, "InvalidSecurity"), strings.Contains(codes, "FailedCheck"):
		return ErrAuth
	case strings.Contains(codes, "ResourceUnknown"), strings.Contains(codes, "InvalidPullPoint"),
		strings.Contains(codes, "UnableToDestroySubscription"), strings.Contains(codes, "SubscriptionNotExist"):
		return errGone
	}
	return errFault
}

// responseEnvelope is a SOAP response: its fault, or its body's content.
type responseEnvelope struct {
	Body struct {
		Fault *soapFault `xml:"Fault"`
		Inner []byte     `xml:",innerxml"`
	} `xml:"Body"`
}

// soapResult is what a request's response gave: the decoded body, or a
// fault's error, or the HTTP status of a response without either.
type soapResult struct {
	status int
	header http.Header
	err    error
}

// refused is a response that refused the request's login: a 401, or an
// authentication fault.
func (r soapResult) refused() bool {
	return r.status == http.StatusUnauthorized || errors.Is(r.err, ErrAuth)
}

// post sends body to endpoint with action, and decodes the response's body
// into out (when not nil). setAuth, when not nil, adds the request's HTTP
// authentication. A transport error is returned as an error that names no
// URL; a response is returned as a soapResult.
func post(ctx context.Context, client *http.Client, endpoint, action string, body []byte,
	setAuth func(*http.Request), out any,
) (soapResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return soapResult{}, fmt.Errorf("%w: invalid address", ErrOnvifPort)
	}
	ct := `application/soap+xml; charset=utf-8`
	if action != "" {
		ct += `; action="` + action + `"`
	}
	req.Header.Set("Content-Type", ct)
	if setAuth != nil {
		setAuth(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		return soapResult{}, transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return soapResult{}, transportError(ctx, err)
	}
	res := soapResult{status: resp.StatusCode, header: resp.Header}
	var env responseEnvelope
	if xml.Unmarshal(raw, &env) != nil {
		if resp.StatusCode/100 == 2 {
			res.err = fmt.Errorf("%w: the response is not SOAP", ErrUnreachable)
		} else {
			res.err = fmt.Errorf("%w: HTTP %d", ErrUnreachable, resp.StatusCode)
		}
		return res, nil
	}
	if env.Body.Fault != nil {
		res.err = env.Body.Fault.err()
		return res, nil
	}
	if resp.StatusCode/100 != 2 {
		res.err = fmt.Errorf("%w: HTTP %d", ErrUnreachable, resp.StatusCode)
		return res, nil
	}
	if out != nil {
		if err := xml.Unmarshal(env.Body.Inner, out); err != nil {
			res.err = fmt.Errorf("%w: the response does not decode", ErrUnreachable)
		}
	}
	return res, nil
}

// rebase is service, an XAddr that the camera returned, on the scheme, host
// and port of device, the XAddr the owner typed: a camera often names its
// services by an address of its own that the hub cannot reach (behind NAT,
// a changed IP). "" when service is empty or not a URL.
func rebase(service, device string) string {
	s, err := url.Parse(strings.TrimSpace(service))
	if err != nil || s.Host == "" {
		return ""
	}
	d, err := url.Parse(device)
	if err != nil || d.Host == "" {
		return ""
	}
	s.Scheme, s.Host = d.Scheme, d.Host
	return s.String()
}
