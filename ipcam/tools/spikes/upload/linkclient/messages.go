// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package linkclient

import "strings"

// hello is the host's first message, as c1's link.Hello: role "host", the
// host's pseudonym (PH0-22), never a host name, and version "c1".
type hello struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Host    string `json:"host"`
	Version string `json:"version"`
}

// Session is what reccontroller --local serves: the RTSP server's base
// URL, the per-start credentials and the streams. It stays in RAM and
// reaches ipcam-hub only in run.json, as in c1; its String and GoString
// hide the credentials from fmt. The session line's other fields
// (beaconPort, needles, sinkPort, sinkCert) are ignored.
type Session struct {
	RTSPBase, User, Pass string
	Streams              []string
}

// String hides the credentials.
func (s Session) String() string {
	return "linkclient.Session{RTSPBase: " + s.RTSPBase + ", User: <redacted>, Pass: <redacted>, Streams: [" + strings.Join(s.Streams, " ") + "]}"
}

// GoString hides the credentials from %#v.
func (s Session) GoString() string { return s.String() }

// sessionLine is the session message as c1's link.Session sends it.
type sessionLine struct {
	Type     string   `json:"type"`
	RTSPBase string   `json:"rtspBase"`
	User     string   `json:"user"`
	Pass     string   `json:"pass"`
	Streams  []string `json:"streams"`
}
