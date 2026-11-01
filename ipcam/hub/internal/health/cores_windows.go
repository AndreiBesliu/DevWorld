package health

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                             = windows.NewLazySystemDLL("kernel32.dll")
	procGetLogicalProcessorInformationEx = kernel32.NewProc("GetLogicalProcessorInformationEx")
	procGlobalMemoryStatusEx             = kernel32.NewProc("GlobalMemoryStatusEx")
)

// The layout of SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX for
// RelationProcessorCore: Relationship and Size (DWORDs), then
// PROCESSOR_RELATIONSHIP's Flags, EfficiencyClass, Reserved[20] and
// GroupCount, then GroupCount GROUP_AFFINITY entries, each a KAFFINITY
// mask, a WORD group and WORD Reserved[3].
const (
	relationProcessorCore = 0
	groupCountOffset      = 30
	groupMaskOffset       = 32
	affinityMaskSize      = int(unsafe.Sizeof(uintptr(0)))
	groupAffinitySize     = affinityMaskSize + 8
)

// Cores returns the physical cores and logical processors that
// GetLogicalProcessorInformationEx(RelationProcessorCore) lists (R07): one
// entry per physical core, whose group masks hold its logical processors.
// It ignores r.
func Cores(Roots) (physical, logical int, err error) {
	var size uint32
	r1, _, err := procGetLogicalProcessorInformationEx.Call(relationProcessorCore, 0, uintptr(unsafe.Pointer(&size))) //nolint:gosec // an out parameter
	if r1 != 0 || !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return 0, 0, fmt.Errorf("health: GetLogicalProcessorInformationEx: %w", err)
	}
	buf := make([]byte, size)
	r1, _, err = procGetLogicalProcessorInformationEx.Call(relationProcessorCore, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))) //nolint:gosec // out parameters
	if r1 == 0 {
		return 0, 0, fmt.Errorf("health: GetLogicalProcessorInformationEx: %w", err)
	}
	buf = buf[:size]
	malformed := errors.New("health: GetLogicalProcessorInformationEx returned a malformed entry")
	for off := 0; off < len(buf); {
		if off+groupMaskOffset > len(buf) {
			return 0, 0, malformed
		}
		n := int(binary.LittleEndian.Uint32(buf[off+4:]))
		if n < groupMaskOffset || off+n > len(buf) {
			return 0, 0, malformed
		}
		if binary.LittleEndian.Uint32(buf[off:]) == relationProcessorCore {
			physical++
			for g := range int(binary.LittleEndian.Uint16(buf[off+groupCountOffset:])) {
				at := off + groupMaskOffset + g*groupAffinitySize
				if at+affinityMaskSize > off+n {
					return 0, 0, malformed
				}
				if affinityMaskSize == 8 {
					logical += bits.OnesCount64(binary.LittleEndian.Uint64(buf[at:]))
				} else {
					logical += bits.OnesCount32(binary.LittleEndian.Uint32(buf[at:]))
				}
			}
		}
		off += n
	}
	if physical == 0 {
		return 0, 0, errors.New("health: GetLogicalProcessorInformationEx listed no core")
	}
	return physical, logical, nil
}

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// RAMMb returns the RAM in MiB that GlobalMemoryStatusEx reports. It
// ignores r.
func RAMMb(Roots) (int, error) {
	m := memoryStatusEx{}
	m.Length = uint32(unsafe.Sizeof(m))
	if r1, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r1 == 0 { //nolint:gosec // an out parameter
		return 0, fmt.Errorf("health: GlobalMemoryStatusEx: %w", err)
	}
	return int(m.TotalPhys >> 20), nil //nolint:gosec // RAM in MiB fits an int
}
