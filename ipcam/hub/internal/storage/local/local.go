// Package local is the local-disk storage driver (§9.1): a destination like
// any other, and the baseline of the conformance suite (STO-01). The object
// of a key is the file <root>/<key>. An upload in progress is <root>/<key>.part,
// its session: PutChunk writes it at offsets and fsyncs each chunk, and
// Commit renames it to the object. A key's directories are created, and made
// durable, on its first write.
package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// partSize is the chunk size that the driver declares.
	partSize = 8 << 20
	// partSuffix and tmpSuffix name the driver's own files next to an
	// object: an upload in progress, and a PutSmall before its rename. No key
	// ends in them.
	partSuffix = ".part"
	tmpSuffix  = ".tmp"
	// probeKey is the object that Probe writes and deletes.
	probeKey storage.ObjectKey = ".ipcam-probe"
	// The modes of the directories and files that the driver creates: the
	// recordings are the hub user's alone.
	dirMode  = 0o700
	fileMode = 0o600
)

// Local is the driver of one root directory. It holds nothing but the root,
// so a Local on the same root, after a restart, continues another's uploads.
type Local struct {
	root string
}

var _ storage.Destination = (*Local)(nil)

// New returns the driver of root, which must be an existing directory. It
// does not create root: a missing root is a disk that is not mounted, and
// writing there would fill the disk that holds the mount point.
func New(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("local: root %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("local: root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local: root %s is not a directory", abs)
	}
	return &Local{root: abs}, nil
}

// Kind is KindLocal.
func (*Local) Kind() storage.Kind { return storage.KindLocal }

// Capabilities is P24's LOCAL Caps; every other field is zero.
func (*Local) Capabilities() storage.Caps {
	return storage.Caps{
		Resumable: storage.ResumeOffsetWrite,
		PartSize:  partSize,
		Playback:  storage.LinkLocalFile,
		RangeRead: true,
		Delete:    storage.DeletePermanent,
		Quota:     storage.QuotaFilesystem,
		Hashes:    []storage.HashKind{storage.HashSHA256},
	}
}

// Probe writes a small object, reads a range of it back, deletes it and reads
// the quota. The report holds the steps that passed, and the error is the
// first step that failed.
func (l *Local) Probe(ctx context.Context) (storage.ProbeReport, error) {
	var report storage.ProbeReport
	data := []byte("ipcam: a probe of the destination\n")
	ref, err := l.PutSmall(ctx, probeKey, data)
	if err != nil {
		return report, err
	}
	report.Writable = true
	const off, n = 7, 5
	r, err := l.OpenRange(ctx, ref, off, n)
	if err != nil {
		return report, err
	}
	got, err := io.ReadAll(r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return report, fmt.Errorf("local: probe: %w", err)
	}
	if !bytes.Equal(got, data[off:off+n]) {
		return report, fmt.Errorf("local: probe: a range read returned %q, want %q: %w", got, data[off:off+n], storage.ErrPermanent)
	}
	report.RangeRead = true
	if _, err := l.Delete(ctx, ref); err != nil {
		return report, err
	}
	report.Deletable = true
	report.Quota, err = l.Quota(ctx)
	return report, err
}

// BeginUpload creates key's directories and an empty <key>.part, the
// upload's session, truncating a .part left by an earlier upload of key. It
// syncs the directory, so that the session survives a power cut.
func (l *Local) BeginUpload(ctx context.Context, key storage.ObjectKey, size int64, _ storage.Meta) (storage.UploadState, error) {
	if err := ctx.Err(); err != nil {
		return storage.UploadState{}, err
	}
	file, err := l.file(key)
	if err != nil {
		return storage.UploadState{}, err
	}
	if size < 0 {
		return storage.UploadState{}, fmt.Errorf("local: upload of %s: size %d: %w", key, size, storage.ErrPermanent)
	}
	if err := l.makeKeyDirs(key); err != nil {
		return storage.UploadState{}, fmt.Errorf("local: upload of %s: %w", key, err)
	}
	if err := writeSynced(file+partSuffix, nil); err != nil {
		return storage.UploadState{}, fmt.Errorf("local: upload of %s: %w", key, err)
	}
	if err := platformSyncDir(filepath.Dir(file)); err != nil {
		return storage.UploadState{}, fmt.Errorf("local: upload of %s: %w", key, err)
	}
	return storage.UploadState{Key: key, Size: size}, nil
}

// PutChunk writes r's n bytes into the .part at off with WriteAt, and fsyncs
// them before it advances st.Offset. A chunk may write again bytes that the
// .part holds, but may not start past its end, which would leave a hole.
func (l *Local) PutChunk(ctx context.Context, st *storage.UploadState, off int64, r io.Reader, n int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := l.file(st.Key)
	if err != nil {
		return err
	}
	if off < 0 || n < 0 || off+n > st.Size {
		return fmt.Errorf("local: upload of %s: bytes [%d, %d) of %d: %w", st.Key, off, off+n, st.Size, storage.ErrPermanent)
	}
	f, err := os.OpenFile(file+partSuffix, os.O_WRONLY, 0) //nolint:gosec // G304: a file under the root, from a checked key
	if err != nil {
		return fmt.Errorf("local: upload of %s: %w", st.Key, sessionGone(err))
	}
	err = writeChunk(f, off, r, n)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("local: upload of %s: %w", st.Key, err)
	}
	st.Offset = off + n
	return nil
}

// Resume returns the size of the .part while one exists: PutChunk fsyncs
// every chunk, so those bytes are on disk. Without a .part, an object of
// st.Size bytes is a Commit that the caller did not record, and Resume
// returns st.Size; anything else is a session that is gone.
func (l *Local) Resume(ctx context.Context, st *storage.UploadState) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	file, err := l.file(st.Key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(file + partSuffix)
	switch {
	case err == nil:
		st.Offset = info.Size()
	case !errors.Is(err, fs.ErrNotExist):
		return 0, fmt.Errorf("local: resume of %s: %w", st.Key, err)
	case committed(file, st.Size):
		st.Offset = st.Size
	default:
		return 0, fmt.Errorf("local: resume of %s: no %s and no object of %d bytes: %w", st.Key, partSuffix, st.Size, storage.ErrSessionExpired)
	}
	return st.Offset, nil
}

// Commit fsyncs the .part, which must hold st.Size bytes, renames it to the
// object (with MOVEFILE_WRITE_THROUGH on Windows), syncs the directory (not on
// Windows) and returns the object, with the SHA-256 of its file. Without a
// .part, an object of st.Size bytes is an earlier Commit of this upload,
// which it returns again.
func (l *Local) Commit(ctx context.Context, st *storage.UploadState) (storage.ObjectRef, error) {
	if err := ctx.Err(); err != nil {
		return storage.ObjectRef{}, err
	}
	file, err := l.file(st.Key)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	part := file + partSuffix
	switch err := syncPart(part, st.Size); {
	case err == nil:
		if err := renameReplace(part, file); err != nil {
			return storage.ObjectRef{}, fmt.Errorf("local: commit of %s: %w", st.Key, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return storage.ObjectRef{}, fmt.Errorf("local: commit of %s: %w", st.Key, err)
	case !committed(file, st.Size):
		return storage.ObjectRef{}, fmt.Errorf("local: commit of %s: no %s and no object of %d bytes: %w", st.Key, partSuffix, st.Size, storage.ErrSessionExpired)
	}
	if err := platformSyncDir(filepath.Dir(file)); err != nil {
		return storage.ObjectRef{}, fmt.Errorf("local: commit of %s: %w", st.Key, err)
	}
	info, err := objectInfo(st.Key, file)
	if err != nil {
		return storage.ObjectRef{}, fmt.Errorf("local: commit of %s: %w", st.Key, err)
	}
	st.Offset = st.Size
	return info.Ref, nil
}

// Abort removes the .part. An upload without one has nothing to abort.
func (l *Local) Abort(ctx context.Context, st *storage.UploadState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := l.file(st.Key)
	if err != nil {
		return err
	}
	if err := os.Remove(file + partSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("local: abort of %s: %w", st.Key, err)
	}
	return nil
}

// Stat returns ref's object as its file is now: its size, the SHA-256 of its
// bytes, which Stat reads, and its modification time.
func (l *Local) Stat(ctx context.Context, ref storage.ObjectRef) (storage.ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return storage.ObjectInfo{}, err
	}
	file, err := l.file(ref.Key)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	info, err := objectInfo(ref.Key, file)
	if err != nil {
		return storage.ObjectInfo{}, fmt.Errorf("local: stat of %s: %w", ref.Key, err)
	}
	return info, nil
}

// OpenRange reads n bytes of ref's object from off, or fewer when the object
// ends sooner.
func (l *Local) OpenRange(ctx context.Context, ref storage.ObjectRef, off, n int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := l.file(ref.Key)
	if err != nil {
		return nil, err
	}
	if off < 0 || n < 0 {
		return nil, fmt.Errorf("local: read of %s: range %d+%d: %w", ref.Key, off, n, storage.ErrPermanent)
	}
	f, err := os.Open(file) //nolint:gosec // G304: a file under the root, from a checked key
	if err != nil {
		return nil, fmt.Errorf("local: read of %s: %w", ref.Key, notFound(err))
	}
	return struct {
		io.Reader
		io.Closer
	}{io.NewSectionReader(f, off, n), f}, nil
}

// PlaybackURL returns the file URL of ref's object, a file on the hub's disk
// (LinkLocalFile). A file does not expire, so ttl does not apply.
func (l *Local) PlaybackURL(ctx context.Context, ref storage.ObjectRef, _ time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := l.file(ref.Key)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(file); err != nil {
		return "", fmt.Errorf("local: playback of %s: %w", ref.Key, notFound(err))
	}
	p := filepath.ToSlash(file)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows path: file:///C:/dir
	}
	return (&url.URL{Scheme: "file", Path: p}).String(), nil
}

// Delete removes ref's object; its bytes are gone (DeletePermanent). A failed
// Delete claims no freed space.
func (l *Local) Delete(ctx context.Context, ref storage.ObjectRef) (storage.DeleteOutcome, error) {
	if err := ctx.Err(); err != nil {
		return storage.DeletedRecycled, err
	}
	file, err := l.file(ref.Key)
	if err != nil {
		return storage.DeletedRecycled, err
	}
	if err := os.Remove(file); err != nil {
		return storage.DeletedRecycled, fmt.Errorf("local: delete of %s: %w", ref.Key, notFound(err))
	}
	return storage.DeletedPermanent, nil
}

// Quota reads the root's filesystem: Free is what this user may still write,
// Total the filesystem's size, and Used the rest of it.
func (l *Local) Quota(ctx context.Context) (storage.Quota, error) {
	if err := ctx.Err(); err != nil {
		return storage.Quota{}, err
	}
	free, total, err := diskSpace(l.root)
	if err != nil {
		return storage.Quota{}, fmt.Errorf("local: free space of %s: %w", l.root, err)
	}
	return storage.Quota{Total: total, Used: total - free, Free: free}, nil
}

// PutSmall creates key's directories, writes data to <key>.tmp, fsyncs it and
// renames it over the object, then syncs the directory: a reader sees the
// old object or the new one, never a part of either.
func (l *Local) PutSmall(ctx context.Context, key storage.ObjectKey, data []byte) (storage.ObjectRef, error) {
	if err := ctx.Err(); err != nil {
		return storage.ObjectRef{}, err
	}
	file, err := l.file(key)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	if err := l.makeKeyDirs(key); err != nil {
		return storage.ObjectRef{}, fmt.Errorf("local: write of %s: %w", key, err)
	}
	tmp := file + tmpSuffix
	err = writeSynced(tmp, data)
	if err == nil {
		err = renameReplace(tmp, file)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return storage.ObjectRef{}, fmt.Errorf("local: write of %s: %w", key, err)
	}
	if err := platformSyncDir(filepath.Dir(file)); err != nil {
		return storage.ObjectRef{}, fmt.Errorf("local: write of %s: %w", key, err)
	}
	info, err := objectInfo(key, file)
	if err != nil {
		return storage.ObjectRef{}, fmt.Errorf("local: write of %s: %w", key, err)
	}
	return info.Ref, nil
}

// List yields the objects whose key starts with prefix, without their hash,
// which Stat reads. It skips the driver's .part and .tmp files, and yields
// nothing when no directory holds the prefix.
func (l *Local) List(ctx context.Context, prefix storage.ObjectKey) iter.Seq2[storage.ObjectInfo, error] {
	return func(yield func(storage.ObjectInfo, error) bool) {
		// Every key with the prefix is under the prefix's directory part.
		var base storage.ObjectKey
		dir := l.root
		if i := strings.LastIndexByte(string(prefix), '/'); i >= 0 {
			base = prefix[:i]
			var err error
			if dir, err = l.file(base); err != nil {
				yield(storage.ObjectInfo{}, err)
				return
			}
		}
		// os.DirFS follows a link at dir, as the other methods follow one in
		// the root's path, and no link below it.
		err := fs.WalkDir(os.DirFS(dir), ".", func(name string, d fs.DirEntry, err error) error {
			if d == nil && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll // the directory of the prefix does not exist
			}
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !d.Type().IsRegular() || strings.HasSuffix(name, partSuffix) || strings.HasSuffix(name, tmpSuffix) {
				return nil
			}
			key := storage.ObjectKey(path.Join(string(base), name))
			if !strings.HasPrefix(string(key), string(prefix)) {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			info := storage.ObjectInfo{
				Ref:       storage.ObjectRef{ID: string(key), Key: key, Size: fi.Size()},
				ModTimeMs: fi.ModTime().UnixMilli(),
			}
			if !yield(info, nil) {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			yield(storage.ObjectInfo{}, fmt.Errorf("local: list of %q: %w", prefix, err))
		}
	}
}

// file returns the path of key's object. A key is a clean relative path with
// '/' (fs.ValidPath) that stays under the root on this system and does not
// end in the driver's suffixes; any other key is ErrPermanent.
func (l *Local) file(key storage.ObjectKey) (string, error) {
	k := string(key)
	native := filepath.FromSlash(k)
	if !fs.ValidPath(k) || k == "." || strings.Contains(k, `\`) || !filepath.IsLocal(native) ||
		strings.HasSuffix(k, partSuffix) || strings.HasSuffix(k, tmpSuffix) {
		return "", fmt.Errorf("local: invalid key %q: %w", key, storage.ErrPermanent)
	}
	return filepath.Join(l.root, native), nil
}

// makeKeyDirs creates the directories of key's object under the root that do
// not exist yet, as MkdirAll does, and syncs the parent of each one it
// creates, so that a power cut cannot lose a new directory with the files in
// it (recorder's mkdirSynced does the same). The root must exist.
func (l *Local) makeKeyDirs(key storage.ObjectKey) error {
	keyDir := path.Dir(string(key))
	if keyDir == "." {
		return nil
	}
	dir := l.root
	for name := range strings.SplitSeq(keyDir, "/") {
		parent := dir
		dir = filepath.Join(dir, name)
		if err := os.Mkdir(dir, dirMode); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := platformSyncDir(parent); err != nil {
			return err
		}
	}
	return nil
}

// objectInfo returns the object of key at file, with the SHA-256 of its
// bytes.
func objectInfo(key storage.ObjectKey, file string) (storage.ObjectInfo, error) {
	f, err := os.Open(file) //nolint:gosec // G304: a file under the root, from a checked key
	if err != nil {
		return storage.ObjectInfo{}, notFound(err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	hash, err := storage.Digest(storage.HashSHA256, f)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	return storage.ObjectInfo{
		Ref:       storage.ObjectRef{ID: string(key), Key: key, Size: fi.Size(), Hash: hash, HashKind: storage.HashSHA256},
		ModTimeMs: fi.ModTime().UnixMilli(),
	}, nil
}

// committed reports whether the object at file holds size bytes.
func committed(file string, size int64) bool {
	fi, err := os.Stat(file)
	return err == nil && fi.Mode().IsRegular() && fi.Size() == size
}

// writeChunk writes r's n bytes at off of f and fsyncs f.
func writeChunk(f *os.File, off int64, r io.Reader, n int64) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if off > fi.Size() {
		return fmt.Errorf("offset %d is past the %d bytes written: %w", off, fi.Size(), storage.ErrPermanent)
	}
	if _, err := io.CopyN(io.NewOffsetWriter(f, off), r, n); err != nil {
		return err
	}
	return f.Sync()
}

// syncPart fsyncs the .part at file once it holds size bytes.
func syncPart(file string, size int64) error {
	f, err := os.OpenFile(file, os.O_WRONLY, 0) //nolint:gosec // G304: a file under the root, from a checked key
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err == nil && fi.Size() != size {
		err = fmt.Errorf("%s holds %d of %d bytes: %w", filepath.Base(file), fi.Size(), size, storage.ErrPermanent)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// writeSynced creates or truncates file, writes data to it and fsyncs it.
func writeSynced(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode) //nolint:gosec // G304: a file under the root, from a checked key
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// notFound adds ErrNotFound to an error that says a file does not exist.
func notFound(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	return err
}

// sessionGone adds ErrSessionExpired to an error that says the .part does not
// exist: the upload starts again with BeginUpload.
func sessionGone(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", storage.ErrSessionExpired, err)
	}
	return err
}
