package recorder

import (
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	hubrecorder "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

// TestMTX2_SmallPartMatchesRecorder: the part size below which MTX-2 pads an
// encrypted part to the chunk boundary when the camera's O17 setting is on
// (§4.5) is the hub's recorder.SmallPartBytes and the age chunk size, so the
// hub, the spec and the fork cannot drift apart.
func TestMTX2_SmallPartMatchesRecorder(t *testing.T) {
	if smallPartBytes != hubrecorder.SmallPartBytes {
		t.Errorf("MTX-2's small part is %d bytes, the hub's recorder.SmallPartBytes %d", smallPartBytes, hubrecorder.SmallPartBytes)
	}
	if smallPartBytes != crypto.ChunkSize {
		t.Errorf("MTX-2's small part is %d bytes, an age chunk (crypto.ChunkSize) %d", smallPartBytes, crypto.ChunkSize)
	}
}
