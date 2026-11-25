//go:build linux

package local

import "syscall"

// diskSpace reads dir's filesystem: free is what an unprivileged writer may
// still write (Bavail, never Bfree, which also counts the blocks reserved for
// root), and total is the filesystem's size. Bsize is int32 on linux/arm and
// the block counts are uint64, hence the explicit conversions.
func diskSpace(dir string) (free, total int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	free = int64(st.Bavail) * int64(st.Bsize)  //nolint:gosec // G115: a block count, far below 2^63
	total = int64(st.Blocks) * int64(st.Bsize) //nolint:gosec // G115: a block count, far below 2^63
	return free, total, nil
}
