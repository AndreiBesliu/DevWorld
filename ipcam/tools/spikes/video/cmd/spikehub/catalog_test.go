package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// wantSources is the sources map of a hub with the given number of simulated
// cameras: "<camera id>/<profile>" (R2) to the camera number that the sent
// records carry (R1).
func wantSources(sims int) map[string]uint8 {
	want := map[string]uint8{
		"mx-h265/main": 17, "mx-h265/sub": 17,
		"mx-high51/main": 18, "mx-high51/sub": 18,
	}
	for n := 1; n <= sims; n++ {
		id := fmt.Sprintf("sim-%02d", n)
		want[id+"/sub"], want[id+"/main"] = uint8(n), uint8(n) //nolint:gosec // sims is at most 16
	}
	return want
}

func newTestCatalog(t *testing.T, sims int) (*catalog, *fakeClock) {
	t.Helper()
	m, err := loadMedia(writeMedia(t))
	if err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock()
	return newCatalog(m, sims, clock), clock
}

func TestCatalog_KeysSourcesByCameraAndProfile(t *testing.T) {
	cat, _ := newTestCatalog(t, 16)

	var got []string
	for key := range cat.sources {
		got = append(got, key)
	}
	var want []string
	for key := range wantSources(16) {
		want = append(want, key)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
	for key, src := range cat.sources {
		if id, _, _ := strings.Cut(key, "/"); src.ID() != id {
			t.Errorf("source %s has ID %q, want %q", key, src.ID(), id)
		}
	}
}

// Every simulator comes from one helper that names its stream profile (R28), so
// that the sent records carry it.
func TestCatalog_EverySimNamesItsStreamAndCamera(t *testing.T) {
	cat, clock := newTestCatalog(t, 16)
	clock.tickAll(t)

	for key, camera := range wantSources(16) {
		rec := waitFor(t, cat.sources[key].SentLog())
		_, profile, _ := strings.Cut(key, "/")
		if rec.Profile == "" {
			t.Errorf("%s: the stream profile of its sent records is empty", key)
		}
		if rec.Profile != profile {
			t.Errorf("%s: stream profile %q, want %q", key, rec.Profile, profile)
		}
		if rec.Camera != camera {
			t.Errorf("%s: camera %d in its sent records, want %d", key, rec.Camera, camera)
		}
	}
}

func TestCatalog_GivesEachSimOfAStreamItsOwnPhase(t *testing.T) {
	cat, clock := newTestCatalog(t, 16)
	clock.tickAll(t)

	// The sub file serves 18 cameras (16 sims and the two codec matrix cameras),
	// the main file the 16 sims; the codec matrix cameras have files of their own.
	groups := map[string]map[uint16]string{"sub": {}, "main": {}}
	for key := range wantSources(16) {
		id, profile, _ := strings.Cut(key, "/")
		if profile == "main" && strings.HasPrefix(id, "mx-") {
			continue
		}
		frame := waitFor(t, cat.sources[key].SentLog()).Frame
		if other, dup := groups[profile][frame]; dup {
			t.Errorf("%s and %s both start at frame %d of the %s file", key, other, frame, profile)
		}
		groups[profile][frame] = key
	}
	if len(groups["sub"]) != 18 || len(groups["main"]) != 16 {
		t.Errorf("phases per file = %d sub, %d main; want 18 and 16", len(groups["sub"]), len(groups["main"]))
	}
}

func TestCatalog_CodecMatrixCamerasKeepTheStandardSubstream(t *testing.T) {
	cat, _ := newTestCatalog(t, 1)

	tests := []struct {
		key     string
		codec   source.Codec
		profile string
		fps     int
	}{
		{"sim-01/sub", source.CodecH264, "4d401f", 12},
		{"sim-01/main", source.CodecH264, "4d4028", 20},
		{"mx-h265/main", source.CodecH265, "017b", 20},
		{"mx-h265/sub", source.CodecH264, "4d401f", 12}, // the G9 fallback stays on the same camera
		{"mx-high51/main", source.CodecH264, "640033", 20},
		{"mx-high51/sub", source.CodecH264, "4d401f", 12},
	}
	for _, tc := range tests {
		src := cat.sources[tc.key]
		if src == nil {
			t.Errorf("%s: no such source", tc.key)
			continue
		}
		if src.Codec() != tc.codec || src.Profile() != tc.profile || src.FPS() != tc.fps {
			t.Errorf("%s = %s %s at %d fps, want %s %s at %d fps",
				tc.key, src.Codec(), src.Profile(), src.FPS(), tc.codec, tc.profile, tc.fps)
		}
	}
}

func TestCatalog_ListsRealCamerasAfterTheMatrixCameras(t *testing.T) {
	cat, _ := newTestCatalog(t, 2)
	realSim := source.NewSim("real-1", 99, oneAU(), 20, 0, newFakeClock())
	cat.addReal(realSim)

	last := cat.cameras[len(cat.cameras)-1]
	if last.ID != "real-1" || last.Camera != 19 || last.Codec != "H264" || !slices.Equal(last.Profiles, []string{"main", "sub"}) {
		t.Errorf("last camera = %+v, want real-1, camera 19, H264, profiles main and sub", last)
	}
	if cat.sources["real-1/main"] != realSim || cat.sources["real-1/sub"] != realSim {
		t.Error("real-1 is not served as main and sub")
	}
	if got := cat.cameras[len(cat.cameras)-2].ID; got != "mx-high51" {
		t.Errorf("camera before real-1 = %q, want mx-high51", got)
	}
}

// oneAU returns a few access units for a source that the test registers as a real camera.
func oneAU() []source.AccessUnit {
	return []source.AccessUnit{{NALUs: [][]byte{{0x67, 0x4d, 0x40, 0x1f, 0xac}, {0x68, 0xee}, {0x65, 0x88, 0x80}}, IDR: true}}
}

func TestLoadMedia_NamesTheMissingFile(t *testing.T) {
	dir := writeMedia(t)
	if err := os.Remove(filepath.Join(dir, "main-high51.h264")); err != nil {
		t.Fatal(err)
	}
	_, err := loadMedia(dir)
	if err == nil || !strings.Contains(err.Error(), "main-high51.h264") {
		t.Fatalf("loadMedia without main-high51.h264: error %v, want one that names the file", err)
	}
}

func TestLoadMedia_RequiresTheRecording(t *testing.T) {
	dir := writeMedia(t)
	if err := os.Remove(filepath.Join(dir, "hls", "index.m3u8")); err != nil {
		t.Fatal(err)
	}
	_, err := loadMedia(dir)
	if err == nil || !strings.Contains(err.Error(), "index.m3u8") {
		t.Fatalf("loadMedia without the recording: error %v, want one that names the playlist", err)
	}
}

// The files that cmd/mediagen generated are what the hub's frame rates and
// camera profiles describe (R19: the CI media job runs this).
func TestMedia_MatchesTheManifest(t *testing.T) {
	dir := mediaDir(t)
	m, err := loadMedia(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json")) //nolint:gosec // the media directory that the test run chose
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Streams []struct {
			Name   string `json:"name"`
			File   string `json:"file"`
			Codec  string `json:"codec"`
			FPS    int    `json:"fps"`
			Frames int    `json:"frames"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	byName := map[string]stream{"sub": m.sub, "main": m.main, "main-high51": m.high51, "main-h265": m.h265}
	if len(manifest.Streams) != len(byName) {
		t.Fatalf("manifest has %d streams, want %d", len(manifest.Streams), len(byName))
	}
	for _, e := range manifest.Streams {
		s, ok := byName[e.Name]
		if !ok {
			t.Errorf("manifest stream %q is unknown to the hub", e.Name)
			continue
		}
		if s.file != e.File || s.fps != e.FPS || len(s.aus) != e.Frames {
			t.Errorf("%s: hub has file %s, %d fps, %d frames; manifest has %s, %d fps, %d frames",
				e.Name, s.file, s.fps, len(s.aus), e.File, e.FPS, e.Frames)
		}
	}

	cat := newCatalog(m, 16, newFakeClock())
	for key, want := range map[string]struct {
		codec   source.Codec
		profile string
	}{
		"sim-01/sub":     {source.CodecH264, "4d401f"},
		"sim-16/main":    {source.CodecH264, "4d4028"},
		"mx-h265/main":   {source.CodecH265, "017b"},
		"mx-high51/main": {source.CodecH264, "640033"},
	} {
		if src := cat.sources[key]; src.Codec() != want.codec || src.Profile() != want.profile {
			t.Errorf("%s = %s %s, want %s %s", key, src.Codec(), src.Profile(), want.codec, want.profile)
		}
	}
}

// mediaDir returns the directory that cmd/mediagen generated. Tests that need
// it skip when MEDIAGEN_DIR is unset and fail instead when SPIKE_REQUIRE_MEDIA=1,
// as the CI media job sets (R19).
func mediaDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("MEDIAGEN_DIR")
	if dir == "" {
		if os.Getenv("SPIKE_REQUIRE_MEDIA") == "1" {
			t.Fatal("MEDIAGEN_DIR is unset but SPIKE_REQUIRE_MEDIA=1")
		}
		t.Skip("MEDIAGEN_DIR is unset: run cmd/mediagen and point MEDIAGEN_DIR at its output")
	}
	return dir
}
