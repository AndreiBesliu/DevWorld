package gdrive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// readmeText is the README that Probe writes to .ipcam/<hubID>/README.txt
// (§8.7 step 6).
const readmeText = "Acest folder conține înregistrările unui hub ipcam. Fișierele .age sunt criptate cu age " +
	"(https://age-encryption.org) și se deschid cu: age -d -i <cheia de recuperare> <fișier>.mp4.age > <fișier>.mp4"

const (
	// probeSize is Probe's write, and probeName its object's name in
	// .ipcam/<hubID>.
	probeSize = 1 << 20
	probeName = "probe.bin"
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
// reads a range of it back, deletes it and reads the quota. The report holds
// the steps that passed, and the error is the first step that failed. A
// probe whose read fails deletes its object, best effort. Probe is a one-off
// check that makes several creates, so where an upload returns
// ErrRateLimited at once, Probe waits for its slot.
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
		_, err := d.PutSmall(ctx, storage.ObjectKey(path.Join(".ipcam", d.hubID, "README.txt")), []byte(readmeText))
		return err
	})
	if err != nil {
		return report, err
	}
	data := make([]byte, probeSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	ref, err := d.probeWrite(ctx, data)
	if err != nil {
		return report, err
	}
	report.Writable = true
	if err := d.probeRange(ctx, ref, data); err != nil {
		_, _ = d.Delete(context.WithoutCancel(ctx), ref) // the error to report is the read's
		return report, err
	}
	report.RangeRead = true
	if _, err := d.Delete(ctx, ref); err != nil {
		return report, err
	}
	report.Deletable = true
	report.Quota, err = d.Quota(ctx)
	return report, err
}

// probeWrite uploads data as Probe's object, through a session as a
// segment's upload, and checks Commit's size and MD5.
func (d *Drive) probeWrite(ctx context.Context, data []byte) (storage.ObjectRef, error) {
	key := storage.ObjectKey(path.Join(".ipcam", d.hubID, probeName))
	var st storage.UploadState
	err := waitForSlot(ctx, func() (err error) {
		st, err = d.BeginUpload(ctx, key, int64(len(data)), storage.Meta{ContentType: "application/octet-stream"})
		return err
	})
	if err != nil {
		return storage.ObjectRef{}, err
	}
	for off := int64(0); off < st.Size && err == nil; off = st.Offset {
		n := min(d.partSize, st.Size-off)
		err = d.PutChunk(ctx, &st, off, bytes.NewReader(data[off:off+n]), n)
		if err == nil && st.Offset <= off {
			err = fmt.Errorf("gdrive: probe: the session holds %d bytes after a chunk at %d: %w", st.Offset, off, storage.ErrTransient)
		}
	}
	var ref storage.ObjectRef
	if err == nil {
		ref, err = d.Commit(ctx, &st)
	}
	if err != nil {
		_ = d.Abort(context.WithoutCancel(ctx), &st) // the error to report is the write's
		return storage.ObjectRef{}, err
	}
	want, err := storage.Digest(storage.HashMD5, bytes.NewReader(data))
	if err == nil && (ref.Size != int64(len(data)) || ref.Hash != want) {
		err = fmt.Errorf("gdrive: probe: Drive holds %d bytes with MD5 %s, want %d with %s: %w", ref.Size, ref.Hash, len(data), want, storage.ErrPermanent)
	}
	if err != nil {
		_, _ = d.Delete(context.WithoutCancel(ctx), ref)
		return storage.ObjectRef{}, err
	}
	return ref, nil
}

// probeRange reads a range of Probe's object, ref, and compares it with the
// same range of data, the bytes that Probe wrote.
func (d *Drive) probeRange(ctx context.Context, ref storage.ObjectRef, data []byte) error {
	const off, n = 1000, 4096
	r, err := d.OpenRange(ctx, ref, off, n)
	if err != nil {
		return err
	}
	got, err := io.ReadAll(r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(got, data[off:off+n]) {
		return fmt.Errorf("gdrive: probe: a range read returned %d bytes that differ from those written: %w", len(got), storage.ErrPermanent)
	}
	return nil
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
