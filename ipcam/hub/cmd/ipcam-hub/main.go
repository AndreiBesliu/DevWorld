// Command ipcam-hub is the hub's binary (§3.5), in its Phase 0 subset: it
// records cameras through a supervised MediaMTX child, reconciles the
// recordings after a crash or a cut, keeps the catalog, and reports what it
// does as JSON lines on stdout (ruling C01).
//
//	ipcam-hub run --config <run.json>
//	ipcam-hub hook segment-complete     (run by MediaMTX)
//
// The events are described with the events type (events.go). Exit codes: 0
// after SIGTERM, Ctrl-C or Ctrl-Break, 1 on a fatal error, 3 when the
// startup reconcile aborted (HUB-09).
package main

import (
	"fmt"
	"io"
	"os"
)

// The exit codes.
const (
	exitOK      = 0
	exitFatal   = 1
	exitAborted = 3
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run runs the subcommand in args and returns its exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return runCommand(args[1:], stdout, stderr)
		case "hook":
			return hookCommand(args[1:], stderr)
		}
	}
	_, _ = fmt.Fprintln(stderr, "usage: ipcam-hub run --config <run.json>\n       ipcam-hub hook segment-complete")
	return exitFatal
}
