package results

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ghAnnotation is one element of `gh api …/check-runs/<id>/annotations`.
func ghAnnotation(t *testing.T, level, title, message string) string {
	t.Helper()
	return mustMarshal(t, map[string]any{
		"path": ".github", "blob_href": "https://github.com/AndreiBesliu/DevWorld/blob/x/.github",
		"start_line": 1, "start_column": nil, "end_line": 1, "end_column": nil,
		"annotation_level": level, "title": title, "message": message, "raw_details": nil,
	})
}

// labRun is a one-run lab envelope, as `recspike annotate split` writes it.
func labRun(part int) Envelope {
	e := sample()
	e.Host, e.Set, e.OS, e.Storage, e.PiModel, e.SdConfirmation = "lab-linux", "lab-linux-kill9", "linux", "unknown", nil, nil
	e.RunID = fmt.Sprintf("lab-linux-kill9-%02d", part)
	e.Runs = e.Runs[:1]
	e.Runs[0].RunID = e.RunID
	e.Part, e.Parts = part, 21
	return e
}

// TestReadAnnotations_Notices: ReadAnnotations reads the JSON that `gh api
// …/annotations --paginate` prints, one array per page: the notices whose
// message is an envelope come back as envelopes, in order, and every other
// notice by its title; failures and warnings are not notices.
func TestReadAnnotations_Notices(t *testing.T) {
	first, second := labRun(1), labRun(2)
	page1 := "[" + strings.Join([]string{
		ghAnnotation(t, "notice", "lab-linux-kill9", mustMarshal(t, first)),
		ghAnnotation(t, "notice", "ENC-06", `{"ageCli":true,"rage":true}`),
		ghAnnotation(t, "failure", "test", "--- FAIL: TestX\n"+mustMarshal(t, second)),
	}, ",") + "]"
	page2 := "[" + strings.Join([]string{
		ghAnnotation(t, "warning", "Node.js 20 is deprecated", "upgrade"),
		ghAnnotation(t, "notice", "lab-linux-kill9", mustMarshal(t, second)),
		ghAnnotation(t, "notice", "mtx2-loss", "4 Mbps: max 1180 ms"),
	}, ",") + "]"

	for name, input := range map[string]string{
		"two pages":           page1 + "\n" + page2 + "\n",
		"two pages, slurped":  "[" + page1 + "," + page2 + "]",
		"pages without space": page1 + page2,
	} {
		t.Run(name, func(t *testing.T) {
			envs, notices, err := ReadAnnotations(strings.NewReader(input))
			if err != nil {
				t.Fatalf("ReadAnnotations: %v", err)
			}
			if !reflect.DeepEqual(envs, []Envelope{first, second}) {
				t.Errorf("envelopes = %+v\nwant the two lab runs", envs)
			}
			want := []Notice{{Title: "ENC-06", Message: `{"ageCli":true,"rage":true}`}, {Title: "mtx2-loss", Message: "4 Mbps: max 1180 ms"}}
			if !reflect.DeepEqual(notices, want) {
				t.Errorf("notices = %+v, want %+v", notices, want)
			}
		})
	}

	t.Run("no annotations", func(t *testing.T) {
		envs, notices, err := ReadAnnotations(strings.NewReader("[]\n"))
		if err != nil || len(envs) != 0 || len(notices) != 0 {
			t.Errorf("ReadAnnotations = %d envelopes, %d notices, %v; want none", len(envs), len(notices), err)
		}
	})

	for name, input := range map[string]string{
		// GitHub cut an annotation of ≈ 4,000 characters (0e9ae3d): an envelope
		// that lost its end must not read as an ordinary notice.
		"a cut envelope":     "[" + ghAnnotation(t, "notice", "lab-linux-kill9", mustMarshal(t, first)[:200]) + "]",
		"an invalid host":    "[" + ghAnnotation(t, "notice", "lab-linux-kill9", strings.Replace(mustMarshal(t, first), `"lab-linux"`, `"runnervmx"`, 1)) + "]",
		"not JSON":           "[{",
		"not an array":       `{"message":"Not Found"}`,
		"an array of arrays": "[[" + ghAnnotation(t, "notice", "x", "y") + "],[[]]]",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ReadAnnotations(strings.NewReader(input)); err == nil {
				t.Error("ReadAnnotations = nil error")
			}
		})
	}
}
