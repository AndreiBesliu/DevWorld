// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package link

import (
	"encoding/json"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
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
// credentials of this reccontroller start, which the host never writes to
// its results or logs. BeaconPort is where the beacon goes on the same
// address (0: BeaconPort). Needles are the byte strings of the served media
// that HUB-04's scan looks for in an encrypted camera's files (G08).
// SinkPort and SinkCert are MI3's upload sink on the same address, HTTPS
// with this self-signed certificate (DER), which the upload trusts alone.
type Session struct {
	RTSPBase   string   `json:"rtspBase"`
	User       string   `json:"user"`
	Pass       string   `json:"pass"`
	Streams    []string `json:"streams"`
	BeaconPort int      `json:"beaconPort,omitempty"`
	Needles    [][]byte `json:"needles,omitempty"`
	SinkPort   int      `json:"sinkPort,omitempty"`
	SinkCert   []byte   `json:"sinkCert,omitempty"`
}

// RunStart announces a run before the host starts ipcam-hub for it, with
// what the controller's envelope says of the host: its pseudonym (PH0-22),
// OS, storage, Pi model and SD-card confirmation. Slot is a real cut's slot;
// Attempt counts the attempts of the run's position, or of a real set (a
// redo has a new run id). Smoke asks for the lab's short smoke windows.
type RunStart struct {
	RunID          string                  `json:"runId"`
	Set            string                  `json:"set"`
	Kind           string                  `json:"kind"`
	Encrypted      bool                    `json:"encrypted"`
	PadLowBitrate  bool                    `json:"padLowBitrate"`
	Streams        []string                `json:"streams"`
	Host           string                  `json:"host"`
	OS             string                  `json:"os"`
	Storage        string                  `json:"storage"`
	PiModel        *results.PiModelInfo    `json:"piModel,omitempty"`
	SdConfirmation *results.SdConfirmation `json:"sdConfirmation,omitempty"`
	Slot           int                     `json:"slot,omitempty"`
	Attempt        int                     `json:"attempt"`
	Smoke          bool                    `json:"smoke,omitempty"`
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

// Verdict answers a report: the run as the controller judged it and wrote
// it into its envelope (C16), so that the host knows whether it counts or
// is redone. Timing is set when a stream lost more than its limit: the
// controller's side of the run's diagnostics (Task 7.2d).
type Verdict struct {
	Run    results.Run `json:"run"`
	Timing *Timing     `json:"timing,omitempty"`
}

// Timing is when, on the controller's clock, in Unix milliseconds, the host
// stopped (StopAt, its last beacon), and when the last access unit that the
// host recorded was sent, for each stream that lost more than its limit and
// whose last recorded index has a send time.
type Timing struct {
	StopAtMs   int64            `json:"stopAtMs"`
	LastSentMs map[string]int64 `json:"lastSentMs"`
}

// Refused says, in Romanian, why the controller does not measure a run.
type Refused struct {
	RunID  string `json:"runId"`
	Reason string `json:"reason"`
}

// BeaconCheck asks the controller whether the beacons of ID, which the host
// sends now, arrive (the preflight, R08); BeaconAck answers it.
type BeaconCheck struct {
	ID string `json:"id"`
}

// BeaconAck answers a BeaconCheck: Received is true when a beacon of ID
// arrived.
type BeaconAck struct {
	ID       string `json:"id"`
	Received bool   `json:"received"`
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

// Mi3Start starts one step of MI3 on the host (G12, R07): the controller
// marks its pacing and makes Sessions receive-only live peers, each a grid
// of Tiles, the streams it binds, every camera's sub then one main (G20).
// It answers with Mi3Offers, or Refused.
type Mi3Start struct {
	RunID    string   `json:"runId"`
	Sessions int      `json:"sessions"`
	Tiles    []string `json:"tiles"`
}

// Mi3Offers are the live peers' offers, which the host's live load answers.
type Mi3Offers struct {
	RunID  string   `json:"runId"`
	Offers []string `json:"offers"`
}

// Mi3Answers are the live load's answers, one per offer, in order. The
// controller answers with Mi3Live once every tile of every peer is bound,
// or Refused.
type Mi3Answers struct {
	RunID   string   `json:"runId"`
	Answers []string `json:"answers"`
}

// Mi3Live says that every live tile of a step is bound and receiving.
type Mi3Live struct {
	RunID string `json:"runId"`
}

// Mi3End ends a step; the controller closes its live peers and answers
// with Mi3Result.
type Mi3End struct {
	RunID string `json:"runId"`
}

// Mi3Result is the controller's side of a step: its pacing (R07) from
// Mi3Start to Mi3End, and the live tiles' added latency, p95 over
// LatencySamples access units, from the SEI index on the controller's
// clock (§4.8).
type Mi3Result struct {
	RunID          string  `json:"runId"`
	PacingP99Ms    int64   `json:"pacingP99Ms"`
	SkippedAUs     int     `json:"skippedAUs"`
	LatencyP95Ms   float64 `json:"latencyP95Ms"`
	LatencySamples int     `json:"latencySamples"`
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

// Type is "verdict".
func (Verdict) Type() string { return "verdict" }

// Type is "refused".
func (Refused) Type() string { return "refused" }

// Type is "beacon_check".
func (BeaconCheck) Type() string { return "beacon_check" }

// Type is "beacon_ack".
func (BeaconAck) Type() string { return "beacon_ack" }

// Type is "results".
func (Results) Type() string { return "results" }

// Type is "pull_results".
func (PullResults) Type() string { return "pull_results" }

// Type is "results_tar".
func (ResultsTar) Type() string { return "results_tar" }

// Type is "mi3_start".
func (Mi3Start) Type() string { return "mi3_start" }

// Type is "mi3_offers".
func (Mi3Offers) Type() string { return "mi3_offers" }

// Type is "mi3_answers".
func (Mi3Answers) Type() string { return "mi3_answers" }

// Type is "mi3_live".
func (Mi3Live) Type() string { return "mi3_live" }

// Type is "mi3_end".
func (Mi3End) Type() string { return "mi3_end" }

// Type is "mi3_result".
func (Mi3Result) Type() string { return "mi3_result" }

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
	"verdict":      decodeAs[Verdict],
	"refused":      decodeAs[Refused],
	"beacon_check": decodeAs[BeaconCheck],
	"beacon_ack":   decodeAs[BeaconAck],
	"results":      decodeAs[Results],
	"pull_results": decodeAs[PullResults],
	"results_tar":  decodeAs[ResultsTar],
	"mi3_start":    decodeAs[Mi3Start],
	"mi3_offers":   decodeAs[Mi3Offers],
	"mi3_answers":  decodeAs[Mi3Answers],
	"mi3_live":     decodeAs[Mi3Live],
	"mi3_end":      decodeAs[Mi3End],
	"mi3_result":   decodeAs[Mi3Result],
	"bye":          decodeAs[Bye],
}

func decodeAs[M Message](line []byte) (Message, error) {
	var m M
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	return m, nil
}
