// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package link

import (
	"encoding/json"
	"time"
)

// Message is one line of the link: a JSON object whose "type" field names
// it, with the fields of the concrete type beside it.
type Message interface {
	Type() string
}

// Hello is the first message of the side that connects. Role is "host"
// (recspike); Host is the device's pseudonym, such as "pc" or "pi"
// (PH0-22), never a host name.
type Hello struct {
	Role    string `json:"role"`
	Host    string `json:"host"`
	Version string `json:"version"`
}

// Session tells the host where the controller serves its cameras: the base
// URL of the RTSP server, as the host reached the controller, and the
// credentials of this reccontroller start. The host never writes them to
// its results or logs.
type Session struct {
	RTSPBase string   `json:"rtspBase"`
	User     string   `json:"user"`
	Pass     string   `json:"pass"`
	Streams  []string `json:"streams"`
}

// RunStart announces a run before the host starts ipcam-hub for it.
type RunStart struct {
	RunID         string   `json:"runId"`
	Set           string   `json:"set"`
	Kind          string   `json:"kind"`
	Encrypted     bool     `json:"encrypted"`
	PadLowBitrate bool     `json:"padLowBitrate"`
	Streams       []string `json:"streams"`
}

// Ready says that the host records the run and beacons.
type Ready struct {
	RunID string `json:"runId"`
}

// Cut tells the host to stop now, or, for a real cut, to mirror the prompt
// that the controller shows. PromptAt is on the controller's clock.
type Cut struct {
	RunID    string    `json:"runId"`
	Kind     string    `json:"kind"`
	PromptAt time.Time `json:"promptAt"`
}

// StreamIndex is the SEI index of the last complete sample that the host
// recorded of a stream before the stop.
type StreamIndex struct {
	Stream            string `json:"stream"`
	LastRecordedIndex uint64 `json:"lastRecordedIndex"`
}

// Report is what the host measured of a run after the restart: the last
// recorded index per stream, ipcam-hub's last reconcile and verify events
// as it printed them, and its own observations. The controller computes the
// loss from the indexes and its own send log, never from the host's clock.
type Report struct {
	RunID      string          `json:"runId"`
	Streams    []StreamIndex   `json:"streams"`
	Reconcile  json.RawMessage `json:"reconcile"`
	Verify     json.RawMessage `json:"verify"`
	Hub04Clear bool            `json:"hub04Clear"`
	ResumeMs   *int64          `json:"resumeMs"`
	ColdBootMs *int64          `json:"coldBootMs"`
	NtpWaitMs  int64           `json:"ntpWaitMs"`
	NtpSynced  bool            `json:"ntpSynced"`
	WriteCache string          `json:"writeCache"`
}

// Results is one results file that the host sends to the controller, by its
// path relative to the results folder.
type Results struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// PullResults asks the far side for its whole results folder.
type PullResults struct{}

// ResultsTar answers PullResults: the results folder as a tar archive.
type ResultsTar struct {
	Data []byte `json:"data"`
}

// Bye ends a session.
type Bye struct{}

// Type is "hello".
func (Hello) Type() string { return "hello" }

// Type is "session".
func (Session) Type() string { return "session" }

// Type is "run_start".
func (RunStart) Type() string { return "run_start" }

// Type is "ready".
func (Ready) Type() string { return "ready" }

// Type is "cut".
func (Cut) Type() string { return "cut" }

// Type is "report".
func (Report) Type() string { return "report" }

// Type is "results".
func (Results) Type() string { return "results" }

// Type is "pull_results".
func (PullResults) Type() string { return "pull_results" }

// Type is "results_tar".
func (ResultsTar) Type() string { return "results_tar" }

// Type is "bye".
func (Bye) Type() string { return "bye" }

// decoders makes the message of each type from the line that carries it.
var decoders = map[string]func([]byte) (Message, error){
	"hello":        decodeAs[Hello],
	"session":      decodeAs[Session],
	"run_start":    decodeAs[RunStart],
	"ready":        decodeAs[Ready],
	"cut":          decodeAs[Cut],
	"report":       decodeAs[Report],
	"results":      decodeAs[Results],
	"pull_results": decodeAs[PullResults],
	"results_tar":  decodeAs[ResultsTar],
	"bye":          decodeAs[Bye],
}

func decodeAs[M Message](line []byte) (Message, error) {
	var m M
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	return m, nil
}
