// Package band is the frame code that the Phase 0 (b) video spike burns into
// every simulated camera frame, so that a player can tell which frame it shows.
//
// Layout. The band is two rows of 48 square blocks in the top-left corner of the
// frame: row 1 starts at the top edge, row 2 sits directly below it, and block i
// of a row spans x in [i*side, (i+1)*side). The block side is 16 px for frames
// of 1080 lines or more and 8 px below that. A white block is 1 and a black
// block is 0. Row 1 holds, most significant bit first, 8 bits of camera, 16 bits
// of frame index within the loop, 16 bits of loop counter (modulo 65536) and 8
// bits of CRC-8/SMBUS over the preceding 40 bits (the five bytes camera, frame
// high, frame low, loop high, loop low). Row 2 is the bitwise inverse of row 1.
//
// The Camera field is the media file id (1 sub, 2 main, 3 main-high51, 4
// main-h265), not an index of a simulated camera: the hub serves many simulated
// cameras from the same file.
//
// vectors.json next to this file is the single source of truth for Encode and
// Decode, shared with the TypeScript reader in the spike app.
package band

import "encoding/binary"

const (
	blocksPerRow = 48
	bytesPerRow  = blocksPerRow / 8

	// Limited-range video levels. A player that converts to RGB gets 255 and 0.
	white = 235
	black = 16
)

// Code is the value carried by the band of one frame.
type Code struct {
	Camera uint8
	Frame  uint16
	Loop   uint16
}

// crc8 is CRC-8/SMBUS: polynomial 0x07, initial value 0x00, no reflection and
// no final xor.
func crc8(data []byte) uint8 {
	var crc uint8
	for _, b := range data {
		crc ^= b
		for range 8 {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// Encode returns the bits of row 1 for c, left to right. Row 2 is the inverse.
func Encode(c Code) [blocksPerRow]bool {
	data := []byte{c.Camera}
	data = binary.BigEndian.AppendUint16(data, c.Frame)
	data = binary.BigEndian.AppendUint16(data, c.Loop)
	data = append(data, crc8(data))

	var row [blocksPerRow]bool
	for i := range row {
		row[i] = data[i/8]>>(7-i%8)&1 == 1
	}
	return row
}

// Decode returns the code carried by the two rows read from a frame. It reports
// false when row 2 is not the inverse of row 1 or when the CRC does not match.
func Decode(row1, row2 [blocksPerRow]bool) (Code, bool) {
	var data [bytesPerRow]byte
	for i := range row1 {
		if row2[i] == row1[i] {
			return Code{}, false
		}
		if row1[i] {
			data[i/8] |= 1 << (7 - i%8)
		}
	}
	if crc8(data[:bytesPerRow-1]) != data[bytesPerRow-1] {
		return Code{}, false
	}
	return Code{
		Camera: data[0],
		Frame:  binary.BigEndian.Uint16(data[1:3]),
		Loop:   binary.BigEndian.Uint16(data[3:5]),
	}, true
}

// blockSide is the side of one block, in pixels, for a frame of the given height.
func blockSide(height int) int {
	if height >= 1080 {
		return 16
	}
	return 8
}

// Paint draws the band of c into the luma plane at the start of an I420 frame.
// Nothing else in frame is touched. It panics if frame cannot hold the luma
// plane or if the frame is too small for the band.
func Paint(frame []byte, width, height int, c Code) {
	side := blockSide(height)
	bandWidth := blocksPerRow * side
	if len(frame) < width*height || width < bandWidth || height < 2*side {
		panic("band: frame too small for the band")
	}

	row1 := Encode(c)
	for r := range 2 {
		// Row 2 is the inverse of row 1.
		inverted := r == 1
		line := frame[r*side*width : r*side*width+bandWidth]
		for i, bit := range row1 {
			level := byte(black)
			if bit != inverted {
				level = white
			}
			block := line[i*side : (i+1)*side]
			for x := range block {
				block[x] = level
			}
		}
		for y := 1; y < side; y++ {
			copy(frame[(r*side+y)*width:], line)
		}
	}
}
