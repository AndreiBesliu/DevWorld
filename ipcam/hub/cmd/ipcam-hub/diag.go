package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// diagLines are the diag subcommands' lines of a usage error.
const diagLines = "ipcam-hub diag host [--rec <dir>]\n       ipcam-hub diag pimodel --raw <model>"

// diagUsage is the usage of the diag subcommands.
const diagUsage = "usage: " + diagLines

// diagCommand is "ipcam-hub diag": it prints what the hub reads about its
// host as one JSON object on stdout.
func diagCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "host":
			return diagHostCommand(args[1:], stdout, stderr)
		case "pimodel":
			return diagPimodelCommand(args[1:], stdout, stderr)
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
