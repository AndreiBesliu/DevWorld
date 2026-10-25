// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Check is one preflight check: Run returns the Romanian reason why the
// window may not start, or "" when the check passes.
type Check func(ctx context.Context) string

// Preflight runs every check and refuses, in Romanian, with every reason
// at once, when any fails (G14, R08).
func Preflight(ctx context.Context, checks []Check) error {
	var reasons []string
	for _, c := range checks {
		if reason := c(ctx); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	return errors.New("Proba nu pornește: " + strings.Join(reasons, "; ") + ".")
}

// NotMyFault is Sysinternals' tool, which the owner puts next to recspike
// (it may not be redistributed, G14): the bugcheck runs it from there and
// the PC's preflight checks its signature there.
const NotMyFault = "notmyfault64.exe"

// PCProbes read what the PC's preflight checks.
type PCProbes struct {
	// NotMyFault is nil when notmyfault64.exe is next to recspike with a
	// valid Authenticode signature by Microsoft Corporation.
	NotMyFault func() error
	Elevated   func() bool
	// AutoReboot is HKLM\SYSTEM\CurrentControlSet\Control\CrashControl\AutoReboot.
	AutoReboot func() (uint32, error)
	// UpdatesPausedUntil is Windows Update's PauseUpdatesExpiryTime.
	UpdatesPausedUntil func() (time.Time, error)
	Now                func() time.Time
}

// PCChecks are the PC's checks (G14): NotMyFault signed by Microsoft, an
// elevated session, a reboot after a bugcheck, and Windows Update paused.
func PCChecks(p PCProbes) []Check {
	return []Check{
		func(context.Context) string {
			if p.NotMyFault() != nil {
				return "notmyfault64.exe lipsește sau nu e semnat de Microsoft (puneți-l lângă recspike.exe, de pe pagina Sysinternals)"
			}
			return ""
		},
		func(context.Context) string {
			if !p.Elevated() {
				return "recspike nu rulează ca administrator (porniți-l dintr-o fereastră deschisă cu „Executare ca administrator”)"
			}
			return ""
		},
		func(context.Context) string {
			if v, err := p.AutoReboot(); err != nil || v != 1 {
				return `Windows nu repornește singur după o eroare de sistem (CrashControl\AutoReboot trebuie să fie 1)`
			}
			return ""
		},
		func(context.Context) string {
			if until, err := p.UpdatesPausedUntil(); err != nil || !until.After(p.Now()) {
				return "actualizările nu sunt întrerupte (întrerupeți-le din Setări → Windows Update)"
			}
			return ""
		},
	}
}

// PiProbes read what the Pi's preflight checks.
type PiProbes struct {
	Euid      func() int
	FreeBytes func() (uint64, error)
	// Cameras is how many cameras the set records.
	Cameras int
}

// RequiredFreeBytes is the free space that a Pi needs for one run of
// cameras main streams: cameras × 4 Mbps × 12 min, plus 2 GiB (G14).
func RequiredFreeBytes(cameras int) uint64 {
	const perCamera = mainKbps * 1000 / 8 * 12 * 60
	return uint64(cameras)*perCamera + 2<<30 //nolint:gosec // 1 to 16 cameras
}

// PiChecks are the Pi's checks (G14): root, and the free space of one run.
func PiChecks(p PiProbes) []Check {
	return []Check{
		func(context.Context) string {
			if p.Euid() != 0 {
				return "recspike nu rulează ca root (porniți-l cu sudo)"
			}
			return ""
		},
		func(context.Context) string {
			need := RequiredFreeBytes(max(p.Cameras, 1))
			if free, err := p.FreeBytes(); err != nil || free < need {
				return fmt.Sprintf("spațiu liber insuficient: sunt necesari cel puțin %s", gigabytes(need))
			}
			return ""
		},
	}
}

// gigabytes renders a size in GB with a decimal comma.
func gigabytes(n uint64) string {
	return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/1e9), ".", ",", 1)
}

// LinkProbes check the controller link in both directions (R08); the TCP
// link itself works when the probes run.
type LinkProbes struct {
	// Beacon is nil when the controller acknowledged the host's beacon over
	// TCP.
	Beacon func(ctx context.Context) error
	// RTSP is nil when the host pulled a camera from the controller.
	RTSP func(ctx context.Context) error
	NTP  NTPCheck
}

// LinkChecks are both devices' checks: the beacon, an RTSP pull, and NTP.
func LinkChecks(p LinkProbes) []Check {
	return []Check{
		func(ctx context.Context) string {
			if p.Beacon(ctx) != nil {
				return "controlorul nu primește semnalul UDP al gazdei (portul 7082; verificați firewall-ul controlorului)"
			}
			return ""
		},
		func(ctx context.Context) string {
			if p.RTSP(ctx) != nil {
				return "gazda nu poate citi camerele controlorului prin RTSP (portul 8554)"
			}
			return ""
		},
		func(ctx context.Context) string {
			ok, err := p.NTP(ctx)
			switch {
			case errors.Is(err, ErrNTPNeedsAdmin):
				return "starea ceasului se poate citi doar ca administrator (porniți recspike cu „Executare ca administrator” sau din scurtătura de pe desktop)"
			case errors.Is(err, ErrTimeServiceStopped):
				return "serviciul de timp Windows nu rulează, deci ceasul nu se sincronizează (într-o fereastră de administrator: net start w32time, apoi w32tm /resync)"
			case err != nil || !ok:
				return "ceasul gazdei nu este sincronizat prin NTP"
			}
			return ""
		},
	}
}
