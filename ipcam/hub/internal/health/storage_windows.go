package health

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The IOCTLs of winioctl.h that Storage sends, and their structures.
const (
	ioctlStorageQueryProperty        = 0x2D1400 // IOCTL_STORAGE_QUERY_PROPERTY
	ioctlStorageGetDeviceNumber      = 0x2D1080 // IOCTL_STORAGE_GET_DEVICE_NUMBER
	ioctlDiskGetCacheInformation     = 0x740D4  // IOCTL_DISK_GET_CACHE_INFORMATION
	storageDeviceSeekPenaltyProperty = 7        // STORAGE_PROPERTY_ID
	propertyStandardQuery            = 0        // STORAGE_QUERY_TYPE
)

// storagePropertyQuery is STORAGE_PROPERTY_QUERY.
type storagePropertyQuery struct {
	PropertyID           uint32
	QueryType            uint32
	AdditionalParameters [4]byte // BYTE[1], padded
}

// deviceSeekPenaltyDescriptor is DEVICE_SEEK_PENALTY_DESCRIPTOR.
type deviceSeekPenaltyDescriptor struct {
	Version           uint32
	Size              uint32
	IncursSeekPenalty byte
	_                 [3]byte
}

// storageDeviceNumber is STORAGE_DEVICE_NUMBER.
type storageDeviceNumber struct {
	DeviceType      uint32
	DeviceNumber    uint32
	PartitionNumber uint32
}

// diskCacheInformation is DISK_CACHE_INFORMATION; Prefetch is the union of
// ScalarPrefetch and BlockPrefetch.
type diskCacheInformation struct {
	ParametersSavable             byte
	ReadCacheEnabled              byte
	WriteCacheEnabled             byte
	_                             byte
	ReadRetentionPriority         int32
	WriteRetentionPriority        int32
	DisablePrefetchTransferLength uint16
	PrefetchScalar                byte
	_                             byte
	Prefetch                      [3]uint16
	_                             uint16
}

// Storage reports the storage under recDir and its write cache (G09), from
// the disk that holds recDir's volume:
//
//   - kind is "hdd" when IOCTL_STORAGE_QUERY_PROPERTY says that the disk
//     incurs a seek penalty (StorageDeviceSeekPenaltyProperty), else "ssd";
//   - writeCache is "on" or "off" as IOCTL_DISK_GET_CACHE_INFORMATION says,
//     which needs an elevated process.
//
// Each is "unknown" on any error, such as a volume that spans disks. It
// ignores r.
func Storage(_ Roots, recDir string) (kind, writeCache string) {
	disk, err := diskOf(recDir)
	if err != nil {
		return unknown, unknown
	}
	return seekPenalty(disk), writeCacheState(disk)
}

// diskOf returns the device path of the disk that holds dir's volume,
// \\.\PhysicalDrive<n>; dir need not exist yet.
func diskOf(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return "", err
	}
	mountPoint := make([]uint16, windows.MAX_LONG_PATH)
	if err := windows.GetVolumePathName(path, &mountPoint[0], uint32(len(mountPoint))); err != nil { //nolint:gosec // a small buffer's length
		return "", err
	}
	volume := make([]uint16, windows.MAX_PATH)
	if err := windows.GetVolumeNameForVolumeMountPoint(&mountPoint[0], &volume[0], uint32(len(volume))); err != nil { //nolint:gosec // a small buffer's length
		return "", err
	}
	// \\?\Volume{…}\ names the volume's root directory; without the last
	// backslash it names the volume device.
	h, err := openDevice(strings.TrimSuffix(windows.UTF16ToString(volume), `\`), 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var n storageDeviceNumber
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlStorageGetDeviceNumber, nil, 0,
		(*byte)(unsafe.Pointer(&n)), uint32(unsafe.Sizeof(n)), &returned, nil); err != nil { //nolint:gosec // the IOCTL's output buffer
		return "", err
	}
	return fmt.Sprintf(`\\.\PhysicalDrive%d`, n.DeviceNumber), nil
}

// openDevice opens a device for IOCTLs with access, which may be 0 for
// those that need no access right.
func openDevice(name string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

// seekPenalty is "hdd" or "ssd" for disk, or "unknown".
func seekPenalty(disk string) string {
	h, err := openDevice(disk, 0)
	if err != nil {
		return unknown
	}
	defer func() { _ = windows.CloseHandle(h) }()
	q := storagePropertyQuery{PropertyID: storageDeviceSeekPenaltyProperty, QueryType: propertyStandardQuery}
	var d deviceSeekPenaltyDescriptor
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty, (*byte)(unsafe.Pointer(&q)), uint32(unsafe.Sizeof(q)), //nolint:gosec // the IOCTL's input buffer
		(*byte)(unsafe.Pointer(&d)), uint32(unsafe.Sizeof(d)), &returned, nil); err != nil || returned < uint32(unsafe.Offsetof(d.IncursSeekPenalty))+1 { //nolint:gosec // its output buffer
		return unknown
	}
	if d.IncursSeekPenalty != 0 {
		return "hdd"
	}
	return "ssd"
}

// writeCacheState is "on" or "off" for disk's write cache, or "unknown".
func writeCacheState(disk string) string {
	h, err := openDevice(disk, windows.GENERIC_READ)
	if err != nil {
		return unknown
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var c diskCacheInformation
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlDiskGetCacheInformation, nil, 0,
		(*byte)(unsafe.Pointer(&c)), uint32(unsafe.Sizeof(c)), &returned, nil); err != nil || returned < uint32(unsafe.Offsetof(c.WriteCacheEnabled))+1 { //nolint:gosec // the IOCTL's output buffer
		return unknown
	}
	if c.WriteCacheEnabled != 0 {
		return "on"
	}
	return "off"
}
