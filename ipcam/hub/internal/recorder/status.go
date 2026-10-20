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
}

// firstPart is the first growth of a new .part after the child started.
type firstPart struct {
	key          streamKey
	at           time.Time // when the poll saw it
	segmentStart time.Time // from the .part's name
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
			w.streams[k] = &partState{baseline: w.partSet(k)}
		}
	}
	for k := range w.streams {
		if !keep[k] {
			delete(w.streams, k)
		}
	}
}

// childStarted takes the .part files there now as every stream's
// baseline: the next new one to grow is the stream's first part.
func (w *partWatcher) childStarted(time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, s := range w.streams {
		s.baseline, s.reported = w.partSet(k), false
	}
}

func (w *partWatcher) segmentClosed(k streamKey, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s, ok := w.streams[k]; ok {
		s.lastSegment = at
	}
}

// poll reads the size of each stream's newest .part and returns the
// streams whose first part this is.
func (w *partWatcher) poll(now time.Time) []firstPart {
	w.mu.Lock()
	defer w.mu.Unlock()
	var firsts []firstPart
	for k, s := range w.streams {
		path, size, ok := newestPart(w.streamDir(k))
		if !ok {
			continue
		}
		grew := size > 0 && (path != s.path || size > s.size)
		s.path, s.size = path, size
		if !grew || s.baseline[path] {
			continue
		}
		s.lastGrowth = now
		if !s.reported {
			s.reported = true
			f := firstPart{key: k, at: now, segmentStart: now}
			if seg, ok := parseSegment(w.recDir, filepath.Join(string(k.cam), string(k.profile), filepath.Base(filepath.Dir(path)), filepath.Base(path))); ok {
				f.segmentStart = seg.start
			}
			firsts = append(firsts, f)
		}
	}
	return firsts
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
	days, err := os.ReadDir(streamDir)
	if err != nil {
		return nil
	}
	var parts []string
	for _, day := range days[max(0, len(days)-3):] {
		if !day.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(streamDir, day.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), suffixPart) {
				parts = append(parts, filepath.Join(streamDir, day.Name(), e.Name()))
			}
		}
	}
	return parts
}

// newestPart returns the stream's newest .part and its size.
func newestPart(streamDir string) (path string, size int64, ok bool) {
	parts := partFiles(streamDir)
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
// for today and tomorrow (local time, as MediaMTX names them), syncing the
// parent of each directory it creates. MediaMTX creates a missing
// directory with MkdirAll and syncs only the segment's own directory (MTX-1),
// so without this a cut could lose the directory entry of a new day or a
// new camera, with the segments in it.
func ensureStreamDirs(recDir string, cam CameraID, p Profile, now time.Time) error {
	stream := filepath.Join(recDir, string(cam), string(p))
	for _, day := range []time.Time{now, now.AddDate(0, 0, 1)} {
		if err := mkdirSynced(filepath.Join(stream, day.Local().Format("2006-01-02"))); err != nil {
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
