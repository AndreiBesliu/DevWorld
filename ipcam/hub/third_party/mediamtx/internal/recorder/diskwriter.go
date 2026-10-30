package recorder

import (
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"
)

// maxQueuedBytes bounds the parts that wait in memory for the disk writer,
// per recorder (C1-R17): 16 s of a 4 Mbps stream, longer than the 12.7 s
// stalls measured in Task 7.2d. Past it the recording waits for the disk, as
// it did before MTX-1's disk writer, and MediaMTX's reader queue then
// discards the frames that do not fit and reports them. A variable for the
// tests.
var maxQueuedBytes = 8 << 20

// diskOp is one piece of work for the disk writer: a part of a segment, or
// the segment's close.
type diskOp struct {
	seg *formatFMP4Segment

	// init is the segment's init, with its first part: the segment's file is
	// created with it. part is the part's moof and mdat.
	init []byte
	part []byte
	// err is why the recording could not marshal init or part.
	err error

	// close ends the segment, of duration.
	close    bool
	duration time.Duration
}

func (op diskOp) size() int { return len(op.init) + len(op.part) }

// diskWriter writes a recorder's fMP4 segments to disk on a goroutine of its
// own (MTX-1, hub/third_party/PATCHES.md), in the order the recording hands
// it their parts and closes: it creates each segment's file with its first
// part, writes each part, asks the segment's partSyncer for its fsync, and
// closes each segment in MTX-1's order before it creates the next one. The
// recording never waits for the disk, so a disk that is slow for seconds
// (an fsync or a directory sync behind another writer's data) does not hold
// up MediaMTX's reader queue, which would discard every frame past it
// (Task 7.2d): the parts wait in memory meanwhile, up to maxQueuedBytes.
type diskWriter struct {
	log logger.Writer

	mu       sync.Mutex
	cond     *sync.Cond // signalled when an op is queued, when one is done and at the stop
	ops      []diskOp
	queued   int // the bytes of the ops queued or being written
	peak     int // the most bytes queued since logPeak last ran
	stopping bool
	err      error // the first failure of a write, a sync or a close
	done     chan struct{}
}

func newDiskWriter(log logger.Writer) *diskWriter {
	d := &diskWriter{log: log, done: make(chan struct{})}
	d.cond = sync.NewCond(&d.mu)
	go d.run()
	return d
}

// send queues op and returns the first failure of the disk so far, which
// the recording returns so that MediaMTX restarts the recorder, as it does
// for any error of the recording. It waits only while the ops queued fill
// maxQueuedBytes; one op is always taken.
func (d *diskWriter) send(op diskOp) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.queued > 0 && d.queued+op.size() > maxQueuedBytes {
		d.cond.Wait()
	}
	d.ops = append(d.ops, op)
	d.queued += op.size()
	d.peak = max(d.peak, d.queued)
	d.cond.Broadcast()
	return d.err
}

// diskFailed runs once a disk writer has noted its first failure, which
// the recording then returns at its next part or segment close. It does
// nothing; the tests replace it to wait for that moment.
var diskFailed = func(error) {}

// fail notes a failure of the disk, which the next send returns.
func (d *diskWriter) fail(err error) {
	d.mu.Lock()
	first := d.err == nil
	if first {
		d.err = err
	}
	d.mu.Unlock()
	if first {
		diskFailed(err)
	}
}

// stop waits until every op sent has been done, so that every segment the
// recording closed is closed on disk, and stops the writer.
func (d *diskWriter) stop() {
	d.mu.Lock()
	d.stopping = true
	d.cond.Broadcast()
	d.mu.Unlock()
	<-d.done
}

func (d *diskWriter) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		for len(d.ops) == 0 && !d.stopping {
			d.cond.Wait()
		}
		if len(d.ops) == 0 {
			d.mu.Unlock()
			return
		}
		op := d.ops[0]
		d.ops[0] = diskOp{}
		d.ops = d.ops[1:]
		d.mu.Unlock()

		if op.close {
			op.seg.finish(op.duration)
		} else {
			op.seg.writePartToDisk(op)
		}

		d.mu.Lock()
		d.queued -= op.size()
		d.cond.Broadcast()
		d.mu.Unlock()
	}
}

// logPeak logs, once a segment's file is closed, the most bytes of parts
// that waited for the disk writer since the previous segment's file was
// closed, the part being written included, and starts the next count from
// the bytes queued now. The hub's load test (MI3, Task 9) reads these lines
// against §4.8's RAM budgets (C1-R17).
func (d *diskWriter) logPeak() {
	d.mu.Lock()
	peak := d.peak
	d.peak = d.queued
	d.mu.Unlock()
	d.log.Log(logger.Info, "disk queue peak: %d bytes", peak)
}
