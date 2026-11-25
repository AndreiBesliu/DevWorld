// Package events subscribes to an ONVIF camera's events through a PullPoint
// (HUB-11, §8.2), and reads what the hub needs of its device and media
// services. Its SOAP requests are its own, over net/http and encoding/xml;
// the ONVIF module gives types only, and only from its event, event/topic,
// xsd, xsd/iso8601, xsd/onvif and device packages (P06).
//
// A camera's address, user and password, and the stream and snapshot URIs it
// returns, never enter an error, a log or a printed value of this package:
// CameraRef redacts them, and transport errors are replaced by errors that
// name no URL.
package events

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// redacted stands for a camera's address, user or password in printed
// values.
const redacted = "[redacted]"

// CameraRef is a camera to reach: the pseudonym ID ("real-1"), the device
// service XAddr (http://<ip>:<port>/onvif/device_service), and the ONVIF
// user and password. Printing it with fmt, marshalling it to JSON or logging
// it with slog shows the ID only (PH0-22).
type CameraRef struct{ ID, XAddr, User, Pass string }

// String is the camera's ID.
func (c CameraRef) String() string { return "camera(" + c.ID + ")" }

// GoString is the camera with its address, user and password redacted.
func (c CameraRef) GoString() string {
	return fmt.Sprintf("events.CameraRef{ID:%q, XAddr:%q, User:%q, Pass:%q}", c.ID, redacted, redacted, redacted)
}

// MarshalJSON is {"id":<ID>}.
func (c CameraRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{c.ID})
}

// LogValue is the camera's ID.
func (c CameraRef) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", c.ID))
}

// Format prints the camera for every verb as String does, and %#v as
// GoString, so that no verb reaches the fields.
func (c CameraRef) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = fmt.Fprint(f, c.GoString())
		return
	}
	_, _ = fmt.Fprint(f, c.String())
}
