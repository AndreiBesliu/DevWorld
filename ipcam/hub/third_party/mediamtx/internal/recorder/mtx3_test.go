package recorder

// The recorder test of patch MTX-3 (hub/third_party/PATCHES.md): the
// segment-complete hook fires once per segment, after MTX-1's rename and
// directory sync, with the final path and the segment's start. The test of the
// command's environment is internal/core's TestMTX3_HookEnvironment.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMTX3_HookFiresAfterRenameWithFinalPath: for a segment closed by the
// next segment's keyframe and for one closed when the recorder stops, the
// recorded order is rename, directory sync, hook, and the hook receives the
// final path and a start equal to the wall time of the segment's first frame
// (± 1 ms).
func TestMTX3_HookFiresAfterRenameWithFinalPath(t *testing.T) {
	firstNTP := time.Now()
	r := startRecording(t, firstNTP)

	// Frames 0 s … 4.5 s: the frame at 3 s closes the first segment, the one at
	// 4 s closes the first part of the second, which then closes when the
	// recorder stops.
	r.writeFrames(91)
	r.waitFor("the first part of the second segment", func(l *durabilityLog) bool {
		return l.count("complete") == 1 && l.count("syncFile") >= 5
	})
	r.stop()

	events := r.log.snapshot()
	require.Equal(t, 2, r.log.count("complete"), "hooks; events: %v", eventOps(events))

	segmentStarts := []time.Time{firstNTP, firstNTP.Add(testSegmentDuration)}
	var finalNames []string

	for at, hook := range events {
		if hook.op != "complete" {
			continue
		}
		i := len(finalNames)

		// Rename, directory sync, hook.
		require.GreaterOrEqual(t, at, 2, "events: %v", eventOps(events))
		require.Equal(t, []string{"rename", "syncDir", "complete"}, eventOps(events[at-2:at+1]),
			"segment %d; events: %v", i+1, eventOps(events))
		ren, dirSync := events[at-2], events[at-1]

		// The final path: the rename's target, in the synced directory, without .part.
		require.Equal(t, ren.path, hook.path, "segment %d: path given to the hook", i+1)
		require.Equal(t, hook.path+".part", ren.from, "segment %d: renamed file", i+1)
		require.Equal(t, filepath.Dir(hook.path), dirSync.path, "segment %d: synced directory", i+1)
		require.True(t, strings.HasSuffix(hook.path, ".mp4"), "segment %d: %s", i+1, hook.path)
		_, err := os.Stat(hook.path)
		require.NoError(t, err, "segment %d", i+1)

		require.WithinDuration(t, segmentStarts[i], hook.start, time.Millisecond, "segment %d: start", i+1)
		finalNames = append(finalNames, filepath.Base(hook.path))
	}

	require.Equal(t, finalNames, dirEntries(t, r.dir), "directory after the recorder stopped")
}
