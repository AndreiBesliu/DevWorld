// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi8"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// canDetach: on Linux MI8 runs its 72 h in a detached copy of uploadspike,
// so the SSH window, and the PC it runs on, may close (P42, P49).
const canDetach = true

// copyBeatWait bounds the parent's wait for the copy's first heartbeat.
const copyBeatWait = 2 * time.Minute

// detachMI8 starts `uploadspike mi8 --detached` in a new session (Setsid),
// its stdout and stderr the null device, MI8's lock its descriptor 3
// (ExtraFiles), so the lock never lapses between the two processes, and
// the cameras as JSON lines on its stdin, a pipe that this process closes:
// RAM to RAM, never a file; the copy's argv and environment hold no camera
// value. It waits for the copy's first heartbeat in the run state, then
// says that MI8 runs in the background and returns.
func detachMI8(ctx context.Context, e *env, lock *os.File, cams []mi8.CameraRef, ffmpeg string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"mi8", "--detached"}
	if ffmpeg != "" {
		args = append(args, "--ffmpeg", ffmpeg)
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = null.Close() }()
	cmd := exec.Command(self, args...) //nolint:gosec // uploadspike itself
	cmd.Env = os.Environ()
	cmd.Stdout, cmd.Stderr = null, null
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	for _, c := range cams {
		if _, err := stdin.Write(cameraLine(c)); err != nil {
			break
		}
	}
	_ = stdin.Close()
	deadline := time.After(copyBeatWait)
	for {
		if st, err := runstate.Load(e.statePath()); err == nil {
			if raw, ok := st.Open["MI8"]; ok {
				if run, err := mi8.Decode(raw); err == nil && run.PID == cmd.Process.Pid && run.HeartbeatMs > 0 {
					e.printf(msgMI8Background)
					return nil
				}
			}
		}
		select {
		case <-exited:
			return ownerError(msgMI8CopyFailed)
		case <-deadline:
			return ownerError(msgMI8CopyFailed)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.poll() / 3):
		}
	}
}

// ignoreHangup makes the detached copy ignore SIGHUP: the closing of the
// SSH window that started its parent never ends it.
func ignoreHangup() { signal.Ignore(syscall.SIGHUP) }

// closeStdin closes the copy's stdin once the cameras are read: the pipe is
// closed and the null device takes descriptor 0, so no file opened later
// takes it.
func closeStdin() {
	null, err := os.Open(os.DevNull)
	if err != nil {
		_ = os.Stdin.Close()
		return
	}
	_ = syscall.Dup2(int(null.Fd()), 0) //nolint:gosec // descriptors
	_ = null.Close()
}

// cameraLine is a camera's line on the detached copy's stdin.
func cameraLine(c mi8.CameraRef) []byte {
	b, _ := json.Marshal(struct{ ID, XAddr, User, Pass string }{c.ID, c.XAddr, c.User, c.Pass}) //nolint:errchkjson,gosec // G117: the copy's stdin, RAM to RAM
	return append(b, '\n')
}
