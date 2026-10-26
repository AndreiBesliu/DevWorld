// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// The diagnostics of a lab run in which a stream lost more than its limit
// (Task 7.2d): one compact line per such stream, `mi1-diag: <json>`, that
// the CI smoke and the lab turn into a notice annotation (C32), so that the
// next such loss is explained where only annotations can be read. A line
// holds the stream's last segment files with their last parts (name, size,
// SEI indexes), what ipcam-hub and MediaMTX said before the stop, the stop
// moment and the send time of the last recorded access unit (the
// controller's clock), and the kill (the host's); in the lab both clocks are
// the runner's. It names no host, address or user path (PH0-22): file names
// are relative to the stream's folder, and the log lines are redacted.

// DiagPrefix starts each diagnostics line.
const DiagPrefix = "mi1-diag: "

const (
	// maxDiagBytes bounds a line after annotate.sh's escaping (C32).
	maxDiagBytes = 3500
	// diagFiles, diagParts, diagEvents and diagLogLines are how many of the
	// newest files, parts per file, ipcam-hub events and log lines a line
	// shows at most; diagLineRunes bounds each log line or event.
	diagFiles     = 2
	diagParts     = 3
	diagEvents    = 8
	diagLogLines  = 8
	diagLineRunes = 160
)

// diagTime is the format of the times of a diagnostics line, UTC.
const diagTime = "15:04:05.000"

// cutTrace is what recspike saw of ipcam-hub around a run's stop, in memory
// only: its newest events, the newest status apart, and the kill with the
// size of the run's ipcam-hub.log then, the first child's lines.
type cutTrace struct {
	mu      sync.Mutex
	events  []string
	status  string
	killAt  time.Time
	logSize int64
}

// event keeps e, a hubproc.Options.OnEvent, until the kill: the restarted
// child's events come after the stop.
func (t *cutTrace) event(e hubproc.Event) {
	line := e.At.UTC().Format(diagTime) + " " + e.Type
	switch e.Type {
	case "status":
		var s struct {
			LastPartAgoMs *int64 `json:"lastPartAgoMs"`
		}
		if json.Unmarshal(e.Line, &s) == nil && s.LastPartAgoMs != nil {
			line += " lastPartAgoMs=" + strconv.FormatInt(*s.LastPartAgoMs, 10)
		}
	default:
		if e.Message != "" {
			line += " " + e.Message
		}
	}
	line = redact(line)
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.killAt.IsZero() {
		return
	}
	if e.Type == "status" {
		t.status = line
		return
	}
	t.events = append(t.events, line)
	if len(t.events) > diagEvents {
		t.events = t.events[len(t.events)-diagEvents:]
	}
}

// kill notes the kill of ipcam-hub at at, when its log held logSize bytes.
func (t *cutTrace) kill(at time.Time, logSize int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.killAt, t.logSize = at, logSize
}

// snapshot returns the events, the newest status last, the kill and the
// log's size then.
func (t *cutTrace) snapshot() (events []string, killAt time.Time, logSize int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	events = slices.Clone(t.events)
	if t.status != "" {
		events = append(events, t.status)
	}
	return events, t.killAt, t.logSize
}

// lossDiag is one diagnostics line.
type lossDiag struct {
	Code     string     `json:"code"`
	RunID    string     `json:"runId"`
	Stream   string     `json:"stream"`
	LossMs   int64      `json:"lossMs"`
	LimitMs  int64      `json:"limitMs"`
	StopAt   string     `json:"stopAt,omitempty"`
	LastSent string     `json:"lastSent,omitempty"`
	Kill     string     `json:"kill,omitempty"`
	Files    []diagFile `json:"files"`
	Events   []string   `json:"events,omitempty"`
	Log      []string   `json:"log,omitempty"`
}

// diagFile is one of the stream's segment files at the stop, read after
// reconcile: its name as listed, its size, and its last complete parts.
type diagFile struct {
	Name  string           `json:"name"`
	Size  int64            `json:"size"`
	Parts []lastindex.Part `json:"parts,omitempty"`
	Err   string           `json:"err,omitempty"`
}

// lossDiags returns the diagnostics lines of the streams of v that lost
// more than their limit; none when no stream did.
func (d *Driver) lossDiags(cur *CurrentRun, vol Volume, v link.Verdict, id age.Identity) []string {
	var lines []string
	for _, s := range v.Run.Streams {
		if !OverLimit(s) {
			continue
		}
		diag := lossDiag{Code: "mi1-diag", RunID: v.Run.RunID, Stream: s.Stream, LossMs: s.LossMs, LimitMs: s.LimitMs,
			Files: describeFiles(vol.DataDir(), s.Stream, cur.Before[s.Stream], id)}
		if v.Timing != nil {
			diag.StopAt = time.UnixMilli(v.Timing.StopAtMs).UTC().Format(diagTime)
			if sent, ok := v.Timing.LastSentMs[s.Stream]; ok {
				diag.LastSent = time.UnixMilli(sent).UTC().Format(diagTime)
			}
		}
		logSize := int64(-1) // the whole log: no kill was seen
		if cur.trace != nil {
			var killAt time.Time
			diag.Events, killAt, logSize = cur.trace.snapshot()
			if !killAt.IsZero() {
				diag.Kill = killAt.UTC().Format(diagTime)
			}
		}
		diag.Log = logTail(filepath.Join(d.runDir(cur.RunID), "ipcam-hub.log"), logSize)
		lines = append(lines, diag.line())
	}
	return lines
}

// line is the diagnostics as one line, within maxDiagBytes after
// annotate.sh's escaping: the oldest log line or event goes first, from the
// longer of the two lists, then the oldest parts.
func (l lossDiag) line() string {
	for {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false) // <url>, <ip> and <path> as they read
		_ = enc.Encode(l)
		line := DiagPrefix + strings.TrimSuffix(b.String(), "\n")
		if escapedLen(strings.TrimPrefix(line, DiagPrefix)) <= maxDiagBytes {
			return line
		}
		switch {
		case len(l.Log) > 0 && len(l.Log) >= len(l.Events):
			l.Log = l.Log[1:]
		case len(l.Events) > 0:
			l.Events = l.Events[1:]
		default:
			l.Files = trimOldest(l.Files)
			if len(l.Files) == 0 {
				return DiagPrefix + `{"code":"mi1-diag","runId":` + jsonString(l.RunID) + `}`
			}
		}
	}
}

// trimOldest drops the oldest part of the files, and a file left without
// parts.
func trimOldest(files []diagFile) []diagFile {
	if len(files) == 0 {
		return nil
	}
	f := files[0]
	if len(f.Parts) > 0 {
		f.Parts = f.Parts[1:]
		return append([]diagFile{f}, files[1:]...)
	}
	return files[1:]
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// escapedLen is the length of s as annotate.sh writes it: each % is %25.
func escapedLen(s string) int { return len(s) + 2*strings.Count(s, "%") }

// describeFiles reads the newest diagFiles files of the stream's files at
// the stop (listed relative to the data directory, the .part names under
// the final names that reconcile gave them), newest last, each with its
// newest diagParts parts.
func describeFiles(dataDir, stream string, listed []string, id age.Identity) []diagFile {
	cam, profile := jobOf(stream)
	streamDir := path.Join("rec", cam, profile) + "/"
	sorted := slices.Clone(listed)
	slices.SortFunc(sorted, func(a, b string) int {
		return strings.Compare(strings.TrimSuffix(a, ".part"), strings.TrimSuffix(b, ".part"))
	})
	if len(sorted) > diagFiles {
		sorted = sorted[len(sorted)-diagFiles:]
	}
	var out []diagFile
	for _, rel := range sorted {
		f := diagFile{Name: strings.TrimPrefix(rel, streamDir)}
		size, parts, err := lastindex.ReadParts(filepath.Join(dataDir, filepath.FromSlash(strings.TrimSuffix(rel, ".part"))), id)
		f.Size = size
		if len(parts) > diagParts {
			parts = parts[len(parts)-diagParts:]
		}
		f.Parts = parts
		if err != nil {
			f.Err = redact(err.Error())
		}
		out = append(out, f)
	}
	return out
}

// logTail returns the last diagLogLines warnings and errors of MediaMTX and
// ipcam-hub in the first size bytes of the log at path (all of it when size
// is negative), redacted.
func logTail(path string, size int64) []string {
	f, err := os.Open(path) //nolint:gosec // the run's own log
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var r io.Reader = f
	if size >= 0 {
		r = io.LimitReader(f, size)
	}
	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(" WAR ")) && !bytes.Contains(line, []byte(" ERR ")) && !bytes.HasPrefix(line, []byte("ipcam-hub")) {
			continue
		}
		lines = append(lines, redact(strings.TrimPrefix(string(line), "mediamtx: ")))
		if len(lines) > diagLogLines {
			lines = lines[1:]
		}
	}
	return lines
}

// The parts of a log line or an error that could name the host or its user
// (PH0-22): a URL (credentials and address), an IPv4 address, an absolute
// path.
var (
	redactURL  = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://\S*`)
	redactIP   = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b`)
	redactPath = regexp.MustCompile(`(^|[\s"'(=])(?:[A-Za-z]:)?[\\/][^\s"')]*`)
)

// redact removes what could name the host or its user from s, and bounds it
// to diagLineRunes.
func redact(s string) string {
	s = redactURL.ReplaceAllString(s, "<url>")
	s = redactIP.ReplaceAllString(s, "<ip>")
	s = redactPath.ReplaceAllString(s, "${1}<path>")
	if r := []rune(s); len(r) > diagLineRunes {
		s = string(r[:diagLineRunes])
	}
	return s
}
