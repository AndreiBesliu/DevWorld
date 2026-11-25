// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package hostinfo reads what uploadspike needs to know about its host from
// `ipcam-hub diag host` across the process boundary (P01): the OS and
// architecture, the Pi's model, the storage under rec/ and the recommended
// camera count. When diag host names no model, it reads the board's
// device-tree model and maps it with `ipcam-hub diag pimodel`, as c1's
// recspike does (c1 Task 7a). RecommendD2 is `uploadspike host`'s
// recommendation (C2-R2).
package hostinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The schemas of diag host and diag pimodel.
const (
	hostSchema    = "ipcam.diag.host/1"
	pimodelSchema = "ipcam.diag.pimodel/1"
)

// The tiers that hold MI6's seven days on a Pi (§4.8).
const (
	tierH1          = "H1"
	tierH1b         = "H1b"
	tierUnsupported = "unsupported"
)

// The D2 recommendations' reasons (C2-R2), as `uploadspike host` prints
// them.
const (
	ReasonPi64          = "Pi-ul (%s) are un sistem pe 64 de biți: el ține hub-ul cele 7 zile ale probei MI6, iar proba MI5 rulează pe PC (D2-B)."
	ReasonPi32          = "Pi-ul (%s) are un sistem pe 32 de biți: PC-ul ține hub-ul pentru toate probele (D2-A)."
	ReasonPiWeak        = "Pi-ul (%s) este prea slab pentru proba de 7 zile: PC-ul ține hub-ul pentru toate probele (D2-A)."
	ReasonPiUnsupported = "Pi-ul nu poate rula hub-ul (%s): PC-ul ține hub-ul pentru toate probele (D2-A)."
	ReasonNoPi          = "Acest dispozitiv nu este un Raspberry Pi cunoscut: PC-ul ține hub-ul pentru toate probele (D2-A)."
)

// Host is the host as diag host reports it. PiModel is nil off a Pi.
type Host struct {
	OS                 string               `json:"os"`
	Arch               string               `json:"arch"`
	PiModel            *results.PiModelInfo `json:"piModel"`
	Storage            string               `json:"storage"`
	RecommendedCameras int                  `json:"recommendedCameras"`
}

// deviceTreeModelPath is where Linux names the board; tests replace it.
var deviceTreeModelPath = "/proc/device-tree/model"

// Read runs `ipcam-hub diag host --rec <recDir>`. When the model reads
// "unknown" with nothing from cpuinfo, it reads the device-tree model and
// maps it with `ipcam-hub diag pimodel --raw` (Source "device-tree"). A
// host with no model at all, a PC, has a nil PiModel.
func Read(ctx context.Context, hubBin, recDir string) (Host, error) {
	out, err := runHub(ctx, hubBin, "diag", "host", "--rec", recDir)
	if err != nil {
		return Host{}, err
	}
	var h Host
	var schema struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(out, &schema); err != nil || schema.Schema != hostSchema {
		return Host{}, fmt.Errorf("hostinfo: diag host printed no %s object", hostSchema)
	}
	if err := json.Unmarshal(out, &h); err != nil {
		return Host{}, fmt.Errorf("hostinfo: diag host: %w", err)
	}
	m := h.PiModel
	if m == nil || m.Known {
		return h, nil
	}
	if m.Raw == "" {
		raw := deviceTreeModel()
		if raw == "" {
			h.PiModel = nil // no board name anywhere: not a Pi
			return h, nil
		}
		if err := fromDeviceTree(ctx, hubBin, raw, m); err != nil {
			return Host{}, err
		}
		h.RecommendedCameras = m.RecommendedMax
	}
	return h, nil
}

// deviceTreeModel reads the board's model, without the NUL that ends it;
// empty off Linux and on a board without one.
func deviceTreeModel() string {
	b, err := os.ReadFile(deviceTreeModelPath)
	if err != nil {
		return ""
	}
	return strings.Trim(string(b), " \t\r\n\x00")
}

// fromDeviceTree maps the device-tree model raw with diag pimodel into m,
// keeping cpuinfo's revision.
func fromDeviceTree(ctx context.Context, hubBin, raw string, m *results.PiModelInfo) error {
	out, err := runHub(ctx, hubBin, "diag", "pimodel", "--raw", raw)
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

// RecommendD2 is D2's recommendation for the host (C2-R2), with its reason
// in Romanian: "B" (the Pi holds the 7 days, MI5 on the PC) for a Pi of
// tier H1 or H1b (Pi 4, 5, 400, 500, CM4, CM5) with a 64-bit system
// (arm64), "A" (the PC for everything) otherwise.
func RecommendD2(h Host) (letter, reason string) {
	m := h.PiModel
	switch {
	case m == nil || !m.Known:
		return "A", ReasonNoPi
	case m.Tier == tierUnsupported:
		return "A", fmt.Sprintf(ReasonPiUnsupported, m.Raw)
	case m.Tier != tierH1 && m.Tier != tierH1b:
		return "A", fmt.Sprintf(ReasonPiWeak, m.Raw)
	case h.Arch != "arm64":
		return "A", fmt.Sprintf(ReasonPi32, m.Raw)
	}
	return "B", fmt.Sprintf(ReasonPi64, m.Raw)
}

// runHub runs ipcam-hub with args and returns its stdout; a failure quotes
// the end of its stderr.
func runHub(ctx context.Context, hub string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, hub, args...) //nolint:gosec // uploadspike's own ipcam-hub
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
