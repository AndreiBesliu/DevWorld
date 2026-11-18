package recorder

import (
	"encoding/binary"
	"io"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

const (
	// ageSuffix ends the final name of an encrypted segment: <name>.mp4.age.
	ageSuffix = ".age"

	// smallPartBytes is §4.5's small part (O17): an encrypted part whose moof
	// and mdat are shorter than one age chunk, as at about 524 kbps with 1 s
	// parts. With the low-bitrate setting on, such a part is always padded.
	smallPartBytes = crypto.ChunkSize

	// freeBoxHeaderSize is the size of an empty fMP4 free box, the smallest
	// box there is.
	freeBoxHeaderSize = 8
)

// ageSegmentWriter writes the plaintext of an encrypted fMP4 segment (MTX-2)
// through a crypto.Writer, strictly in order: the init, then each part and its
// padding, and at the close the final chunk. It holds at most one chunk of
// plaintext in RAM.
type ageSegmentWriter struct {
	w             *crypto.Writer
	padLowBitrate bool
	plaintext     int64 // the plaintext bytes written, padding included
}

// newAgeSegmentWriter writes the age header for scope to dst, which it never
// closes, and returns the writer of the segment's plaintext. padLowBitrate is
// the setting O17.
func newAgeSegmentWriter(dst io.Writer, scope crypto.KeyScope, padLowBitrate bool) (*ageSegmentWriter, error) {
	w, err := crypto.NewWriter(dst, scope)
	if err != nil {
		return nil, err
	}
	return &ageSegmentWriter{w: w, padLowBitrate: padLowBitrate}, nil
}

// Write writes plaintext: the init, or a part's moof and mdat.
func (a *ageSegmentWriter) Write(p []byte) (int, error) {
	n, err := a.w.Write(p)
	a.plaintext += int64(n)
	return n, err
}

// size returns the plaintext bytes written so far.
func (a *ageSegmentWriter) size() int64 { return a.plaintext }

// endPart ends the part written since the plaintext offset start, before its
// fsync: it pads the part with a free box when partPadding says so, then
// seals every complete chunk, so that only the bytes after the last chunk
// boundary stay in RAM (§4.5).
func (a *ageSegmentWriter) endPart(start int64) error {
	if p := partPadding(a.plaintext, a.plaintext-start, a.padLowBitrate); p != 0 {
		if err := a.writeFreeBox(p); err != nil {
			return err
		}
	}
	return a.w.Flush()
}

// close seals the final chunk. When every byte is sealed already, as after a
// part padded to a chunk boundary, it first writes an empty free box, so that
// the final chunk is never empty: age rejects an empty final chunk after
// other chunks.
func (a *ageSegmentWriter) close() error {
	if a.w.Buffered() == 0 {
		if err := a.writeFreeBox(freeBoxHeaderSize); err != nil {
			return err
		}
	}
	return a.w.Close()
}

// writeFreeBox writes an fMP4 free box of size bytes, its header included.
func (a *ageSegmentWriter) writeFreeBox(size int64) error {
	box := make([]byte, size)
	binary.BigEndian.PutUint32(box, uint32(size)) //nolint:gosec // at most a chunk and a header
	copy(box[4:], "free")
	_, err := a.Write(box)
	return err
}

// partPadding returns the size of the free box that pads a part of size bytes,
// its moof and mdat, which ends at the plaintext offset end, or 0 for none
// (§4.5 as amended by O17). The padding p reaches the next chunk boundary, or
// the one after when the gap is too short for a free box. A small part is
// padded when the setting padLowBitrate is on; any part when p is at most
// 10 % of it.
func partPadding(end, size int64, padLowBitrate bool) int64 {
	p := (crypto.ChunkSize - end%crypto.ChunkSize) % crypto.ChunkSize
	if p == 0 {
		return 0
	}
	if p < freeBoxHeaderSize {
		p += crypto.ChunkSize
	}
	if padLowBitrate && size < smallPartBytes {
		return p
	}
	if 10*p <= size {
		return p
	}
	return 0
}
