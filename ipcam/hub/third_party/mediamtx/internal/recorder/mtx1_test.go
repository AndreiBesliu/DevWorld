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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	amp4 "github.com/abema/go-mp4"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	rtspformat "github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
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
	// op is "create" (OnSegmentCreate), "syncFile", "rename", "syncDir" or
	// "complete" (OnSegmentComplete).
	op string
	// path is the file that is synced, the rename's target, the directory
	// that is synced or the path that a hook received.
	path string
	// from is the rename's source.
	from string
	// fileOpen is, at a syncFile, whether the handle is still open, and, at a
	// rename, whether the handle of the last syncFile is still open.
	fileOpen bool
	// content is the file's content at a syncFile, or the source's at a rename.
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

	realSyncFile, realSyncDir, realRenameFinal := syncFile, syncDir, renameFinal
	t.Cleanup(func() {
		syncFile, syncDir, renameFinal = realSyncFile, realSyncDir, realRenameFinal
	})

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
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []rtspformat.Format{forma},
	}}}

	strm := &stream.Stream{
		OrigDesc:          desc,
		WriteQueueSize:    512,
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

// TestMTX1_FsyncOnEachPartClose: a segment of 3 parts gets 4 syncFile calls,
// one asked for when each part has been written, so that the file holds that
// part at its fsync (the fsync runs beside the recording, which may have
// written the next parts by then), and one after the close's last write (the
// mvhd duration) and before Close.
func TestMTX1_FsyncOnEachPartClose(t *testing.T) {
	r := recordOneSegment(t)

	final := filepath.Join(r.dir, "2008-05-20_22-15-25-000000.mp4")
	content, err := os.ReadFile(final)
	require.NoError(t, err)
	partEnds := mdatEnds(t, content)
	require.Len(t, partEnds, 3, "parts in the segment")

	syncs := eventsOf(r.log.snapshot(), "syncFile")
	require.Equal(t, 4, len(syncs), "syncFile calls; events: %v", eventOps(r.log.snapshot()))

	for i, ev := range syncs {
		require.Equal(t, final+".part", ev.path, "syncFile %d", i+1)
		require.True(t, ev.fileOpen, "syncFile %d ran on a closed file", i+1)
	}
	for i, end := range partEnds {
		require.GreaterOrEqual(t, int64(len(syncs[i].content)), end, "file size at syncFile %d, the fsync of part %d", i+1, i+1)
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

	// The first comes before any part is written, the second right after the rename.
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
