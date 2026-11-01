package recorder

import (
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

func TestNewJob_Defaults(t *testing.T) {
	j := NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", nil)
	if j.SegmentSecs != 300 || j.PartSecs != 1 || !j.PadLowBitrate {
		t.Errorf("NewJob = %d s segments, %d s parts, PadLowBitrate %v; want 300, 1, true", j.SegmentSecs, j.PartSecs, j.PadLowBitrate)
	}
	if j.Cam != "cam1" || j.Profile != "main" || j.RestreamURL != "rtsp://127.0.0.1:8554/cam1/main" || j.Scope != nil {
		t.Errorf("NewJob = %+v", j)
	}
}

// TestSmallPartBytes_IsTheAgeChunk: §4.5 (O17) calls a part small when it is
// shorter than one age chunk.
func TestSmallPartBytes_IsTheAgeChunk(t *testing.T) {
	if SmallPartBytes != crypto.ChunkSize {
		t.Errorf("SmallPartBytes = %d, crypto.ChunkSize = %d", SmallPartBytes, crypto.ChunkSize)
	}
}
