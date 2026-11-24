// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi2

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"slices"
	"sync"
	"time"
)

// The rolling window of the 72 h run (G19): 72 h of recording is ≈ 1 TB on
// the PC, so recspike deletes the verified segments older than the window.
const (
	// cameraBps is one main stream's bitrate, 4 Mbps (§4.8).
	cameraBps = 4_000_000
	// MaxWindow and MinWindow bound the window: recspike refuses to start
	// below 1 h.
	MaxWindow = 6 * time.Hour
	MinWindow = time.Hour
)

// ErrWindowTooSmall is Window's error when half the free space holds less
// than an hour of recording. recspike prints it to the owner, in Romanian.
var ErrWindowTooSmall = errors.New("spațiul liber nu ajunge pentru o fereastră de cel puțin 1 h")

// WriteRate is the write rate of cameras main streams, in bytes per second:
// 4 MB/s for 8 cameras.
func WriteRate(cameras int) float64 { return float64(cameras) * cameraBps / 8 }

// Window is the rolling window N = min(6 h, ½ × free ÷ WriteRate) for free
// bytes free and cameras cameras, with ErrWindowTooSmall when N < 1 h: the
// other half of the free space is the margin for the segments that wait for
// their verification and for the catalog.
func Window(free uint64, cameras int) (time.Duration, error) {
	n := min(time.Duration(float64(free)/2/WriteRate(cameras)*float64(time.Second)), MaxWindow)
	if n < MinWindow {
		return n, ErrWindowTooSmall
	}
	return n, nil
}

// NeededGB is the free space, in GB (10⁹ bytes) rounded up, that a window
// of n needs for cameras: 2 × n × WriteRate; 29 GB for 8 cameras at 1 h.
func NeededGB(cameras int, n time.Duration) int {
	return int(math.Ceil(2*n.Seconds()*WriteRate(cameras)/1e9 - 1e-9))
}

// Pruner deletes the segments that are older than the window (G19), but
// only those whose segment_verified event recspike saw: tools cannot read
// ipcam-hub's catalog (C01), and the verify event carries only counts. The
// rows of the deleted files read lost and count in verify's
// catalogMismatches, which MI2 does not judge: the deletes are the spike's,
// outside ipcam-hub.
type Pruner struct {
	window time.Duration

	mu       sync.Mutex
	verified map[string]bool // absolute paths, as segment_verified gives them
}

// NewPruner returns a Pruner of the window.
func NewPruner(window time.Duration) *Pruner {
	return &Pruner{window: window, verified: map[string]bool{}}
}

// Verified notes a segment that ipcam-hub's verification passed.
func (p *Pruner) Verified(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verified[path] = true
}

// Prune deletes the verified segments whose last write, their close, is at
// least the window before now, and returns them, sorted. A segment that is
// gone is forgotten; one that cannot be deleted now is kept for the next
// prune, with its error, the file system's own, which names the segment.
func (p *Pruner) Prune(now time.Time) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var deleted []string
	var errs []error
	for path := range p.verified {
		info, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			delete(p.verified, path)
			continue
		case err != nil:
			errs = append(errs, err)
			continue
		case now.Sub(info.ModTime()) < p.window:
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		delete(p.verified, path)
		deleted = append(deleted, path)
	}
	slices.Sort(deleted)
	return deleted, errors.Join(errs...)
}
