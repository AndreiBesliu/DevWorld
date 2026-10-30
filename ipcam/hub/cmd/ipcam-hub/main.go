// Command ipcam-hub is the hub's binary (§3.5), in its Phase 0 subset: it
// records cameras through a supervised MediaMTX child, reconciles the
// recordings after a crash or a cut, keeps the catalog, and reports what it
// does as JSON lines on stdout (ruling C01).
//
//	ipcam-hub run --config <run.json> [--stop-on-stdin-eof]
//	ipcam-hub hook segment-complete      (run by MediaMTX)
//	ipcam-hub diag host [--rec <dir>]    (the host, its Pi model and storage)
//	ipcam-hub diag pimodel --raw <model> (§4.8's row for a model string)
//	ipcam-hub diag cryptobench|readbench|seekbench … (ENC-08's benches)
//
// The events are described with the events type (events.go). Exit codes: 0
// after SIGTERM, Ctrl-C or Ctrl-Break (or the end of stdin, with
// --stop-on-stdin-eof), 1 on a fatal error, 3 when the
// startup reconcile aborted (HUB-09), 4 when rec/ is on an SD card that the
// owner has not confirmed (HUB-17).
package main

import (
	"fmt"
	"io"
	"os"
)

// The exit codes.
const (
	exitOK                = 0
	exitFatal             = 1
	exitAborted           = 3
	exitSdCardUnconfirmed = 4
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run runs the subcommand in args and returns its exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return runCommand(args[1:], stdin, stdout, stderr)
		case "hook":
			return hookCommand(args[1:], stderr)
		case "diag":
			return diagCommand(args[1:], stdout, stderr)
		}
	}
	_, _ = fmt.Fprintln(stderr, "usage: ipcam-hub run --config <run.json> [--stop-on-stdin-eof]\n       ipcam-hub hook segment-complete\n       "+diagLines)
	return exitFatal
}
