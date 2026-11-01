package playback

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrNotFMP4 is a recording that is not laid out as MediaMTX writes one
// (§4.2): an ftyp and a moov, then one moof and one mdat per part, with free
// boxes as padding.
var ErrNotFMP4 = errors.New("the plaintext is not an fMP4 recording")

// ErrPastEnd is a box, or a box header, that ends past the bytes that can
// be read: a torn tail. Errors of ReadFMP4Box that wrap it wrap ErrNotFMP4
// too.
var ErrPastEnd = errors.New("past the end")

// Box is an ISO BMFF box header: where the box starts, its size with its
// header, its header's size and its type.
type Box struct {
	Off, Size, Hdr int64
	Type           string
}

// End is where the box ends.
func (b Box) End() int64 { return b.Off + b.Size }

// ReadFMP4Box reads the header of the box at off, in the size bytes of r.
// It moved here from cmd/ipcam-hub's diag_fmp4.go, which calls it from
// here, so the diag benches and ReadOpen read box headers alike.
func ReadFMP4Box(r io.ReaderAt, off, size int64) (Box, error) {
	var h [16]byte
	if size-off < 8 {
		return Box{}, fmt.Errorf("%w: a box header %w, at %d", ErrNotFMP4, ErrPastEnd, off)
	}
	if err := ReadFullAt(r, h[:8], off); err != nil {
		return Box{}, err
	}
	b := Box{Off: off, Size: int64(binary.BigEndian.Uint32(h[:4])), Hdr: 8, Type: string(h[4:8])}
	if b.Size == 1 {
		if size-off < 16 {
			return Box{}, fmt.Errorf("%w: a box header %w, at %d", ErrNotFMP4, ErrPastEnd, off)
		}
		if err := ReadFullAt(r, h[8:16], off+8); err != nil {
			return Box{}, err
		}
		b.Size, b.Hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16 //nolint:gosec // checked below
	}
	switch {
	case b.Size < b.Hdr:
		return Box{}, fmt.Errorf("%w: a %q box of %d bytes at %d", ErrNotFMP4, b.Type, b.Size, off)
	case b.End() > size || b.End() < b.Off:
		return Box{}, fmt.Errorf("%w: a %q box of %d bytes at %d, %w", ErrNotFMP4, b.Type, b.Size, off, ErrPastEnd)
	}
	return b, nil
}

// ReadFullAt reads len(p) bytes at off: an io.ReaderAt may give io.EOF
// with all of them when they end its data.
func ReadFullAt(r io.ReaderAt, p []byte, off int64) error {
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return err
}
