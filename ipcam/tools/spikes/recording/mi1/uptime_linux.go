// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Uptime is how long the host has run since its boot, on its own boot
// clock, which NTP never shifts: the first field of /proc/uptime.
func Uptime() (time.Duration, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("mi1: /proc/uptime is empty")
	}
	s, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("mi1: /proc/uptime: %w", err)
	}
	return time.Duration(s * float64(time.Second)), nil
}

// ThisBoot is the host's boot: the kernel's boot_id, which each boot draws
// anew.
func ThisBoot() (Boot, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return Boot{}, err
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return Boot{}, fmt.Errorf("mi1: /proc/sys/kernel/random/boot_id is empty")
	}
	return Boot{ID: id}, nil
}
