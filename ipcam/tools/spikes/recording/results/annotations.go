// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Notice is a notice annotation that is not an envelope: the ENC-06,
// mtx2-loss, diag-host and mi9 lines, by their titles.
type Notice struct{ Title, Message string }

// annotation is the part of a check-run annotation that ReadAnnotations
// reads.
type annotation struct {
	Level   string `json:"annotation_level"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// ReadAnnotations parses what `gh api repos/<owner>/<repo>/check-runs/<id>/annotations
// --paginate` prints: one JSON array of annotations per page, or one array
// of those arrays (--slurp). Each notice whose message names Schema is an
// envelope, which must parse and validate: an envelope that GitHub cut
// short is an error, never an ordinary notice. Every other notice comes
// back by its title; failures and warnings are skipped.
func ReadAnnotations(r io.Reader) ([]Envelope, []Notice, error) {
	var envs []Envelope
	var notices []Notice
	dec := json.NewDecoder(r)
	for {
		var page []json.RawMessage
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			return envs, notices, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("results: annotations: %w", err)
		}
		as, err := annotations(page)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range as {
			switch {
			case a.Level != "notice":
			case strings.Contains(a.Message, Schema):
				e, err := Parse([]byte(a.Message))
				if err != nil {
					return nil, nil, fmt.Errorf("results: the notice %q holds no valid envelope: %w", a.Title, err)
				}
				envs = append(envs, e)
			default:
				notices = append(notices, Notice{Title: a.Title, Message: a.Message})
			}
		}
	}
}

// annotations decodes a page: the annotations themselves, or, slurped, the
// arrays of the pages.
func annotations(page []json.RawMessage) ([]annotation, error) {
	var out []annotation
	for _, raw := range page {
		if inner := strings.TrimSpace(string(raw)); strings.HasPrefix(inner, "[") {
			var as []annotation
			if err := json.Unmarshal(raw, &as); err != nil {
				return nil, fmt.Errorf("results: annotations: %w", err)
			}
			out = append(out, as...)
			continue
		}
		var a annotation
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("results: annotations: %w", err)
		}
		out = append(out, a)
	}
	return out, nil
}
