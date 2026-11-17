// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Command uploadspike is the upload spike's program on the owner's devices
// and in the lab (Task 9): it runs ipcam-hub across the process boundary
// (P01), one hub per device under <base>/hub, holds the device's hub.lock
// while it runs one, serves the handoff pages on port 7091, and drives MI6,
// OAuth on the hub (G15). Every line it prints is Romanian.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/handoff"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// The variables of the environment that the lab and the tests set:
// another base, ipcam-hub, MediaMTX, the fakes' base URL, the hub's
// identity ("-": none), the server's port, the host's pseudonym, the
// browser left closed, a file for the hub's events, short waits, and the
// tests' seams.
const (
	envBase         = "UPLOADSPIKE_BASE"
	envHubBin       = "UPLOADSPIKE_HUB_BIN"
	envMediaMTX     = "UPLOADSPIKE_MEDIAMTX"
	envFakes        = "UPLOADSPIKE_FAKES"
	envRunAs        = "UPLOADSPIKE_RUN_AS_USER"
	envPort         = "UPLOADSPIKE_PORT"
	envHost         = "UPLOADSPIKE_HOST"
	envNoBrowser    = "UPLOADSPIKE_NO_BROWSER"
	envEventLog     = "UPLOADSPIKE_EVENT_LOG"
	envFast         = "UPLOADSPIKE_TEST_FAST"
	envPauseAfter   = "UPLOADSPIKE_TEST_PAUSE_AFTER_CHECK"
	envSignalFile   = "UPLOADSPIKE_TEST_SIGNAL_FILE"
	envNotElevated  = "UPLOADSPIKE_TEST_NOT_ELEVATED"
	defaultHubName  = "ipcam-hub"
	baseDirName     = "ipcam-spike"
	linuxBase       = "/var/lib/ipcam-spike"
	handoverTimeout = 60 * time.Second
)

// env is one uploadspike process's paths and settings.
type env struct {
	base, hubBin, mediamtx, fakes, runAs, host, eventLog string
	port                                                 int
	noBrowser, fast                                      bool
	in                                                   io.Reader
	out, errOut                                          io.Writer
	now                                                  func() time.Time
}

// newEnv reads the environment.
func newEnv(in io.Reader, out, errOut io.Writer) (*env, error) {
	e := &env{in: in, out: out, errOut: errOut, now: time.Now, port: handoff.HandoffPort,
		fakes: os.Getenv(envFakes), eventLog: os.Getenv(envEventLog), mediamtx: os.Getenv(envMediaMTX),
		noBrowser: os.Getenv(envNoBrowser) == "1", fast: os.Getenv(envFast) == "1"}
	e.base = os.Getenv(envBase)
	if e.base == "" {
		if runtime.GOOS == "windows" {
			local := os.Getenv("LOCALAPPDATA")
			if local == "" {
				return nil, errors.New("LOCALAPPDATA nu este setat")
			}
			e.base = filepath.Join(local, baseDirName)
		} else {
			e.base = linuxBase
		}
	}
	e.hubBin = os.Getenv(envHubBin)
	if e.hubBin == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		e.hubBin = filepath.Join(filepath.Dir(self), defaultHubName)
		if runtime.GOOS == "windows" {
			e.hubBin += ".exe"
		}
	}
	if p := os.Getenv(envPort); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return nil, fmt.Errorf("%s nu este un port", envPort)
		}
		e.port = n
	}
	switch v, set := os.LookupEnv(envRunAs); {
	case set && v == "-":
	case set:
		e.runAs = v
	case runtime.GOOS == "linux" && os.Geteuid() == 0:
		e.runAs = hubproc.SpikeUser
	}
	e.host = os.Getenv(envHost)
	return e, nil
}

// The paths under <base>: uploadspike's state/ and the hub's data
// directory hub/, whose spike/ holds run.json.
func (e *env) stateDir() string   { return filepath.Join(e.base, "state") }
func (e *env) statePath() string  { return filepath.Join(e.stateDir(), runstate.StateFile) }
func (e *env) hubLock() string    { return filepath.Join(e.stateDir(), runstate.HubLock) }
func (e *env) pidFile() string    { return filepath.Join(e.stateDir(), runstate.HubPID) }
func (e *env) dataDir() string    { return filepath.Join(e.base, "hub") }
func (e *env) runJSON() string    { return filepath.Join(e.dataDir(), "spike", "run.json") }
func (e *env) resultsDir() string { return filepath.Join(e.base, "results") }

// poll is how often the waits look again: 5 s, or 300 ms in the tests.
func (e *env) poll() time.Duration {
	if e.fast {
		return 300 * time.Millisecond
	}
	return 5 * time.Second
}

// printf prints a line for the owner.
func (e *env) printf(format string, args ...any) { _, _ = fmt.Fprintf(e.out, format+"\n", args...) }

// hubConfig is the hubproc configuration of this device's hub, without its
// files and token.
func (e *env) hubConfig() hubproc.Config {
	return hubproc.Config{Bin: e.hubBin, RunJSON: e.runJSON(), RunAsUser: e.runAs, PIDFile: e.pidFile(), Stderr: e.errOut}
}

func main() {
	e, err := newEnv(os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		os.Exit(report(err, os.Stderr))
	}
	os.Exit(report(run(context.Background(), e, os.Args[1:]), os.Stderr))
}

// exitError is an end that has said what it had to say: its code only.
type exitError int

func (e exitError) Error() string { return "exit " + strconv.Itoa(int(e)) }

// ownerError is an end whose text is a whole line for the owner.
type ownerError string

func (e ownerError) Error() string { return string(e) }

// report prints the last line of a run that ended with err on stderr, and
// returns uploadspike's exit code.
func report(err error, stderr io.Writer) int {
	var code exitError
	var owner ownerError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &code):
		return int(code)
	case errors.Is(err, flag.ErrHelp):
	case errors.Is(err, context.Canceled):
		_, _ = fmt.Fprintln(stderr, msgStoppedByYou)
	case errors.As(err, &owner), errors.Is(err, hubproc.ErrSdCardUnconfirmed), errors.Is(err, hubproc.ErrNoSpikeUser):
		_, _ = fmt.Fprintln(stderr, err)
	default:
		_, _ = fmt.Fprintln(stderr, msgError, err)
	}
	return 1
}

// run runs the command in args.
func run(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(e.out, usage)
		return ownerError(msgNoCommand)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(e.out, usage)
		return nil
	case "setup":
		return runSetup(ctx, e, rest)
	case "host":
		return runHost(ctx, e, rest)
	case "mi6":
		return runMI6(ctx, e, rest)
	case "resume":
		return runResume(ctx, e, rest)
	case "status":
		return runStatus(ctx, e, rest)
	case "results":
		return runResults(ctx, e, rest)
	}
	_, _ = fmt.Fprint(e.out, usage)
	return ownerError(fmt.Sprintf(msgUnknownCommand, cmd))
}
