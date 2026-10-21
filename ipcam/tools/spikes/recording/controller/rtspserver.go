// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package controller is reccontroller's side of the T43 measurements: it
// serves the pre-coded simulated cameras over RTSP with an SEI index in each
// access unit, logs when it sent each one, receives the host's liveness
// beacon, schedules the cuts and judges them, and computes the loss on its
// own clock (C15, C16). Every inbound listener of the spike is here (R08).
package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/auth"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// Stream is one simulated camera stream: the Annex B file that the server
// replays in a loop at FPS frames per second, at the RTSP path Path.
type Stream struct {
	// Path is "cam01_main" … "cam16_main", "cam01_sub" … "cam16_sub", or
	// "cam99_main", the 256 kbps camera of HUB-07's low-bitrate runs.
	Path   string
	File   string
	FPS    int
	Camera uint8
}

// The media that reccontroller serves, as mediagen and recmediagen write
// them: main.h264 (4 Mbps, 20 fps), sub.h264 (512 kbps, 12 fps) and
// sub-256k.h264 (256 kbps, 12 fps).
const (
	mainFile, mainFPS = "main.h264", 20
	subFile, subFPS   = "sub.h264", 12
	lowFile, lowFPS   = "sub-256k.h264", 12
	cameras           = 16
	lowBitrateCamera  = 99
)

// Streams lists every stream that reccontroller serves from the media in
// mediaDir: a main and a sub stream for cameras 1 to 16, and cam99_main.
func Streams(mediaDir string) []Stream {
	var streams []Stream
	for cam := uint8(1); cam <= cameras; cam++ {
		streams = append(streams,
			Stream{Path: fmt.Sprintf("cam%02d_main", cam), File: filepath.Join(mediaDir, mainFile), FPS: mainFPS, Camera: cam},
			Stream{Path: fmt.Sprintf("cam%02d_sub", cam), File: filepath.Join(mediaDir, subFile), FPS: subFPS, Camera: cam})
	}
	return append(streams, Stream{Path: fmt.Sprintf("cam%02d_main", lowBitrateCamera), File: filepath.Join(mediaDir, lowFile), FPS: lowFPS, Camera: lowBitrateCamera})
}

// Credentials are the RTSP login of one reccontroller start. They are
// random, go to the host only over the link, and never into a log line: they
// format as redacted.
type Credentials struct{ User, Pass string }

// NewCredentials draws a random user (16 hex digits) and password (32).
func NewCredentials() (Credentials, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Credentials{}, err
	}
	return Credentials{User: hex.EncodeToString(b[:8]), Pass: hex.EncodeToString(b[8:])}, nil
}

const redacted = "<redacted>"

// String hides the credentials from fmt.
func (Credentials) String() string { return redacted }

// GoString hides the credentials from fmt's %#v.
func (Credentials) GoString() string { return redacted }

// LogValue hides the credentials from slog.
func (Credentials) LogValue() slog.Value { return slog.StringValue(redacted) }

// writeQueueSize holds the burst of a cached GOP at a session's start: a
// 4 Mbps GOP is a few hundred packets.
const writeQueueSize = 4096

// Server serves the streams over RTSP/TCP. Each path is fed by one
// simulator, which keeps the GOP since its last keyframe; each session gets
// a stream of its own that starts with that cached GOP, as a camera does, and
// every access unit carries its index in an SEI.
type Server struct {
	srv    *gortsplib.Server
	creds  Credentials
	clock  source.Clock
	log    *slog.Logger
	paths  map[string]*servedPath
	pacing *pacing
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed chan struct{}

	mu       sync.Mutex
	stopping bool // no new describe stream after the server's close
}

// servedPath is one stream and its simulator.
type servedPath struct {
	stream   Stream
	sim      source.Source
	frames   int
	sps, pps []byte
	describe *gortsplib.ServerStream // answers DESCRIBE, made at the first; no session reads it
}

// newDesc returns a fresh description of the path's one H.264 track.
func (p *servedPath) newDesc() (*description.Session, *description.Media, *format.H264) {
	forma := &format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: p.sps, PPS: p.pps}
	medi := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{forma}}
	return &description.Session{Medias: []*description.Media{medi}}, medi, forma
}

// rtspSession is one client session that set a path up.
type rtspSession struct {
	path        *servedPath
	stream      *gortsplib.ServerStream
	medi        *description.Media
	forma       *format.H264
	ctx         context.Context
	cancel      context.CancelFunc
	playing     atomic.Bool
	writeErrors atomic.Int64 // packets that did not fit the session's write queue
}

// ServePrecoded serves streams at addr, an IPv4 address, until ctx is done
// or Close is called. Each sent access unit is recorded in log with its
// index and the time from clock.
func ServePrecoded(ctx context.Context, addr string, streams []Stream, creds Credentials, log *SendLog, clock source.Clock) (*Server, error) {
	ln, err := listenTCP4(ctx, addr)
	if err != nil {
		return nil, err
	}
	s, err := ServePrecodedOn(ctx, ln, streams, creds, log, clock)
	if err != nil {
		_ = ln.Close()
	}
	return s, err
}

// ServePrecodedOn is ServePrecoded on a listener that ListenAll opened.
func ServePrecodedOn(ctx context.Context, ln net.Listener, streams []Stream, creds Credentials, log *SendLog, clock source.Clock) (*Server, error) {
	if creds.User == "" || creds.Pass == "" {
		return nil, errors.New("controller: the RTSP server needs a user and a password")
	}
	sctx, cancel := context.WithCancel(ctx)
	s := &Server{
		creds:  creds,
		clock:  clock,
		log:    slog.Default().With("component", "rtsp"),
		paths:  map[string]*servedPath{},
		cancel: cancel,
		closed: make(chan struct{}),
	}
	fail := func(err error) (*Server, error) {
		cancel()
		s.wg.Wait()
		return nil, err
	}
	files := map[string][]source.AccessUnit{}
	sims := map[string]source.Source{}
	for _, st := range streams {
		aus, ok := files[st.File]
		if !ok {
			var err error
			if aus, err = source.ReadAnnexB(st.File); err != nil {
				return fail(fmt.Errorf("controller: %s: %w", st.Path, err))
			}
			files[st.File] = aus
		}
		sps, pps := parameterSets(aus)
		if sps == nil || pps == nil {
			return fail(fmt.Errorf("controller: %s: %s has no H.264 SPS and PPS", st.Path, filepath.Base(st.File)))
		}
		if _, dup := s.paths[st.Path]; dup {
			return fail(fmt.Errorf("controller: the path %s is listed twice", st.Path))
		}
		sim := source.NewSim(st.Path, st.Camera, aus, st.FPS, 0, ctxClock{ctx: sctx, Clock: clock})
		s.paths[st.Path] = &servedPath{stream: st, sim: sim, frames: len(aus), sps: sps, pps: pps}
		sims[st.Path] = sim
		s.wg.Go(func() { feedSendLog(sctx, st.Path, len(aus), sim.SentLog(), log) })
	}
	s.pacing = newPacing(sims)

	s.srv = &gortsplib.Server{
		Handler:        s,
		RTSPAddress:    ln.Addr().String(),
		Listen:         func(string, string) (net.Listener, error) { return ln, nil },
		WriteQueueSize: writeQueueSize,
		// Digest only: the password never crosses the LAN in clear.
		AuthMethods: []auth.VerifyMethod{auth.VerifyMethodDigestMD5},
	}
	if err := s.srv.Start(); err != nil {
		return fail(fmt.Errorf("controller: start the RTSP server: %w", err))
	}
	go func() {
		<-sctx.Done()
		s.srv.Close()
		s.mu.Lock()
		s.stopping = true
		for _, p := range s.paths {
			if p.describe != nil {
				p.describe.Close()
			}
		}
		s.mu.Unlock()
		s.wg.Wait()
		close(s.closed)
	}()
	return s, nil
}

// Addr is the server's listening address.
func (s *Server) Addr() net.Addr { return s.srv.NetListener().Addr() }

// Close stops serving and waits until every session and simulator has
// stopped.
func (s *Server) Close() {
	s.cancel()
	<-s.closed
}

// MarkPacing captures the pacing state at the start of a run.
func (s *Server) MarkPacing() PacingMark { return s.pacing.mark(s.clock.Now()) }

// Pacing returns the pacing of the run that started at m and whose host
// stopped at until (its StopAt).
func (s *Server) Pacing(m PacingMark, until time.Time) PacingStats { return s.pacing.since(m, until) }

// parameterSets returns the first SPS and PPS of the access units.
func parameterSets(aus []source.AccessUnit) (sps, pps []byte) {
	for _, au := range aus {
		for _, n := range au.NALUs {
			switch h264.NALUType(n[0] & 0x1f) {
			case h264.NALUTypeSPS:
				if sps == nil {
					sps = n
				}
			case h264.NALUTypePPS:
				if pps == nil {
					pps = n
				}
			}
		}
		if sps != nil && pps != nil {
			return sps, pps
		}
	}
	return sps, pps
}

// authorised checks the request's credentials. A request without them gets
// the digest challenge, as every client's first one does; wrong ones are
// logged with the path only.
func (s *Server) authorised(conn *gortsplib.ServerConn, req *base.Request, path string) bool {
	if conn.VerifyCredentials(req, s.creds.User, s.creds.Pass) {
		return true
	}
	if _, sent := req.Header["Authorization"]; sent {
		s.log.Warn("rtsp: wrong credentials", "path", path, "method", string(req.Method))
	} else {
		s.log.Debug("rtsp: credentials asked", "path", path)
	}
	return false
}

func (s *Server) lookup(path string) (*servedPath, string) {
	name := strings.TrimPrefix(path, "/")
	return s.paths[name], name
}

func unauthorised() (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusUnauthorized}, liberrors.ErrServerAuth{}
}

// OnDescribe answers with the path's description.
func (s *Server) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	p, name := s.lookup(ctx.Path)
	if !s.authorised(ctx.Conn, ctx.Request, name) {
		res, err := unauthorised()
		return res, nil, err
	}
	if p == nil {
		s.log.Warn("rtsp: no such path", "path", name)
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	stream, err := s.describeStream(p)
	if err != nil {
		return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil, err
	}
	return &base.Response{StatusCode: base.StatusOK}, stream, nil
}

// describeStream returns the path's stream for DESCRIBE, made at the first
// one: a stream needs the started server.
func (s *Server) describeStream(p *servedPath) (*gortsplib.ServerStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, errors.New("the server is closing")
	}
	if p.describe == nil {
		desc, _, _ := p.newDesc()
		stream := &gortsplib.ServerStream{Server: s.srv, Desc: desc}
		if err := stream.Initialize(); err != nil {
			return nil, err
		}
		p.describe = stream
	}
	return p.describe, nil
}

// OnSetup gives the session a stream of its own, so that its first access
// unit is the cached keyframe whenever it starts.
func (s *Server) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	p, name := s.lookup(ctx.Path)
	if !s.authorised(ctx.Conn, ctx.Request, name) {
		res, err := unauthorised()
		return res, nil, err
	}
	if p == nil {
		s.log.Warn("rtsp: no such path", "path", name)
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	if ss, ok := ctx.Session.UserData().(*rtspSession); ok {
		if ss.path != p {
			return &base.Response{StatusCode: base.StatusBadRequest}, nil, errors.New("one session reads one path")
		}
		return &base.Response{StatusCode: base.StatusOK}, ss.stream, nil
	}
	desc, medi, forma := p.newDesc()
	stream := &gortsplib.ServerStream{Server: s.srv, Desc: desc}
	if err := stream.Initialize(); err != nil {
		return &base.Response{StatusCode: base.StatusInternalServerError}, nil, err
	}
	sctx, cancel := context.WithCancel(context.Background())
	ctx.Session.SetUserData(&rtspSession{path: p, stream: stream, medi: medi, forma: forma, ctx: sctx, cancel: cancel})
	s.log.Info("rtsp: session set up", "path", name)
	return &base.Response{StatusCode: base.StatusOK}, stream, nil
}

// OnPlay starts writing to the session.
func (s *Server) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	ss, ok := ctx.Session.UserData().(*rtspSession)
	if !ok {
		return &base.Response{StatusCode: base.StatusBadRequest}, errors.New("PLAY before SETUP")
	}
	if !s.authorised(ctx.Conn, ctx.Request, ss.path.stream.Path) {
		return unauthorised()
	}
	if ss.playing.CompareAndSwap(false, true) {
		s.wg.Go(func() { s.play(ss) })
	}
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// OnSessionClose stops the session's writer and its stream.
func (s *Server) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	if ss, ok := ctx.Session.UserData().(*rtspSession); ok {
		ss.cancel()
		ss.stream.Close()
		s.log.Info("rtsp: session closed", "path", ss.path.stream.Path)
	}
}

// OnStreamWriteError counts a packet that did not fit the session's write
// queue; the session's writer turns it into a skipped access unit.
func (s *Server) OnStreamWriteError(ctx *gortsplib.ServerHandlerOnStreamWriteErrorCtx) {
	if ss, ok := ctx.Session.UserData().(*rtspSession); ok {
		ss.writeErrors.Add(1)
	}
}

// OnPacketsLost and OnDecodeError concern packets that clients send, which
// a reader never does; without them gortsplib would write to the standard
// logger.
func (s *Server) OnPacketsLost(*gortsplib.ServerHandlerOnPacketsLostCtx) {}

// OnDecodeError does nothing, as OnPacketsLost.
func (s *Server) OnDecodeError(*gortsplib.ServerHandlerOnDecodeErrorCtx) {}

// play writes the path's cached GOP to the session at once, then every new
// access unit, each with its index in an SEI, until the session or the
// simulator ends. An access unit that the subscription dropped, or one with
// a packet that did not fit the write queue, counts as skipped (R07).
func (s *Server) play(ss *rtspSession) {
	cached, live, cancel := ss.path.sim.Subscribe()
	defer cancel()
	enc, err := ss.forma.CreateEncoder()
	if err != nil {
		s.log.Error("rtsp: no RTP encoder", "path", ss.path.stream.Path, "err", err)
		return
	}
	var prev uint64
	first := true
	write := func(au source.AccessUnit) bool {
		index := frameIndex(au.Loop, au.Frame, ss.path.frames)
		if !first && index > prev+1 {
			s.pacing.skipped(s.clock.Now(), int(index-prev-1)) //nolint:gosec // a subscriber drops at most a few GOPs
		}
		first, prev = false, index
		pkts, err := enc.Encode(StampIndex(au.NALUs, index))
		if err != nil {
			s.log.Warn("rtsp: cannot packetise an access unit", "path", ss.path.stream.Path, "err", err)
			s.pacing.skipped(s.clock.Now(), 1)
			return true
		}
		before := ss.writeErrors.Load()
		ts := rtpTimestamp(au.PTS)
		for _, pkt := range pkts {
			pkt.Timestamp = ts
			if err := ss.stream.WritePacketRTP(ss.medi, pkt); err != nil {
				if errors.As(err, new(liberrors.ErrServerStreamClosed)) {
					return false
				}
				ss.writeErrors.Add(1)
			}
		}
		if ss.writeErrors.Load() != before {
			s.pacing.skipped(s.clock.Now(), 1)
		}
		return true
	}
	for _, au := range cached {
		if !write(au) {
			return
		}
	}
	for {
		select {
		case <-ss.ctx.Done():
			return
		case au, ok := <-live:
			if !ok || !write(au) {
				return
			}
		}
	}
}

// rtpTimestamp is a PTS in the 90 kHz clock of H.264 over RTP; it wraps as
// RTP timestamps do.
func rtpTimestamp(pts time.Duration) uint32 {
	return uint32(pts.Nanoseconds() * 9 / 100_000) //nolint:gosec // wraps on purpose
}

// ctxClock is a clock whose tickers close their channel when ctx is done, so
// that a simulator, which runs until its ticker's channel closes, stops with
// the server.
type ctxClock struct {
	ctx context.Context
	source.Clock
}

func (c ctxClock) NewTicker(d time.Duration) source.Ticker {
	inner := c.Clock.NewTicker(d)
	t := &ctxTicker{inner: inner, c: make(chan time.Time)}
	go func() {
		defer close(t.c)
		defer inner.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case tick, ok := <-inner.C():
				if !ok {
					return
				}
				select {
				case t.c <- tick:
				case <-c.ctx.Done():
					return
				}
			}
		}
	}()
	return t
}

type ctxTicker struct {
	inner source.Ticker
	c     chan time.Time
}

func (t *ctxTicker) C() <-chan time.Time { return t.c }

// Stop stops the inner ticker; the channel closes when the context is done.
func (t *ctxTicker) Stop() { t.inner.Stop() }
