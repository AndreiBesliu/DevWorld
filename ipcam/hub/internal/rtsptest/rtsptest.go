// Package rtsptest is a test RTSP server for the hub's process tests: it
// plays an H.264 Annex B file in real time, in a loop, as a camera that the
// real MediaMTX pulls and records. Only tests import it.
//
// Each RTSP session gets a stream of its own that starts at the cached last
// IDR, the frames from it to now sent at once, as a camera with a GOP cache
// does, so that a MediaMTX that connects again starts its first part
// without waiting for the next keyframe.
//
// The process tests take MediaMTX and the media from the environment
// (rulings G02, G06): MEDIAMTX_BIN is the built fork, MEDIAGEN_DIR holds
// the media. A test skips when either is unset, and fails instead when
// RECSPIKE_REQUIRE_MEDIAMTX=1 or SPIKE_REQUIRE_MEDIA=1, as CI sets them.
package rtsptest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// FPS is the frame rate of the media: mediagen writes main.h264 at 20 fps,
// with a key frame every second.
const FPS = 20

// MediaMTX returns the MediaMTX binary from MEDIAMTX_BIN.
func MediaMTX(t testing.TB) string {
	t.Helper()
	bin := os.Getenv("MEDIAMTX_BIN")
	if bin == "" {
		if os.Getenv("RECSPIKE_REQUIRE_MEDIAMTX") == "1" {
			t.Fatal("RECSPIKE_REQUIRE_MEDIAMTX=1, but MEDIAMTX_BIN is empty")
		}
		t.Skip("no MediaMTX: set MEDIAMTX_BIN to the built fork")
	}
	if _, err := os.Stat(bin); err != nil { //nolint:gosec // the binary that the test run names
		t.Fatalf("MEDIAMTX_BIN: %v", err)
	}
	return bin
}

// MainH264 returns MEDIAGEN_DIR/main.h264.
func MainH264(t testing.TB) string {
	t.Helper()
	dir := os.Getenv("MEDIAGEN_DIR")
	if dir == "" {
		if os.Getenv("SPIKE_REQUIRE_MEDIA") == "1" {
			t.Fatal("SPIKE_REQUIRE_MEDIA=1, but MEDIAGEN_DIR is empty")
		}
		t.Skip("no media: set MEDIAGEN_DIR to mediagen's output")
	}
	path := filepath.Join(dir, "main.h264")
	if _, err := os.Stat(path); err != nil { //nolint:gosec // the media that the test run names
		t.Fatalf("MEDIAGEN_DIR: %v", err)
	}
	return path
}

// Server serves one H.264 file on 127.0.0.1, at any URL path.
type Server struct {
	server *gortsplib.Server
	aus    []accessUnit
	sps    []byte
	pps    []byte
	t0     time.Time // when frame 0 was due

	mu       sync.Mutex
	sessions map[*gortsplib.ServerConn]*session
	wg       sync.WaitGroup
}

type accessUnit struct {
	nalus [][]byte
	idr   bool
}

type session struct {
	stream *gortsplib.ServerStream
	medi   *description.Media
	ctx    context.Context // done when the session ends
	stop   func()
}

// Start serves the Annex B file at path until the test ends.
func Start(t testing.TB, path string) *Server {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a media file that the test chose
	if err != nil {
		t.Fatal(err)
	}
	aus, sps, pps, err := accessUnits(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	s := &Server{aus: aus, sps: sps, pps: pps, t0: time.Now(), sessions: map[*gortsplib.ServerConn]*session{}}
	// The queue holds the burst of a whole GOP at a session's start.
	s.server = &gortsplib.Server{Handler: s, RTSPAddress: "127.0.0.1:0", WriteQueueSize: 4096}
	if err := s.server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.server.Close()
		s.mu.Lock()
		for _, ss := range s.sessions {
			ss.stop()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

// URL is the camera URL of path.
func (s *Server) URL(path string) string {
	return "rtsp://" + s.server.NetListener().Addr().String() + "/" + path
}

// OnConnClose ends the session of a connection that closed.
func (s *Server) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	s.mu.Lock()
	ss := s.sessions[ctx.Conn]
	delete(s.sessions, ctx.Conn)
	s.mu.Unlock()
	if ss != nil {
		ss.stop()
	}
}

// OnDescribe gives each connection a stream of its own.
func (s *Server) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	medi := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{
		&format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: s.sps, PPS: s.pps},
	}}
	stream := &gortsplib.ServerStream{Server: s.server, Desc: &description.Session{Medias: []*description.Media{medi}}}
	if err := stream.Initialize(); err != nil {
		return &base.Response{StatusCode: base.StatusInternalServerError}, nil, err
	}
	s.mu.Lock()
	if old := s.sessions[ctx.Conn]; old != nil {
		old.stop()
	}
	sctx, cancel := context.WithCancel(context.Background())
	s.sessions[ctx.Conn] = &session{stream: stream, medi: medi, ctx: sctx, stop: func() { cancel(); stream.Close() }}
	s.mu.Unlock()
	return &base.Response{StatusCode: base.StatusOK}, stream, nil
}

// OnSetup sets the connection's stream up.
func (s *Server) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	s.mu.Lock()
	ss := s.sessions[ctx.Conn]
	s.mu.Unlock()
	if ss == nil {
		return &base.Response{StatusCode: base.StatusBadRequest}, nil, errors.New("SETUP before DESCRIBE")
	}
	return &base.Response{StatusCode: base.StatusOK}, ss.stream, nil
}

// OnPlay starts the connection's stream at the last IDR.
func (s *Server) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	s.mu.Lock()
	ss := s.sessions[ctx.Conn]
	s.mu.Unlock()
	if ss == nil {
		return &base.Response{StatusCode: base.StatusBadRequest}, errors.New("PLAY before DESCRIBE")
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.publish(ss.ctx, ss)
	}()
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// publish writes the frames from the last IDR before now at once, then each
// frame when it is due, with the NTP time when it was due.
func (s *Server) publish(ctx context.Context, ss *session) {
	enc, err := ss.medi.Formats[0].(*format.H264).CreateEncoder()
	if err != nil {
		return
	}
	step := time.Second / FPS
	n := int(time.Since(s.t0) / step)
	first := n
	for first > 0 && !s.aus[first%len(s.aus)].idr {
		first--
	}
	for i := first; ; i++ {
		due := s.t0.Add(time.Duration(i) * step)
		if wait := time.Until(due); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		if ctx.Err() != nil {
			return
		}
		pkts, err := enc.Encode(s.aus[i%len(s.aus)].nalus)
		if err != nil {
			return
		}
		for _, p := range pkts {
			p.Timestamp = uint32(int64(i) * 90000 / FPS) //nolint:gosec // wraps as RTP timestamps do
			if ss.stream.WritePacketRTPWithNTP(ss.medi, p, due) != nil {
				return
			}
		}
	}
}

// accessUnits splits an Annex B H.264 stream into access units: an access
// unit ends with the slices of one picture, and the parameter sets and SEI
// before a picture belong to it.
func accessUnits(data []byte) (aus []accessUnit, sps, pps []byte, err error) {
	var au [][]byte
	var picture bool
	flush := func() {
		if picture {
			aus = append(aus, accessUnit{nalus: au, idr: h264.IsRandomAccess(au)})
		}
		au, picture = nil, false
	}
	for _, nalu := range splitAnnexB(data) {
		typ := h264.NALUType(nalu[0] & 0x1f)
		slice := typ == h264.NALUTypeIDR || typ == h264.NALUTypeNonIDR
		firstSlice := slice && len(nalu) > 1 && nalu[1]&0x80 != 0
		if picture && (!slice || firstSlice) {
			flush()
		}
		switch typ {
		case h264.NALUTypeSPS:
			sps = nalu
		case h264.NALUTypePPS:
			pps = nalu
		}
		au = append(au, nalu)
		picture = picture || slice
	}
	flush()
	if len(aus) == 0 || sps == nil || pps == nil {
		return nil, nil, nil, errors.New("not an H.264 stream with parameter sets and pictures")
	}
	if !aus[0].idr {
		return nil, nil, nil, errors.New("the stream does not start with an IDR")
	}
	return aus, sps, pps, nil
}

// splitAnnexB returns the NAL units between start codes, without the zero
// bytes before the next start code.
func splitAnnexB(data []byte) [][]byte {
	var nalus [][]byte
	for {
		i := bytes.Index(data, []byte{0, 0, 1})
		if i < 0 {
			break
		}
		data = data[i+3:]
		end := bytes.Index(data, []byte{0, 0, 1})
		if end < 0 {
			end = len(data)
		}
		if nalu := bytes.TrimRight(data[:end], "\x00"); len(nalu) > 0 {
			nalus = append(nalus, nalu)
		}
		data = data[end:]
	}
	return nalus
}
