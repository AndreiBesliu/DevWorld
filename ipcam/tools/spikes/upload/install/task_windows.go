// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// UserSID is the SID of the user that runs uploadspike: the task runs as
// that user, whether logged on or not (S4U).
func UserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

// AddTask registers the task for exe from its XML, written UTF-16 LE
// with a BOM into a temporary file, and starts it once.
func AddTask(ctx context.Context, run Commander, exe string, now time.Time) error {
	sid, err := UserSID()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "ipcam-uploadspike-*.xml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = f.Write(UTF16File(TaskXML(exe, filepath.Dir(exe), sid, now)))
	if err = errorsJoin(err, f.Close()); err != nil {
		return err
	}
	return RunAll(ctx, run, TaskCommands(f.Name()))
}

// InboundBlocked reports a dismissed firewall prompt, or missing inbound
// rules: "blocked", "missing" or "ok".
func InboundBlocked(ctx context.Context, run Commander, uploadspike string) string {
	out, err := PowerShell(ctx, run, []string{InboundCheckLine(uploadspike)})
	if err != nil {
		return "ok" // nothing known: say nothing
	}
	return strings.TrimSpace(out)
}

func errorsJoin(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
