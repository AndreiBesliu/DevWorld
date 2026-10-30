package outbox

import (
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestKeyFor_Deterministic (§9.10): an encrypted copy's key holds only
// opaque IDs; a clear one's holds readable names, cleaned of the characters
// that Windows and SMB refuse; both are UTC from the segment's start.
func TestKeyFor_Deterministic(t *testing.T) {
	start := time.Date(2026, 10, 17, 8, 15, 0, 0, time.UTC).UnixMilli()
	for _, tc := range []struct {
		hubID, hubName string
		seg            catalog.Segment
		class          string
		want           storage.ObjectKey
	}{
		{"H", "hub", catalog.Segment{Cam: "cam01", Profile: "main", StartMs: start, Encrypted: true}, catalog.ClassContinuous,
			"e/H/cam01/2026-10-17/20261017T081500Z_cont_main.mp4.age"},
		{"H", "hub", catalog.Segment{Cam: "cam01", Profile: "main", StartMs: start}, catalog.ClassEventClip,
			"hub/cam01/2026-10-17/20261017T081500Z_eveniment_main.mp4"},
		// 23:59:59.999 in Bucharest's summer time is still 20:59:59Z.
		{"H", "hub", catalog.Segment{Cam: "cam01", Profile: "sub", StartMs: time.Date(2026, 10, 17, 23, 59, 59, 999e6, time.FixedZone("EEST", 3*3600)).UnixMilli()},
			catalog.ClassContinuous, "hub/cam01/2026-10-17/20261017T205959Z_cont_sub.mp4"},
		{"H", `Hub <a>:"b"/c\d|e?f*` + "\x01", catalog.Segment{Cam: "cam/01:", Profile: "ma?in", StartMs: start}, catalog.ClassContinuous,
			"Hub abcdef/cam01/2026-10-17/20261017T081500Z_cont_main.mp4"},
	} {
		if got := KeyFor(tc.hubID, tc.hubName, tc.seg, tc.class); got != tc.want {
			t.Errorf("KeyFor(%q, %q, %+v, %s) = %q, want %q", tc.hubID, tc.hubName, tc.seg, tc.class, got, tc.want)
		}
		if again := KeyFor(tc.hubID, tc.hubName, tc.seg, tc.class); again != tc.want {
			t.Errorf("a second KeyFor gave %q", again)
		}
	}
}
