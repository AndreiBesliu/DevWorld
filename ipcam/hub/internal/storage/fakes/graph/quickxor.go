package fakegraph

import (
	"encoding/base64"
	"encoding/binary"
)

// quickXorHash returns OneDrive's quickXorHash of data in standard base64, as
// Graph writes it in file.hashes. It is the fake's own implementation, after
// the cell layout of Microsoft's C# reference (QuickXorHash snippet), so that
// the driver's tests compare storage.Digest with an independent one (P34): a
// 160-bit register held as two 64-bit cells and a 32-bit one, byte n of the
// stream XORed in at bit 11·n mod 160, a byte that crosses the end of a cell
// going on in the next (the last cell wraps to the first), and the length, as
// 8 little-endian bytes, XORed into the register's last 8 bytes.
func quickXorHash(data []byte) string {
	const (
		width = 160 // the register's bits
		shift = 11  // the bits between consecutive bytes
	)
	cellBits := [3]int{64, 64, 32}
	var cells [3]uint64
	cell, offset := 0, 0 // where the next byte lands
	for _, b := range data {
		v := uint64(b)
		cells[cell] ^= v << offset
		if over := offset + 8 - cellBits[cell]; over > 0 {
			cells[(cell+1)%3] ^= v >> (8 - over)
		}
		offset += shift
		for offset >= cellBits[cell] {
			offset -= cellBits[cell]
			cell = (cell + 1) % 3
		}
	}
	var register [width / 8]byte
	binary.LittleEndian.PutUint64(register[0:8], cells[0])
	binary.LittleEndian.PutUint64(register[8:16], cells[1])
	binary.LittleEndian.PutUint32(register[16:20], uint32(cells[2])) //nolint:gosec // G115: the last cell holds 32 bits
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(data)))
	for i, v := range length {
		register[len(register)-len(length)+i] ^= v
	}
	return base64.StdEncoding.EncodeToString(register[:])
}
