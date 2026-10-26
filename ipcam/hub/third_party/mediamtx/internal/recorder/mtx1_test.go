package recorder

// The tests of patch MTX-1 (hub/third_party/PATCHES.md): an fMP4 segment is
// written as <name>.mp4.part, fsynced at every part close, and renamed to
// <name>.mp4 after its last write, one more fsync and Close; its directory is
// synced once after the .part is created and once after the rename. They run
// a real Recorder and wrap the seams of syncer.go, so every call still reaches
// the disk, and each wrapper records what it saw.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amp4 "github.com/abema/go-mp4"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	rtspformat "github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/stream"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/test"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/unit"
)

const (
	// Frames are 50 ms apart; parts last 1 s and segments 3 s, so the frame at
	// 3 s closes the first segment when it reaches the recorder, while it holds
	// 3 parts and lasts exactly 3 s.
	testFrameInterval   = 50 * time.Millisecond
	testPartDuration    = time.Second
	testSegmentDuration = 3 * time.Second

	// oneSegmentFrames ends with the frame at 3 s: it closes the first segment
	// and stays in the recorder as the next segment's first sample, so the next
	// segment never creates a file.
	oneSegmentFrames = 61
)

// testPathName is the MediaMTX path name of the recordings, the directory of
// their segments.
const testPathName = "mypath"

// durabilityEvent is one call of a seam of syncer.go or of a recorder hook,
// with what the recording looked like at that moment.
type durabilityEvent struct {
	// op is "create" (OnSegmentCreate), "syncAsked" (a part's fsync asked
	// for), "syncFile", "rename", "syncDir" or "complete" (OnSegmentComplete).
	op string
	// path is the file that is synced or whose fsync is asked for, the
	// rename's target, the directory that is synced or the path that a hook
	// received.
	path string
	// from is the rename's source.
	from string
	// fileOpen is, at a syncFile, whether the handle is still open, and, at a
	// rename, whether the handle of the last syncFile is still open.
	fileOpen bool
	// content is the file's content at a syncAsked or a syncFile, or the
	// source's at a rename.
	content []byte
	// entries are the names in the segment directory at the call.
	entries []string
	// start and duration are what OnSegmentComplete received.
	start    time.Time
	duration time.Duration
}

// durabilityLog records the events of one test, in call order.
type durabilityLog struct {
	mu       sync.Mutex
	events   []durabilityEvent
	lastSync segmentFile
}

func (l *durabilityLog) add(ev durabilityEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
}

// snapshot returns the events recorded so far.
func (l *durabilityLog) snapshot() []durabilityEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

// count returns how many events of op have been recorded.
func (l *durabilityLog) count(op string) int {
	n := 0
	for _, ev := range l.snapshot() {
		if ev.op == op {
			n++
		}
	}
	return n
}

// recordDurability replaces the seams of syncer.go, for the rest of the test,
// with wrappers that record each call and then make the real one. The wrappers
// run on the recorder's goroutines, so they report problems with assert, not
// require, whose FailNow belongs to the test's goroutine.
func recordDurability(t *testing.T) *durabilityLog {
	l := &durabilityLog{}

	realSyncFile, realSyncDir, realRenameFinal, realSyncAsked := syncFile, syncDir, renameFinal, syncAsked
	t.Cleanup(func() {
		syncFile, syncDir, renameFinal, syncAsked = realSyncFile, realSyncDir, realRenameFinal, realSyncAsked
	})

	syncAsked = func(f segmentFile) {
		content, err := os.ReadFile(f.Name())
		assert.NoError(t, err)
		l.add(durabilityEvent{op: "syncAsked", path: f.Name(), content: content})
		realSyncAsked(f)
	}

	syncFile = func(f segmentFile) error {
		content, err := os.ReadFile(f.Name())
		assert.NoError(t, err)
		l.mu.Lock()
		l.lastSync = f
		l.mu.Unlock()
		l.add(durabilityEvent{
			op:       "syncFile",
			path:     f.Name(),
			fileOpen: isOpen(t, f),
			content:  content,
			entries:  dirEntries(t, filepath.Dir(f.Name())),
		})
		return realSyncFile(f)
	}

	renameFinal = func(from, to string) error {
		l.mu.Lock()
		last := l.lastSync
		l.mu.Unlock()
		lastOpen := last != nil && isOpen(t, last)
		content, err := os.ReadFile(from)
		assert.NoError(t, err)
		l.add(durabilityEvent{
			op:       "rename",
			path:     to,
			from:     from,
			fileOpen: lastOpen,
			content:  content,
			entries:  dirEntries(t, filepath.Dir(to)),
		})
		return realRenameFinal(from, to)
	}

	syncDir = func(dir string) error {
		l.add(durabilityEvent{op: "syncDir", path: dir, entries: dirEntries(t, dir)})
		return realSyncDir(dir)
	}

	return l
}

// isOpen reports whether f has not been closed yet. A Seek that moves nothing
// fails with os.ErrClosed on a closed file, on every OS. It looks through
// mtx2_test.go's appendOnlyFile, which refuses every Seek.
func isOpen(t *testing.T, f segmentFile) bool {
	if a, ok := f.(*appendOnlyFile); ok {
		f = a.f
	}
	_, err := f.Seek(0, io.SeekCurrent)
	if errors.Is(err, os.ErrClosed) {
		return false
	}
	assert.NoError(t, err)
	return true
}

// dirEntries returns the sorted names in dir.
func dirEntries(t *testing.T, dir string) []string {
	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// testRecording is a Recorder fed with H.264 IDR frames 50 ms apart.
type testRecording struct {
	t         *testing.T
	dir       string // the directory of the segments
	log       *durabilityLog
	desc      *description.Session
	subStream *stream.SubStream
	recorder  *Recorder
	firstNTP  time.Time
	frames    int
	stopOnce  sync.Once
}

// startRecording starts a Recorder with 1 s parts and 3 s segments whose seams
// record into a new durabilityLog. The first frame's wall time is firstNTP.
func startRecording(t *testing.T, firstNTP time.Time) *testRecording {
	return startRecordingWith(t, firstNTP, test.FormatH264, nil)
}

// startRecordingWith is startRecording for a stream of the H.264 format forma,
// with configure, when not nil, applied to the Recorder before it starts.
func startRecordingWith(
	t *testing.T,
	firstNTP time.Time,
	forma *rtspformat.H264,
	configure func(*Recorder),
) *testRecording {
	return startRecordingQueue(t, firstNTP, forma, 512, configure)
}

// startRecordingQueue is startRecordingWith for a stream whose readers queue
// at most queueSize frames (MediaMTX's writeQueueSize, a power of two): a
// reader that falls further behind loses the frames past it.
func startRecordingQueue(
	t *testing.T,
	firstNTP time.Time,
	forma *rtspformat.H264,
	queueSize int,
	configure func(*Recorder),
) *testRecording {
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []rtspformat.Format{forma},
	}}}

	strm := &stream.Stream{
		OrigDesc:          desc,
		WriteQueueSize:    queueSize,
		RTPMaxPayloadSize: 1450,
		Parent:            test.NilLogger,
	}
	require.NoError(t, strm.Initialize())
	t.Cleanup(strm.Close)

	subStream := &stream.SubStream{
		Stream:        strm,
		UseRTPPackets: false,
	}
	require.NoError(t, subStream.Initialize())

	root := t.TempDir()
	r := &testRecording{
		t:         t,
		dir:       filepath.Join(root, testPathName),
		log:       recordDurability(t),
		desc:      desc,
		subStream: subStream,
		firstNTP:  firstNTP,
	}

	r.recorder = &Recorder{
		PathFormat:      filepath.Join(root, "%path", "%Y-%m-%d_%H-%M-%S-%f"),
		Format:          conf.RecordFormatFMP4,
		PartDuration:    testPartDuration,
		MaxPartSize:     50 * 1024 * 1024,
		SegmentDuration: testSegmentDuration,
		PathName:        testPathName,
		Stream:          strm,
		OnSegmentCreate: func(segPath string) {
			r.log.add(durabilityEvent{op: "create", path: segPath})
		},
		OnSegmentComplete: func(segPath string, start time.Time, duration time.Duration) {
			r.log.add(durabilityEvent{op: "complete", path: segPath, start: start, duration: duration})
		},
		Parent: test.NilLogger,
	}
	if configure != nil {
		configure(r.recorder)
	}
	r.recorder.Initialize()
	t.Cleanup(r.stop)

	return r
}

// stop closes the recorder, which closes its current segment. It runs once.
func (r *testRecording) stop() {
	r.stopOnce.Do(r.recorder.Close)
}

// writeFrames sends the next n frames: IDR frames 50 ms apart in both PTS and
// wall time, each with the SPS and PPS of test.FormatH264.
func (r *testRecording) writeFrames(n int) {
	for range n {
		r.writeFrame(unit.PayloadH264{
			test.FormatH264.SPS,
			test.FormatH264.PPS,
			testIDR(r.frames),
		})
	}
}

// writeFramesPaced is writeFrames with 1 ms between frames, which a recorder
// that is not held up keeps up with even through a short reader queue.
func (r *testRecording) writeFramesPaced(n int) {
	for range n {
		r.writeFrames(1)
		time.Sleep(time.Millisecond)
	}
}

// writeFrame sends au as the next frame, 50 ms after the previous one in both
// PTS and wall time.
func (r *testRecording) writeFrame(au unit.PayloadH264) {
	offset := time.Duration(r.frames) * testFrameInterval
	r.subStream.WriteUnit(r.desc.Medias[0], r.desc.Medias[0].Formats[0], &unit.Unit{
		PTS:     10*90000 + int64(offset)*90000/int64(time.Second),
		NTP:     r.firstNTP.Add(offset),
		Payload: au,
	})
	r.frames++
}

// testIDR returns an IDR NAL unit of 1 KiB whose bytes after the header
// number the frame, so that the parts differ.
func testIDR(frame int) []byte {
	nalu := make([]byte, 1024)
	nalu[0] = 5 // IDR
	binary.BigEndian.PutUint32(nalu[1:], uint32(frame))
	return nalu
}

// waitFor waits until cond holds for the recorded events.
func (r *testRecording) waitFor(what string, cond func(l *durabilityLog) bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond(r.log) {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s; events: %v", what, eventOps(r.log.snapshot()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// recordOneSegment records the 61 frames of oneSegmentFrames, waits until the
// segment they close is complete and stops the recorder.
func recordOneSegment(t *testing.T) *testRecording {
	r := startRecording(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC))
	r.writeFrames(oneSegmentFrames)
	r.waitFor("the segment-complete hook", func(l *durabilityLog) bool { return l.count("complete") == 1 })
	r.stop()
	return r
}

// eventOps lists the ops of events, for failure messages.
func eventOps(events []durabilityEvent) []string {
	ops := make([]string, len(events))
	for i, ev := range events {
		ops[i] = ev.op
	}
	return ops
}

// eventsOf returns the events of op, in order.
func eventsOf(events []durabilityEvent, op string) []durabilityEvent {
	var out []durabilityEvent
	for _, ev := range events {
		if ev.op == op {
			out = append(out, ev)
		}
	}
	return out
}

// mdatEnds returns the file offset where each mdat box of an fMP4 file ends,
// which is where each of its parts ends.
func mdatEnds(t *testing.T, content []byte) []int64 {
	var ends []int64
	for pos := int64(0); pos < int64(len(content)); {
		require.GreaterOrEqual(t, int64(len(content))-pos, int64(8), "truncated box header at %d", pos)
		size := int64(binary.BigEndian.Uint32(content[pos:]))
		typ := string(content[pos+4 : pos+8])
		if size == 1 {
			require.GreaterOrEqual(t, int64(len(content))-pos, int64(16), "truncated box size at %d", pos)
			size = int64(binary.BigEndian.Uint64(content[pos+8:]))
		}
		require.GreaterOrEqual(t, size, int64(8), "box %q at %d has size %d", typ, pos, size)
		pos += size
		if typ == "mdat" {
			ends = append(ends, pos)
		}
	}
	return ends
}

// sampleCount returns the number of samples in the parts of an fMP4 file: the
// sum of its trun boxes' sample counts.
func sampleCount(t *testing.T, content []byte) int {
	boxes, err := amp4.ExtractBoxWithPayload(bytes.NewReader(content), nil,
		amp4.BoxPath{amp4.BoxTypeMoof(), amp4.BoxTypeTraf(), amp4.BoxTypeTrun()})
	require.NoError(t, err)
	n := 0
	for _, b := range boxes {
		n += int(b.Payload.(*amp4.Trun).SampleCount)
	}
	return n
}

// mvhdDuration returns the duration in the mvhd box of an fMP4 file.
func mvhdDuration(t *testing.T, content []byte) time.Duration {
	boxes, err := amp4.ExtractBoxWithPayload(bytes.NewReader(content), nil,
		amp4.BoxPath{amp4.BoxTypeMoov(), amp4.BoxTypeMvhd()})
	require.NoError(t, err)
	require.Equal(t, 1, len(boxes))
	mvhd := boxes[0].Payload.(*amp4.Mvhd)
	require.Equal(t, uint32(1000), mvhd.Timescale, "mvhd timescale")
	return time.Duration(mvhd.DurationV0) * time.Millisecond
}

// TestMTX1_FsyncOnEachPartClose: a segment of 3 parts gets 4 syncFile calls.
// One per part is asked for once the part is written, before the next
// part's first write: the file then ends exactly where that part ends. The
// three run in that order, beside the writes (partSyncer), so at each the
// file holds at least its part. The fourth comes after the close's last
// write (the mvhd duration) and before Close.
func TestMTX1_FsyncOnEachPartClose(t *testing.T) {
	r := recordOneSegment(t)

	final := filepath.Join(r.dir, "2008-05-20_22-15-25-000000.mp4")
	content, err := os.ReadFile(final)
	require.NoError(t, err)
	partEnds := mdatEnds(t, content)
	require.Len(t, partEnds, 3, "parts in the segment")

	events := r.log.snapshot()
	asks := eventsOf(events, "syncAsked")
	require.Len(t, asks, 3, "part fsyncs asked for; events: %v", eventOps(events))
	syncs := eventsOf(events, "syncFile")
	require.Equal(t, 4, len(syncs), "syncFile calls; events: %v", eventOps(events))

	for i, ev := range syncs {
		require.Equal(t, final+".part", ev.path, "syncFile %d", i+1)
		require.True(t, ev.fileOpen, "syncFile %d ran on a closed file", i+1)
	}
	for i, end := range partEnds {
		require.Equal(t, final+".part", asks[i].path, "part %d's fsync", i+1)
		require.Equal(t, end, int64(len(asks[i].content)), "file size when part %d's fsync was asked for", i+1)
		require.GreaterOrEqual(t, int64(len(syncs[i].content)), end, "file size at syncFile %d, the fsync of part %d", i+1, i+1)
	}
	// The n-th syncFile, the fsync of part n, runs after part n's fsync was
	// asked for; the close's runs after all three.
	var order []string
	for _, ev := range events {
		if ev.op == "syncAsked" || ev.op == "syncFile" {
			order = append(order, ev.op)
		}
	}
	asked, ran := 0, 0
	for _, op := range order {
		if op == "syncAsked" {
			asked++
			continue
		}
		ran++
		require.LessOrEqual(t, min(ran, 3), asked, "syncFile %d ran before its part's fsync was asked for: %v", ran, order)
	}

	last := syncs[3]
	require.Equal(t, 3*time.Second, mvhdDuration(t, last.content), "mvhd at the last syncFile")
	require.True(t, bytes.Equal(content, last.content),
		"the file changed after the last syncFile (%d bytes then, %d at the end)", len(last.content), len(content))
}

// TestMTX1_DirSyncAtCreateAndRenameOnly: a segment's directory is synced
// exactly twice, once after the .part is created and once after the rename.
func TestMTX1_DirSyncAtCreateAndRenameOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows the directory sync is a no-op (TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename)")
	}

	r := recordOneSegment(t)
	events := r.log.snapshot()
	name := "2008-05-20_22-15-25-000000.mp4"

	dirSyncs := eventsOf(events, "syncDir")
	require.Equal(t, 2, len(dirSyncs), "syncDir calls; events: %v", eventOps(events))
	for i, ev := range dirSyncs {
		require.Equal(t, r.dir, ev.path, "syncDir %d", i+1)
	}
	require.Equal(t, []string{name + ".part"}, dirSyncs[0].entries, "directory at the first syncDir")
	require.Equal(t, []string{name}, dirSyncs[1].entries, "directory at the second syncDir")

	// The first comes before any part's fsync, the second right after the rename.
	firstSync := slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "syncFile" })
	rename := slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "rename" })
	require.Less(t, slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "syncDir" }), firstSync,
		"events: %v", eventOps(events))
	require.GreaterOrEqual(t, rename, 0, "no rename; events: %v", eventOps(events))
	require.Less(t, rename+1, len(events), "nothing after the rename; events: %v", eventOps(events))
	require.Equal(t, "syncDir", events[rename+1].op, "events: %v", eventOps(events))
}

// TestMTX1_RenameAfterCloseWithFinalName: while the segment is written only
// <name>.mp4.part exists, after its close only <name>.mp4 does, and the close
// runs last write, syncFile, Close, renameFinal, syncDir, in that order.
func TestMTX1_RenameAfterCloseWithFinalName(t *testing.T) {
	r := recordOneSegment(t)
	events := r.log.snapshot()
	name := "2008-05-20_22-15-25-000000.mp4"

	creates := eventsOf(events, "create")
	require.Equal(t, 1, len(creates))
	require.Equal(t, filepath.Join(r.dir, name+".part"), creates[0].path, "path given to OnSegmentCreate")

	for i, ev := range eventsOf(events, "syncFile") {
		require.Equal(t, []string{name + ".part"}, ev.entries, "directory at syncFile %d", i+1)
	}
	require.Equal(t, []string{name}, dirEntries(t, r.dir), "directory after the close")

	// The close: syncFile (after the mvhd write, file open), Close (seen by the
	// rename: the handle is closed by then), renameFinal, syncDir.
	rename := slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "rename" })
	require.GreaterOrEqual(t, rename, 1, "events: %v", eventOps(events))
	require.Equal(t, []string{"syncFile", "rename", "syncDir", "complete"}, eventOps(events[rename-1:]))

	lastSync := events[rename-1]
	require.True(t, lastSync.fileOpen, "the last syncFile ran after Close")
	require.Equal(t, 3*time.Second, mvhdDuration(t, lastSync.content), "mvhd at the last syncFile")

	ren := events[rename]
	require.False(t, ren.fileOpen, "renameFinal ran before Close")
	require.Equal(t, filepath.Join(r.dir, name+".part"), ren.from)
	require.Equal(t, filepath.Join(r.dir, name), ren.path)
	require.Equal(t, []string{name + ".part"}, ren.entries, "directory at the rename")
}

// TestMTX1_ClearDurationWrittenBeforeRename: the mvhd of a clear segment holds
// the segment's duration, in the .part before the rename and in the final file.
func TestMTX1_ClearDurationWrittenBeforeRename(t *testing.T) {
	r := recordOneSegment(t)
	events := r.log.snapshot()

	renames := eventsOf(events, "rename")
	require.Equal(t, 1, len(renames))
	require.Equal(t, 3*time.Second, mvhdDuration(t, renames[0].content), "mvhd of the .part at the rename")

	content, err := os.ReadFile(filepath.Join(r.dir, "2008-05-20_22-15-25-000000.mp4"))
	require.NoError(t, err)
	require.Equal(t, 3*time.Second, mvhdDuration(t, content), "mvhd of the final file")

	completes := eventsOf(events, "complete")
	require.Equal(t, 1, len(completes))
	require.Equal(t, 3*time.Second, completes[0].duration, "duration given to OnSegmentComplete")
}

// TestMTX1_FailedPartSyncKeepsPartName: when a part's fsync fails, the
// recorder restarts and the segment is closed without its final name: it
// stays <name>.mp4.part, for the hub's reconcile, and no hook runs for it.
func TestMTX1_FailedPartSyncKeepsPartName(t *testing.T) {
	r := startRecording(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC))

	recordedSync := syncFile
	calls := 0
	syncFile = func(f segmentFile) error {
		err := recordedSync(f)
		calls++
		if calls == 2 {
			return errors.New("injected fsync failure")
		}
		return err
	}

	r.writeFrames(oneSegmentFrames)
	r.waitFor("the failed fsync of the second part", func(l *durabilityLog) bool { return l.count("syncFile") >= 2 })
	r.stop()

	events := r.log.snapshot()
	require.Zero(t, r.log.count("rename"), "events: %v", eventOps(events))
	require.Zero(t, r.log.count("complete"), "events: %v", eventOps(events))
	require.Equal(t, []string{"2008-05-20_22-15-25-000000.mp4.part"}, dirEntries(t, r.dir))
}

// TestMTX1_SeamFailureFiresNoHook: when the close's syncFile, renameFinal, the
// directory sync after the rename or the one after the .part is created
// fails, no segment-complete hook runs, and the segment keeps the name it had
// at the failure: the .part name, except after a rename that succeeded, which
// leaves the final name.
func TestMTX1_SeamFailureFiresNoHook(t *testing.T) {
	name := "2008-05-20_22-15-25-000000.mp4"
	errInjected := errors.New("injected failure")

	for _, tc := range []struct {
		name string
		// inject replaces a seam of syncer.go so that one of its calls fails,
		// after calling failed.
		inject func(failed func())
		// want is the only file left in the segment directory.
		want string
	}{
		{
			name: "final syncFile",
			inject: func(failed func()) {
				recorded := syncFile
				calls := 0
				syncFile = func(f segmentFile) error {
					err := recorded(f)
					calls++
					if calls == 4 { // after the syncs of the 3 parts
						failed()
						return errInjected
					}
					return err
				}
			},
			want: name + ".part",
		},
		{
			name: "renameFinal",
			inject: func(failed func()) {
				renameFinal = func(string, string) error {
					failed()
					return errInjected
				}
			},
			want: name + ".part",
		},
		{
			name: "syncDir after the rename",
			inject: func(failed func()) {
				recorded := syncDir
				calls := 0
				syncDir = func(dir string) error {
					err := recorded(dir)
					calls++
					if calls == 2 {
						failed()
						return errInjected
					}
					return err
				}
			},
			want: name,
		},
		{
			name: "syncDir after the creation",
			inject: func(failed func()) {
				recorded := syncDir
				syncDir = func(dir string) error {
					_ = recorded(dir)
					failed()
					return errInjected
				}
			},
			want: name + ".part",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := startRecording(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC))
			tc.inject(func() { r.log.add(durabilityEvent{op: "failed"}) })

			r.writeFrames(oneSegmentFrames)
			r.waitFor("the injected failure", func(l *durabilityLog) bool { return l.count("failed") == 1 })
			r.stop()

			events := r.log.snapshot()
			require.Zero(t, r.log.count("complete"), "events: %v", eventOps(events))
			require.Equal(t, []string{tc.want}, dirEntries(t, r.dir), "events: %v", eventOps(events))
		})
	}
}

// TestMTX1_SlowPartFsyncKeepsRecording (Task 7.2d): a part's fsync runs
// beside the recording, not in its way. While the first part's fsync is
// held, as a busy disk holds it for seconds, the recorder goes on writing:
// the second part reaches the file. Once the fsync returns, every part has
// had its own fsync and the segment closes in MTX-1's order. A recorder that
// waited for each fsync would write nothing more during it, and a cut then
// would lose every frame since the fsync began.
func TestMTX1_SlowPartFsyncKeepsRecording(t *testing.T) {
	r := startRecording(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC))
	part := filepath.Join(r.dir, "2008-05-20_22-15-25-000000.mp4.part")

	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseFsync := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseFsync() // before the recorder's stop, on a failure too
	recorded := syncFile
	var holdOnce sync.Once
	syncFile = func(f segmentFile) error {
		holdOnce.Do(func() {
			close(held)
			<-release
		})
		return recorded(f)
	}

	r.writeFrames(50) // parts 1 and 2 close; the frame at 2.45 s is in part 3
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("the first part's fsync never started")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		content, err := os.ReadFile(part)
		require.NoError(t, err)
		if len(mdatEnds(t, content)) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("while the first part's fsync was held, the file kept %d part(s), want the second one written too", len(mdatEnds(t, content)))
		}
		time.Sleep(5 * time.Millisecond)
	}

	releaseFsync()
	r.writeFrames(oneSegmentFrames - 50)
	r.waitFor("the segment-complete hook", func(l *durabilityLog) bool { return l.count("complete") == 1 })
	r.stop()

	events := r.log.snapshot()
	require.Len(t, eventsOf(events, "syncFile"), 4, "a syncFile per part and one at the close; events: %v", eventOps(events))
	rename := slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "rename" })
	require.GreaterOrEqual(t, rename, 1, "events: %v", eventOps(events))
	require.Equal(t, []string{"syncFile", "rename", "syncDir", "complete"}, eventOps(events[rename-1:]))
}

// TestMTX1_SlowSegmentCloseKeepsRecording (Task 7.2d): a segment's close runs
// beside the recording too. While the first segment's close is held in its
// fsync, as a busy disk holds it for seconds, the recorder goes on taking the
// next segment's frames, though the reader's queue holds only 16: none is
// lost. The next segment's .part appears only once the first segment has its
// final name, because ipcam-hub takes a .part that a newer one follows for
// one that MediaMTX has left and repairs it (its partWatcher): meanwhile the
// next segment's parts wait in memory, and they reach its file once the
// close is done. A recorder that closed the segment in its own way would
// discard every frame past the queue.
func TestMTX1_SlowSegmentCloseKeepsRecording(t *testing.T) {
	r := startRecordingQueue(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC), test.FormatH264, 16, nil)
	first, second := "2008-05-20_22-15-25-000000.mp4", "2008-05-20_22-15-28-000000.mp4"

	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseClose := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseClose() // before the recorder's stop, on a failure too
	recorded := syncFile
	var calls atomic.Int32
	syncFile = func(f segmentFile) error {
		if calls.Add(1) == 4 { // the close's, after the fsyncs of the 3 parts
			close(held)
			<-release
		}
		return recorded(f)
	}

	r.writeFramesPaced(oneSegmentFrames) // the frame at 3 s closes the first segment
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatalf("the first segment's close never reached its fsync; events: %v", eventOps(r.log.snapshot()))
	}

	// 2 s of the second segment, 40 frames, while the close is held.
	r.writeFramesPaced(40)
	require.Equal(t, []string{first + ".part"}, dirEntries(t, r.dir), "the directory while the first segment's close is held")

	releaseClose()
	r.waitFor("the first segment's hook", func(l *durabilityLog) bool { return l.count("complete") == 1 })
	r.writeFramesPaced(2*oneSegmentFrames - 1 - r.frames) // up to the frame at 6 s, which closes the second segment
	r.waitFor("the second segment's hook", func(l *durabilityLog) bool { return l.count("complete") == 2 })
	r.stop()

	// Every frame of the second segment, from 3 s to 5.95 s, is in its file.
	content, err := os.ReadFile(filepath.Join(r.dir, second))
	require.NoError(t, err)
	require.Equal(t, 60, sampleCount(t, content), "frames in the second segment")

	// MTX-3's order holds: the second segment is created after the first is complete.
	events := r.log.snapshot()
	completeAt := slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "complete" })
	secondCreateAt := slices.IndexFunc(events, func(ev durabilityEvent) bool {
		return ev.op == "create" && filepath.Base(ev.path) == second+".part"
	})
	require.GreaterOrEqual(t, secondCreateAt, 0, "events: %v", eventOps(events))
	require.Less(t, completeAt, secondCreateAt, "events: %v", eventOps(events))
}

// TestMTX1_SlowCreationDirSyncKeepsRecording (Task 7.2d): the directory sync
// after a segment's .part is created runs beside the recording, ahead of the
// parts' fsyncs. While it is held, the parts still reach the file, and no
// part's fsync runs before it has returned, so a part is durable only once
// its name is.
func TestMTX1_SlowCreationDirSyncKeepsRecording(t *testing.T) {
	r := startRecording(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC))
	part := filepath.Join(r.dir, "2008-05-20_22-15-25-000000.mp4.part")

	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseDirSync := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseDirSync() // before the recorder's stop, on a failure too
	recorded := syncDir
	var holdOnce sync.Once
	syncDir = func(dir string) error {
		holdOnce.Do(func() {
			close(held)
			<-release
		})
		return recorded(dir)
	}

	r.writeFrames(50) // parts 1 and 2 close; the frame at 2.45 s is in part 3
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("the directory sync of the creation never started")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		content, err := os.ReadFile(part)
		require.NoError(t, err)
		if len(mdatEnds(t, content)) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("while the creation's directory sync was held, the file kept %d part(s), want the first two", len(mdatEnds(t, content)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Zero(t, r.log.count("syncFile"), "a part's fsync ran while the creation's directory sync was held")

	releaseDirSync()
	r.writeFrames(oneSegmentFrames - 50)
	r.waitFor("the segment-complete hook", func(l *durabilityLog) bool { return l.count("complete") == 1 })
	r.stop()

	events := r.log.snapshot()
	require.Len(t, eventsOf(events, "syncFile"), 4, "a syncFile per part and one at the close; events: %v", eventOps(events))
	require.Less(t, slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "syncDir" }),
		slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "syncFile" }), "events: %v", eventOps(events))
}

// TestMTX1_FailedPartSyncStopsTheRecorderAtTheNextPart: a part's fsync that
// fails stops the recorder at the next part's close, as a failed write does:
// MediaMTX logs the error and starts the recorder again, rather than
// recording on to the segment's end into a file that keeps its .part name.
func TestMTX1_FailedPartSyncStopsTheRecorderAtTheNextPart(t *testing.T) {
	errs := make(chan string, 16)
	r := startRecordingWith(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC), test.FormatH264, func(rec *Recorder) {
		rec.Parent = test.Logger(func(level logger.Level, format string, args ...any) {
			if level == logger.Error {
				select {
				case errs <- fmt.Sprintf(format, args...):
				default:
				}
			}
		})
	})

	recorded := syncFile
	var calls atomic.Int32
	syncFile = func(f segmentFile) error {
		err := recorded(f)
		if calls.Add(1) == 1 {
			r.log.add(durabilityEvent{op: "failed"})
			return errors.New("injected fsync failure")
		}
		return err
	}

	r.writeFrames(30) // part 1 closes at 1 s, and its fsync fails
	r.waitFor("the failed fsync of the first part", func(l *durabilityLog) bool { return l.count("failed") == 1 })
	r.writeFrames(20) // the frame at 2 s closes part 2: the first part close since the failure
	select {
	case msg := <-errs:
		require.Contains(t, msg, "injected fsync failure")
	case <-time.After(10 * time.Second):
		t.Fatalf("the recorder did not stop at the part close after a failed part fsync; events: %v", eventOps(r.log.snapshot()))
	}
	r.stop()

	require.Zero(t, r.log.count("complete"), "events: %v", eventOps(r.log.snapshot()))
	require.Equal(t, []string{"2008-05-20_22-15-25-000000.mp4.part"}, dirEntries(t, r.dir))
}

// TestMTX1_QueuedPartsAreBounded (Task 7.2d): while the disk writer is held,
// here in the creation of the first segment's file, the parts wait in
// memory and the recording loses no frame. The memory they take is bounded
// (maxQueuedBytes): past the bound the recording waits for the disk, as it
// did before the disk writer, and the reader's queue then discards frames.
func TestMTX1_QueuedPartsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound int // maxQueuedBytes; 0 keeps the default
		lost  bool
	}{
		{"the default bound keeps every frame", 0, false},
		{"past the bound the recording waits", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.bound != 0 {
				realBound := maxQueuedBytes
				t.Cleanup(func() { maxQueuedBytes = realBound })
				maxQueuedBytes = tc.bound
			}
			r := startRecordingQueue(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC), test.FormatH264, 16, nil)

			held, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseCreate := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseCreate() // before the recorder's stop, on a failure too
			realCreateFile := createFile
			t.Cleanup(func() { createFile = realCreateFile })
			var holdOnce sync.Once
			createFile = func(name string) (segmentFile, error) {
				holdOnce.Do(func() {
					close(held)
					<-release
				})
				return realCreateFile(name)
			}

			// 3.5 s of frames while the first segment's file is not there yet: the
			// frame at 3 s closes the segment.
			r.writeFramesPaced(30)
			select {
			case <-held:
			case <-time.After(10 * time.Second):
				t.Fatal("the first segment's file was never created")
			}
			r.writeFramesPaced(40)

			// The stop closes the segment, also when the frames that would have
			// closed it were discarded.
			releaseCreate()
			r.stop()

			content, err := os.ReadFile(filepath.Join(r.dir, "2008-05-20_22-15-25-000000.mp4"))
			require.NoError(t, err)
			if tc.lost {
				require.Less(t, sampleCount(t, content), 60, "frames in the segment")
			} else {
				require.Equal(t, 60, sampleCount(t, content), "frames in the segment")
			}
		})
	}
}

// TestMTX1_QueueCapWaitsAndReportsTheDiscards (C1-R17): the disk writer
// holds at most maxQueuedBytes of parts per recorder, 8 MiB. The test lowers
// the bound to two parts of its frames: under the race detector a recorder
// fed 8 MiB of frames through a short reader queue falls behind for reasons
// of its own. While the creation of the first segment's file is held, as a
// busy disk holds it, two parts wait in memory and the third makes the
// recording wait for the disk. MediaMTX's reader queue, 16 frames here, then
// discards the frames past it and reports them ("reader is too slow"), as it
// does for any reader that falls behind. Nothing is lost silently and
// nothing is torn: every frame sent is in a segment, whole and in order, or
// was reported discarded, or is the one that the recorder holds at its
// stop. Each segment's close logs the most bytes of parts that waited for
// the disk since the previous one, which Task 9 (MI3) reads: here two parts.
func TestMTX1_QueueCapWaitsAndReportsTheDiscards(t *testing.T) {
	require.Equal(t, 8<<20, maxQueuedBytes, "the disk writer's bound per recorder (C1-R17)")
	const idrSize = 1024
	partBytes := framesPerPart * (idrSize + mtx2FrameOverhead)
	realBound := maxQueuedBytes
	t.Cleanup(func() { maxQueuedBytes = realBound })
	maxQueuedBytes = 2*partBytes + partBytes/2 // two parts and their boxes fit, three do not

	var mu sync.Mutex
	var logs []string
	r := startRecordingQueue(t, time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC), mtx2Format, 16, func(rec *Recorder) {
		rec.Parent = test.Logger(func(_ logger.Level, format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, fmt.Sprintf(format, args...))
		})
	})
	logged := func(prefix string) []int {
		mu.Lock()
		defer mu.Unlock()
		var counts []int
		for _, l := range logs {
			var n int
			if _, err := fmt.Sscanf(l, prefix+"%d", &n); err == nil {
				counts = append(counts, n)
			}
		}
		return counts
	}
	discarded := r.recorder.currentInstance.reader.OutboundFramesDiscarded

	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseCreate := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCreate() // before the recorder's stop, on a failure too
	realCreateFile := createFile
	t.Cleanup(func() { createFile = realCreateFile })
	var holdOnce sync.Once
	createFile = func(name string) (segmentFile, error) {
		holdOnce.Do(func() {
			close(held)
			<-release
		})
		return realCreateFile(name)
	}

	writeFrames := func(n int) {
		for range n {
			r.writeAgeFrame(idrSize)
			time.Sleep(time.Millisecond)
		}
	}
	writeFrames(30) // part 1 closes, and its segment's creation is held
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("the first segment's file was never created")
	}
	writeFrames(70) // parts 2 and 3: the recording waits at the third
	require.Positive(t, discarded(), "frames discarded while the recording waited for the disk")

	// Once the disk is back, the frames before 6 s, the end of the second
	// segment. A frame that the reader's queue takes from 6 s on closes that
	// segment and is the last one sent, so once the segment is complete the
	// queue is empty, and that frame is the one the recorder holds.
	releaseCreate()
	writeFrames(2*(oneSegmentFrames-1) - r.frames)
	for {
		before := discarded()
		r.writeAgeFrame(idrSize)
		if discarded() == before {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.waitFor("the second segment's hook", func(l *durabilityLog) bool { return l.count("complete") == 2 })
	lost := discarded()
	deadline := time.Now().Add(10 * time.Second)
	for sum(logged("[recorder] reader is too slow, discarding ")) < int(lost) {
		if time.Now().After(deadline) {
			t.Fatalf("MediaMTX reported %d discarded frames, want %d", sum(logged("[recorder] reader is too slow, discarding ")), lost)
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.stop()

	var recorded []int
	for _, name := range dirEntries(t, r.dir) {
		content, err := os.ReadFile(filepath.Join(r.dir, name))
		require.NoError(t, err)
		mdatEnds(t, content) // whole boxes up to the end
		frames := idrFrames(content)
		require.Equal(t, sampleCount(t, content), len(frames), "samples of %s", name)
		recorded = append(recorded, frames...)
	}
	require.True(t, slices.IsSorted(recorded) && len(slices.Compact(slices.Clone(recorded))) == len(recorded),
		"the recorded frames are not in order: %v", recorded)
	require.Equal(t, r.frames, len(recorded)+int(lost)+1,
		"frames sent = recorded (%d) + reported discarded (%d) + the one held at the stop", len(recorded), lost)

	peaks := logged("[recorder] disk queue peak: ")
	require.Len(t, peaks, 2, "a disk queue peak per segment")
	require.GreaterOrEqual(t, peaks[0], 2*partBytes, "the first segment's peak: two parts waited")
	require.LessOrEqual(t, slices.Max(peaks), maxQueuedBytes, "the disk queue's peak")
}

// idrFrames returns the frame numbers of the mtx2IDR NAL units in content,
// in the order of the file.
func idrFrames(content []byte) []int {
	marker := []byte{0x65, 'M', 'T', 'X', '2', '-', 'I', 'D', 'R'}
	var frames []int
	for i := 0; ; {
		j := bytes.Index(content[i:], marker)
		if j < 0 || i+j+len(marker)+4 > len(content) {
			return frames
		}
		i += j + len(marker)
		frames = append(frames, int(binary.BigEndian.Uint32(content[i:])))
	}
}

// sum returns the sum of ns.
func sum(ns []int) int {
	s := 0
	for _, n := range ns {
		s += n
	}
	return s
}
