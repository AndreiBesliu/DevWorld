// Package conformance is the storage conformance suite (STO-01, §9.1): the
// checks that every storage.Destination passes, whatever its provider. A
// new provider is a new driver that passes them, with no change to the
// rules, the outbox or the UI (ARH-05). Run runs them as subtests of a
// driver's test, by name:
//
//   - Caps/ResumableNotNone: the driver resumes an interrupted upload.
//   - Resume/KillAtRandomOffset: an upload killed at a chunk boundary, with
//     only its UploadState kept, resumes in a new driver and commits once,
//     one object at its key, with the size and the hash of its bytes.
//   - Resume/CompleteSessionDetected: Resume after Commit returns Size and
//     makes nothing.
//   - Resume/…/OneObjectPerKey: List shows one object at each key of those
//     two; it skips where List is unsupported (OneDrive personal, C2-R12).
//   - RangeRead: three ranges read back as written.
//   - Delete: Delete does what Caps.Delete says.
//   - Quota: Quota reads what Caps.Quota says.
//   - EmptyObjectRefused: an object holds one byte at least (C2-R12).
//
// CI runs it on the local driver, and on the Drive and OneDrive personal
// drivers through their fakes (hub/internal/storage/fakes). Every hub test
// also runs in ipcam-spike-recording's two 45-min jobs (g2-12), so each of
// the suite's uploads is Options.Size bytes, 3 × Caps.PartSize + 7 by
// default, and the fakes run with PartSize 256 KiB (Drive) and 320 KiB
// (Graph).
//
// Nightly runs on real test accounts are not built or run here (§14.5):
// Drive after the project moves to Production (OWN-07, before Faza 3);
// OneDrive personal is free when the registration is free (OWN-08) and is
// scheduled once the weekly secret rotation exists (OWN-23, Faza 1);
// OneDrive business (the CA test tenant, OWN-09), B2, R2, Wasabi and AWS
// SSE-KMS are Faza 0-bis (cost, §16.9).
package conformance

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// defaultChunk is the chunk size for a driver that declares no
	// PartSize, as the outbox sends.
	defaultChunk = 8 << 20
	// rangeLen is the length of RangeRead's ranges.
	rangeLen = 4096
	// rateLimitTries bounds the tries of a create that the destination
	// rate-limits, and maxRateLimitWait the wait before each.
	rateLimitTries   = 5
	maxRateLimitWait = 5 * time.Second
	// keyDir holds the suite's objects.
	keyDir = "conformance/"
)

// Options configure Run.
type Options struct {
	KillOffsets int   // Resume/KillAtRandomOffset's kills: 20 in CI
	Seed        int64 // draws the kill offsets and the objects' bytes
	Size        int64 // each upload's bytes; 0: 3 × Caps.PartSize + 7 bytes
}

// Report names Run's subtests relative to its test, such as
// "Resume/KillAtRandomOffset": those that ran, failed ones included, and
// those that skipped, a check of a capability that the driver lacks or a
// count that its List cannot make.
type Report struct{ Ran, Skipped []string }

// suite is one Run: the driver's Caps, and the bytes of each upload with
// their hash.
type suite struct {
	root    string // the name of Run's test
	newDest func(t *testing.T) storage.Destination
	caps    storage.Caps
	opts    Options
	chunk   int64
	data    []byte
	digest  string // Digest(caps.Hashes[0]) of data
	report  Report
}

// Run runs the suite's subtests on t, one at a time, on the destinations
// that newDest builds, and reports which ran. Each newDest call must build a
// new driver of the same storage, as a restarted hub does: the same root or
// account, the same folder store. Each subtest builds its own, and
// Resume/KillAtRandomOffset one before and one after each kill.
func Run(t *testing.T, newDest func(t *testing.T) storage.Destination, opts Options) Report {
	t.Helper()
	d := newDest(t)
	caps := d.Capabilities()
	s := &suite{root: t.Name(), newDest: newDest, caps: caps, opts: opts, chunk: caps.PartSize}
	if s.chunk <= 0 {
		s.chunk = defaultChunk
	}
	if s.opts.Size == 0 {
		s.opts.Size = 3*s.chunk + 7
	}
	if s.opts.Size < 1 || s.opts.KillOffsets < 1 {
		t.Fatalf("conformance: Options.Size %d and KillOffsets %d must be positive", opts.Size, opts.KillOffsets)
	}
	if len(s.caps.Hashes) == 0 {
		t.Fatalf("conformance: %s declares no hash in Caps.Hashes, with which the suite checks Commit", d.Kind())
	}
	s.data = objectBytes(s.opts.Size, s.opts.Seed)
	digest, err := storage.Digest(s.caps.Hashes[0], bytes.NewReader(s.data))
	if err != nil {
		t.Fatalf("conformance: %v", err)
	}
	s.digest = digest

	t.Run("Caps", func(t *testing.T) { s.run(t, "ResumableNotNone", s.resumableNotNone) })
	t.Run("Resume", func(t *testing.T) {
		s.run(t, "KillAtRandomOffset", s.killAtRandomOffset)
		s.run(t, "CompleteSessionDetected", s.completeSessionDetected)
	})
	s.run(t, "RangeRead", s.rangeRead)
	s.run(t, "Delete", s.delete)
	s.run(t, "Quota", s.quota)
	s.run(t, "EmptyObjectRefused", s.emptyObjectRefused)
	return s.report
}

// run runs check as t's subtest name and records it in the report.
func (s *suite) run(t *testing.T, name string, check func(t *testing.T)) {
	t.Run(name, func(t *testing.T) {
		defer func() {
			name := strings.TrimPrefix(t.Name(), s.root+"/")
			if t.Skipped() {
				s.report.Skipped = append(s.report.Skipped, name)
			} else {
				s.report.Ran = append(s.report.Ran, name)
			}
		}()
		check(t)
	})
}

// objectBytes returns size bytes drawn from seed.
func objectBytes(size, seed int64) []byte {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], uint64(seed)) //nolint:gosec // G115: a seed's bits, not a quantity
	data := make([]byte, size)
	_, _ = rand.NewChaCha8(key).Read(data)
	return data
}

// resumableNotNone fails a driver that cannot resume an interrupted upload
// (P24); every Phase 0 driver can.
func (s *suite) resumableNotNone(t *testing.T) {
	if s.caps.Resumable == storage.ResumeNone {
		t.Errorf("Caps.Resumable is ResumeNone: an interrupted upload starts again from 0")
	}
}

// skipUnresumable skips a resume check of a driver that declares
// ResumeNone, which Caps/ResumableNotNone fails.
func (s *suite) skipUnresumable(t *testing.T) {
	if s.caps.Resumable == storage.ResumeNone {
		t.Skip("Caps.Resumable is ResumeNone, which Caps/ResumableNotNone fails")
	}
}

// killOffsets draws Options.KillOffsets offsets from Options.Seed among the
// boundaries of the object's chunks: 0, a kill right after BeginUpload; the
// offset that each chunk confirms; and Size, a kill after the last chunk,
// before Commit.
func (s *suite) killOffsets() []int64 {
	var bounds []int64
	for off := int64(0); off < s.opts.Size; off += s.chunk {
		bounds = append(bounds, off)
	}
	bounds = append(bounds, s.opts.Size)
	r := rand.New(rand.NewPCG(uint64(s.opts.Seed), 0)) //nolint:gosec // G404, G115: a reproducible draw from a seed, not a secret
	kills := make([]int64, s.opts.KillOffsets)
	for i := range kills {
		kills[i] = bounds[r.IntN(len(bounds))]
	}
	return kills
}

// killAtRandomOffset stops each upload once a chunk confirmed offset K, or
// right after BeginUpload for K = 0. It keeps only the UploadState's JSON, as
// the outbox keeps it in the catalog, drops the destination and resumes in
// a new one (Resume ≥ K), which sends the rest and commits once. Commit must
// give the size and the hash of the bytes, and List one object at the key;
// each object is then deleted, so that a local disk holds one at a time.
func (s *suite) killAtRandomOffset(t *testing.T) {
	s.skipUnresumable(t)
	ctx := t.Context()
	kills := s.killOffsets()
	t.Logf("kill offsets %v of %d bytes in chunks of %d", kills, s.opts.Size, s.chunk)
	var listed []listing
	var unlisted error
	for i, k := range kills {
		key := storage.ObjectKey(fmt.Sprintf("%skill-%02d.bin", keyDir, i))
		killed := s.newDest(t)
		st := s.begin(t, killed, key)
		s.send(t, killed, &st, 0, k)
		saved, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}

		d := s.newDest(t)
		var resumed storage.UploadState
		if err := json.Unmarshal(saved, &resumed); err != nil {
			t.Fatal(err)
		}
		next, err := d.Resume(ctx, &resumed)
		if err != nil || next < k || next > s.opts.Size {
			t.Fatalf("kill %d at %d of %s: Resume in a new destination = %d, %v; want %d to %d", i, k, key, next, err, k, s.opts.Size)
		}
		s.send(t, d, &resumed, next, s.opts.Size)
		ref, err := d.Commit(ctx, &resumed)
		if err != nil {
			t.Fatalf("kill %d at %d of %s: Commit: %v", i, k, key, err)
		}
		s.checkRef(t, ref, key)
		l, err := list(ctx, d, ref)
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			unlisted = err
		case err != nil:
			t.Fatalf("kill %d at %d: %v", i, k, err)
		default:
			listed = append(listed, l)
		}
		if _, err := d.Delete(ctx, ref); err != nil {
			t.Fatalf("kill %d at %d: Delete of %s: %v", i, k, key, err)
		}
	}
	s.oneObjectPerKey(t, listed, unlisted)
}

// completeSessionDetected resumes a committed upload twice: in its
// destination, and in a new one from the UploadState that its last chunk
// left, as a hub restarted between Commit and its record does. Each Resume
// returns Size, the new destination's Commit returns the same object, and
// nothing changes that object or makes another.
func (s *suite) completeSessionDetected(t *testing.T) {
	s.skipUnresumable(t)
	ctx := t.Context()
	key := storage.ObjectKey(keyDir + "complete.bin")
	d := s.newDest(t)
	st := s.begin(t, d, key)
	s.send(t, d, &st, 0, s.opts.Size)
	saved, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := d.Commit(ctx, &st)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	s.checkRef(t, ref, key)
	before, err := d.Stat(ctx, ref)
	if err != nil {
		t.Fatalf("Stat after Commit: %v", err)
	}
	if next, err := d.Resume(ctx, &st); err != nil || next != s.opts.Size {
		t.Errorf("Resume after Commit = %d, %v; want %d", next, err, s.opts.Size)
	}

	fresh := s.newDest(t)
	var again storage.UploadState
	if err := json.Unmarshal(saved, &again); err != nil {
		t.Fatal(err)
	}
	if next, err := fresh.Resume(ctx, &again); err != nil || next != s.opts.Size {
		t.Fatalf("Resume after Commit, in a new destination = %d, %v; want %d", next, err, s.opts.Size)
	}
	if ref2, err := fresh.Commit(ctx, &again); err != nil || ref2.ID != ref.ID || ref2.Size != ref.Size || ref2.Hash != ref.Hash {
		t.Errorf("Commit after that Resume = %+v, %v; want %+v", ref2, err, ref)
	}
	if after, err := fresh.Stat(ctx, ref); err != nil || after != before {
		t.Errorf("Stat after the resumes = %+v, %v; want it unchanged, %+v", after, err, before)
	}
	l, err := list(ctx, fresh, ref)
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		s.oneObjectPerKey(t, nil, err)
	case err != nil:
		t.Fatal(err)
	default:
		s.oneObjectPerKey(t, []listing{l}, nil)
	}
}

// listing is what List showed at a key right after the object ref was
// committed there.
type listing struct {
	ref   storage.ObjectRef
	found []storage.ObjectRef // the objects at ref's key
}

// list lists d at ref's key. A List that yields errors.ErrUnsupported, as
// OneDrive personal's does (C2-R12), returns it.
func list(ctx context.Context, d storage.Destination, ref storage.ObjectRef) (listing, error) {
	l := listing{ref: ref}
	for info, err := range d.List(ctx, ref.Key) {
		if err != nil {
			return l, fmt.Errorf("List(%s): %w", ref.Key, err)
		}
		if info.Ref.Key == ref.Key {
			l.found = append(l.found, info.Ref)
		}
	}
	return l, nil
}

// oneObjectPerKey checks, as the subtest OneObjectPerKey, that List showed
// one object at each key, Commit's: a resumed upload made no second one. A
// destination whose List is unsupported cannot be counted, and the subtest
// skips with unlisted, that List's error. Where a key is an object's
// address, as on OneDrive, a second object at it cannot exist.
func (s *suite) oneObjectPerKey(t *testing.T, listed []listing, unlisted error) {
	s.run(t, "OneObjectPerKey", func(t *testing.T) {
		if unlisted != nil {
			t.Skipf("the objects at a key cannot be counted: %v", unlisted)
		}
		for _, l := range listed {
			if len(l.found) != 1 || l.found[0].ID != l.ref.ID || l.found[0].Size != l.ref.Size {
				t.Errorf("List(%s) = %+v, want Commit's object only, %+v", l.ref.Key, l.found, l.ref)
			}
		}
	})
}

// rangeRead reads three ranges of an object back: from its start, across
// the end of its first chunk, and to its last byte.
func (s *suite) rangeRead(t *testing.T) {
	if !s.caps.RangeRead {
		t.Skip("Caps.RangeRead is false")
	}
	d := s.newDest(t)
	ref := s.upload(t, d, keyDir+"range.bin")
	size := s.opts.Size
	n := min(rangeLen, size)
	for _, off := range []int64{0, max(min(s.chunk, size)-n/2, 0), size - n} {
		n := min(n, size-off)
		r, err := d.OpenRange(t.Context(), ref, off, n)
		if err != nil {
			t.Fatalf("OpenRange(%d, %d): %v", off, n, err)
		}
		got, err := io.ReadAll(r)
		if cerr := r.Close(); err == nil {
			err = cerr
		}
		if err != nil || !bytes.Equal(got, s.data[off:off+n]) {
			t.Errorf("OpenRange(%d, %d) read %d bytes, %v; want the %d bytes written there", off, n, len(got), err, n)
		}
	}
}

// delete deletes an object as Caps.Delete says: with DeletePermanent the
// object is gone and Stat finds nothing; with DeleteRecycleBin it is in a
// recycle bin, whose bytes the quota counts.
func (s *suite) delete(t *testing.T) {
	ctx := t.Context()
	d := s.newDest(t)
	ref := s.upload(t, d, keyDir+"delete.bin")
	switch s.caps.Delete {
	case storage.DeletePermanent:
		if out, err := d.Delete(ctx, ref); err != nil || out != storage.DeletedPermanent {
			t.Fatalf("Delete = %v, %v; want DeletedPermanent", out, err)
		}
		if info, err := d.Stat(ctx, ref); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Stat after a permanent Delete = %+v, %v; want ErrNotFound", info, err)
		}
	case storage.DeleteRecycleBin:
		before, err := d.Quota(ctx)
		if err != nil {
			t.Fatalf("Quota: %v", err)
		}
		if out, err := d.Delete(ctx, ref); err != nil || out != storage.DeletedRecycled {
			t.Fatalf("Delete = %v, %v; want DeletedRecycled", out, err)
		}
		if after, err := d.Quota(ctx); err != nil || after.InBin <= before.InBin {
			t.Errorf("Quota after a Delete to the recycle bin = %+v, %v; want InBin above %d", after, err, before.InBin)
		}
	default:
		t.Errorf("Caps.Delete %d is no DeleteKind", s.caps.Delete)
	}
}

// quota reads the quota that Caps.Quota declares: an account's holds its
// usage, and a filesystem has room.
func (s *suite) quota(t *testing.T) {
	if s.caps.Quota == storage.QuotaNone {
		t.Skip("Caps.Quota is QuotaNone")
	}
	q, err := s.newDest(t).Quota(t.Context())
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	switch s.caps.Quota {
	case storage.QuotaAccount:
		if q.Total < q.Used || q.Free < 0 {
			t.Errorf("an account's Quota = %+v, want Total ≥ Used and Free ≥ 0", q)
		}
	case storage.QuotaFilesystem:
		if q.Free <= 0 {
			t.Errorf("a filesystem's Quota = %+v, want Free > 0", q)
		}
	default:
		t.Errorf("Caps.Quota %d is no QuotaKind", s.caps.Quota)
	}
}

// emptyObjectRefused refuses an upload of 0 bytes and a small write of none
// with ErrPermanent, on every destination (storage.Destination, C2-R12).
func (s *suite) emptyObjectRefused(t *testing.T) {
	ctx := t.Context()
	d := s.newDest(t)
	if st, err := d.BeginUpload(ctx, keyDir+"empty.bin", 0, storage.Meta{}); !errors.Is(err, storage.ErrPermanent) {
		t.Errorf("BeginUpload of 0 bytes = %+v, %v; want ErrPermanent", st, err)
	}
	if ref, err := d.PutSmall(ctx, keyDir+"empty.json", nil); !errors.Is(err, storage.ErrPermanent) {
		t.Errorf("PutSmall of no data = %+v, %v; want ErrPermanent", ref, err)
	}
}

// upload uploads the suite's bytes to key through d and commits them.
func (s *suite) upload(t *testing.T, d storage.Destination, key storage.ObjectKey) storage.ObjectRef {
	t.Helper()
	st := s.begin(t, d, key)
	s.send(t, d, &st, 0, s.opts.Size)
	ref, err := d.Commit(t.Context(), &st)
	if err != nil {
		t.Fatalf("Commit of %s: %v", key, err)
	}
	s.checkRef(t, ref, key)
	return ref
}

// begin begins an upload of the suite's bytes to key through d.
func (s *suite) begin(t *testing.T, d storage.Destination, key storage.ObjectKey) storage.UploadState {
	t.Helper()
	var st storage.UploadState
	err := retryRateLimited(t, func() (err error) {
		st, err = d.BeginUpload(t.Context(), key, s.opts.Size, storage.Meta{ContentType: "application/octet-stream"})
		return err
	})
	if err != nil {
		t.Fatalf("BeginUpload of %s: %v", key, err)
	}
	return st
}

// send sends the suite's bytes from off through st's session, as the
// outbox does: chunks of the driver's PartSize, each from the offset that
// the last one confirmed, until the session holds until bytes.
func (s *suite) send(t *testing.T, d storage.Destination, st *storage.UploadState, off, until int64) {
	t.Helper()
	for off < until {
		n := min(s.chunk, s.opts.Size-off)
		if err := d.PutChunk(t.Context(), st, off, bytes.NewReader(s.data[off:off+n]), n); err != nil {
			t.Fatalf("PutChunk of %s at %d: %v", st.Key, off, err)
		}
		if st.Offset <= off || st.Offset > s.opts.Size {
			t.Fatalf("PutChunk of %s at %d confirmed %d bytes", st.Key, off, st.Offset)
		}
		off = st.Offset
	}
}

// checkRef checks Commit's object of key: an ID, the suite's size, and the
// Digest of its bytes in the driver's first hash kind.
func (s *suite) checkRef(t *testing.T, ref storage.ObjectRef, key storage.ObjectKey) {
	t.Helper()
	kind := s.caps.Hashes[0]
	if ref.Key != key || ref.ID == "" || ref.Size != s.opts.Size || ref.HashKind != kind || ref.Hash != s.digest {
		t.Fatalf("Commit of %s = %+v; want an ID, %d bytes and %s %s", key, ref, s.opts.Size, kind, s.digest)
	}
}

// retryRateLimited runs create, and again after each ErrRateLimited that it
// returns, once the wait that the error names has passed (a second when it
// names none, at most maxRateLimitWait): a driver returns ErrRateLimited
// without sleeping (P26), as Drive's create bucket does (C2-R11 (b)), and
// the suite waits as the outbox does.
func retryRateLimited(t *testing.T, create func() error) error {
	for try := 1; ; try++ {
		err := create()
		var limited storage.ErrRateLimited
		if !errors.As(err, &limited) || try == rateLimitTries {
			return err
		}
		wait := min(limited.RetryAfter, maxRateLimitWait)
		if wait <= 0 {
			wait = time.Second
		}
		select {
		case <-t.Context().Done():
			return err
		case <-time.After(wait):
		}
	}
}
