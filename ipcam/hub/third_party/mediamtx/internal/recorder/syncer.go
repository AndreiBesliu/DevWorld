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
// calls or to wrap the file; every call site is in format_fmp4_segment.go.
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
)

// partSuffix ends the name of a segment that is still being written. The
// segment is renamed without it once its last bytes are durable.
const partSuffix = ".part"

// partSyncQueue is how many parts' fsyncs may wait behind a slow one before
// the recorder waits too: over 17 minutes of 1 s parts.
const partSyncQueue = 1024

// partSyncer runs the fsyncs of one segment's parts on a goroutine of its
// own (MTX-1), in order, one per part: the recorder asks for a part's fsync
// once the part is written and goes on recording. A disk that is slow to
// sync, for seconds while another writer's data goes out, then holds up
// neither the recording nor MediaMTX's reader queue, which would discard
// every frame past it, and a crash during the slow fsync loses only the
// part being recorded. The segment's close waits for these fsyncs before its
// own last writes and fsync.
type partSyncer struct {
	f    segmentFile
	reqs chan struct{}
	done chan struct{}

	stopMu  sync.Mutex // held while a request is sent, and when the queue closes
	stopped bool

	errMu sync.Mutex
	err   error // the first fsync that failed
}

// newPartSyncer starts the part syncer of the segment file f.
func newPartSyncer(f segmentFile) *partSyncer {
	p := &partSyncer{f: f, reqs: make(chan struct{}, partSyncQueue), done: make(chan struct{})}
	go p.run()
	return p
}

func (p *partSyncer) run() {
	defer close(p.done)
	for range p.reqs {
		if err := syncFile(p.f); err != nil {
			p.errMu.Lock()
			if p.err == nil {
				p.err = err
			}
			p.errMu.Unlock()
		}
	}
}

// sync asks for an fsync of everything written so far, without waiting for
// it. It waits only when partSyncQueue fsyncs are queued already.
func (p *partSyncer) sync() {
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	if !p.stopped {
		p.reqs <- struct{}{}
	}
}

// failed returns the first fsync that has failed so far, if any.
func (p *partSyncer) failed() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

// wait waits until every fsync asked for has run, stops the syncer and
// returns the first fsync that failed. It can be called again.
func (p *partSyncer) wait() error {
	p.stopMu.Lock()
	if !p.stopped {
		p.stopped = true
		close(p.reqs)
	}
	p.stopMu.Unlock()
	<-p.done
	return p.failed()
}
