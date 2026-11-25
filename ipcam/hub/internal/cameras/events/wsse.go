package events

import (
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: WS-UsernameToken's PasswordDigest is SHA-1 by its standard
	"encoding/base64"
	"time"
)

// The WS-UsernameToken's attribute values.
const (
	wssePasswordDigest = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest" //nolint:gosec // G101: a URI of the standard
	wsseBase64Binary   = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
)

// createdLayout is the WS-UsernameToken's Created, in UTC.
const createdLayout = "2006-01-02T15:04:05.000Z"

// wsseHeader is a SOAP header with a WS-UsernameToken PasswordDigest for
// user and pass (§8.2): Base64(SHA-1(nonce + created + password)), where
// created is the camera's time, hubNow shifted by the camera's clock offset
// (GetSystemDateAndTime), so a camera with a skewed clock still accepts it.
// The password itself is never sent: there is no PasswordText.
func wsseHeader(user, pass string, hubNow time.Time, offset time.Duration) string {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	created := hubNow.Add(offset).UTC().Format(createdLayout)
	h := sha1.New() //nolint:gosec // G401: see the import
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(pass))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return `<wsse:Security s:mustUnderstand="1" xmlns:wsse="` + nsWSSE + `" xmlns:wsu="` + nsWSU + `">` +
		`<wsse:UsernameToken><wsse:Username>` + escapeText(user) + `</wsse:Username>` +
		`<wsse:Password Type="` + wssePasswordDigest + `">` + digest + `</wsse:Password>` +
		`<wsse:Nonce EncodingType="` + wsseBase64Binary + `">` + base64.StdEncoding.EncodeToString(nonce) + `</wsse:Nonce>` +
		`<wsu:Created>` + created + `</wsu:Created></wsse:UsernameToken></wsse:Security>`
}
