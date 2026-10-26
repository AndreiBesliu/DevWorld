package recorder

import (
	"io"
	"os"
	"sync"
)

// segmentFile is the open file of an fMP4 segment. *os.File is one. A clear
// segment's close seeks back to write the mvhd duration; an encrypted one
// only ever appends (MTX-2).
type segmentFile interface {
	io.ReadWriteSeeker
	Name() string
	Sync() error
	Close() error
}

// MTX-1 and MTX-2 (hub/third_party/PATCHES.md): the seams through which the
// fMP4 segment writer creates a segment file and makes it durable. Each one is
// a single function, so that the tests can replace it to count and order the
// calls or to wrap the file; every call site is in format_fmp4_segment.go or
// in partSyncer below.
var (
	// createFile creates a segment file, truncating a file of that name.
	createFile = func(name string) (segmentFile, error) {
		f, err := os.Create(name)
		if err != nil {
			return nil, err
		}
		return f, nil
	}

	// syncFile flushes a segment file's data and size to stable storage.
	syncFile = func(f segmentFile) error { return f.Sync() }

	// syncDir makes the entries of a directory durable: on Windows a no-op
	// (dirsync_windows.go), on other systems an fsync of the directory
	// (dirsync_other.go).
	syncDir = func(dir string) error { return syncDirectory(dir) }

	// renameFinal gives a closed segment its final name, replacing a file of
	// that name (rename_other.go, rename_windows.go).
	renameFinal = func(from, to string) error { return renameReplace(from, to) }

	// syncAsked runs when the disk writer asks for a part's fsync, with the
	// file as that part left it: right after the part's last write and before
	// the next part's first. It does nothing; the tests replace it to see the
	// file at that moment.
	syncAsked = func(segmentFile) {}
)

// partSuffix ends the name of a segment that is still being written. The
// segment is renamed without it once its last bytes are durable.
const partSuffix = ".part"

// partSyncQueue is how many syncs may wait behind a slow one before the
// disk writer waits too: over 17 minutes of 1 s parts, while a segment holds
// 5 minutes of them.
const partSyncQueue = 1024

// partSyncer runs one segment's syncs on a goroutine of its own (MTX-1), in
// the order they are asked for: first the directory sync that makes the new
// .part's name durable, then one file fsync per part. The disk writer
// (diskwriter.go) asks for each and goes on writing: a disk that is slow to
// sync, for seconds while it writes back another writer's data, then holds
// up neither the writes that follow nor the recording. Since the jobs run in
// order, a part whose fsync has returned is in a file whose name is durable.
// The segment's close waits for every job before its own last writes. Only
// the disk writer asks and waits.
type partSyncer struct {
	jobs    chan func() error
	done    chan struct{}
	onFail  func(error)
	stopped bool

	errMu sync.Mutex
	err   error // the first sync that failed
}

// newPartSyncer starts a segment's syncer. onFail learns of the first sync
// that fails, as soon as it fails.
func newPartSyncer(onFail func(error)) *partSyncer {
	p := &partSyncer{jobs: make(chan func() error, partSyncQueue), done: make(chan struct{}), onFail: onFail}
	go p.run()
	return p
}

func (p *partSyncer) run() {
	defer close(p.done)
	for job := range p.jobs {
		if err := job(); err != nil {
			p.errMu.Lock()
			first := p.err == nil
			if first {
				p.err = err
			}
			p.errMu.Unlock()
			if first {
				p.onFail(err)
			}
		}
	}
}

// askDirSync asks for a sync of the directory dir, without waiting for it.
func (p *partSyncer) askDirSync(dir string) {
	p.ask(func() error { return syncDir(dir) })
}

// askFileSync asks for an fsync of everything written to f so far, without
// waiting for it.
func (p *partSyncer) askFileSync(f segmentFile) {
	p.ask(func() error { return syncFile(f) })
}

// ask queues a job. It waits only when partSyncQueue jobs are queued already.
func (p *partSyncer) ask(job func() error) {
	if !p.stopped {
		p.jobs <- job
	}
}

// failed returns the first sync that has failed so far, if any.
func (p *partSyncer) failed() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

// wait waits until every sync asked for has run, stops the syncer and
// returns the first sync that failed. It can be called again.
func (p *partSyncer) wait() error {
	if !p.stopped {
		p.stopped = true
		close(p.jobs)
	}
	<-p.done
	return p.failed()
}
