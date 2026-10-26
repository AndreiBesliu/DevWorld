package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// labEnvelope is reccontroller's envelope of a lab run: run index of set,
// its attempt, counted, encrypted on odd indexes, with cam99_main in run 9
// (setting on) and run 21 (the O17-off run).
func labEnvelope(set string, index, attempt int) results.Envelope {
	id := fmt.Sprintf("%s-%02d-%d", set, index, attempt)
	encrypted, pad := index%2 == 1, index != 21
	resume := int64(1180)
	run := results.Run{
		RunID: id, Valid: true, Attempt: attempt, ShutdownKind: "kill9", Encrypted: encrypted, PadLowBitrate: pad,
		Streams: []results.StreamLoss{{Stream: "cam01_main", BitrateKbps: 4000, Encrypted: encrypted, PadLowBitrate: pad, LossMs: 640, LimitMs: 2000}},
		LossMs:  640, ErrBoundMs: 52, Reconciled: true, Hub04Clear: true, PacingP99Ms: 4, ResumeMs: &resume, Cameras: 1,
		NtpSynced: true, WriteCache: "unknown",
	}
	if index == 9 || index == 21 {
		limit := int64(2000)
		if !pad {
			limit = 4000
		}
		run.Streams = append(run.Streams, results.StreamLoss{Stream: "cam99_main", BitrateKbps: 256, Encrypted: true, PadLowBitrate: pad, LossMs: 1900, LimitMs: limit})
		run.Cameras, run.LossMs = 2, 1900
	}
	return results.Envelope{Schema: results.Schema, Code: "MI1", RunID: id, Set: set, Host: "lab-linux", OS: "linux", Storage: "unknown", Runs: []results.Run{run}}
}

// writeEnvelopes writes envs as reccontroller does and returns their files.
func writeEnvelopes(t *testing.T, envs ...results.Envelope) []string {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for _, e := range envs {
		if err := results.Write(dir, e); err != nil {
			t.Fatal(err)
		}
		files = append(files, filepath.Join(dir, e.Code, e.Set, e.RunID+".json"))
	}
	return files
}

// shard is the envelopes of one lab shard of set: runs from to to, each
// counted at its first attempt.
func shard(set string, from, to int) []results.Envelope {
	var envs []results.Envelope
	for i := from; i <= to; i++ {
		envs = append(envs, labEnvelope(set, i, 1))
	}
	return envs
}

// recspikeOut runs recspike with args and returns what it printed.
func recspikeOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := run(t.Context(), args, nil, &out)
	return out.String(), err
}

// split runs `recspike annotate split <files>… --first <first> --out <dir>`,
// the files before the flags as the lab workflow passes its glob.
func split(t *testing.T, first int, files []string) (string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "lines")
	args := append(append([]string{"annotate", "split"}, files...), "--first", fmt.Sprint(first), "--out", out)
	printed, err := recspikeOut(t, args...)
	if err != nil {
		return out, fmt.Errorf("%w\n%s", err, printed)
	}
	return out, nil
}

// emit returns the lines of `recspike annotate emit <dir> --batch <k>`.
func emit(t *testing.T, dir string, batch int) []string {
	t.Helper()
	printed, err := recspikeOut(t, "annotate", "emit", dir, "--batch", fmt.Sprint(batch))
	if err != nil {
		t.Fatalf("annotate emit --batch %d: %v\n%s", batch, err, printed)
	}
	if printed == "" {
		return nil
	}
	if !strings.HasSuffix(printed, "\n") {
		t.Fatalf("annotate emit --batch %d: the output does not end with a line break: %q", batch, printed)
	}
	return strings.Split(strings.TrimSuffix(printed, "\n"), "\n")
}

// escaped is a line as tools/scripts/annotate.sh writes it into the
// workflow command: "%" as "%25", a line break as "%0A", carriage returns
// dropped.
func escaped(line string) string {
	line = strings.ReplaceAll(line, "\r", "")
	line = strings.ReplaceAll(line, "%", "%25")
	return strings.ReplaceAll(line, "\n", "%0A")
}

// unescaped is a notice's message as the runner reads the workflow command
// that annotate.sh wrote, and as `gh api …/annotations` returns it.
func unescaped(command string) string {
	return strings.NewReplacer("%0D", "\r", "%0A", "\n", "%25", "%").Replace(command)
}

// TestAnnotate_SplitsAt3500Bytes (C32): the 11 runs of shard 2 become 11
// notice lines, each at most 3,500 bytes after annotate.sh's escaping and
// each a valid one-run envelope with Part 11 to 21 and Parts 21, in the
// order of the set; a run that does not count, redone at its position, is
// not a part. `emit --batch 1` prints the first 10 lines and `--batch 2`
// the 11th, so that no step passes GitHub's 10 notices. A run whose line
// would pass 3,500 bytes, `%` escaped included, makes split fail, and so
// does a shard of 21 runs.
func TestAnnotate_SplitsAt3500Bytes(t *testing.T) {
	const set = "lab-linux-kill9"
	envs := shard(set, 11, 21)
	redone := labEnvelope(set, 14, 1) // run 14's first attempt did not count
	redone.Runs[0].Valid, redone.Runs[0].InvalidReason = false, "oprire în afara ferestrei"
	envs[3] = labEnvelope(set, 14, 2)
	dir, err := split(t, 11, writeEnvelopes(t, append(envs, redone)...))
	if err != nil {
		t.Fatalf("annotate split: %v", err)
	}
	first, second := emit(t, dir, 1), emit(t, dir, 2)
	if len(first) != 10 || len(second) != 1 {
		t.Fatalf("emit --batch 1 printed %d lines and --batch 2 %d, want 10 and 1", len(first), len(second))
	}
	if third := emit(t, dir, 3); len(third) != 0 {
		t.Errorf("emit --batch 3 printed %d lines, want none", len(third))
	}
	for i, line := range append(first, second...) {
		if n := len(escaped(line)); n > 3500 {
			t.Errorf("line %d is %d bytes after escaping, over 3,500", i+1, n)
		}
		e, err := results.Parse([]byte(line))
		if err != nil {
			t.Errorf("line %d: %v", i+1, err)
			continue
		}
		want := envs[i]
		want.Part, want.Parts = 11+i, 21
		if !reflect.DeepEqual(e, want) {
			t.Errorf("line %d = %+v\nwant %+v", i+1, e, want)
		}
	}

	t.Run("a line over 3,500 bytes", func(t *testing.T) {
		big := labEnvelope(set, 11, 1)
		for len(big.Runs[0].Streams) < 30 {
			big.Runs[0].Streams = append(big.Runs[0].Streams, big.Runs[0].Streams[0])
		}
		if _, err := split(t, 11, writeEnvelopes(t, big)); err == nil || !strings.Contains(err.Error(), "3500") {
			t.Errorf("annotate split of a run of %d streams = %v, want a refusal that names 3500 bytes", len(big.Runs[0].Streams), err)
		}
	})
	t.Run("a line over 3,500 bytes only once escaped", func(t *testing.T) {
		pct := labEnvelope(set, 11, 1)
		data, err := json.Marshal(pct)
		if err != nil {
			t.Fatal(err)
		}
		pct.Runs[0].WriteCache = strings.Repeat("%", (3500-len(data))/2+50)
		if data, err = json.Marshal(pct); err != nil || len(data) > 3500 {
			t.Fatalf("the envelope is %d bytes before escaping, want at most 3,500 (%v)", len(data), err)
		}
		if _, err := split(t, 11, writeEnvelopes(t, pct)); err == nil || !strings.Contains(err.Error(), "3500") {
			t.Errorf("annotate split of a run of %d bytes, more once escaped = %v, want a refusal that names 3500 bytes", len(data), err)
		}
	})
	t.Run("a shard of 21 runs", func(t *testing.T) {
		if _, err := split(t, 1, writeEnvelopes(t, shard(set, 1, 21)...)); err == nil || !strings.Contains(err.Error(), "20") {
			t.Errorf("annotate split of 21 runs = %v, want a refusal that names the 20 runs of a shard", err)
		}
	})
}

// ghAnnotations is what `gh api repos/<owner>/<repo>/check-runs/<id>/annotations
// --paginate` prints for a shard's job: its notices, whose messages the
// runner unescaped from annotate.sh's workflow commands, and the job's
// other annotations.
func ghAnnotations(t *testing.T, title string, lines []string) string {
	t.Helper()
	var page []map[string]any
	add := func(level, title, message string) {
		page = append(page, map[string]any{
			"path": ".github", "blob_href": "https://github.com/AndreiBesliu/DevWorld/blob/x/.github",
			"start_line": 1, "start_column": nil, "end_line": 1, "end_column": nil,
			"annotation_level": level, "title": title, "message": message, "raw_details": nil,
		})
	}
	for _, line := range lines {
		add("notice", title, unescaped(escaped(line)))
	}
	add("notice", "mi1-diag", `{"run":"x","stream":"cam01_main"}`)
	add("warning", "Node.js 20 actions are deprecated", "upgrade")
	data, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

// TestReadAnnotations_RoundTrip (C32, G15): the lines that split writes,
// escaped by annotate.sh and unescaped by the runner into notices, come
// back from results.ReadAnnotations as the same envelopes; `annotate read`
// reports lab-linux-kill9: 21/21 for both shards' annotations together,
// and refuses a part that appears twice, a part outside 1..Parts, an
// envelope of more than one run, and annotations that do not parse.
func TestReadAnnotations_RoundTrip(t *testing.T) {
	const set = "lab-linux-kill9"
	ann := t.TempDir()
	var want []results.Envelope
	var jobs []string
	for i, sh := range []struct{ first, last int }{{1, 10}, {11, 21}} {
		dir, err := split(t, sh.first, writeEnvelopes(t, shard(set, sh.first, sh.last)...))
		if err != nil {
			t.Fatalf("annotate split of shard %d: %v", i+1, err)
		}
		lines := append(emit(t, dir, 1), emit(t, dir, 2)...)
		job := ghAnnotations(t, set, lines)
		jobs = append(jobs, job)
		if err := os.WriteFile(filepath.Join(ann, fmt.Sprintf("12345678%d.json", i)), []byte(job), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			e, err := results.Parse([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, e)
		}

		envs, notices, err := results.ReadAnnotations(strings.NewReader(job))
		if err != nil {
			t.Fatalf("ReadAnnotations of shard %d: %v", i+1, err)
		}
		if !reflect.DeepEqual(envs, want[len(want)-len(lines):]) || len(notices) != 1 {
			t.Errorf("ReadAnnotations of shard %d = %d envelopes, %d notices; want the %d split lines and the mi1-diag notice", i+1, len(envs), len(notices), len(lines))
		}
	}
	if len(want) != 21 {
		t.Fatalf("%d lines in the two shards, want 21", len(want))
	}

	printed, err := recspikeOut(t, "annotate", "read", ann)
	if err != nil || printed != set+": 21/21\n" {
		t.Errorf("annotate read = %v, printed %q; want %q", err, printed, set+": 21/21\n")
	}
	one := filepath.Join(ann, "123456781.json")
	if printed, err := recspikeOut(t, "annotate", "read", one); err != nil || printed != set+": 11/21\n" {
		t.Errorf("annotate read of shard 2's file = %v, printed %q; want %q", err, printed, set+": 11/21\n")
	}

	refused := func(name, job, wantErr string) {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for i, j := range append(append([]string{}, jobs...), job) {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("job%d.json", i)), []byte(j), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if printed, err := recspikeOut(t, "annotate", "read", dir); err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Errorf("annotate read = %v, printed %q; want an error with %q", err, printed, wantErr)
			}
		})
	}
	line := func(e results.Envelope) string {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	refused("a part twice", ghAnnotations(t, set, []string{line(want[4])}), "partea 5")
	outside := want[0]
	outside.Part, outside.Parts = 22, 22
	refused("a part outside the set's parts", ghAnnotations(t, set, []string{line(outside)}), "între 1 și 21")
	two := want[0] // part 1, which the envelope of one run holds too
	two.Runs = append(two.Runs, want[1].Runs...)
	refused("an envelope of two runs", ghAnnotations(t, set, []string{line(two)}), "2 rulări")
	refused("annotations that do not parse", "[{", "results")
}
