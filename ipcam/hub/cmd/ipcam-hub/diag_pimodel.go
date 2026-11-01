package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
)

// pimodelSchema names the output of diag pimodel.
const pimodelSchema = "ipcam.diag.pimodel/1"

// diagPimodelCommand is "ipcam-hub diag pimodel --raw <model>": it prints
// {"schema", "piModel"}, §4.8's row for the model string, which the spike
// reads from /proc/device-tree/model when cpuinfo has no Model line (G32),
// so the table exists once. It reads no file.
func diagPimodelCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag pimodel", flag.ContinueOnError)
	flags.SetOutput(stderr)
	raw := flags.String("raw", "", "the model string, such as \"Raspberry Pi 4 Model B Rev 1.5\"")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	given := false
	flags.Visit(func(f *flag.Flag) { given = given || f.Name == "raw" })
	if !given || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	return printJSONLine(stdout, stderr, struct {
		Schema  string         `json:"schema"`
		PiModel health.PiModel `json:"piModel"`
	}{pimodelSchema, health.ParsePiModel(*raw, "")})
}
