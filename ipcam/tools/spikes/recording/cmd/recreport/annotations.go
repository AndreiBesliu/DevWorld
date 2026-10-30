// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// repo is the repository whose workflow runs recreport reads.
const repo = "repos/AndreiBesliu/DevWorld/"

// ghWait bounds one gh call, all of its pages.
const ghWait = 2 * time.Minute

// The CI results that are notices, not envelopes (C32), by their titles.
const (
	titleEnc06 = "ENC-06"
	titleMI9   = "mi9"
	titleMtx2  = "mtx2-loss"
)

// ciHost is the host of CI's results: a Linux runner.
const ciHost = "lab-linux"

// job is a job of a workflow run, as the jobs API lists it; its id is its
// check run's.
type job struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Conclusion  string    `json:"conclusion"`
	CompletedAt time.Time `json:"completed_at"`
}

// doneAtMs is when j completed, in Unix ms: the time of the envelopes that
// its annotations carry (C1-R22). A job still running, whose completed_at
// is null, gives 0, no time.
func (j job) doneAtMs() int64 {
	if j.CompletedAt.IsZero() {
		return 0
	}
	return j.CompletedAt.UnixMilli()
}

// mtx2Loss is the mtx2-loss line of the recorder job (R01, O17): the loss at
// a cut and the disk overhead per bitrate, padding on and off.
type mtx2Loss struct {
	Code string `json:"code"`
	Cuts int    `json:"cuts"`
	Runs []struct {
		Kbps        int     `json:"kbps"`
		Pad         bool    `json:"pad"`
		MedianMs    int64   `json:"medianMs"`
		MaxMs       int64   `json:"maxMs"`
		OverheadPct float64 `json:"overheadPct"`
	} `json:"runs"`
	Pass bool `json:"pass"`
}

// ciNotices is what CI's notices carried besides envelopes: the mtx2-loss
// lines, kept for the R01 decision, and how many of each other title.
type ciNotices struct {
	mtx2  []mtx2Loss
	other map[string]int
}

// add adds n's notices.
func (c *ciNotices) add(n ciNotices) {
	c.mtx2 = append(c.mtx2, n.mtx2...)
	for title, count := range n.other {
		if c.other == nil {
			c.other = map[string]int{}
		}
		c.other[title] += count
	}
}

// workflowRun is what a workflow run's annotations carried: its envelopes and
// its other notices, with when its last job completed.
type workflowRun struct {
	id      string
	doneAt  time.Time
	envs    []results.Envelope
	notices ciNotices
}

// readRuns reads the workflow runs ids (readRun), the newest first: by when
// their last job completed, then by their id, whatever the order of ids, so
// that of two lab runs G10 counts the newer's copy of a run (conflicts).
func readRuns(ids []string) ([]workflowRun, error) {
	runs := make([]workflowRun, 0, len(ids))
	for _, id := range ids {
		r, err := readRun(id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	slices.SortStableFunc(runs, func(a, b workflowRun) int {
		if c := b.doneAt.Compare(a.doneAt); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(len(b.id), len(a.id)), strings.Compare(b.id, a.id))
	})
	return runs, nil
}

// readRun reads the workflow run id's results (C32): it lists the run's
// jobs, then reads each job's check-run annotations, every call through
// `gh api … --paginate`. The envelopes come back as they are, stamped with
// when their job completed when they carry no time (M1); the ENC-06 line
// becomes its envelope (enc06Envelope), the Chrome line of MI9 its own
// (mi9Line), and the other notices are kept or counted.
func readRun(id string) (workflowRun, error) {
	run := workflowRun{id: id}
	out, err := gh(repo + "actions/runs/" + id + "/jobs")
	if err != nil {
		return run, err
	}
	jobs, err := parseJobs(out)
	if err != nil {
		return run, fmt.Errorf("workflow run %s: %w", id, err)
	}
	for _, j := range jobs {
		if j.CompletedAt.After(run.doneAt) {
			run.doneAt = j.CompletedAt
		}
		if j.Conclusion == "skipped" {
			continue
		}
		out, err := gh(repo + "check-runs/" + strconv.FormatInt(j.ID, 10) + "/annotations")
		if err != nil {
			return run, err
		}
		e, n, err := results.ReadAnnotations(bytes.NewReader(out))
		if err != nil {
			return run, fmt.Errorf("workflow run %s, job %q: %w", id, j.Name, err)
		}
		for i := range e {
			if e[i].AtMs == 0 {
				e[i].AtMs = j.doneAtMs()
			}
		}
		run.envs = append(run.envs, e...)
		for _, notice := range n {
			e, err := fromNotice(notice, j, &run.notices)
			if err != nil {
				return run, fmt.Errorf("workflow run %s, job %q: %w", id, j.Name, err)
			}
			if e != nil {
				run.envs = append(run.envs, *e)
			}
		}
	}
	return run, nil
}

// fromNotice turns notice, of job j, into its envelope, or keeps it in
// notices.
func fromNotice(notice results.Notice, j job, notices *ciNotices) (*results.Envelope, error) {
	switch notice.Title {
	case titleEnc06:
		return enc06Envelope(notice.Message, j)
	case titleMI9:
		return mi9Line(notice.Message, j)
	case titleMtx2:
		var m mtx2Loss
		if err := strict(notice.Message, &m); err != nil {
			return nil, fmt.Errorf("the mtx2-loss line: %w", err)
		}
		notices.mtx2 = append(notices.mtx2, m)
		return nil, nil
	}
	if notices.other == nil {
		notices.other = map[string]int{}
	}
	notices.other[notice.Title]++
	return nil, nil
}

// enc06Envelope is the ENC-06 line of job j as the envelope that mi4.Judge
// reads: set ci-enc06 on lab-linux, stamped with the time its check
// completed (C1-R22), its run id the job's.
func enc06Envelope(message string, j job) (*results.Envelope, error) {
	var line struct {
		Code  string   `json:"code"`
		Tools []string `json:"tools"`
		Files int      `json:"files"`
		Pass  *bool    `json:"pass"`
	}
	if err := strict(message, &line); err != nil || line.Code != "ENC-06" || line.Pass == nil {
		return nil, fmt.Errorf("the ENC-06 line is not ENC-06's result: %v", err)
	}
	e := results.Envelope{Schema: results.Schema, Code: "ENC-06", RunID: "ci-enc06-" + strconv.FormatInt(j.ID, 10), AtMs: j.doneAtMs(),
		Set: "ci-enc06", Host: ciHost, OS: "linux", MI4: &results.Mi4Data{Enc06: line.Pass}}
	return &e, e.Validate()
}

// mi9Line is an mi9 notice of job j that is no envelope, told by its
// transport: "chrome" is the Chrome line (chromeEnvelope), whatever the
// order of its keys. Any other shape is an error, as for ENC-06 and
// mtx2-loss: a changed line would otherwise leave G18 without its Chrome
// part, silently.
func mi9Line(message string, j job) (*results.Envelope, error) {
	var probe struct {
		Transport string `json:"transport"`
	}
	if err := json.Unmarshal([]byte(message), &probe); err != nil {
		return nil, fmt.Errorf("an mi9 line that is neither an envelope nor the Chrome line: %w", err)
	}
	if probe.Transport != string(mi9.Chrome) {
		return nil, fmt.Errorf("an mi9 line that is neither an envelope nor the Chrome line: transport %q", probe.Transport)
	}
	return chromeEnvelope(message, j)
}

// chromeEnvelope is MI9's Chrome line of job j as an envelope with one leg
// of source ci-chrome (G26), its run id the job's, stamped with when the
// job completed.
func chromeEnvelope(message string, j job) (*results.Envelope, error) {
	var line struct {
		Transport string    `json:"transport"`
		DisplayMs []float64 `json:"displayMs"`
		SeekMs    []float64 `json:"seekMs"`
	}
	if err := strict(message, &line); err != nil {
		return nil, fmt.Errorf("the mi9 Chrome line: %w", err)
	}
	e := results.Envelope{Schema: results.Schema, Code: "MI9", RunID: "ci-chrome-" + strconv.FormatInt(j.ID, 10), AtMs: j.doneAtMs(), Set: mi9.SourceChrome,
		Host: ciHost, OS: "linux", MI9: &results.Mi9Data{Legs: []results.Mi9Leg{{Transport: line.Transport, SeekMs: line.SeekMs,
			DisplayMs: line.DisplayMs, Source: mi9.SourceChrome}}}}
	return &e, e.Validate()
}

// strict decodes message into v, refusing a field that v lacks: a line
// whose format changed is an error, not a silent gap.
func strict(message string, v any) error {
	dec := json.NewDecoder(strings.NewReader(message))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("data after the line")
	}
	return nil
}

// parseJobs reads the pages of the jobs API that gh --paginate prints, one
// JSON object after another.
func parseJobs(out []byte) ([]job, error) {
	var jobs []job
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var page struct {
			Jobs []job `json:"jobs"`
		}
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the jobs: %w", err)
		}
		jobs = append(jobs, page.Jobs...)
	}
	slices.SortFunc(jobs, func(a, b job) int { return compareInt(a.ID, b.ID) })
	return jobs, nil
}

// gh runs `gh api <path> --paginate` and returns what it printed. Job logs
// and artifacts are refused here (403); check-run annotations and the
// jobs list are readable (C32).
func gh(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghWait)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", "api", path, "--paginate") //nolint:gosec // a fixed API path with a run or job id of digits
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
