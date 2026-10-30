package local_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
)

const mib = 1 << 20

// TestLocal_CapsGolden (P24): the local driver declares exactly these Caps,
// every other field zero.
func TestLocal_CapsGolden(t *testing.T) {
	dst := newLocal(t, t.TempDir())
	want := storage.Caps{
		Resumable: storage.ResumeOffsetWrite,
		PartSize:  8 * mib,
		Hashes:    []storage.HashKind{storage.HashSHA256},
		Delete:    storage.DeletePermanent,
		Quota:     storage.QuotaFilesystem,
		Playback:  storage.LinkLocalFile,
		RangeRead: true,
	}
	if got := dst.Capabilities(); !reflect.DeepEqual(got, want) {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
	if got := dst.Kind(); got != storage.KindLocal {
		t.Errorf("Kind() = %q, want %q", got, storage.KindLocal)
	}
}

// TestLocal_WriteVerifyRange: an upload in PartSize chunks commits, Stat
// gives its size and SHA-256, and a range read returns exactly its bytes.
func TestLocal_WriteVerifyRange(t *testing.T) {
	ctx := t.Context()
	dst := newLocal(t, t.TempDir())
	data := randomBytes(20*mib+3, 1)

	ref := upload(t, dst, "cam-1/seg.mp4", data)
	if ref.Size != int64(len(data)) || ref.Hash != sha256Hex(data) || ref.HashKind != storage.HashSHA256 {
		t.Errorf("Commit = size %d, %s %s; want size %d, SHA256 %s", ref.Size, ref.HashKind, ref.Hash, len(data), sha256Hex(data))
	}
	info, err := dst.Stat(ctx, ref)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Ref.Size != int64(len(data)) || info.Ref.Hash != sha256Hex(data) || info.Ref.HashKind != storage.HashSHA256 {
		t.Errorf("Stat = size %d, %s %s; want size %d, SHA256 %s", info.Ref.Size, info.Ref.HashKind, info.Ref.Hash, len(data), sha256Hex(data))
	}

	const off, n = 10*mib - 5, 4096
	if got := readRange(t, dst, ref, off, n); !bytes.Equal(got, data[off:off+n]) {
		t.Errorf("OpenRange(%d, %d) returned %d bytes that differ from the upload's", off, n, len(got))
	}
}

// TestLocal_ResumeAfterPartialWrite: after a restart, an upload whose .part
// holds 3 MiB + 5 bytes resumes there, not at 0, and completes.
func TestLocal_ResumeAfterPartialWrite(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)
	data := randomBytes(9*mib, 2)
	const left = 3*mib + 5

	st, err := dst.BeginUpload(ctx, "cam-1/seg.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if err := dst.PutChunk(ctx, &st, 0, bytes.NewReader(data[:left]), left); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	part := filepath.Join(root, "cam-1", "seg.mp4.part")
	if fi, err := os.Stat(part); err != nil || fi.Size() != left {
		t.Fatalf("after the first chunk, %s: %v, %v; want %d bytes", part, fi, err, left)
	}

	// A new driver on the same root, as after a hub restart. The state's
	// Offset is 0, so only the .part can tell where the upload stands.
	dst = newLocal(t, root)
	resumed := storage.UploadState{Key: st.Key, Size: st.Size}
	next, err := dst.Resume(ctx, &resumed)
	if err != nil || next != left {
		t.Fatalf("Resume = %d, %v; want %d", next, err, left)
	}
	putChunks(t, dst, &resumed, data, next)
	ref, err := dst.Commit(ctx, &resumed)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ref.Size != int64(len(data)) || ref.Hash != sha256Hex(data) {
		t.Errorf("Commit = size %d, hash %s; want %d, %s", ref.Size, ref.Hash, len(data), sha256Hex(data))
	}
}

// TestLocal_ResumeDetectsComplete: after Commit, Resume on the state saved
// before it returns Size, and Commit on that state again returns the same
// object, as when the hub stops before it records the commit.
func TestLocal_ResumeDetectsComplete(t *testing.T) {
	ctx := t.Context()
	dst := newLocal(t, t.TempDir())
	data := randomBytes(mib+7, 3)

	st, err := dst.BeginUpload(ctx, "cam-1/seg.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	putChunks(t, dst, &st, data, 0)
	saved := st
	ref, err := dst.Commit(ctx, &st)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	next, err := dst.Resume(ctx, &saved)
	if err != nil || next != saved.Size {
		t.Fatalf("Resume after Commit = %d, %v; want %d", next, err, saved.Size)
	}
	again, err := dst.Commit(ctx, &saved)
	if err != nil || again != ref {
		t.Errorf("Commit again = %+v, %v; want %+v", again, err, ref)
	}
}

// TestLocal_Quota (P32): Free and Total agree with an independent reading of
// the same filesystem, and Free drops when 64 MiB reach the disk. Other tests,
// c1's among them, write and delete files on the same disk meanwhile, hence
// the retries.
func TestLocal_Quota(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)

	for attempt := 1; ; attempt++ {
		q, err := dst.Quota(ctx)
		if err != nil {
			t.Fatalf("Quota: %v", err)
		}
		free, total := independentQuota(t, root)
		if q.Used != q.Total-q.Free || q.InBin != 0 || q.Unknown {
			t.Errorf("Quota() = %+v; want Used = Total - Free, no InBin, not Unknown", q)
		}
		if within(q.Free, free, 0.01) && within(q.Total, total, 0.01) {
			break
		}
		t.Logf("attempt %d: Quota() Free %d, Total %d; independent reading Free %d, Total %d", attempt, q.Free, q.Total, free, total)
		if attempt == 2 {
			t.Errorf("Quota()'s Free and Total are not within 1 %% of the independent reading")
			break
		}
	}

	const written = 64 * mib
	for attempt := 1; ; attempt++ {
		before := freeBytes(t, dst)
		fill := filepath.Join(root, fmt.Sprintf("fill-%d", attempt))
		writeSynced(t, fill, randomBytes(written, uint64(attempt)))
		after := freeBytes(t, dst)
		if err := os.Remove(fill); err != nil {
			t.Fatal(err)
		}
		if drop := before - after; drop >= 32*mib && drop <= 256*mib {
			break
		}
		t.Logf("attempt %d: Free %d before and %d after %d bytes with fsync: a drop of %d", attempt, before, after, written, before-after)
		if attempt == 3 {
			t.Errorf("Free never dropped by 32-256 MiB after %d bytes with fsync", written)
			break
		}
	}
}

// TestLocal_CommitCreatesKeyDirectories: keys hold '/', so an upload and a
// PutSmall under an empty root create the key's directories, and leave only
// the objects behind.
func TestLocal_CommitCreatesKeyDirectories(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)

	data := randomBytes(1000, 4)
	upload(t, dst, "cam-1/2026-10-30/seg.mp4", data)
	small := []byte("small object")
	ref, err := dst.PutSmall(ctx, "meta/hub-1/catalog.json", small)
	if err != nil {
		t.Fatalf("PutSmall: %v", err)
	}
	if ref.Size != int64(len(small)) || ref.Hash != sha256Hex(small) {
		t.Errorf("PutSmall = size %d, hash %s; want %d, %s", ref.Size, ref.Hash, len(small), sha256Hex(small))
	}

	want := map[string][]byte{"cam-1/2026-10-30/seg.mp4": data, "meta/hub-1/catalog.json": small}
	if got := filesUnder(t, root); !reflect.DeepEqual(got, want) {
		t.Errorf("files under the root = %v; want exactly %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

// TestLocal_PutSmallReplaces: a second PutSmall to the same key replaces the
// object.
func TestLocal_PutSmallReplaces(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)

	for _, data := range []string{"first version", "second"} {
		if _, err := dst.PutSmall(ctx, "meta/catalog.json", []byte(data)); err != nil {
			t.Fatalf("PutSmall(%q): %v", data, err)
		}
	}
	want := map[string][]byte{"meta/catalog.json": []byte("second")}
	if got := filesUnder(t, root); !reflect.DeepEqual(got, want) {
		t.Errorf("files under the root = %q; want %q", got, want)
	}
}

// TestLocal_RejectsKeysOutsideRoot: a key that is not a clean relative path
// with '/', or that ends in the driver's own suffixes, is refused, and
// nothing is written.
func TestLocal_RejectsKeysOutsideRoot(t *testing.T) {
	ctx := t.Context()
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	dst := newLocal(t, root)

	for _, key := range []storage.ObjectKey{"", ".", "..", "../x", "/x", "a/../b", "a//b", "a/", `a\b`, "x.part", "a/x.tmp"} {
		if _, err := dst.BeginUpload(ctx, key, 1, storage.Meta{}); !errors.Is(err, storage.ErrPermanent) {
			t.Errorf("BeginUpload(%q) = %v; want ErrPermanent", key, err)
		}
		if _, err := dst.PutSmall(ctx, key, []byte("x")); !errors.Is(err, storage.ErrPermanent) {
			t.Errorf("PutSmall(%q) = %v; want ErrPermanent", key, err)
		}
	}
	if got := filesUnder(t, parent); len(got) != 0 {
		t.Errorf("files written: %v", slices.Sorted(maps.Keys(got)))
	}
}

// TestLocal_ListSkipsUnfinishedUploads: List yields the committed objects
// whose key starts with the prefix, never a .part.
func TestLocal_ListSkipsUnfinishedUploads(t *testing.T) {
	ctx := t.Context()
	dst := newLocal(t, t.TempDir())

	upload(t, dst, "cam-1/a.mp4", []byte("aaaa"))
	upload(t, dst, "cam-1/sub/b.mp4", []byte("bb"))
	upload(t, dst, "cam-10/c.mp4", []byte("c"))
	if _, err := dst.BeginUpload(ctx, "cam-1/open.mp4", 10, storage.Meta{}); err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}

	for prefix, want := range map[storage.ObjectKey]map[storage.ObjectKey]int64{
		"cam-1/":  {"cam-1/a.mp4": 4, "cam-1/sub/b.mp4": 2},
		"cam-1/s": {"cam-1/sub/b.mp4": 2},
		"":        {"cam-1/a.mp4": 4, "cam-1/sub/b.mp4": 2, "cam-10/c.mp4": 1},
		"cam-9/":  {},
	} {
		got := map[storage.ObjectKey]int64{}
		for info, err := range dst.List(ctx, prefix) {
			if err != nil {
				t.Fatalf("List(%q): %v", prefix, err)
			}
			got[info.Ref.Key] = info.Ref.Size
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("List(%q) = %v; want %v", prefix, got, want)
		}
	}
}

// TestLocal_ListThroughLinkedRoot: List walks a root whose path is a link to
// the directory, which the driver's other methods follow too.
func TestLocal_ListThroughLinkedRoot(t *testing.T) {
	link := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}
	dst := newLocal(t, link)
	upload(t, dst, "cam-1/a.mp4", []byte("aaaa"))

	var got []storage.ObjectKey
	for info, err := range dst.List(t.Context(), "") {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		got = append(got, info.Ref.Key)
	}
	if want := []storage.ObjectKey{"cam-1/a.mp4"}; !slices.Equal(got, want) {
		t.Errorf("List through a linked root = %v; want %v", got, want)
	}
}

// TestLocal_DeleteIsPermanent: Delete removes the file, and the object is
// then not found.
func TestLocal_DeleteIsPermanent(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)
	ref := upload(t, dst, "cam-1/seg.mp4", []byte("segment"))

	if outcome, err := dst.Delete(ctx, ref); err != nil || outcome != storage.DeletedPermanent {
		t.Fatalf("Delete = %v, %v; want DeletedPermanent", outcome, err)
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", "seg.mp4")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the file after Delete: %v; want it gone", err)
	}
	if _, err := dst.Stat(ctx, ref); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after Delete = %v; want ErrNotFound", err)
	}
	if _, err := dst.Delete(ctx, ref); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Delete again = %v; want ErrNotFound", err)
	}
}

// TestLocal_AbortRemovesPart: Abort removes the .part, so the upload cannot
// resume.
func TestLocal_AbortRemovesPart(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)

	st, err := dst.BeginUpload(ctx, "cam-1/seg.mp4", 10, storage.Meta{})
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if err := dst.PutChunk(ctx, &st, 0, strings.NewReader("12345"), 5); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if err := dst.Abort(ctx, &st); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if got := filesUnder(t, root); len(got) != 0 {
		t.Errorf("files after Abort: %v", slices.Sorted(maps.Keys(got)))
	}
	if next, err := dst.Resume(ctx, &st); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("Resume after Abort = %d, %v; want ErrSessionExpired", next, err)
	}
}

// TestLocal_PlaybackURLIsTheFile: the playback link of a local object is a
// file URL of its file.
func TestLocal_PlaybackURLIsTheFile(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	dst := newLocal(t, root)
	ref := upload(t, dst, "cam-1/seg.mp4", []byte("segment"))

	link, err := dst.PlaybackURL(ctx, ref, 0)
	if err != nil {
		t.Fatalf("PlaybackURL: %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("PlaybackURL = %q: %v", link, err)
	}
	path := u.Path
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/") // file:///C:/dir
	}
	if want := filepath.Join(root, "cam-1", "seg.mp4"); u.Scheme != "file" || filepath.FromSlash(path) != want {
		t.Errorf("PlaybackURL = %q; want the file URL of %s", link, want)
	}
}

// TestLocal_ProbeLeavesNoObject: Probe writes, reads and deletes its test
// object, reads the quota, and leaves the root as it found it.
func TestLocal_ProbeLeavesNoObject(t *testing.T) {
	root := t.TempDir()
	dst := newLocal(t, root)

	report, err := dst.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !report.Writable || !report.RangeRead || !report.Deletable || report.Quota.Free <= 0 {
		t.Errorf("Probe = %+v; want writable, range read, deletable and a free space", report)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Errorf("the root after Probe: %v, %v; want it empty", entries, err)
	}
}

// newLocal returns the driver on root.
func newLocal(t *testing.T, root string) storage.Destination {
	t.Helper()
	dst, err := local.New(root)
	if err != nil {
		t.Fatalf("New(%s): %v", root, err)
	}
	return dst
}

// upload writes data to key in PartSize chunks and commits it.
func upload(t *testing.T, dst storage.Destination, key storage.ObjectKey, data []byte) storage.ObjectRef {
	t.Helper()
	st, err := dst.BeginUpload(t.Context(), key, int64(len(data)), storage.Meta{ContentType: "video/mp4"})
	if err != nil {
		t.Fatalf("BeginUpload(%s): %v", key, err)
	}
	putChunks(t, dst, &st, data, 0)
	ref, err := dst.Commit(t.Context(), &st)
	if err != nil {
		t.Fatalf("Commit(%s): %v", key, err)
	}
	return ref
}

// putChunks sends data from offset from on, in PartSize chunks.
func putChunks(t *testing.T, dst storage.Destination, st *storage.UploadState, data []byte, from int64) {
	t.Helper()
	part := dst.Capabilities().PartSize
	for off := from; off < int64(len(data)); off += part {
		n := min(part, int64(len(data))-off)
		if err := dst.PutChunk(t.Context(), st, off, bytes.NewReader(data[off:off+n]), n); err != nil {
			t.Fatalf("PutChunk(%d, %d): %v", off, n, err)
		}
	}
}

// readRange returns the n bytes of ref from off, as OpenRange reads them.
func readRange(t *testing.T, dst storage.Destination, ref storage.ObjectRef, off, n int64) []byte {
	t.Helper()
	rc, err := dst.OpenRange(t.Context(), ref, off, n)
	if err != nil {
		t.Fatalf("OpenRange(%d, %d): %v", off, n, err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("OpenRange(%d, %d): read: %v", off, n, err)
	}
	return got
}

// freeBytes returns Quota().Free.
func freeBytes(t *testing.T, dst storage.Destination) int64 {
	t.Helper()
	q, err := dst.Quota(t.Context())
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	return q.Free
}

// writeSynced writes data to a new file at path and fsyncs it.
func writeSynced(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.Create(path) //nolint:gosec // a file in the test's temporary root
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
}

// filesUnder returns the files under dir, by their slash path relative to
// it, with their contents.
func filesUnder(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // a file under the test's temporary root
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = data
		return err
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

// within reports whether got is within frac of want.
func within(got, want int64, frac float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) <= frac*float64(want)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// randomBytes returns size bytes from a seeded generator, so a failure
// repeats, and a filesystem cannot compress them.
func randomBytes(size int, seed uint64) []byte {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:8], seed)
	p := make([]byte, size)
	_, _ = rand.NewChaCha8(key).Read(p)
	return p
}
