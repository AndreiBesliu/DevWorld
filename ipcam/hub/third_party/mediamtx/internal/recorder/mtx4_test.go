package recorder

// The test of patch MTX-4 (hub/third_party/PATCHES.md): an fMP4 segment is
// named after its start in UTC, so that a name stands for one instant also
// in the hour that repeats when daylight saving time ends.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMTX4_SegmentNamedInUTC: the name of a segment is the start of its
// first frame in UTC, whatever the zone of the clock that stamped that
// frame. On 2026-10-25, 03:30 in Romania comes twice, at 00:30 UTC (EEST)
// and at 01:30 UTC (EET): the two segments get two names.
func TestMTX4_SegmentNamedInUTC(t *testing.T) {
	for _, tc := range []struct {
		start time.Time
		name  string
	}{
		{time.Date(2026, 10, 25, 3, 30, 0, 0, time.FixedZone("EEST", 3*3600)), "2026-10-25_00-30-00-000000.mp4"},
		{time.Date(2026, 10, 25, 3, 30, 0, 0, time.FixedZone("EET", 2*3600)), "2026-10-25_01-30-00-000000.mp4"},
	} {
		r := startRecording(t, tc.start)
		r.writeFrames(oneSegmentFrames)
		r.waitFor("the segment-complete hook", func(l *durabilityLog) bool { return l.count("complete") == 1 })
		r.stop()
		require.Equal(t, []string{tc.name}, dirEntries(t, r.dir), "the segment of %v", tc.start)
	}
}
