package storage_test

import (
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestKind_SpecValues: each of §9.1's eight provider types has its constant,
// with the spec's spelling.
func TestKind_SpecValues(t *testing.T) {
	for kind, want := range map[storage.Kind]string{
		storage.KindLocal:            "LOCAL",
		storage.KindNASSMB:           "NAS_SMB",
		storage.KindGoogleDrive:      "GOOGLE_DRIVE",
		storage.KindOneDrivePersonal: "ONEDRIVE_PERSONAL",
		storage.KindOneDriveBusiness: "ONEDRIVE_BUSINESS",
		storage.KindS3:               "S3",
		storage.KindWebDAV:           "WEBDAV",
		storage.KindSFTP:             "SFTP",
	} {
		if string(kind) != want {
			t.Errorf("Kind %q, want %q", kind, want)
		}
	}
}
