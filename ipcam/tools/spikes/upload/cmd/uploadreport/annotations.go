// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bytes"
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
)

// repo is the repository whose workflow runs uploadreport reads.
const repo = "repos/AndreiBesliu/DevWorld/"

// ghWait bounds one gh call, all of its pages.
const ghWait = 2 * time.Minute

// titleMI5Lab is the title of the lab's notices: each one's message is one
// raw envelope that uploadspike annotate emit printed (Task 8).
const titleMI5Lab = "mi5-lab"

// job is a job of a workflow run, as the jobs API lists it; its id is its
// check run's.
type job struct {
	ID          int64     `json:"id"`
	Conclusion  string    `json:"conclusion"`
	CompletedAt time.Time `json:"completed_at"`
}

// annotation is the part of a check-run annotation that uploadreport reads.
type annotation struct {
	Level   string `json:"annotation_level"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// readRun reads workflow run id's mi5-lab notices (C32, P09): it lists the
// run's jobs, then reads each job's check-run annotations, each call
// through `gh api … --paginate`. Each mi5-lab notice is a lab-linux MI5
// envelope, whose AtMs is its job's completed_at (0 while null); one that
// is not is listed as unreadable. Other notices and failures are no
// results.
func readRun(id string) ([]read, []skipped, error) {
	out, err := gh(repo + "actions/runs/" + id + "/jobs")
	if err != nil {
		return nil, nil, err
	}
	jobs, err := parseJobs(out)
	if err != nil {
		return nil, nil, fmt.Errorf("rularea CI %s: %w", id, err)
	}
	var reads []read
	var skip []skipped
	for _, j := range jobs {
		if j.Conclusion == "skipped" {
			continue
		}
		out, err := gh(repo + "check-runs/" + strconv.FormatInt(j.ID, 10) + "/annotations")
		if err != nil {
			return nil, nil, err
		}
		as, err := parseAnnotations(out)
		if err != nil {
			return nil, nil, fmt.Errorf("rularea CI %s, job %d: %w", id, j.ID, err)
		}
		from := fmt.Sprintf("rulare CI %s, job %d", id, j.ID)
		for _, a := range as {
			if a.Level != "notice" || a.Title != titleMI5Lab {
				continue
			}
			e, reason := parseEnvelope([]byte(a.Message))
			if reason == "" && (e.Code != "MI5" || e.Host != "lab-linux") {
				reason = reasonNotLab
			}
			if reason != "" {
				skip = append(skip, skipped{from, reason})
				continue
			}
			e.AtMs = 0
			if !j.CompletedAt.IsZero() {
				e.AtMs = j.CompletedAt.UnixMilli()
			}
			reads = append(reads, read{env: e, from: from, size: int64(len(a.Message))})
		}
	}
	return reads, skip, nil
}

// parseJobs reads the pages of the jobs API that gh --paginate prints, one
// JSON object after another, sorted by id.
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
			return nil, fmt.Errorf("lista joburilor: %w", err)
		}
		jobs = append(jobs, page.Jobs...)
	}
	slices.SortFunc(jobs, func(a, b job) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	return jobs, nil
}

// parseAnnotations reads the pages of the annotations API that gh
// --paginate prints: one JSON array per page.
func parseAnnotations(out []byte) ([]annotation, error) {
	var all []annotation
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var page []annotation
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			return all, nil
		}
		if err != nil {
			return nil, fmt.Errorf("adnotările: %w", err)
		}
		all = append(all, page...)
	}
}

// gh runs `gh api <path> --paginate` and returns what it printed. Job logs
// and artifacts are refused here (403); check-run annotations and the jobs
// list are readable (C32).
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
