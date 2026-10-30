// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import "fmt"

// bugcheck starts NotMyFault at path, which crashes the PC, once verify
// has checked its signature again (security-1): the preflight checked it
// minutes or hours before, and the boot task runs it elevated.
func bugcheck(path string, verify, start func(path string) error) error {
	if err := verify(path); err != nil {
		return fmt.Errorf("notmyfault64.exe nu mai e semnat de Microsoft, așa că nu îl pornesc: %w", err)
	}
	return start(path)
}
