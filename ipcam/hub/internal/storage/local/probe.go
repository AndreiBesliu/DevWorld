package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// openFile is OpenRange's open of an object's file. Tests replace it to make
// a probe's range read fail.
var openFile = os.Open

// Probe writes a small object, reads a range of it back, deletes it and reads
// the quota. The report holds the steps that passed, and the error is the
// first step that failed. A probe whose range read fails deletes its object,
// best effort, so that List never shows it.
func (l *Local) Probe(ctx context.Context) (storage.ProbeReport, error) {
	var report storage.ProbeReport
	data := []byte("ipcam: a probe of the destination\n")
	ref, err := l.PutSmall(ctx, probeKey, data)
	if err != nil {
		return report, err
	}
	report.Writable = true
	if err := l.probeRange(ctx, ref, data); err != nil {
		_, _ = l.Delete(context.WithoutCancel(ctx), ref) // the error to report is the range read's
		return report, err
	}
	report.RangeRead = true
	if _, err := l.Delete(ctx, ref); err != nil {
		return report, err
	}
	report.Deletable = true
	report.Quota, err = l.Quota(ctx)
	return report, err
}

// probeRange reads a range of the probe's object, ref, and compares it with
// the same range of data, the bytes that Probe wrote.
func (l *Local) probeRange(ctx context.Context, ref storage.ObjectRef, data []byte) error {
	const off, n = 7, 5
	r, err := l.OpenRange(ctx, ref, off, n)
	if err != nil {
		return err
	}
	got, err := io.ReadAll(r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("local: probe: %w", err)
	}
	if !bytes.Equal(got, data[off:off+n]) {
		return fmt.Errorf("local: probe: a range read returned %q, want %q: %w", got, data[off:off+n], storage.ErrPermanent)
	}
	return nil
}
