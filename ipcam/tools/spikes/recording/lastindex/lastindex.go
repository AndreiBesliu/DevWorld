// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package lastindex reads which access unit was the last that a host
// recorded of a stream before a stop: the SEI index (C15) of the last
// complete sample in the stream's newest segment that holds one. It reads
// the fMP4 files that ipcam-hub's MediaMTX wrote, decrypting .age files
// with filippo.io/age's public API, and imports nothing of the hub (C01).
package lastindex

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// Before is the stream's segment files that existed when the host stopped.
// recspike lists them before it restarts ipcam-hub, which records new
// segments at once (ResumeMs, ColdBootMs); a .part name stands for the final
// name reconcile gives it.
type Before []string

// ErrNoSample is returned when no file of Before holds a complete sample.
var ErrNoSample = errors.New("lastindex: no segment holds a complete sample")

// The suffixes of segment names: .part while MediaMTX writes it (MTX-1),
// .age when encrypted (MTX-2).
const (
	suffixPart = ".part"
	suffixAge  = ".age"
)

// maxPart bounds the moof and mdat of one part that LastRecorded reads into
// memory; MediaMTX's recordMaxPartSize is 50 MiB.
const maxPart = 64 << 20

// LastRecorded returns the SEI index of the last complete sample in the
// newest non-empty segment of files; .age files are decrypted with id. The
// newest is the one whose name (date folder, then time, both UTC) sorts
// last. A file that reconcile removed is skipped, as is one with no
// complete part.
func LastRecorded(ctx context.Context, files Before, id age.Identity) (uint64, error) {
	finals := make([]string, 0, len(files))
	for _, f := range files {
		finals = append(finals, strings.TrimSuffix(f, suffixPart))
	}
	slices.SortFunc(finals, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(filepath.Base(filepath.Dir(a)), filepath.Base(filepath.Dir(b))),
			cmp.Compare(filepath.Base(a), filepath.Base(b)))
	})
	finals = slices.Compact(finals)
	for i := len(finals) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		index, ok, err := lastInFile(finals[i], id)
		if err != nil {
			return 0, fmt.Errorf("lastindex: %s: %w", filepath.Base(finals[i]), err)
		}
		if ok {
			return index, nil
		}
	}
	return 0, ErrNoSample
}

// lastInFile reads the index of the file's last complete sample; ok is false
// when the file does not exist or holds no complete part.
func lastInFile(path string, id age.Identity) (index uint64, ok bool, err error) {
	f, data, size, err := openPlaintext(path, id)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil || f == nil {
		return 0, false, err
	}
	defer func() { _ = f.Close() }()
	var moof, end int64
	found := false
	err = walkParts(data, size, func(m, e int64) error {
		moof, end, found = m, e, true
		return nil
	})
	if err != nil || !found {
		return 0, false, err
	}
	parts, err := readPart(data, moof, end)
	if err != nil {
		return 0, false, err
	}
	return lastIndexIn(parts)
}

// openPlaintext opens a segment file and returns its plaintext, decrypted
// with id for an .age file. f is nil, with no error, for an encrypted file
// of no bytes, which has no plaintext.
func openPlaintext(path string, id age.Identity) (f *os.File, data io.ReaderAt, size int64, err error) {
	f, err = os.Open(path) //nolint:gosec // a segment file that recspike listed
	if err != nil {
		return nil, nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, 0, err
	}
	data, size = f, info.Size()
	if strings.HasSuffix(path, suffixAge) {
		if id == nil {
			_ = f.Close()
			return nil, nil, 0, errors.New("an encrypted segment and no identity")
		}
		if size == 0 {
			_ = f.Close()
			return nil, nil, 0, nil
		}
		if data, size, err = age.DecryptReaderAt(f, size, id); err != nil {
			_ = f.Close()
			return nil, nil, 0, err
		}
	}
	return f, data, size, nil
}

// readPart reads the part, a moof and its mdat, at [moof, end) of data.
func readPart(data io.ReaderAt, moof, end int64) (fmp4.Parts, error) {
	if end-moof > maxPart {
		return nil, fmt.Errorf("a part of %d bytes", end-moof)
	}
	buf := make([]byte, end-moof)
	if _, err := data.ReadAt(buf, moof); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var parts fmp4.Parts
	if err := parts.Unmarshal(buf); err != nil {
		return nil, err
	}
	return parts, nil
}

// Part is one complete part of a segment as the diagnostics of a loss show
// it (Task 7.2d): its sequence number, its size (moof and mdat) and the SEI
// indexes of its first and last samples.
type Part struct {
	Seq   uint32 `json:"seq"`
	Bytes int64  `json:"bytes"`
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}

// ReadParts reads every complete part of the segment file at path, an .age
// one decrypted with id, and the file's size on disk. A torn part at the end
// is not one of them.
func ReadParts(path string, id age.Identity) (size int64, parts []Part, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, nil, err
	}
	f, data, plain, err := openPlaintext(path, id)
	if err != nil || f == nil {
		return info.Size(), nil, err
	}
	defer func() { _ = f.Close() }()
	err = walkParts(data, plain, func(moof, end int64) error {
		decoded, err := readPart(data, moof, end)
		if err != nil {
			return err
		}
		for _, p := range decoded {
			part := Part{Seq: p.SequenceNumber, Bytes: end - moof}
			for _, track := range p.Tracks {
				for i, sample := range track.Samples {
					var au h264.AVCC
					if err := au.Unmarshal(sample.Payload); err != nil {
						return err
					}
					index, _ := controller.ReadIndex(au)
					if i == 0 {
						part.First = index
					}
					part.Last = index
				}
			}
			parts = append(parts, part)
		}
		return nil
	})
	return info.Size(), parts, err
}

// lastIndexIn reads the index of the last sample of the last part; ok is
// false when there is no part or the part has no sample, so that the file
// reads as one without a complete sample.
func lastIndexIn(parts fmp4.Parts) (index uint64, ok bool, err error) {
	if len(parts) == 0 {
		return 0, false, nil
	}
	for _, track := range parts[len(parts)-1].Tracks {
		if len(track.Samples) == 0 {
			continue
		}
		var au h264.AVCC
		if err := au.Unmarshal(track.Samples[len(track.Samples)-1].Payload); err != nil {
			return 0, false, err
		}
		if index, ok := controller.ReadIndex(au); ok {
			return index, true, nil
		}
		return 0, false, errors.New("the last sample carries no index")
	}
	return 0, false, nil
}

// walkParts walks the top-level boxes of a recording's plaintext (ftyp,
// moov, then a moof and an mdat per part, with free boxes as MTX-2's
// padding) and calls visit with where each complete part starts and ends,
// in order. A box that runs past the end, as a cut leaves, ends the walk.
func walkParts(r io.ReaderAt, size int64, visit func(moof, end int64) error) error {
	var pendingMoof int64 = -1
	for off := int64(0); size-off >= 8; {
		var h [16]byte
		if _, err := r.ReadAt(h[:8], off); err != nil {
			return err
		}
		boxSize, hdr := int64(binary.BigEndian.Uint32(h[:4])), int64(8)
		if boxSize == 1 {
			if size-off < 16 {
				break
			}
			if _, err := r.ReadAt(h[8:16], off+8); err != nil {
				return err
			}
			boxSize, hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16 //nolint:gosec // checked below
		}
		if boxSize < hdr || boxSize > size-off {
			break
		}
		switch string(h[4:8]) {
		case "moof":
			pendingMoof = off
		case "mdat":
			if pendingMoof >= 0 {
				if err := visit(pendingMoof, off+boxSize); err != nil {
					return err
				}
			}
			pendingMoof = -1
		case "free", "skip":
		default:
			pendingMoof = -1
		}
		off += boxSize
	}
	return nil
}
