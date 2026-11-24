// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import "fmt"

// The rpc protocol of the harness, JSON text on the client's rpc channel
// (§7.6's procedures stand-in): the client asks for the recording's info,
// for a byte range ("read"), or for a seek to a time from the first sample;
// the hub side answers each call on rpc ("info", "range" or "error") and
// sends a range's bytes on bulk/<call>, which it opens for the call. The
// reply names the range's length; the client closes the channel once it
// has every byte (closeGrace says why the client, not the hub side).
type request struct {
	T    string `json:"t"`
	Call uint64 `json:"call"`
	Off  int64  `json:"off,omitempty"`
	N    int64  `json:"n,omitempty"`
	Ms   int64  `json:"ms,omitempty"`
}

type reply struct {
	T    string `json:"t"`
	Call uint64 `json:"call"`
	// info: the plaintext's length, its init's, the video codec as MSE
	// names it, and the last IDR's time from the first sample, up to which
	// seeks go.
	Size      int64  `json:"size,omitempty"`
	InitBytes int64  `json:"initBytes,omitempty"`
	Codec     string `json:"codec,omitempty"`
	LastIDRMs int64  `json:"lastIdrMs,omitempty"`
	// range: the bytes that bulk/<call> carries and, for a seek, where its
	// IDR lies in the plaintext.
	Off     int64 `json:"off,omitempty"`
	N       int64 `json:"n,omitempty"`
	IDROff  int64 `json:"idrOff,omitempty"`
	IDRSize int64 `json:"idrSize,omitempty"`
	// error: a Connect code (§7.7) and the error's text.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// bulkLabel is the label of a call's bulk channel.
func bulkLabel(call uint64) string { return fmt.Sprintf("bulk/%d", call) }
