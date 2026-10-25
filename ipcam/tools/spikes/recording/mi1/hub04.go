// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// The needles of HUB-04's scan (G08) besides the media's: an fMP4 file's
// ftyp with MediaMTX's brand, and a moof whose first child is its mfhd.
var (
	ftypNeedle = []byte("ftypmp42")
	moofNeedle = []byte("moof")
	mfhdNeedle = []byte("mfhd")
)

const (
	// mediaNeedle is how much of each SPS, PPS and IDR NAL unit the scan
	// looks for (C1-R10).
	mediaNeedle = 16
	// minNeedle is the shortest needle: one of 4 bytes would match about one
	// position in 2^32 of random ciphertext, a gigabyte of recordings by
	// chance, so a NAL unit shorter than 8 bytes, as x264's PPS, is left out.
	minNeedle = 8
	// scanChunk is how much of a file the scan reads at once.
	scanChunk = 4 << 20
)

// The H.264 NAL unit types whose first bytes are needles.
const (
	nalIDR = 5
	nalSPS = 7
	nalPPS = 8
)

// MediaNeedles is the first 16 bytes of each SPS, PPS and IDR NAL unit in
// the Annex B files, each once, without those under 8 bytes.
func MediaNeedles(files ...string) ([][]byte, error) {
	var out [][]byte
	for _, f := range files {
		aus, err := source.ReadAnnexB(f)
		if err != nil {
			return nil, err
		}
		for _, au := range aus {
			for _, n := range au.NALUs {
				if len(n) < minNeedle {
					continue
				}
				switch n[0] & 0x1f {
				case nalIDR, nalSPS, nalPPS:
					needle := bytes.Clone(n[:min(len(n), mediaNeedle)])
					if !slices.ContainsFunc(out, func(x []byte) bool { return bytes.Equal(x, needle) }) {
						out = append(out, needle)
					}
				}
			}
		}
	}
	return out, nil
}

// Finding is one needle that HUB-04's scan found: the file, relative to the
// scanned root, the offset, and what: "ftyp", "moof/mfhd" or "media".
type Finding struct {
	Path   string
	Offset int64
	What   string
}

// ScanHub04 is HUB-04's scan of a run's data directory root (G08): every
// file under it, except under the folders skip (relative to root: the
// clear streams' rec/<cam>/<profile>), searched for the structural needles
// and for needles, the media's. An encrypted stream's files, tmp/ and the
// run directory hold no plaintext video: each finding is clear video on
// disk. It returns the first finding of each file.
func ScanHub04(root string, skip []string, needles [][]byte) ([]Finding, error) {
	index := &needleIndex{byPrefix: map[uint64][][]byte{}}
	longest := len(moofNeedle) + 4 + len(mfhdNeedle)
	for _, n := range append([][]byte{ftypNeedle}, needles...) {
		if len(n) < minNeedle {
			continue
		}
		k := binary.LittleEndian.Uint64(n)
		index.byPrefix[k] = append(index.byPrefix[k], n)
		first := binary.LittleEndian.Uint16(n)
		index.first2[first/64] |= 1 << (first % 64)
		longest = max(longest, len(n))
	}
	var found []Finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(skip, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, ok, err := scanFile(path, index, longest)
		if err != nil {
			return err
		}
		if ok {
			f.Path = rel
			found = append(found, f)
		}
		return nil
	})
	return found, err
}

// needleIndex finds the needles fast: a bit per value of their first two
// bytes rules out almost every position at the cost of one load, and the
// rest are looked up by their first 8 bytes.
type needleIndex struct {
	first2   [1 << 16 / 64]uint64
	byPrefix map[uint64][][]byte
}

// scanFile looks for the needles of index and for a moof with its mfhd in
// the file at path, reading it in chunks that overlap by the longest
// needle.
func scanFile(path string, index *needleIndex, longest int) (Finding, bool, error) {
	f, err := os.Open(path) //nolint:gosec // a file of the run's data directory
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Finding{}, false, nil // removed since the walk
		}
		return Finding{}, false, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, scanChunk+longest)
	var base int64 // the file offset of buf[0]
	kept := 0      // bytes at the start of buf from the previous chunk
	for {
		n, err := io.ReadFull(f, buf[kept:])
		end := kept + n
		if at, what, ok := findNeedle(buf[:end], index); ok {
			return Finding{Offset: base + int64(at), What: what}, true, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return Finding{}, false, nil
			}
			return Finding{}, false, err
		}
		keep := min(longest-1, end)
		copy(buf, buf[end-keep:end])
		base += int64(end - keep)
		kept = keep
	}
}

// findNeedle returns the first needle in b.
func findNeedle(b []byte, index *needleIndex) (at int, what string, ok bool) {
	for i := 0; i+minNeedle <= len(b); i++ {
		if first := binary.LittleEndian.Uint16(b[i:]); index.first2[first/64]&(1<<(first%64)) == 0 {
			continue
		}
		if candidates := index.byPrefix[binary.LittleEndian.Uint64(b[i:])]; candidates != nil {
			for _, n := range candidates {
				if bytes.HasPrefix(b[i:], n) {
					if bytes.Equal(n, ftypNeedle) {
						return i, "ftyp", true
					}
					return i, "media", true
				}
			}
		}
	}
	for from := 0; ; {
		j := bytes.Index(b[from:], mfhdNeedle)
		if j < 0 {
			return 0, "", false
		}
		if i := from + j - 8; i >= 0 && bytes.Equal(b[i:i+4], moofNeedle) {
			return i, "moof/mfhd", true
		}
		from += j + 1
	}
}
