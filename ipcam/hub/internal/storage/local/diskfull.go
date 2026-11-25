package local

import (
	"fmt"
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// syncFile is the driver's fsync of a file: every write it makes ends in one,
// so a disk that is full fails there at the latest. Tests replace it.
var syncFile = (*os.File).Sync

// quotaFull adds storage.ErrQuotaFull to an error that says the disk is full
// (isDiskFull), so that a full local destination is STORAGE_QUOTA_FULL, as a
// full Drive is.
func quotaFull(err error) error {
	if err != nil && isDiskFull(err) {
		return fmt.Errorf("%w: %w", storage.ErrQuotaFull, err)
	}
	return err
}
