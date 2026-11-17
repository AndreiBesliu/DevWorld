// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// runResults closes the open MI6 run's results now (its envelope is
// rewritten at day 7), runs ipcam-hub's known-secrets scan over results/
// (P45; elevated on Windows, C2-R20) right before it zips results/ into
// results-<host>.zip in the current folder, also when no run is open any
// more; under sudo the zip goes to SUDO_USER. A scan that moved or could
// not move a file fails the run; one that leaves a file it found in
// results/, or could not scan it, stops the zip.
func runResults(ctx context.Context, e *env, args []string) error {
	if err := cli.ParseFlags(flag.NewFlagSet("results", flag.ContinueOnError), args, e.out, usage); err != nil {
		return err
	}
	if !isElevated() {
		return ownerError(msgNeedsElevation)
	}
	host, info := hostName(ctx, e)
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return err
	}
	var rewrite func() error
	if raw, ok := st.Open["MI6"]; ok {
		run, err := mi6.Decode(raw)
		if err != nil {
			return err
		}
		if err := writeMI6(ctx, e, run, st, host, info); err != nil {
			return err
		}
		e.printf(msgResultsWritten)
		rewrite = func() error { return rewriteMI6(ctx, e, host, info) }
	}
	failed, last := closingScan(ctx, e, nil, rewrite)
	if last != nil && !last.clean {
		e.printf(msgResultsNotZipped)
		return exitError(1)
	}
	zipPath, err := zipResults(e, host)
	if err != nil {
		return err
	}
	e.printf(msgResultsZip, zipPath)
	if failed {
		return exitError(1)
	}
	return nil
}

// closingScan runs the known-secrets scan over results/ with tokens. When
// it fails, the open MI6 run fails with the scan's fixed reason, rewrite
// (nil: none) writes the run's envelope again, and the scan runs once
// more, so the last write of results/ is always scanned (PH0-22). It
// reports whether a scan failed, and the last scan's failure, nil when
// results/ came out clean.
func closingScan(ctx context.Context, e *env, tokens []string, rewrite func() error) (bool, *scanFailure) {
	f := scanResults(ctx, e, tokens)
	if f == nil {
		return false, nil
	}
	e.printf("%s", f.line)
	failMI6(e, f.reason)
	if rewrite == nil {
		return true, f
	}
	if err := rewrite(); err != nil {
		return true, &scanFailure{line: fmt.Sprintf("%s %v", msgError, err)}
	}
	if last := scanResults(ctx, e, tokens); last != nil {
		e.printf("%s", last.line)
		return true, last
	}
	return true, nil
}

// rewriteMI6 writes the open MI6 run's envelope again, from the run state.
func rewriteMI6(ctx context.Context, e *env, host string, info *hostinfo.Host) error {
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return err
	}
	run, err := mi6.Decode(st.Open["MI6"])
	if err != nil {
		return err
	}
	return writeMI6(ctx, e, run, st, host, info)
}

// writeMI6 writes the run's envelope into results/, now.
func writeMI6(ctx context.Context, e *env, run *mi6.Run, st *runstate.State, host string, info *hostinfo.Host) error {
	meta := mi6.Meta{Host: host, Set: host, OS: runtime.GOOS, SdConfirmation: st.Sd, Sealer: "software", Storage: "unknown"}
	if runtime.GOOS == "windows" {
		meta.Sealer = "dpapi-machine"
	}
	if info == nil {
		if h, err := hostinfo.Read(ctx, e.hubBin, recDir(e)); err == nil {
			info = &h
		}
	}
	if info != nil {
		meta.PiModel, meta.Storage = info.PiModel, info.Storage
	}
	return results.Write(e.resultsDir(), run.Envelope(meta, st.Gaps, st.Handoffs, e.now().UnixMilli()))
}

// scanLine is diag scan-secrets' line.
type scanLine struct {
	Schema     string   `json:"schema"`
	Moved      []string `json:"moved"`
	Unmoved    []string `json:"unmoved"`
	Unreadable []string `json:"unreadable"`
	Quarantine string   `json:"quarantine"`
}

// scanFailure is a failed known-secrets scan (C2-R20): line is the
// owner's terminal line, which may name the quarantine's folder on this
// device; reason is the run's and its envelope's, fixed and path-free
// (PH0-22: on Windows the base is under the owner's profile folder).
type scanFailure struct {
	line, reason string
	// clean is set when results/ holds no file that the scan found or
	// could not read: each one moved to the quarantine, so the folder may
	// be zipped.
	clean bool
}

// scanResults runs `ipcam-hub diag scan-secrets --config <run.json> --dir
// <results>` as uploadspike's own identity, with values and the handover
// token on its stdin; the hub adds every client ID, client secret and
// refresh token that it holds sealed. --dir is always the results root.
// A non-empty moved or unmoved, or a failed scan, is its failure; nil when
// results/ is clean.
func scanResults(ctx context.Context, e *env, values []string) *scanFailure {
	unrun := func(err error) *scanFailure {
		return &scanFailure{line: fmt.Sprintf(msgScanError, err), reason: "scan-secrets: did not run"}
	}
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return unrun(err)
	}
	if st.HandoverToken != "" {
		values = append(values, st.HandoverToken)
	}
	if err := os.MkdirAll(e.resultsDir(), 0o700); err != nil {
		return unrun(err)
	}
	var stdin bytes.Buffer
	for _, v := range values {
		b, _ := json.Marshal(map[string]string{"value": v}) //nolint:errchkjson // a string
		stdin.Write(append(b, '\n'))
	}
	args := []string{"scan-secrets"}
	if _, err := os.Stat(e.runJSON()); err == nil {
		args = append(args, "--config", e.runJSON())
	}
	args = append(args, "--dir", e.resultsDir())
	cfg := e.hubConfig()
	cfg.RunAsUser = "" // as uploadspike: root reads results/ and the hub's key
	lines, runErr := hubproc.Diag(ctx, cfg, args, &stdin)
	var line scanLine
	for _, l := range lines {
		var s scanLine
		if json.Unmarshal(l, &s) == nil && s.Schema == "ipcam.diag.scansecrets/1" {
			line = s
		}
	}
	clean := runErr == nil && line.Schema != "" && len(line.Unmoved)+len(line.Unreadable) == 0
	switch {
	case len(line.Moved)+len(line.Unmoved) > 0:
		f := &scanFailure{reason: fmt.Sprintf("scan-secrets: moved %d, unmoved %d", len(line.Moved), len(line.Unmoved)), clean: clean}
		if len(line.Unmoved) > 0 {
			f.line = msgScanUnmoved
		} else {
			f.line = fmt.Sprintf(msgScanFailed, filepath.Join(filepath.Dir(e.resultsDir()), filepath.FromSlash(line.Quarantine)))
		}
		return f
	case runErr != nil:
		return &scanFailure{line: fmt.Sprintf(msgScanError, scanErrorText(runErr)), reason: "scan-secrets: " + scanExit(runErr)}
	case line.Schema == "":
		return &scanFailure{line: fmt.Sprintf(msgScanError, "no line"), reason: "scan-secrets: no line"}
	}
	return nil
}

// scanExit is a failed scan's end without a path: its exit code, or that
// it did not run.
func scanExit(err error) string {
	var de *hubproc.DiagError
	if errors.As(err, &de) {
		return "exit " + strconv.Itoa(de.Code)
	}
	return "did not run"
}

// scanErrorText is a failed scan's error without its stderr, which names
// paths only.
func scanErrorText(err error) string {
	var de *hubproc.DiagError
	if errors.As(err, &de) {
		return "exit " + strconv.Itoa(de.Code)
	}
	return err.Error()
}

// zipResults zips results/ into results-<host>.zip in the current folder,
// and gives it to SUDO_USER under sudo.
func zipResults(e *env, host string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path := filepath.Join(cwd, "results-"+host+".zip")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // the owner's own folder
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	root := e.resultsDir()
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(root), p)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(p) //nolint:gosec // a file of results/
		if err != nil {
			return err
		}
		_, err = io.Copy(w, src)
		return errors.Join(err, src.Close())
	})
	if err := errors.Join(walkErr, zw.Close(), f.Close()); err != nil {
		return "", err
	}
	chownToSudoUser(path)
	return path, nil
}

// chownToSudoUser gives path to the user who ran sudo, if any.
func chownToSudoUser(path string) {
	uid, err1 := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, err2 := strconv.Atoi(os.Getenv("SUDO_GID"))
	if err1 == nil && err2 == nil && runtime.GOOS != "windows" {
		_ = os.Chown(path, uid, gid)
	}
}
