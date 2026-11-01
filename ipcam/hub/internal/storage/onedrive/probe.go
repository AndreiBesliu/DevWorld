package onedrive

import (
	"context"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// Probe checks the destination (§8.7 step 6, P35) in the app folder, where
// it creates no folder: it writes .ipcam/<hubID>/README.txt, uploads 1 MiB
// through a session, reads a range of it back and deletes it
// (storage.ProbeObject, with quickXorHash), and reads the quota. The deleted
// 1 MiB goes to the recycle bin, as every delete does, and the report's
// quota counts it in InBin. The report holds the steps that passed, and the
// error is the first step that failed. A probe whose read fails deletes its
// object, best effort. OneDrive has no create bucket, so Probe never waits.
func (o *OneDrive) Probe(ctx context.Context) (storage.ProbeReport, error) {
	var report storage.ProbeReport
	if _, err := o.PutSmall(ctx, storage.ProbeReadmeKey(o.hubID), []byte(storage.ProbeReadme)); err != nil {
		return report, err
	}
	err := storage.ProbeObject(ctx, o, storage.ProbeParams{
		Driver: driverName, HubID: o.hubID, Hash: storage.HashQuickXor, PartSize: o.partSize,
	}, &report)
	if err != nil {
		return report, err
	}
	report.Quota, err = o.Quota(ctx)
	return report, err
}
