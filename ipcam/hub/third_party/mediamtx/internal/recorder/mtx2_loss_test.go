package recorder

// TestMTX2_WorstCaseLoss (R01, O17): the loss at a power cut that MTX-2's
// format allows, for 10 minutes of synthetic H.264-like samples written by
// the real part writer (formatFMP4Part, ageSegmentWriter and crypto.Writer)
// into a sink that only counts the encrypted bytes.

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	mcodecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/test"
)

const (
	lossFrameInterval = 50 * time.Millisecond // 20 fps
	lossPartFrames    = 20                    // 1 s parts
	lossSegmentParts  = 300                   // 5 min segments
	lossSegments      = 2                     // 10 min
	lossCuts          = 10_000

	// maxAnnotationLength is the most bytes of a notice line after
	// annotate.sh's escaping (C32).
	maxAnnotationLength = 3500
)

// lossRun is the result of one stream: its bitrate and padding setting, the
// median and maximum loss over the cuts, and the disk overhead of the
// encrypted segments over the clear fMP4 of the same parts.
type lossRun struct {
	Kbps        int     `json:"kbps"`
	Pad         bool    `json:"pad"`
	MedianMs    int64   `json:"medianMs"`
	MaxMs       int64   `json:"maxMs"`
	OverheadPct float64 `json:"overheadPct"` // rounded to 0.1 %

	overhead float64 // the encrypted bytes over the clear ones, minus 1
}

// lossResult is the "mtx2-loss" notice of the recorder job (C32).
type lossResult struct {
	Code string    `json:"code"`
	Cuts int       `json:"cuts"`
	Runs []lossRun `json:"runs"`
	Pass bool      `json:"pass"`
}

// countingSink stands for a segment file: it counts what is written to it.
type countingSink struct{ n int64 }

func (c *countingSink) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// lossFsync is a part's fsync: its wall time from the start of the stream,
// and the plaintext bytes of its segment that were sealed by then.
type lossFsync struct {
	at        time.Duration
	segment   int
	plaintext int64
}

// lossSample is a sample of a segment: its PTS and where its bytes end in the
// segment's plaintext.
type lossSample struct {
	pts time.Duration
	end int64
}

// lossStream is what the part writer did with one stream.
type lossStream struct {
	fsyncs  []lossFsync
	samples [][]lossSample // per segment, in order
	// cipher is the size of the encrypted segments, clear that of their init
	// and parts without padding: a clear recording of the same samples.
	cipher, clear int64
}

// writeLossStream writes 10 minutes of samples through the MTX-2 part writer.
// Part sizes are uniform within ± 30 % of the bitrate, split evenly over the
// part's 20 frames. The fsyncs follow MediaMTX's close rule: a part is closed,
// padded and fsynced when the frame after the next part's first frame
// arrives, one frame interval after its end; a segment's last part when the
// next segment's keyframe arrives, at its end, which also seals the final
// chunk.
func writeLossStream(t *testing.T, bps int, pad bool, scope crypto.KeyScope) lossStream {
	rng := rand.New(rand.NewPCG(uint64(bps), 0)) // the same parts with and without padding
	track := &formatFMP4Track{
		id:        1,
		clockRate: 90000,
		codec:     &mcodecs.H264{SPS: test.FormatH264.SPS, PPS: test.FormatH264.PPS},
	}
	track.initialize()
	payload := make([]byte, bps/8*13/10/lossPartFrames+lossPartFrames)

	var s lossStream
	for seg := range lossSegments {
		var file countingSink
		w, err := newAgeSegmentWriter(&file, scope, pad)
		require.NoError(t, err)
		header := file.n // the age header and the payload nonce
		segmentStart := time.Duration(seg*lossSegmentParts) * time.Second
		require.NoError(t, writeInit(w, uuid.New(), uint64(seg), segmentStart, time.Time{}, []*formatFMP4Track{track}))
		s.clear += w.size()

		var samples []lossSample
		for k := range lossSegmentParts {
			partStart := segmentStart + time.Duration(k)*time.Second
			part := &formatFMP4Part{
				maxPartSize:     50 * 1024 * 1024,
				segmentStartDTS: segmentStart,
				number:          uint32(k),
				startDTS:        partStart,
			}
			part.initialize()

			total := int(float64(bps/8) * (0.7 + 0.6*rng.Float64()))
			sizes := make([]int, lossPartFrames)
			for j := range sizes {
				sizes[j] = total / lossPartFrames
			}
			sizes[lossPartFrames-1] += total - total/lossPartFrames*lossPartFrames
			for j, size := range sizes {
				dts := partStart + time.Duration(j)*lossFrameInterval
				require.NoError(t, part.write(track, &formatFMP4Sample{Sample: &fmp4.Sample{
					Duration:        uint32(lossFrameInterval * 90000 / time.Second),
					IsNonSyncSample: j != 0,
					Payload:         payload[:size],
				}}, dts))
			}

			// The same calls as formatFMP4Segment.closeCurPart.
			start := w.size()
			require.NoError(t, part.close(w))
			end := w.size()
			require.NoError(t, w.endPart(start))
			s.clear += end - start

			// The mdat ends the part and holds the samples in order.
			ends := make([]int64, len(sizes))
			for j, off := len(sizes)-1, end; j >= 0; j-- {
				ends[j] = off
				off -= int64(sizes[j])
			}
			for j := range sizes {
				samples = append(samples, lossSample{pts: partStart + time.Duration(j)*lossFrameInterval, end: ends[j]})
			}

			if k == lossSegmentParts-1 {
				require.NoError(t, w.close())
				s.fsyncs = append(s.fsyncs, lossFsync{at: partStart + time.Second, segment: seg, plaintext: w.size()})
				continue
			}
			sealed := file.n - header
			require.Zero(t, sealed%(crypto.ChunkSize+16), "a partial chunk reached the file at part %d", k)
			s.fsyncs = append(s.fsyncs, lossFsync{
				at:        partStart + time.Second + lossFrameInterval,
				segment:   seg,
				plaintext: sealed / (crypto.ChunkSize + 16) * crypto.ChunkSize,
			})
		}
		s.samples = append(s.samples, samples)
		s.cipher += file.n
	}
	return s
}

// lastDurablePTS is the PTS of the last sample wholly inside the plaintext
// that the fsyncs up to and with the last one in fsyncs made durable, or 0,
// the start of the stream, when there is none. All the whole samples count
// (R01's salvage: under C1-R9, one whose changes lie before the last kept
// chunk is a rewrite under a fresh file key).
func (s lossStream) lastDurablePTS(fsyncs []lossFsync) time.Duration {
	for i := len(fsyncs) - 1; i >= 0; i-- {
		f := fsyncs[i]
		samples := s.samples[f.segment]
		n := sort.Search(len(samples), func(j int) bool { return samples[j].end > f.plaintext })
		if n > 0 {
			return samples[n-1].pts
		}
		// Nothing of this segment is durable yet: fall back to the fsyncs of
		// the segment before it.
		for i > 0 && fsyncs[i-1].segment == f.segment {
			i--
		}
	}
	return 0
}

// losses returns, for each cut, the cut's time minus lastDurablePTS at the
// last fsync before it.
func (s lossStream) losses(cuts []time.Duration) []time.Duration {
	out := make([]time.Duration, len(cuts))
	for i, cut := range cuts {
		n := sort.Search(len(s.fsyncs), func(j int) bool { return s.fsyncs[j].at > cut })
		out[i] = cut - s.lastDurablePTS(s.fsyncs[:n])
	}
	return out
}

// unpaddedLimitMs is §4.5's bound for an encrypted stream without the
// low-bitrate padding (O17): 1 part plus 64 KiB ÷ bitrate, rounded up to
// whole parts of 1 s.
func unpaddedLimitMs(bps int) int64 {
	bits := int64(crypto.ChunkSize) * 8
	return (1 + (bits+int64(bps)-1)/int64(bps)) * 1000
}

// TestMTX2_WorstCaseLoss draws 10,000 uniform cut times over 10 minutes of a
// 1 s-part, 20 fps stream at each bitrate, with the low-bitrate padding on and
// off, and measures the loss: the cut minus the PTS of the last sample wholly
// inside the plaintext made durable by the last part fsync before the cut.
// With the padding on, the median is at most 1 s and the maximum at most 2 s
// at 4 Mbps, 450 kbps and 256 kbps (HUB-07), and the disk overhead at 4 Mbps
// is at most 10 %; with it off, the maximum at 256 kbps is within §4.5's
// unpadded bound. The 512 and 384 kbps figures are information. It prints the
// figures as one "mtx2-loss: <json>" line, which the recorder job turns into
// a notice.
func TestMTX2_WorstCaseLoss(t *testing.T) {
	scope := newAgeKeys(t).scope
	rng := rand.New(rand.NewPCG(2026, 10))
	end := time.Duration(lossSegments*lossSegmentParts) * time.Second
	cuts := make([]time.Duration, lossCuts)
	for i := range cuts {
		cuts[i] = time.Duration(rng.Int64N(int64(end)))
	}

	result := lossResult{Code: "mtx2-loss", Cuts: lossCuts}
	for _, kbps := range []int{4000, 512, 450, 384, 256} {
		for _, pad := range []bool{true, false} {
			s := writeLossStream(t, kbps*1000, pad, scope)
			losses := s.losses(cuts)
			slices.Sort(losses)
			median := (losses[len(losses)/2-1] + losses[len(losses)/2]) / 2
			overhead := float64(s.cipher)/float64(s.clear) - 1
			result.Runs = append(result.Runs, lossRun{
				Kbps:        kbps,
				Pad:         pad,
				MedianMs:    median.Milliseconds(),
				MaxMs:       losses[len(losses)-1].Milliseconds(),
				OverheadPct: math.Round(overhead*1000) / 10,
				overhead:    overhead,
			})
		}
	}

	// assert, not require: the line below is printed whatever the outcome.
	assert.Equal(t, int64(4000), unpaddedLimitMs(256_000), "§4.5's unpadded bound at 256 kbps")
	for _, run := range result.Runs {
		name := fmt.Sprintf("%d kbps, padding %v", run.Kbps, map[bool]string{true: "on", false: "off"}[run.Pad])
		switch {
		case run.Pad && (run.Kbps == 4000 || run.Kbps == 450 || run.Kbps == 256):
			assert.LessOrEqual(t, run.MedianMs, int64(1000), "%s: median", name)
			assert.LessOrEqual(t, run.MaxMs, int64(2000), "%s: maximum", name)
			if run.Kbps == 4000 {
				assert.LessOrEqual(t, run.overhead, 0.10, "%s: disk overhead", name)
			}
		case !run.Pad && run.Kbps == 256:
			assert.LessOrEqual(t, run.MaxMs, unpaddedLimitMs(run.Kbps*1000), "%s: maximum", name)
		}
	}

	// The length is checked on the line with "pass":false, the longer one.
	if n := annotationLength(lossLine(t, result)); n > maxAnnotationLength {
		t.Errorf("the mtx2-loss line is %d bytes once escaped, over %d", n, maxAnnotationLength)
	}
	result.Pass = !t.Failed()
	fmt.Println(lossLine(t, result))
}

// lossLine is the line that the recorder job's mtx2-loss step turns into a
// notice.
func lossLine(t *testing.T, result lossResult) string {
	t.Helper()
	b, err := json.Marshal(result)
	require.NoError(t, err)
	return "mtx2-loss: " + string(b)
}

// annotationLength is the length of line once annotate.sh has escaped it:
// each "%" becomes "%25" (a single line has no line break to escape).
func annotationLength(line string) int {
	return len(line) + 2*strings.Count(line, "%")
}
