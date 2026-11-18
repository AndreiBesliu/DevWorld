package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestProbe_Readme pins the owner-facing README that both cloud drivers'
// Probe write, and its key: the one golden of the text, which the drivers'
// tests compare with what reached their fakes.
func TestProbe_Readme(t *testing.T) {
	const want = "Acest folder conține înregistrările unui hub ipcam. Fișierele .age sunt criptate cu age " +
		"(https://age-encryption.org) și se deschid cu: age -d -i <cheia de recuperare> <fișier>.mp4.age > <fișier>.mp4"
	if storage.ProbeReadme != want {
		t.Errorf("ProbeReadme = %q, want %q", storage.ProbeReadme, want)
	}
	if got := storage.ProbeReadmeKey("hub-1"); got != ".ipcam/hub-1/README.txt" {
		t.Errorf("ProbeReadmeKey(hub-1) = %q, want .ipcam/hub-1/README.txt", got)
	}
}

// memDest is a Destination in memory, with switches that break one step of
// ProbeObject. Its embedded Destination is nil: ProbeObject calls none of
// the other methods.
type memDest struct {
	storage.Destination
	hash      storage.HashKind
	limited   int   // the ErrRateLimited answers of BeginUpload before it starts a session
	failPut   error // PutChunk's error
	stall     bool  // PutChunk confirms no byte
	badHash   bool  // Commit reports the hash of other bytes
	badRange  bool  // OpenRange returns other bytes
	failRange error // OpenRange's error

	calls     []string
	chunks    []int64
	pending   []byte
	committed map[storage.ObjectKey][]byte // every object that Commit made
	objects   map[storage.ObjectKey][]byte // the objects that Delete has not deleted
}

func newMemDest(hash storage.HashKind) *memDest {
	return &memDest{hash: hash, committed: map[storage.ObjectKey][]byte{}, objects: map[storage.ObjectKey][]byte{}}
}

func (m *memDest) BeginUpload(_ context.Context, key storage.ObjectKey, size int64, meta storage.Meta) (storage.UploadState, error) {
	m.calls = append(m.calls, "begin")
	if m.limited > 0 {
		m.limited--
		return storage.UploadState{}, fmt.Errorf("mem: begin: %w", storage.ErrRateLimited{RetryAfter: time.Millisecond})
	}
	if meta.ContentType != "application/octet-stream" {
		return storage.UploadState{}, fmt.Errorf("mem: content type %q: %w", meta.ContentType, storage.ErrPermanent)
	}
	m.pending = nil
	return storage.UploadState{Key: key, Size: size, Session: "mem-session"}, nil
}

func (m *memDest) PutChunk(_ context.Context, st *storage.UploadState, off int64, r io.Reader, n int64) error {
	m.calls = append(m.calls, "put")
	m.chunks = append(m.chunks, n)
	switch {
	case m.failPut != nil:
		return m.failPut
	case m.stall:
		return nil
	}
	b, err := io.ReadAll(r)
	if err != nil || off != int64(len(m.pending)) || int64(len(b)) != n {
		return fmt.Errorf("mem: put at %d of %d bytes (%v), want %d: %w", off, len(b), err, len(m.pending), storage.ErrPermanent)
	}
	m.pending = append(m.pending, b...)
	st.Offset = off + n
	return nil
}

func (m *memDest) Commit(_ context.Context, st *storage.UploadState) (storage.ObjectRef, error) {
	m.calls = append(m.calls, "commit")
	data := m.pending
	m.committed[st.Key], m.objects[st.Key] = data, data
	hashed := data
	if m.badHash {
		hashed = []byte("other bytes")
	}
	hash, err := storage.Digest(m.hash, bytes.NewReader(hashed))
	return storage.ObjectRef{ID: "mem-1", Key: st.Key, Size: int64(len(data)), Hash: hash, HashKind: m.hash}, err
}

func (m *memDest) Abort(context.Context, *storage.UploadState) error {
	m.calls = append(m.calls, "abort")
	m.pending = nil
	return nil
}

func (m *memDest) OpenRange(_ context.Context, ref storage.ObjectRef, off, n int64) (io.ReadCloser, error) {
	m.calls = append(m.calls, "range")
	if m.failRange != nil {
		return nil, m.failRange
	}
	got := slices.Clone(m.objects[ref.Key][off : off+n])
	if m.badRange {
		got[0] ^= 1
	}
	return io.NopCloser(bytes.NewReader(got)), nil
}

func (m *memDest) Delete(_ context.Context, ref storage.ObjectRef) (storage.DeleteOutcome, error) {
	m.calls = append(m.calls, "delete")
	delete(m.objects, ref.Key)
	return storage.DeletedRecycled, nil
}

// TestProbe_ObjectSteps: ProbeObject uploads 1 MiB of its pattern to
// .ipcam/<HubID>/probe.bin in chunks of PartSize, checks Commit's size and
// hash, reads a range back and deletes the object, and sets each step's
// field of the report as it passes. A step that fails ends it with its
// error: a failed upload is aborted, and an object whose hash or range read
// is wrong is deleted. Only Wait runs BeginUpload again.
func TestProbe_ObjectSteps(t *testing.T) {
	const kib = 1 << 10
	retry := func(step func() error) error { // as Drive's waitForSlot, without the wait
		for {
			err := step()
			var limited storage.ErrRateLimited
			if !errors.As(err, &limited) {
				return err
			}
		}
	}
	upload := []string{"begin", "put", "put", "put", "put", "commit"}
	tests := []struct {
		name       string
		hash       storage.HashKind
		set        func(m *memDest)
		wait       func(step func() error) error
		wantErr    error
		wantReport storage.ProbeReport
		wantCalls  []string
	}{
		{
			name: "every step passes, MD5", hash: storage.HashMD5,
			wantReport: storage.ProbeReport{Writable: true, RangeRead: true, Deletable: true},
			wantCalls:  append(slices.Clone(upload), "range", "delete"),
		},
		{
			name: "every step passes, QuickXor", hash: storage.HashQuickXor,
			wantReport: storage.ProbeReport{Writable: true, RangeRead: true, Deletable: true},
			wantCalls:  append(slices.Clone(upload), "range", "delete"),
		},
		{
			name: "Wait runs BeginUpload again", hash: storage.HashMD5,
			set: func(m *memDest) { m.limited = 2 }, wait: retry,
			wantReport: storage.ProbeReport{Writable: true, RangeRead: true, Deletable: true},
			wantCalls:  append([]string{"begin", "begin"}, append(slices.Clone(upload), "range", "delete")...),
		},
		{
			name: "without Wait, BeginUpload runs once", hash: storage.HashMD5,
			set:     func(m *memDest) { m.limited = 1 },
			wantErr: storage.ErrRateLimited{}, wantCalls: []string{"begin"},
		},
		{
			name: "a chunk fails", hash: storage.HashMD5,
			set:     func(m *memDest) { m.failPut = fmt.Errorf("mem: put: %w", storage.ErrTransient) },
			wantErr: storage.ErrTransient, wantCalls: []string{"begin", "put", "abort"},
		},
		{
			name: "the session confirms no byte", hash: storage.HashMD5,
			set:     func(m *memDest) { m.stall = true },
			wantErr: storage.ErrTransient, wantCalls: []string{"begin", "put", "abort"},
		},
		{
			name: "Commit's hash is of other bytes", hash: storage.HashQuickXor,
			set:     func(m *memDest) { m.badHash = true },
			wantErr: storage.ErrPermanent, wantCalls: append(slices.Clone(upload), "delete"),
		},
		{
			name: "the range read returns other bytes", hash: storage.HashMD5,
			set:        func(m *memDest) { m.badRange = true },
			wantErr:    storage.ErrPermanent,
			wantReport: storage.ProbeReport{Writable: true},
			wantCalls:  append(slices.Clone(upload), "range", "delete"),
		},
		{
			name: "the range read fails", hash: storage.HashMD5,
			set:        func(m *memDest) { m.failRange = fmt.Errorf("mem: range: %w", storage.ErrNotFound) },
			wantErr:    storage.ErrNotFound,
			wantReport: storage.ProbeReport{Writable: true},
			wantCalls:  append(slices.Clone(upload), "range", "delete"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemDest(tt.hash)
			if tt.set != nil {
				tt.set(m)
			}
			var report storage.ProbeReport
			err := storage.ProbeObject(t.Context(), m, storage.ProbeParams{
				Driver: "drv", HubID: "H", Hash: tt.hash, PartSize: 320 * kib, Wait: tt.wait,
			}, &report)
			switch {
			case tt.wantErr == nil && err != nil:
				t.Fatalf("ProbeObject: %v", err)
			case tt.wantErr != nil && !isKind(err, tt.wantErr):
				t.Fatalf("ProbeObject = %v, want %v", err, tt.wantErr)
			case tt.wantErr != nil && !strings.HasPrefix(err.Error(), "drv: probe: ") && !strings.HasPrefix(err.Error(), "mem: "):
				t.Errorf("ProbeObject's error %q starts with neither the driver's name nor the destination's", err)
			}
			if !reflect.DeepEqual(report, tt.wantReport) {
				t.Errorf("the report %+v, want %+v", report, tt.wantReport)
			}
			if !slices.Equal(m.calls, tt.wantCalls) {
				t.Errorf("the calls %v, want %v", m.calls, tt.wantCalls)
			}
			if len(m.objects) != 0 {
				t.Errorf("ProbeObject left %d objects", len(m.objects))
			}
			if !slices.Contains(m.calls, "commit") {
				return
			}
			if want := []int64{320 * kib, 320 * kib, 320 * kib, 64 * kib}; !slices.Equal(m.chunks, want) {
				t.Errorf("the chunks %v, want %v", m.chunks, want)
			}
			data, ok := m.committed[".ipcam/H/probe.bin"]
			if !ok || len(data) != storage.ProbeSize {
				t.Fatalf("the committed objects %d, want 1 MiB at .ipcam/H/probe.bin", len(m.committed))
			}
			for i, b := range data {
				if b != byte(i%251) {
					t.Fatalf("byte %d of the probe object is %d, want %d", i, b, i%251)
				}
			}
		})
	}
}

// isKind reports whether err is of want's kind: wraps want, or, for an
// ErrRateLimited, any ErrRateLimited.
func isKind(err, want error) bool {
	var limited storage.ErrRateLimited
	if errors.As(want, &limited) {
		return errors.As(err, &limited)
	}
	return errors.Is(err, want)
}
