// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"bytes"
	"encoding/binary"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// SEIUUID marks the controller's SEI: a user_data_unregistered message (H.264
// payload type 5) with this UUID and the access unit's index (C15). MediaMTX
// keeps SEI NAL units when it records, so the index of the last recorded
// sample says exactly which frame was the last to reach the disk.
var SEIUUID = [16]byte{0x6b, 0x1c, 0x0e, 0x52, 0x9a, 0x3f, 0x4d, 0x27, 0x8e, 0x15, 0xc4, 0x70, 0x2b, 0x9d, 0x61, 0xa8}

const (
	seiPayloadUserDataUnregistered = 5
	seiIndexPayloadSize            = len(SEIUUID) + 8
	rbspStopBit                    = 0x80
)

// StampIndex returns the access unit with an SEI NAL unit in front that holds
// index: SEIUUID, then the index as 8 big-endian bytes, with emulation
// prevention applied. The NAL units of nalus are shared, not copied, and
// nalus itself is left as it is.
func StampIndex(nalus [][]byte, index uint64) [][]byte {
	rbsp := make([]byte, 0, 2+seiIndexPayloadSize+1)
	rbsp = append(rbsp, seiPayloadUserDataUnregistered, byte(seiIndexPayloadSize))
	rbsp = append(rbsp, SEIUUID[:]...)
	rbsp = binary.BigEndian.AppendUint64(rbsp, index)
	rbsp = append(rbsp, rbspStopBit)

	sei := append([]byte{byte(h264.NALUTypeSEI)}, emulationPrevent(rbsp)...)
	return append([][]byte{sei}, nalus...)
}

// ReadIndex returns the index that StampIndex put into the access unit;
// ok is false when no SEI of the access unit holds one.
func ReadIndex(nalus [][]byte) (index uint64, ok bool) {
	for _, nalu := range nalus {
		if len(nalu) < 2 || h264.NALUType(nalu[0]&0x1f) != h264.NALUTypeSEI {
			continue
		}
		if index, ok := indexInSEI(h264.EmulationPreventionRemove(nalu[1:])); ok {
			return index, true
		}
	}
	return 0, false
}

// indexInSEI walks the sei_message()s of an SEI's RBSP.
func indexInSEI(rbsp []byte) (uint64, bool) {
	pos := 0
	// The last byte is the rbsp_stop_one_bit and its alignment zeros.
	for pos < len(rbsp)-1 {
		payloadType, n, ok := seiValue(rbsp, pos)
		if !ok {
			return 0, false
		}
		payloadSize, n, ok := seiValue(rbsp, n)
		if !ok || len(rbsp)-n < payloadSize {
			return 0, false
		}
		payload := rbsp[n : n+payloadSize]
		if payloadType == seiPayloadUserDataUnregistered && payloadSize == seiIndexPayloadSize &&
			bytes.Equal(payload[:len(SEIUUID)], SEIUUID[:]) {
			return binary.BigEndian.Uint64(payload[len(SEIUUID):]), true
		}
		pos = n + payloadSize
	}
	return 0, false
}

// seiValue reads a payload type or size: 0xff bytes that each add 255, then
// a last byte. It returns the value and the position after it.
func seiValue(rbsp []byte, pos int) (value, next int, ok bool) {
	for pos < len(rbsp) {
		b := rbsp[pos]
		pos++
		value += int(b)
		if b != 0xff {
			return value, pos, true
		}
	}
	return 0, 0, false
}

// emulationPrevent inserts 0x03 after every two zero bytes that are followed
// by a byte of 0x03 or less, so that the NAL unit never holds a start code.
func emulationPrevent(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+len(rbsp)/2)
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}
