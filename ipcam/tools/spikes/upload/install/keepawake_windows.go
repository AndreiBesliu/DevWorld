// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package install

import (
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

// The flags of SetThreadExecutionState (winbase.h). ES_DISPLAY_REQUIRED is
// not asked for: the screen may turn off, only the system must not sleep.
const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// KeepAwake keeps the PC from sleeping while a run goes on (copied from
// spikehub's keepWindowsAwake): SetThreadExecutionState(ES_CONTINUOUS |
// ES_SYSTEM_REQUIRED) on one goroutine locked to its thread, withdrawn on
// that thread by the returned release.
func KeepAwake() (func(), error) {
	if err := setThreadExecutionState.Find(); err != nil {
		return nil, err
	}
	requested := make(chan error, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		if r, _, err := setThreadExecutionState.Call(esContinuous | esSystemRequired); r == 0 {
			requested <- fmt.Errorf("SetThreadExecutionState: %w", err)
			return
		}
		requested <- nil
		<-stop
		_, _, _ = setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-requested; err != nil {
		<-done
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}, nil
}
