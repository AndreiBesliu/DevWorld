package gdrive

import (
	"context"
	"errors"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// probeWaits bounds how many times one step of Probe waits for a create
	// slot.
	probeWaits = 10
	// warnNo409 is Probe's warning when Drive creates an existing folder ID
	// again instead of answering 409.
	warnNo409 = "Google Drive nu a răspuns 409 la crearea din nou a unui dosar existent; " +
		"hub-ul tratează răspunsul ca reușită, dar comportamentul diferă de cel așteptat."
)

// Probe checks the destination (§8.7 step 6, P35): it makes the root and
// stores its ID, creates the root's ID again to confirm Drive's 409 for a
// folder that exists, writes .ipcam/<hubID>/README.txt, uploads 1 MiB,
// reads a range of it back and deletes it (storage.ProbeObject, with
// md5Checksum), and reads the quota. The report holds the steps that passed,
// and the error is the first step that failed. A probe whose read fails
// deletes its object, best effort. Probe is a one-off check that makes
// several creates, so where an upload returns ErrRateLimited at once, Probe
// waits for its slot (C2-R11 (b)).
func (d *Drive) Probe(ctx context.Context) (storage.ProbeReport, error) {
	var report storage.ProbeReport
	var root string
	err := waitForSlot(ctx, func() error {
		chain, err := d.folders(ctx, ".", nil)
		if err == nil {
			root = chain[0].id
		}
		return err
	})
	if err != nil {
		return report, err
	}
	for created, i := true, 0; created; i++ {
		if i == 2 {
			report.Warnings = append(report.Warnings, warnNo409)
			break
		}
		// A first 200 means the root was gone and is made again.
		err = waitForSlot(ctx, func() (err error) {
			created, err = d.createFolder(ctx, root, d.rootName, myDrive, true, nil)
			return err
		})
		if err != nil {
			return report, err
		}
	}
	err = waitForSlot(ctx, func() error {
		_, err := d.PutSmall(ctx, storage.ProbeReadmeKey(d.hubID), []byte(storage.ProbeReadme))
		return err
	})
	if err != nil {
		return report, err
	}
	err = storage.ProbeObject(ctx, d, storage.ProbeParams{
		Driver: driverName, HubID: d.hubID, Hash: storage.HashMD5, PartSize: d.partSize,
		Wait: func(step func() error) error { return waitForSlot(ctx, step) },
	}, &report)
	if err != nil {
		return report, err
	}
	report.Quota, err = d.Quota(ctx)
	return report, err
}

// waitForSlot runs step, and runs it again after each ErrRateLimited that it
// returns once the wait has passed (a second when the error names none), at
// most probeWaits times.
func waitForSlot(ctx context.Context, step func() error) error {
	for i := 0; ; i++ {
		err := step()
		var limited storage.ErrRateLimited
		if !errors.As(err, &limited) || i == probeWaits {
			return err
		}
		wait := limited.RetryAfter
		if wait <= 0 {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
