package recorder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// configName is MediaMTX's configuration file in the run directory.
const configName = "mediamtx.yml"

// namePattern is what a camera id may be: one directory name, which
// MediaMTX's path names allow too.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// mtxConfig is the MediaMTX configuration that the hub writes. Every server
// of MediaMTX is off: it only pulls the restream and records (§4.2). The
// playback server and the control API stay off in particular because
// MediaMTX's record store matches <name>.mp4.part as a segment (its regexp
// is not anchored at the end), and the record cleaner too, by
// recordDeleteAfter 0: retention belongs to the hub, and a .part waits for
// the hub's reconcile (hub/third_party/PATCHES.md, known interactions).
type mtxConfig struct {
	RTSP     bool               `json:"rtsp"`
	RTMP     bool               `json:"rtmp"`
	HLS      bool               `json:"hls"`
	WebRTC   bool               `json:"webrtc"`
	SRT      bool               `json:"srt"`
	MoQ      bool               `json:"moq"`
	Playback bool               `json:"playback"`
	API      bool               `json:"api"`
	Metrics  bool               `json:"metrics"`
	PPROF    bool               `json:"pprof"`
	Paths    map[string]mtxPath `json:"paths"`
}

// mtxPath is one job's path. Every key is set per path: pathDefaults stays
// empty, because recipients there would apply to every path (MTX-2).
type mtxPath struct {
	Source                string `json:"source"`
	RTSPTransport         string `json:"rtspTransport"`
	Record                bool   `json:"record"`
	RecordPath            string `json:"recordPath"`
	RecordFormat          string `json:"recordFormat"`
	RecordPartDuration    string `json:"recordPartDuration"`
	RecordSegmentDuration string `json:"recordSegmentDuration"`
	RecordDeleteAfter     string `json:"recordDeleteAfter"`
	// Only for an encrypted job: MediaMTX refuses an empty or null list,
	// and only an absent key records in clear (MTX-2).
	RecordAgeRecipients        []string `json:"recordAgeRecipients,omitempty"`
	RecordAgePadLowBitrate     *bool    `json:"recordAgePadLowBitrate,omitempty"`
	RunOnRecordSegmentComplete string   `json:"runOnRecordSegmentComplete"`
}

// GenerateConfig returns the MediaMTX configuration for jobs: one path
// <cam>/<profile> per job that pulls its RestreamURL over RTSP/TCP and
// records fMP4 into <recDir>/<cam>/<profile>/<date>/<HHMMSS>_<µs>.mp4, and
// runs hookCmd when a segment is complete. recDir must be absolute. The
// configuration is JSON, which MediaMTX reads as YAML; it holds the jobs'
// public recipients and never a secret (C11, C22).
func GenerateConfig(jobs []Job, hookCmd []string, recDir string) ([]byte, error) {
	if !filepath.IsAbs(recDir) {
		return nil, fmt.Errorf("recorder: the recordings directory %q is not absolute", recDir)
	}
	if strings.Contains(recDir, "%") {
		return nil, fmt.Errorf("recorder: the recordings directory %q holds a %%, which MediaMTX's recordPath would expand", recDir)
	}
	hook, err := quoteCommand(hookCmd)
	if err != nil {
		return nil, err
	}
	c := mtxConfig{Paths: map[string]mtxPath{}}
	for _, j := range jobs {
		if err := j.validate(); err != nil {
			return nil, err
		}
		name := j.pathName()
		if _, ok := c.Paths[name]; ok {
			return nil, fmt.Errorf("recorder: two jobs record %s", name)
		}
		p := mtxPath{
			Source:                     j.RestreamURL,
			RTSPTransport:              "tcp",
			Record:                     true,
			RecordPath:                 filepath.ToSlash(recDir) + "/%path/%Y-%m-%d/%H%M%S_%f",
			RecordFormat:               "fmp4",
			RecordPartDuration:         strconv.Itoa(j.PartSecs) + "s",
			RecordSegmentDuration:      strconv.Itoa(j.SegmentSecs) + "s",
			RecordDeleteAfter:          "0s",
			RunOnRecordSegmentComplete: hook,
		}
		if j.Scope != nil {
			recipients, err := j.Scope.Strings()
			if err != nil {
				return nil, fmt.Errorf("recorder: job %s: %w", name, err)
			}
			pad := j.PadLowBitrate
			p.RecordAgeRecipients, p.RecordAgePadLowBitrate = recipients[:], &pad
		}
		c.Paths[name] = p
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (j Job) validate() error {
	name := j.pathName()
	switch {
	case !namePattern.MatchString(string(j.Cam)) || j.Cam == "." || j.Cam == "..":
		return fmt.Errorf("recorder: the camera id %q is not one directory name", j.Cam)
	case j.Profile != ProfileMain && j.Profile != ProfileSub:
		return fmt.Errorf("recorder: job %s: the profile is neither main nor sub", name)
	case j.RestreamURL == "":
		return fmt.Errorf("recorder: job %s has no restream URL", name)
	case j.SegmentSecs < 1 || j.PartSecs < 1 || j.PartSecs > j.SegmentSecs:
		return fmt.Errorf("recorder: job %s: parts of %d s in segments of %d s", name, j.PartSecs, j.SegmentSecs)
	}
	return nil
}

// quoteCommand joins args into a command line that MediaMTX's externalcmd
// splits back into args (go-shellquote, on every OS): each argument in
// single quotes. externalcmd expands $NAME in each argument, so a $ is
// refused.
func quoteCommand(args []string) (string, error) {
	if len(args) == 0 || args[0] == "" {
		return "", errors.New("recorder: no hook command")
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, "$\x00") {
			return "", fmt.Errorf("recorder: hook argument %q holds a $ or a NUL, which MediaMTX would expand or cut", a)
		}
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " "), nil
}
