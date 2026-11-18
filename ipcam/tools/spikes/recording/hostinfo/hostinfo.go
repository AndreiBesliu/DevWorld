// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package hostinfo reads what recspike needs to know about its host from
// `ipcam-hub diag host` (Task 7a), across the process boundary (C01): the
// OS, the cores, the RAM, the Pi's model, the storage under rec/ and the
// recommended camera count. When cpuinfo names no model, it reads the
// board's device-tree model and maps it with `ipcam-hub diag pimodel` (G32);
// when the model stays unknown, the owner gives the camera count (C24).
package hostinfo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The schemas of diag host and diag pimodel.
const (
	hostSchema    = "ipcam.diag.host/1"
	pimodelSchema = "ipcam.diag.pimodel/1"
)

// maxCameras bounds the owner's camera count: reccontroller serves 16.
const maxCameras = 16

// The owner's prompt for an unknown model (C24).
const (
	askCameras = "Modelul acestui Raspberry Pi nu este în tabelul probei. " +
		"Câte camere să înregistreze proba? Numărul se raportează ca estimare. Apăsați Enter pentru 1: "
	askAgain = "Scrieți un număr de la 1 la 16, sau apăsați Enter pentru 1: "
)

// Info is the host as diag host reports it. PiModel is nil on Windows.
// RecommendedCameras is 8 on a PC, the model's RecommendedMax on a Pi, the
// owner's estimate for an unknown model, and 0 for an unsupported one, or
// an unknown one when nobody was asked.
type Info struct {
	OS                 string               `json:"os"`
	Arch               string               `json:"arch"`
	PhysicalCores      int                  `json:"physicalCores"`
	LogicalCPUs        int                  `json:"logicalCpus"`
	RAMMb              int                  `json:"ramMb"`
	PiModel            *results.PiModelInfo `json:"piModel"`
	Storage            string               `json:"storage"`
	WriteCache         string               `json:"writeCache"`
	RecommendedCameras int                  `json:"recommendedCameras"`
	Cores6Plus         bool                 `json:"cores6Plus"`
}

// Options says which ipcam-hub to run and how to reach the owner.
type Options struct {
	// Hub is the path of ipcam-hub.
	Hub string
	// RecDir is the recordings directory whose storage diag host reports.
	RecDir string
	// DeviceTree is the board's device-tree model file; empty is
	// /proc/device-tree/model. Only Linux has one.
	DeviceTree string
	// In and Out are the owner's console for an unknown model; with In nil,
	// nobody is asked and the model stays unknown, as in the lab. The prompt
	// reads one line of In, so recspike shares In with its later prompts on
	// the same input (G16's «Scrieți DA»).
	In  *bufio.Reader
	Out io.Writer
}

// Read runs diag host and, when the model needs them, the device-tree
// fallback and the owner's prompt.
func Read(ctx context.Context, o Options) (Info, error) {
	var info Info
	var schema struct {
		Schema string `json:"schema"`
	}
	out, err := runHub(ctx, o.Hub, "diag", "host", "--rec", o.RecDir)
	if err != nil {
		return Info{}, err
	}
	if err := json.Unmarshal(out, &schema); err != nil || schema.Schema != hostSchema {
		return Info{}, fmt.Errorf("hostinfo: diag host printed no %s object", hostSchema)
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return Info{}, fmt.Errorf("hostinfo: diag host: %w", err)
	}
	m := info.PiModel
	if m == nil || m.Known {
		return info, nil
	}
	if m.Raw == "" {
		if raw := deviceTreeModel(o.DeviceTree); raw != "" {
			if err := fromDeviceTree(ctx, o.Hub, raw, m); err != nil {
				return Info{}, err
			}
			info.RecommendedCameras = m.RecommendedMax
		}
	}
	if !m.Known && o.In != nil {
		n := askCount(o.In, o.Out)
		m.RecommendedMin, m.RecommendedMax, m.Estimate = n, n, true
		info.RecommendedCameras = n
	}
	return info, nil
}

// fromDeviceTree maps the device-tree model raw with diag pimodel, the same
// table as cpuinfo's (G32), into m, keeping cpuinfo's revision.
func fromDeviceTree(ctx context.Context, hub, raw string, m *results.PiModelInfo) error {
	out, err := runHub(ctx, hub, "diag", "pimodel", "--raw", raw)
	if err != nil {
		return err
	}
	var row struct {
		Schema  string               `json:"schema"`
		PiModel *results.PiModelInfo `json:"piModel"`
	}
	if err := json.Unmarshal(out, &row); err != nil || row.Schema != pimodelSchema || row.PiModel == nil {
		return fmt.Errorf("hostinfo: diag pimodel printed no %s object", pimodelSchema)
	}
	revision := m.Revision
	*m = *row.PiModel
	m.Source = "device-tree"
	if m.Revision == "" {
		m.Revision = revision
	}
	return nil
}

// askCount asks the owner for a camera count until it is 1 to 16; Enter,
// or the end of the input, is 1. It reads in a line at a time and no
// further than its answer.
func askCount(in *bufio.Reader, out io.Writer) int {
	if out == nil {
		out = io.Discard
	}
	_, _ = fmt.Fprint(out, askCameras)
	for {
		line, readErr := in.ReadString('\n')
		answer := strings.TrimSpace(line)
		if answer == "" {
			return 1
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= maxCameras {
			return n
		}
		if readErr != nil { // the end of the input after a wrong answer
			return 1
		}
		_, _ = fmt.Fprint(out, askAgain)
	}
}

// runHub runs ipcam-hub with args and returns its stdout; a failure quotes
// the end of its stderr.
func runHub(ctx context.Context, hub string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, hub, args...) //nolint:gosec // recspike's own ipcam-hub
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && msg != "" {
			return nil, fmt.Errorf("hostinfo: ipcam-hub %s: %w: %s", strings.Join(args[:2], " "), err, msg)
		}
		return nil, fmt.Errorf("hostinfo: ipcam-hub %s: %w", strings.Join(args[:2], " "), err)
	}
	return stdout.Bytes(), nil
}
