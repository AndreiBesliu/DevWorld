// Package health reads what the hub needs to know about its host (§4.3,
// §4.9): the Raspberry Pi model and its recommended camera count (T39,
// §4.8), whether rec/ lies on an SD card (T40, HUB-17), the kind of storage
// under rec/ and its write cache, and the CPU cores and RAM. Nothing here
// keeps or returns a serial number, a host name, an address or a user name:
// what it reports reaches published results (PH0-22).
package health

// Roots are the files that the Linux readers parse; tests point them at
// fixtures. On Windows, Cores, RAMMb and Storage ask the system and ignore
// them.
type Roots struct {
	CPUInfo   string // /proc/cpuinfo
	MemInfo   string // /proc/meminfo
	MountInfo string // /proc/self/mountinfo
	Sys       string // /sys
}

// SystemRoots are the running system's files.
var SystemRoots = Roots{CPUInfo: "/proc/cpuinfo", MemInfo: "/proc/meminfo", MountInfo: "/proc/self/mountinfo", Sys: "/sys"}

// unknown is what Storage reports when it cannot tell.
const unknown = "unknown"
