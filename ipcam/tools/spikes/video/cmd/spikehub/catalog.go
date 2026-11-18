package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// The numbers of the cameras (R1, R3). A simulated camera's number is its index
// in the list, 1 to maxSims; the sent records and the "sent" messages carry it,
// and the band of the pixels does not (it carries the media file id).
const (
	maxSims            = 16
	cameraMatrixH265   = 17
	cameraMatrixHigh51 = 18
)

// stream is one pre-encoded file of cmd/mediagen's output, read once and
// shared by every simulator that replays it. The frame rates are the ones of
// the spec (Global Constraints, "Simulated media"); a test compares them with
// the manifest that mediagen writes.
type stream struct {
	file string
	fps  int
	aus  []source.AccessUnit
}

// media is the content of the -media directory.
type media struct {
	sub, main, high51, h265 stream
}

// hlsPlaylist is the recording that GET /media/hls/ serves, relative to the
// media directory.
const hlsPlaylist = "hls/index.m3u8"

// loadMedia reads each media file once. The access units are shared, read-only,
// by all the simulators that replay a file (R28).
func loadMedia(dir string) (*media, error) {
	m := &media{
		sub:    stream{file: "sub.h264", fps: 12},
		main:   stream{file: "main.h264", fps: 20},
		high51: stream{file: "main-high51.h264", fps: 20},
		h265:   stream{file: "main-h265.h265", fps: 20},
	}
	for _, s := range []*stream{&m.sub, &m.main, &m.high51, &m.h265} {
		aus, err := source.ReadAnnexB(filepath.Join(dir, s.file))
		if err != nil {
			return nil, err
		}
		s.aus = aus
	}
	if _, err := os.Stat(filepath.Join(dir, hlsPlaylist)); err != nil {
		return nil, fmt.Errorf("the recording: %w", err)
	}
	return m, nil
}

// camera is an entry of GET /spike/v1/cameras.
type camera struct {
	ID     string `json:"id"`
	Camera int    `json:"camera"`
	Codec  string `json:"codec"`
	// ProfileLevel is the profile-level-id of an H.264 stream that a browser may
	// not decode (the High 5.1 camera of the codec matrix, G9).
	ProfileLevel string   `json:"profileLevel,omitempty"`
	Profiles     []string `json:"profiles"`
}

// catalog is the cameras of the hub and the sources that serve them: the
// simulators, and never a real camera (R43, see real.go). It is built once at
// start; the sources live for the process (R28).
type catalog struct {
	cameras []camera
	// sources maps "<camera id>/<profile>" (R2), for example "sim-04/sub", to the
	// source a bind may name.
	sources map[string]source.Source
}

// newCatalog starts the simulated cameras: sims of them with a sub and a main
// stream, then the two cameras of the codec matrix. The matrix cameras play
// their own file as main and the standard sub stream as sub, so that the
// fallback to sub (G9) stays on the same camera. sims is 0 to maxSims: the
// numbers above it belong to the codec matrix.
func newCatalog(m *media, sims int, clock source.Clock) (*catalog, error) {
	if sims < 0 || sims > maxSims {
		return nil, fmt.Errorf("%d simulated cameras; the hub serves 0 to %d", sims, maxSims)
	}
	c := &catalog{sources: map[string]source.Source{}}
	for n := 1; n <= sims; n++ {
		c.addSimCamera(fmt.Sprintf("sim-%02d", n), uint8(n), []string{"sub", "main"}, m.sub, m.main, false, clock) //nolint:gosec // n is 1 to maxSims, checked above
	}
	c.addSimCamera("mx-h265", cameraMatrixH265, []string{"main", "sub"}, m.sub, m.h265, false, clock)
	c.addSimCamera("mx-high51", cameraMatrixHigh51, []string{"main", "sub"}, m.sub, m.high51, true, clock)
	return c, nil
}

// addSimCamera starts the sub and main simulators of one camera and lists it
// with the given profile order. withProfileLevel puts the profile-level-id of
// the main stream in the listing.
func (c *catalog) addSimCamera(id string, number uint8, profiles []string, subStream, mainStream stream, withProfileLevel bool, clock source.Clock) {
	srcMain := newSim(id, number, "main", mainStream, clock)
	c.sources[id+"/main"] = srcMain
	c.sources[id+"/sub"] = newSim(id, number, "sub", subStream, clock)

	cam := camera{ID: id, Camera: int(number), Codec: string(srcMain.Codec()), Profiles: profiles}
	if withProfileLevel {
		cam.ProfileLevel = srcMain.Profile()
	}
	c.cameras = append(c.cameras, cam)
}

// newSim is the one place that starts a simulator, so that every one names its
// stream profile, "sub" or "main", which the sent records carry (R28). Each
// camera starts at a frame of its own, number-1 slots of the file apart, so
// that the tiles of a grid that show the same file do not show the same frame;
// 18 cameras (16 simulators and the two of the codec matrix) share one file at
// most.
func newSim(id string, number uint8, profile string, s stream, clock source.Clock) source.Source {
	const cameras = maxSims + 2
	phase := (int(number) - 1) * (len(s.aus) / cameras)
	return source.NewSim(id, number, s.aus, s.fps, phase, clock, source.WithStreamProfile(profile))
}
