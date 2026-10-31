package local_test

import "golang.org/x/sys/windows"

// diskFullErrors are the errors with which Windows refuses a write to a full
// disk: a full volume, and a full file system that a handle writes to.
func diskFullErrors() []error {
	return []error{windows.ERROR_DISK_FULL, windows.ERROR_HANDLE_DISK_FULL}
}
