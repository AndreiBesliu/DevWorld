package band

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

type vector struct {
	Note   string `json:"note"`
	Camera uint8  `json:"camera"`
	Frame  uint16 `json:"frame"`
	Loop   uint16 `json:"loop"`
	Row1   string `json:"row1"`
	Row2   string `json:"row2"`
}

type vectorFile struct {
	CRC struct {
		Check      string `json:"check"`
		CheckInput string `json:"checkInput"`
	} `json:"crc"`
	Vectors []vector `json:"vectors"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("vectors.json")
	if err != nil {
		t.Fatalf("read vectors.json: %v", err)
	}
	var f vectorFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse vectors.json: %v", err)
	}
	return f
}

func parseRow(t *testing.T, s string) [48]bool {
	t.Helper()
	if len(s) != 48 {
		t.Fatalf("row %q has %d characters, want 48", s, len(s))
	}
	var row [48]bool
	for i, c := range s {
		switch c {
		case '0':
		case '1':
			row[i] = true
		default:
			t.Fatalf("row %q has character %q at %d", s, c, i)
		}
	}
	return row
}

func invert(row [48]bool) [48]bool {
	for i := range row {
		row[i] = !row[i]
	}
	return row
}

func TestBand_Vectors(t *testing.T) {
	f := loadVectors(t)
	if len(f.Vectors) < 20 {
		t.Fatalf("vectors.json has %d vectors, want at least 20", len(f.Vectors))
	}

	var camera0, camera255, frame0, frame65535, loop0, loop65535 bool
	for _, v := range f.Vectors {
		camera0 = camera0 || v.Camera == 0
		camera255 = camera255 || v.Camera == 255
		frame0 = frame0 || v.Frame == 0
		frame65535 = frame65535 || v.Frame == 65535
		loop0 = loop0 || v.Loop == 0
		loop65535 = loop65535 || v.Loop == 65535
	}
	if !camera0 || !camera255 || !frame0 || !frame65535 || !loop0 || !loop65535 {
		t.Fatalf("vectors.json misses an edge: camera 0/255 %v/%v, frame 0/65535 %v/%v, loop 0/65535 %v/%v",
			camera0, camera255, frame0, frame65535, loop0, loop65535)
	}

	for _, v := range f.Vectors {
		t.Run(v.Note, func(t *testing.T) {
			code := Code{Camera: v.Camera, Frame: v.Frame, Loop: v.Loop}
			want1 := parseRow(t, v.Row1)
			want2 := parseRow(t, v.Row2)
			if want2 != invert(want1) {
				t.Fatalf("vector row2 %s is not the inverse of row1 %s", v.Row2, v.Row1)
			}
			if got := Encode(code); got != want1 {
				t.Errorf("Encode(%+v) = %s, want %s", code, bitString(got), v.Row1)
			}
			got, ok := Decode(want1, want2)
			if !ok || got != code {
				t.Errorf("Decode(%s, %s) = %+v, %v; want %+v, true", v.Row1, v.Row2, got, ok, code)
			}
		})
	}
}

func TestBand_CRCMatchesCatalogueCheckValue(t *testing.T) {
	f := loadVectors(t)
	want, err := strconv.ParseUint(strings.TrimPrefix(f.CRC.Check, "0x"), 16, 8)
	if err != nil {
		t.Fatalf("crc.check %q: %v", f.CRC.Check, err)
	}
	if got := crc8([]byte(f.CRC.CheckInput)); got != uint8(want) {
		t.Errorf("crc8(%q) = %#02x, want %#02x", f.CRC.CheckInput, got, want)
	}
}

func TestBand_RejectsCorruptedBit(t *testing.T) {
	for _, v := range loadVectors(t).Vectors {
		row1 := parseRow(t, v.Row1)
		row2 := parseRow(t, v.Row2)
		if _, ok := Decode(row1, row2); !ok {
			t.Fatalf("vector %q does not decode before corruption", v.Note)
		}
		for i := range row1 {
			bad1 := row1
			bad1[i] = !bad1[i]
			if _, ok := Decode(bad1, row2); ok {
				t.Errorf("%s: flipping row 1 bit %d still decodes", v.Note, i)
			}

			bad2 := row2
			bad2[i] = !bad2[i]
			if _, ok := Decode(row1, bad2); ok {
				t.Errorf("%s: flipping row 2 bit %d still decodes", v.Note, i)
			}

			// Both rows flipped at the same place keep them inverse, so only the
			// CRC can catch it.
			if _, ok := Decode(bad1, bad2); ok {
				t.Errorf("%s: flipping bit %d in both rows still decodes", v.Note, i)
			}
		}
	}
}

// readBlocks reads one row of the band back the way a player-side reader does:
// the mean luma of the central 50 % of each block, thresholded at 128.
func readBlocks(frame []byte, width, rowIndex, side int) [48]bool {
	var row [48]bool
	inset := side / 4
	for i := range row {
		sum, n := 0, 0
		for y := rowIndex*side + inset; y < (rowIndex+1)*side-inset; y++ {
			for x := i*side + inset; x < (i+1)*side-inset; x++ {
				sum += int(frame[y*width+x])
				n++
			}
		}
		row[i] = sum/n >= 128
	}
	return row
}

func TestPaintDecode_RoundTripAtBothSizes(t *testing.T) {
	sizes := []struct {
		name          string
		width, height int
		side          int
	}{
		{"1920x1080", 1920, 1080, 16},
		{"640x360", 640, 360, 8},
		{"3840x2160", 3840, 2160, 16},
	}
	vectors := loadVectors(t).Vectors

	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			// A busy background that is not a single level: a full-period
			// linear congruential sequence over the byte values.
			background := make([]byte, sz.width*sz.height*3/2)
			var level byte
			for i := range background {
				level = level*165 + 13
				background[i] = level
			}

			for _, v := range vectors {
				code := Code{Camera: v.Camera, Frame: v.Frame, Loop: v.Loop}
				frame := append([]byte(nil), background...)
				Paint(frame, sz.width, sz.height, code)

				row1 := readBlocks(frame, sz.width, 0, sz.side)
				row2 := readBlocks(frame, sz.width, 1, sz.side)
				if want := Encode(code); row1 != want {
					t.Errorf("%+v: row 1 reads %s, want %s", code, bitString(row1), bitString(want))
				}
				if want := invert(Encode(code)); row2 != want {
					t.Errorf("%+v: row 2 reads %s, want %s", code, bitString(row2), bitString(want))
				}
				got, ok := Decode(row1, row2)
				if !ok || got != code {
					t.Errorf("%+v: decoded %+v, %v", code, got, ok)
				}

				// Paint owns the band and nothing else: every byte outside the
				// two rows of 48 blocks, chroma planes included, is untouched.
				bandX := 48 * sz.side
				for y := 0; y < 2*sz.side; y++ {
					from, to := y*sz.width+bandX, (y+1)*sz.width
					if !bytes.Equal(frame[from:to], background[from:to]) {
						t.Fatalf("%+v: luma right of the band changed in line %d", code, y)
					}
				}
				rest := 2 * sz.side * sz.width
				if !bytes.Equal(frame[rest:], background[rest:]) {
					t.Fatalf("%+v: bytes below the band changed", code)
				}
			}
		})
	}
}

func TestPaint_BlockSideFollowsHeight(t *testing.T) {
	for _, tc := range []struct{ height, side int }{
		{360, 8}, {720, 8}, {1079, 8}, {1080, 16}, {2160, 16},
	} {
		if got := blockSide(tc.height); got != tc.side {
			t.Errorf("blockSide(%d) = %d, want %d", tc.height, got, tc.side)
		}
	}
}

func bitString(row [48]bool) string {
	var b strings.Builder
	for _, v := range row {
		if v {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}
