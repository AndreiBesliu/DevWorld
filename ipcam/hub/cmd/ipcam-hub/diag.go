package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// diagLines are the diag subcommands' lines of a usage error.
const diagLines = "ipcam-hub diag host [--rec <dir>]\n       ipcam-hub diag pimodel --raw <model>\n" +
	"       ipcam-hub diag cryptobench [--seconds 60]\n" +
	"       ipcam-hub diag readbench --file <seg.mp4.age> --identity <file> [--reads 1000] [--size part|<bytes>]\n" +
	"       ipcam-hub diag seekbench --file <seg.mp4.age> --identity <file> [--seeks 20] [--whole-ram]"

// diagUsage is the usage of the diag subcommands.
const diagUsage = "usage: " + diagLines

// diagCommand is "ipcam-hub diag": it prints what the hub reads about its
// host, or what its benches measure on it (ENC-08: the encryption
// throughput, and reads and seeks in an encrypted recording), as one JSON
// object on stdout.
func diagCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "host":
			return diagHostCommand(args[1:], stdout, stderr)
		case "pimodel":
			return diagPimodelCommand(args[1:], stdout, stderr)
		case "cryptobench":
			return diagCryptobenchCommand(args[1:], stdout, stderr)
		case "readbench":
			return diagReadbenchCommand(args[1:], stdout, stderr)
		case "seekbench":
			return diagSeekbenchCommand(args[1:], stdout, stderr)
		}
	}
	_, _ = fmt.Fprintln(stderr, diagUsage)
	return exitFatal
}

// printJSONLine prints v as one line of JSON.
func printJSONLine(stdout, stderr io.Writer, v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	if _, err := stdout.Write(append(b, '\n')); err != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub:", err)
		return exitFatal
	}
	return exitOK
}
