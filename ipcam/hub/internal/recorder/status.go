package recorder

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// pollInterval is how often the size of each stream's open .part is read
// (G05): Status's LastPartAgoMs, and FirstPart, come from it.
const pollInterval = 250 * time.Millisecond

// partWatcher follows the open .part file of each stream.
type partWatcher struct {
	recDir string

	mu      sync.Mutex
	streams map[streamKey]*partState
	polls   uint64 // the polls so far
}

type partState struct {
	path        string // the newest .part seen
	size        int64
	lastGrowth  time.Time // zero until it grows
	lastSegment time.Time // zero until a segment closes
	// baseline holds the .part files that were there when the child started:
	// a dead child's, which do not grow.
	baseline map[string]bool
	reported bool // FirstPart was reported since the child started
	// seen holds the poll that first saw each other .part, which this child
	// created, and left those that poll reported as left behind.
	seen map[string]uint64
	left map[string]bool
	// resumeAfter, when set, is a failed creation of a segment's file: the
	// next new .part that starts after it is reported as recording resumed
	// (expectResume).
	resumeAfter time.Time
}

func newPartState(baseline map[string]bool) *partState {
	return &partState{baseline: baseline, seen: map[string]uint64{}, left: map[string]bool{}}
}

// firstPart is the first growth of a new .part after the child started, or
// after a failed creation of a segment's file (expectResume).
type firstPart struct {
	key          streamKey
	at           time.Time // when the poll saw it
	segmentStart time.Time // from the .part's name
	// resumed is a part after a failed creation: not the stream's first
	// since the child started.
	resumed bool
}

func newPartWatcher(recDir string) *partWatcher {
	return &partWatcher{recDir: recDir, streams: map[streamKey]*partState{}}
}

// setStreams follows keys and forgets the other streams. A stream that is
// new gets the .part files there now as its baseline.
func (w *partWatcher) setStreams(keys []streamKey) {
	w.mu.Lock()
	defer w.mu.Unlock()
	keep := map[streamKey]bool{}
	for _, k := range keys {
		keep[k] = true
		if _, ok := w.streams[k]; !ok {
			w.streams[k] = newPartState(w.partSet(k))
		}
	}
	for k := range w.streams {
		if !keep[k] {
			delete(w.streams, k)
		}
	}
}

// childStarted takes the .part files there now as every stream's
// baseline: the next new one to grow is the stream's first part. The
// supervisor calls it just before it starts a child, so every file in the
// baseline is one that no child writes.
func (w *partWatcher) childStarted(time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, s := range w.streams {
		s.baseline, s.reported = w.partSet(k), false
		s.seen, s.left = map[string]uint64{}, map[string]bool{}
	}
}

// claimFirst marks the stream's first part since the child started as
// reported, and reports whether no poll had reported it: a segment that
// closed before a poll saw its .part grow.
func (w *partWatcher) claimFirst(k streamKey) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.streams[k]
	if !ok || s.reported {
		return false
	}
	s.reported = true
	return true
}

// expectResume makes poll report the stream's next .part that starts after
// at and grows as a firstPart, resumed: MediaMTX could not create the file
// of a segment at at, and the stream's open gap ends where recording
// resumes, also when that is not its first part since the child started.
func (w *partWatcher) expectResume(k streamKey, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s, ok := w.streams[k]; ok {
		s.resumeAfter = at
	}
}

// inBaseline reports whether path is one of the stream's .part files that
// were there when the child started: one that no child writes.
func (w *partWatcher) inBaseline(k streamKey, path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.streams[k]
	return ok && s.baseline[path]
}

func (w *partWatcher) segmentClosed(k streamKey, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s, ok := w.streams[k]; ok {
		s.lastSegment = at
	}
}

// poll reads the size of each stream's newest .part and returns the
// streams whose first part this is, or whose recording resumed after a
// failed creation (expectResume), and the .part files that a newer one left
// behind (leftBehind).
func (w *partWatcher) poll(now time.Time) (firsts []firstPart, left []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.polls++
	for k, s := range w.streams {
		parts := partFiles(w.streamDir(k))
		left = append(left, s.leftBehind(parts, w.polls)...)
		path, size, ok := newestOf(parts)
		if !ok {
			continue
		}
		grew := size > 0 && (path != s.path || size > s.size)
		s.path, s.size = path, size
		if !grew || s.baseline[path] {
			continue
		}
		s.lastGrowth = now
		if s.reported && s.resumeAfter.IsZero() {
			continue
		}
		f := firstPart{key: k, at: now, segmentStart: now}
		if seg, ok := parseSegment(w.recDir, filepath.Join(string(k.cam), string(k.profile), filepath.Base(filepath.Dir(path)), filepath.Base(path))); ok {
			f.segmentStart = seg.start
		}
		if s.reported {
			if !f.segmentStart.After(s.resumeAfter) {
				continue // a .part from before the failed creation
			}
			f.resumed = true
		}
		s.reported, s.resumeAfter = true, time.Time{}
		firsts = append(firsts, f)
	}
	return firsts, left
}

// leftBehind notes poll n as the first that saw each of parts, the stream's
// .part files now, and returns, once each, those that this child created
// and that a newer one left behind: one that a later poll saw first and
// whose path (date directory and start) sorts later. MediaMTX closes a
// stream's segment before it creates the stream's next one, also when a
// failed write or fsync makes its recorder start again in the same process,
// which leaves the .part (MTX-1), so such a file is no longer written. The
// names alone would not do: a clock set back names the next segment
// earlier. Nor would the order of the polls alone: when a newer date
// directory goes, an older one comes into the three that are listed, and
// its .part files are seen only then.
func (s *partState) leftBehind(parts []string, n uint64) []string {
	listed := map[string]bool{}
	for _, p := range parts {
		listed[p] = true
		if _, ok := s.seen[p]; !ok && !s.baseline[p] {
			s.seen[p] = n
		}
	}
	var left []string
	for p, first := range s.seen {
		if !listed[p] {
			if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
				delete(s.seen, p) // renamed or removed
				delete(s.left, p)
			}
			continue
		}
		if s.left[p] {
			continue
		}
		for q, later := range s.seen {
			if later > first && listed[q] && q > p {
				s.left[p] = true
				left = append(left, p)
				break
			}
		}
	}
	slices.Sort(left)
	return left
}

// status is the stream's RecStatus at now; Running is the caller's.
func (w *partWatcher) status(k streamKey, now time.Time) RecStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := RecStatus{LastPartAgoMs: -1, LastSegmentAgoMs: -1}
	if s, ok := w.streams[k]; ok {
		if !s.lastGrowth.IsZero() {
			st.LastPartAgoMs = now.Sub(s.lastGrowth).Milliseconds()
		}
		if !s.lastSegment.IsZero() {
			st.LastSegmentAgoMs = now.Sub(s.lastSegment).Milliseconds()
		}
	}
	return st
}

func (w *partWatcher) streamDir(k streamKey) string {
	return filepath.Join(w.recDir, string(k.cam), string(k.profile))
}

// partSet returns the .part files of the stream now.
func (w *partWatcher) partSet(k streamKey) map[string]bool {
	set := map[string]bool{}
	for _, p := range partFiles(w.streamDir(k)) {
		set[p] = true
	}
	return set
}

// partFiles lists the .part files in the three newest date directories of
// a stream: a segment open at midnight is still in yesterday's, and the
// recorder creates tomorrow's ahead of time.
func partFiles(streamDir string) []string {
	var parts []string
	for _, path := range recentFiles(streamDir) {
		if strings.HasSuffix(path, suffixPart) {
			parts = append(parts, path)
		}
	}
	return parts
}

// readDir lists a directory under rec/; tests replace it.
var readDir = os.ReadDir

// recentFiles lists the entries of the three newest date directories of a
// stream.
func recentFiles(streamDir string) []string {
	days, err := readDir(streamDir)
	if err != nil {
		return nil
	}
	var files []string
	for _, day := range days[max(0, len(days)-3):] {
		if !day.IsDir() {
			continue
		}
		entries, err := readDir(filepath.Join(streamDir, day.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			files = append(files, filepath.Join(streamDir, day.Name(), e.Name()))
		}
	}
	return files
}

// newestPart returns the stream's newest .part and its size.
func newestPart(streamDir string) (path string, size int64, ok bool) {
	return newestOf(partFiles(streamDir))
}

// newestOf returns the newest of a stream's .part files and its size.
func newestOf(parts []string) (path string, size int64, ok bool) {
	if len(parts) == 0 {
		return "", 0, false
	}
	path = slices.Max(parts) // names sort by start
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, false
	}
	return path, info.Size(), true
}

// syncDir makes a directory's entries durable; tests replace it.
var syncDir = platformSyncDir

// ensureStreamDirs creates the stream's directory and its date directories
// for today and tomorrow (UTC days, as MediaMTX names them: MTX-4), syncing
// the parent of each directory it creates. MediaMTX creates a missing
// directory with MkdirAll and syncs only the segment's own directory (MTX-1),
// so without this a cut could lose the directory entry of a new day or a
// new camera, with the segments in it.
func ensureStreamDirs(recDir string, cam CameraID, p Profile, now time.Time) error {
	stream := filepath.Join(recDir, string(cam), string(p))
	for _, day := range []time.Time{now, now.AddDate(0, 0, 1)} {
		if err := mkdirSynced(filepath.Join(stream, day.UTC().Format("2006-01-02"))); err != nil {
			return err
		}
	}
	return nil
}

// mkdirSynced creates dir and each missing parent, and syncs the parent of
// each directory it creates.
func mkdirSynced(dir string) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := syncDir(filepath.Dir(missing[i])); err != nil {
			return err
		}
	}
	return nil
}
