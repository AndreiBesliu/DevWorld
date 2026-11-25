// Package playback reads the hub's recordings for playback (STO-02), in
// its Phase 0 subset: ReadOpen reads the segment that MediaMTX is still
// writing, which MI5 measures (P14).
package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// The names of an open segment, which MediaMTX renames once it is complete
// (MTX-1).
const (
	clearPart = ".mp4.part"
	agePart   = ".mp4.age.part"
)

// ErrNoOpenSegment means the stream has no open segment: no .part under its
// directory of today or of yesterday (UTC).
var ErrNoOpenSegment = errors.New("playback: no open segment")

// OpenRead is what ReadOpen read of an open segment: whether it is
// encrypted, how many whole parts (moof and mdat) it holds, its durable
// bytes, and how long the read took, its search included.
type OpenRead struct {
	Encrypted    bool
	Parts        int
	DurableBytes int64
	ReadMs       float64
}

// ReadOpen reads the open segment of cam's profile under recDir: the
// newest *.mp4.part or *.mp4.age.part of <recDir>/<cam>/<profile>/<UTC
// date>/, today's or, before a segment of today, yesterday's. A clear file
// is read directly; its durable bytes are its size. An .age file is opened
// read-only through tail.OpenTorn and read over ReadAt up to its
// DurableSize, the chunks that the writer flushed, since a finished
// recording's reader needs the final chunk. Either way ReadOpen reads box
// headers only: the ftyp, the moov, then each moof and mdat pair, and it
// stops at the first box that ends past the durable bytes, the part that is
// being written, where a finished segment's reader would fail. It changes
// nothing. A segment that MediaMTX renames between the search and the open
// sends ReadOpen to the next one, once.
func ReadOpen(ctx context.Context, recDir, cam, profile string, tail crypto.TailSealer) (OpenRead, error) {
	start := time.Now()
	for retried := false; ; retried = true {
		path, err := newestPart(filepath.Join(recDir, cam, profile), start.UTC())
		if err != nil {
			return OpenRead{}, err
		}
		read, err := readPart(ctx, path, tail)
		if errors.Is(err, fs.ErrNotExist) && !retried {
			continue // the segment completed, and its .part has its final name now
		}
		if err != nil {
			return OpenRead{}, err
		}
		read.ReadMs = float64(time.Since(start).Microseconds()) / 1000
		return read, nil
	}
}

// newestPart returns the newest .part of the stream directory streamDir: in
// its directory of the UTC date of now, or of the day before when that
// holds none, as a segment begun before midnight is still open after it.
// Names start with the segment's start time (MTX-4), so the greatest is
// the newest.
func newestPart(streamDir string, now time.Time) (string, error) {
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		dir := filepath.Join(streamDir, day.Format(time.DateOnly))
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("playback: read the day's segments: %w", stripPath(err))
		}
		newest := ""
		for _, e := range entries {
			name := e.Name()
			if e.Type().IsRegular() && (strings.HasSuffix(name, clearPart) || strings.HasSuffix(name, agePart)) && name > newest {
				newest = name
			}
		}
		if newest != "" {
			return filepath.Join(dir, newest), nil
		}
	}
	return "", ErrNoOpenSegment
}

// readPart reads the open segment at path, as ReadOpen says, but its time.
func readPart(ctx context.Context, path string, tail crypto.TailSealer) (OpenRead, error) {
	f, err := openShared(path)
	if err != nil {
		return OpenRead{}, fmt.Errorf("playback: %w", stripPath(err))
	}
	defer func() { _ = f.Close() }()
	read := OpenRead{Encrypted: strings.HasSuffix(path, agePart)}
	var r io.ReaderAt = f
	if read.Encrypted {
		if tail == nil {
			return OpenRead{}, errors.New("playback: an encrypted segment, and no identity to read it")
		}
		torn, err := tail.OpenTorn(ctx, f)
		if err != nil {
			return OpenRead{}, err
		}
		r, read.DurableBytes = torn, torn.DurableSize()
	} else {
		info, err := f.Stat()
		if err != nil {
			return OpenRead{}, fmt.Errorf("playback: %w", stripPath(err))
		}
		read.DurableBytes = info.Size()
	}
	read.Parts, err = countParts(r, read.DurableBytes)
	return read, err
}

// countParts counts the whole parts, moof and mdat pairs, in the size bytes
// of r, after the ftyp and the moov; free and skip boxes are padding. It
// stops at the first box that ends past size: the torn tail, whose type, when
// its header is there, must still be one that belongs there.
func countParts(r io.ReaderAt, size int64) (int, error) {
	whole, torn, err := walk(r, size)
	if err != nil {
		return 0, err
	}
	types := whole
	if torn != "" {
		types = append(slices.Clone(whole), torn)
	}
	for i, typ := range types {
		switch {
		case i == 0 && typ != "ftyp", i == 1 && typ != "moov":
			return 0, fmt.Errorf("%w: a %q box at its start", ErrNotFMP4, typ)
		case i >= 2 && !slices.Contains([]string{"moof", "mdat", "free", "skip"}, typ):
			return 0, fmt.Errorf("%w: a %q box where a part should be", ErrNotFMP4, typ)
		}
	}
	parts := 0
	for i := 2; i+1 < len(whole); i++ {
		if whole[i] == "moof" && whole[i+1] == "mdat" {
			parts++
			i++
		}
	}
	return parts, nil
}

// walk returns the types of the whole boxes in the size bytes of r, in
// order, up to the first box that ends past size, and that box's type when
// its header is within size.
func walk(r io.ReaderAt, size int64) (whole []string, torn string, err error) {
	for off := int64(0); off < size; {
		b, err := ReadFMP4Box(r, off, size)
		if errors.Is(err, ErrPastEnd) {
			var h [8]byte
			if size-off >= int64(len(h)) && ReadFullAt(r, h[:], off) == nil {
				torn = string(h[4:])
			}
			return whole, torn, nil
		}
		if err != nil {
			return nil, "", err
		}
		whole = append(whole, b.Type)
		off = b.End()
	}
	return whole, "", nil
}

// stripPath is err without the path of an *fs.PathError: ReadOpen's errors
// name no directory.
func stripPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return err
}
