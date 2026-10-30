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

// TestKeyFor_SafeSegments (§9.10): every segment of a key is a name that
// Windows and SMB accept and that walks nowhere: one left empty or made of
// dots only is "_", a reserved device name, with or without an extension and
// in any case, gets "_" after its base, and trailing dots and spaces go.
func TestKeyFor_SafeSegments(t *testing.T) {
	start := time.Date(2026, 10, 17, 8, 15, 0, 0, time.UTC).UnixMilli()
	const tail = "/2026-10-17/20261017T081500Z_cont_main.mp4"
	for _, tc := range []struct {
		hubID, hubName, cam string
		encrypted           bool
		want                storage.ObjectKey
	}{
		{"H", `<>:"|?*`, "cam01", false, "_/cam01" + tail},
		{"H", "", "cam01", false, "_/cam01" + tail},
		{"H", "hub", "..", false, "hub/_" + tail},
		{"H", "hub", ".", false, "hub/_" + tail},
		{"...", "hub", "cam01", true, "e/_/cam01" + tail + ".age"},
		{"H", "hub", "cam01. .", false, "hub/cam01" + tail},
		{"H", "hub ", "cam01 ", false, "hub/cam01" + tail},
		{"H", "CON", "cam01", false, "CON_/cam01" + tail},
		{"H", "hub", "prn", false, "hub/prn_" + tail},
		{"aux", "hub", "cam01", true, "e/aux_/cam01" + tail + ".age"},
		{"H", "Nul.box", "cam01", false, "Nul_.box/cam01" + tail},
		{"H", "hub", "COM1", false, "hub/COM1_" + tail},
		{"H", "hub", "com9.cam", false, "hub/com9_.cam" + tail},
		{"H", "hub", "LPT1", false, "hub/LPT1_" + tail},
		{"H", "hub", "lpt9.", false, "hub/lpt9_" + tail},
		{"H", "hub", "nul. ", false, "hub/nul_" + tail},
		{"H", "hub", "COM10", false, "hub/COM10" + tail},
		{"H", "CONSOLE", "lpt", false, "CONSOLE/lpt" + tail},
	} {
		s := catalog.Segment{Cam: tc.cam, Profile: "main", StartMs: start, Encrypted: tc.encrypted}
		if got := KeyFor(tc.hubID, tc.hubName, s, catalog.ClassContinuous); got != tc.want {
			t.Errorf("KeyFor(%q, %q, camera %q) = %q, want %q", tc.hubID, tc.hubName, tc.cam, got, tc.want)
		}
	}
}
