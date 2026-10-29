// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"fmt"
	"time"
)

// tree is pid and its descendants, as far as they can be listed: ipcam-hub
// and MediaMTX, its child, and a load's process.
func tree(pid int) []int {
	out := []int{pid}
	for i := 0; i < len(out); i++ {
		if kids, err := childrenOf(out[i]); err == nil {
			out = append(out, kids...)
		}
	}
	return out
}

// usage is the CPU time of the machine, and of each process of a step with
// its descendants, at one moment (cpu_linux.go, cpu_windows.go).
type usage struct {
	at          time.Time
	busy, total time.Duration
	procs       map[string]map[int]time.Duration // root name → pid → CPU time
}

// sampleUsage reads the CPU time of the machine and of each root's tree;
// a root that has exited is an error: a load, or ipcam-hub, stopped.
func sampleUsage(roots map[string]int) (usage, error) {
	u := usage{at: time.Now(), procs: map[string]map[int]time.Duration{}}
	var err error
	if u.busy, u.total, err = machineTimes(); err != nil {
		return u, fmt.Errorf("mi3: the machine's CPU time: %w", err)
	}
	for name, root := range roots {
		u.procs[name] = map[int]time.Duration{}
		for i, pid := range tree(root) {
			t, err := processTimes(pid)
			switch {
			case err != nil && i == 0:
				return u, fmt.Errorf("mi3: %s: %w", name, err)
			case err == nil:
				u.procs[name][pid] = t
			}
		}
	}
	return u, nil
}

// cpuSince is the CPU time that name's tree used since s: each process
// that runs now, less what it had used at s. A process that ended in
// between is not counted: a hook of ipcam-hub's lives a moment.
func (u usage) cpuSince(s usage, name string) time.Duration {
	var d time.Duration
	for pid, t := range u.procs[name] {
		d += t - s.procs[name][pid]
	}
	return d
}

// pctOfMachine is d as a percentage of the machine's CPU time since s.
func (u usage) pctOfMachine(s usage, d time.Duration) float64 {
	if u.total <= s.total {
		return 0
	}
	return 100 * float64(d) / float64(u.total-s.total)
}

// pctOfCore is d as a percentage of one core over the time since s.
func (u usage) pctOfCore(s usage, d time.Duration) float64 {
	wall := u.at.Sub(s.at)
	if wall <= 0 {
		return 0
	}
	return 100 * float64(d) / float64(wall)
}

// memory is the resident memory of the roots' trees together, in bytes;
// a process that ends while it is read counts nothing.
func memory(roots map[string]int) uint64 {
	var sum uint64
	for _, root := range roots {
		for _, pid := range tree(root) {
			if rss, err := processRSS(pid); err == nil {
				sum += rss
			}
		}
	}
	return sum
}
