package main

import (
	"flag"
	"fmt"
	"io"
	"runtime"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
)

// hostSchema names the output of diag host.
const hostSchema = "ipcam.diag.host/1"

// pcCameras is the recommended camera count of a Windows PC, H3 (§4.8):
// 16 with 6 cores or more is not measured (R07), so it stays 8.
const pcCameras = 8

// hostFacts is what diag host reads about the host and the storage under
// rec/.
type hostFacts struct {
	OS, Arch                          string
	PhysicalCores, LogicalCPUs, RAMMb int
	PiModel                           *health.PiModel // nil on Windows
	Storage, WriteCache               string
	errs                              []error // what could not be read
}

// hostProbe reads the facts of this host about recDir; tests replace it.
var hostProbe = func(recDir string) hostFacts { return probeHost(health.SystemRoots, recDir) }

// probeHost reads the facts through health, with the Linux readers on
// roots. On Linux, the model comes from cpuinfo (T39) and is "unknown"
// without a Model line; Windows is no Raspberry Pi.
func probeHost(roots health.Roots, recDir string) hostFacts {
	f := hostFacts{OS: runtime.GOOS, Arch: runtime.GOARCH}
	var err error
	if f.PhysicalCores, f.LogicalCPUs, err = health.Cores(roots); err != nil {
		f.errs = append(f.errs, err)
	}
	if f.RAMMb, err = health.RAMMb(roots); err != nil {
		f.errs = append(f.errs, err)
	}
	if runtime.GOOS != "windows" {
		m, err := health.ReadPiModel(roots.CPUInfo)
		if err != nil {
			f.errs = append(f.errs, err)
		}
		f.PiModel = &m
	}
	f.Storage, f.WriteCache = health.Storage(roots, recDir)
	return f
}

// hostReport is the output of diag host.
type hostReport struct {
	Schema        string          `json:"schema"`
	OS            string          `json:"os"`
	Arch          string          `json:"arch"`
	PhysicalCores int             `json:"physicalCores"`
	LogicalCPUs   int             `json:"logicalCpus"`
	RAMMb         int             `json:"ramMb"`
	PiModel       *health.PiModel `json:"piModel"`
	Storage       string          `json:"storage"`
	WriteCache    string          `json:"writeCache"`
	// RecommendedCameras is 8 on a Windows PC (H3), the model's
	// RecommendedMax on a Raspberry Pi, and 0 for an unknown or
	// unsupported model (C24).
	RecommendedCameras int `json:"recommendedCameras"`
	// Cores6Plus is true with 6 physical cores or more, where §4.8 gives
	// H3 16 cameras, which MI3 measures as information (R07).
	Cores6Plus bool `json:"cores6Plus"`
}

func reportHost(f hostFacts) hostReport {
	r := hostReport{Schema: hostSchema, OS: f.OS, Arch: f.Arch, PhysicalCores: f.PhysicalCores, LogicalCPUs: f.LogicalCPUs,
		RAMMb: f.RAMMb, PiModel: f.PiModel, Storage: f.Storage, WriteCache: f.WriteCache, Cores6Plus: f.PhysicalCores >= 6}
	switch {
	case f.OS == "windows":
		r.RecommendedCameras = pcCameras
	case f.PiModel != nil:
		r.RecommendedCameras = f.PiModel.RecommendedMax
	}
	return r
}

// diagHostCommand is "ipcam-hub diag host [--rec <dir>]": it prints the
// host's facts and recommended camera count as one ipcam.diag.host/1
// object; the storage is the one under --rec, by default the current
// directory. What it cannot read goes to stderr, and its field says
// "unknown" or 0.
func diagHostCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag host", flag.ContinueOnError)
	flags.SetOutput(stderr)
	recDir := flags.String("rec", ".", "the recordings directory, whose storage is reported")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	f := hostProbe(*recDir)
	for _, err := range f.errs {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag host:", err)
	}
	return printJSONLine(stdout, stderr, reportHost(f))
}
