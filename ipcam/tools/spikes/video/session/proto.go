package session

import (
	"encoding/json"
	"fmt"
	"time"
)

// The rpc data channel carries one JSON object per message. The field "t"
// names the message; the struct field order below is the wire order.

// Error codes of the hub's "error" message.
const (
	// codeCodecUnsupported: the client's offer has no codec that carries the
	// camera's stream (APP-05: the client falls back to the substream).
	codeCodecUnsupported = "CODEC_UNSUPPORTED"
	// codeUnknownCamera: no source is called "<cam>/<profile>".
	codeUnknownCamera = "UNKNOWN_CAMERA"
	// codeBadSlot: the offer has no such slot.
	codeBadSlot = "BAD_SLOT"
)

// request is a client message: bind, unbind or ping.
type request struct {
	T       string  `json:"t"`
	Slot    int     `json:"slot"`
	Cam     string  `json:"cam"`
	Profile string  `json:"profile"`
	Epoch   int64   `json:"epoch"`
	C0      float64 `json:"c0"`
}

// parseRequest decodes a client message and checks its type.
func parseRequest(data []byte) (request, error) {
	var r request
	if err := json.Unmarshal(data, &r); err != nil {
		return request{}, fmt.Errorf("decode rpc message: %w", err)
	}
	switch r.T {
	case "bind", "unbind", "ping":
		return r, nil
	}
	return request{}, fmt.Errorf("unknown rpc message type %q", r.T)
}

// boundMsg says that a slot now carries the camera of the bind with this
// epoch: the next access unit on the slot is that camera's IDR. Profile is the
// stream's codec profile. Fallback, set only when a substream goes out in
// another offered H.264 profile than its own (R23), is that profile-level-id.
type boundMsg struct {
	T        string `json:"t"`
	Slot     int    `json:"slot"`
	Epoch    int64  `json:"epoch"`
	Codec    string `json:"codec"`
	Profile  string `json:"profile"`
	Fallback string `json:"fallback,omitempty"`
}

// errorMsg says that a bind failed; the slot keeps what it carried.
type errorMsg struct {
	T     string `json:"t"`
	Slot  int    `json:"slot"`
	Epoch int64  `json:"epoch"`
	Code  string `json:"code"`
}

// pongMsg answers a ping with the client's time c0 and the hub's time h in
// Unix ms, for the client's clock offset estimate.
type pongMsg struct {
	T  string  `json:"t"`
	C0 float64 `json:"c0"`
	H  float64 `json:"h"`
}

// sentMsg lists the frames a simulated camera sent since the last such
// message: [frame, loop, hub Unix ms] each. Cam is the simulated camera index
// and P the stream profile of the bind ("sub" or "main").
type sentMsg struct {
	T   string       `json:"t"`
	Cam uint8        `json:"cam"`
	P   string       `json:"p"`
	R   [][3]float64 `json:"r"`
}

// pairMsg names the ICE candidate types of the selected pair, never addresses
// (PH0-22).
type pairMsg struct {
	T      string `json:"t"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// jitterMsg reports the hub's pacing jitter, in ms: the worst, over the bound
// simulators, of the p99 deviation of the frame send interval from the frame
// period, over the 10 s before the message (R28).
type jitterMsg struct {
	T  string  `json:"t"`
	MS float64 `json:"ms"`
}

// unixMs is t as Unix milliseconds with microsecond precision.
func unixMs(t time.Time) float64 {
	return float64(t.UnixMicro()) / 1000
}

// durationMs is d in milliseconds with microsecond precision.
func durationMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
