package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
)

// ProbeReadme is the README that a cloud driver's Probe writes to
// ProbeReadmeKey (§8.7 step 6), for the owner who opens the folder: it is
// owner-facing, so Romanian.
const ProbeReadme = "Acest folder conține înregistrările unui hub ipcam. Fișierele .age sunt criptate cu age " +
	"(https://age-encryption.org) și se deschid cu: age -d -i <cheia de recuperare> <fișier>.mp4.age > <fișier>.mp4"

const (
	// ProbeSize is the size of ProbeObject's test object.
	ProbeSize = 1 << 20
	// probeName is the test object's name in .ipcam/<hubID>.
	probeName = "probe.bin"
	// probeOff and probeLen are the range that ProbeObject reads back.
	probeOff, probeLen = 1000, 4096
)

// ProbeReadmeKey is the key of hubID's README: .ipcam/<hubID>/README.txt.
func ProbeReadmeKey(hubID string) ObjectKey {
	return ObjectKey(path.Join(".ipcam", hubID, "README.txt"))
}

// ProbeParams are a driver's part in ProbeObject.
type ProbeParams struct {
	// Driver names the driver at the start of ProbeObject's own errors, as
	// it does its own ("gdrive").
	Driver string
	// HubID names the hub's folder, .ipcam/<HubID>, which holds the test
	// object, probe.bin.
	HubID string
	// Hash is the kind of hash that the driver's Commit returns.
	Hash HashKind
	// PartSize is the length of each chunk but the last.
	PartSize int64
	// Wait runs BeginUpload, its step, and runs it again for as long as it
	// waits for the driver's own limit: Drive's create slots, where only
	// Probe waits (C2-R11 (b)). nil runs it once.
	Wait func(step func() error) error
}

// ProbeObject is the part of a cloud driver's Probe that its test object
// takes (§8.7 step 6, P35). It uploads ProbeSize bytes to
// .ipcam/<HubID>/probe.bin through a session, as a segment's upload, and
// checks Commit's size and hash against Digest of the bytes. It then reads a
// range back and compares it with the same range of the bytes, and deletes
// the object. Each step that passes sets its field of report (Writable,
// RangeRead, Deletable), and the error is that of the first step that
// failed. An upload that fails is aborted, and an object whose check or read
// fails is deleted, best effort.
func ProbeObject(ctx context.Context, d Destination, p ProbeParams, report *ProbeReport) error {
	data := make([]byte, ProbeSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	ref, err := probeWrite(ctx, d, p, data)
	if err != nil {
		return err
	}
	report.Writable = true
	if err := probeRange(ctx, d, p, ref, data); err != nil {
		_, _ = d.Delete(context.WithoutCancel(ctx), ref) // the error to report is the read's
		return err
	}
	report.RangeRead = true
	if _, err := d.Delete(ctx, ref); err != nil {
		return err
	}
	report.Deletable = true
	return nil
}

// probeWrite uploads data as ProbeObject's object and checks Commit's size
// and hash.
func probeWrite(ctx context.Context, d Destination, p ProbeParams, data []byte) (ObjectRef, error) {
	key := ObjectKey(path.Join(".ipcam", p.HubID, probeName))
	wait := p.Wait
	if wait == nil {
		wait = func(step func() error) error { return step() }
	}
	var st UploadState
	err := wait(func() (err error) {
		st, err = d.BeginUpload(ctx, key, int64(len(data)), Meta{ContentType: "application/octet-stream"})
		return err
	})
	if err != nil {
		return ObjectRef{}, err
	}
	for off := int64(0); off < st.Size && err == nil; off = st.Offset {
		n := min(p.PartSize, st.Size-off)
		err = d.PutChunk(ctx, &st, off, bytes.NewReader(data[off:off+n]), n)
		if err == nil && st.Offset <= off {
			err = fmt.Errorf("%s: probe: the session holds %d bytes after a chunk at %d: %w", p.Driver, st.Offset, off, ErrTransient)
		}
	}
	var ref ObjectRef
	if err == nil {
		ref, err = d.Commit(ctx, &st)
	}
	if err != nil {
		_ = d.Abort(context.WithoutCancel(ctx), &st) // the error to report is the write's
		return ObjectRef{}, err
	}
	want, err := Digest(p.Hash, bytes.NewReader(data))
	if err == nil && (ref.Size != int64(len(data)) || ref.Hash != want) {
		err = fmt.Errorf("%s: probe: the destination holds %d bytes with %s %s, want %d with %s: %w",
			p.Driver, ref.Size, p.Hash, ref.Hash, len(data), want, ErrPermanent)
	}
	if err != nil {
		_, _ = d.Delete(context.WithoutCancel(ctx), ref)
		return ObjectRef{}, err
	}
	return ref, nil
}

// probeRange reads a range of ProbeObject's object, ref, and compares it
// with the same range of data, the bytes that it wrote.
func probeRange(ctx context.Context, d Destination, p ProbeParams, ref ObjectRef, data []byte) error {
	r, err := d.OpenRange(ctx, ref, probeOff, probeLen)
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
	if !bytes.Equal(got, data[probeOff:probeOff+probeLen]) {
		return fmt.Errorf("%s: probe: a range read returned %d bytes that differ from those written: %w", p.Driver, len(got), ErrPermanent)
	}
	return nil
}
