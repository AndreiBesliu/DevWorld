// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"context"
	"errors"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/pion/rtp"
)

// The substream reference's errors, which name no URL, address or
// credential (the client's own errors may).
var (
	ErrRTSPConnect = errors.New("mi8: the camera's substream did not play")
	ErrRTSPCodec   = errors.New("mi8: the camera's substream is neither H.264 nor H.265")
	ErrNoFrames    = errors.New("mi8: the camera's substream gave no decoded frame")
)

// RTSPSource is the substream reference of a camera (P41): uploadspike reads
// the substream itself with gortsplib, from the stream URI that diag onvif
// --uris gave and the typed credentials, and pipes its H.264 or H.265
// elementary stream to FFmpeg's stdin, which decodes it to 2 fps of 160 × 90
// gray frames. The URI, the address and the credentials stay in this
// process's RAM: none is in FFmpeg's argv or environment. Frames stay in
// RAM.
type RTSPSource struct {
	URI, User, Pass string
	// FFmpeg is the pinned ffmpeg's path; Start starts it (nil: plainly).
	FFmpeg string
	Start  Starter
	// FirstFrame bounds the wait for the first decoded frame: a substream
	// that does not decode falls back to the snapshot diff. 0: 30 s.
	FirstFrame time.Duration
	Now        func() time.Time

	mu      sync.Mutex
	samples lagSamples
}

// Lag is the reference's delay L: the p90 of (a frame's decode time − the
// receipt time of its access unit) plus one frame interval, capped at 3 s.
func (s *RTSPSource) Lag() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samples.lag()
}

func (s *RTSPSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Frames connects to the substream and returns its frames once the first
// one decoded; ErrRTSPConnect, ErrRTSPCodec or ErrNoFrames otherwise.
func (s *RTSPSource) Frames(ctx context.Context) (<-chan Frame, error) {
	u, err := base.ParseURL(s.URI)
	if err != nil {
		return nil, ErrRTSPConnect
	}
	if s.User != "" || s.Pass != "" {
		u.User = url.UserPassword(s.User, s.Pass)
	}
	tcp := gortsplib.ProtocolTCP
	c := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &tcp,
		OnDecodeError: func(error) {}, OnPacketsLost: func(uint64) {}, OnTransportSwitch: func(error) {}}
	if err := c.Start(); err != nil {
		return nil, ErrRTSPConnect
	}
	stop := context.AfterFunc(ctx, c.Close)
	fail := func(err error) (<-chan Frame, error) {
		stop()
		c.Close()
		return nil, err
	}
	desc, _, err := c.Describe(u)
	if err != nil {
		return fail(ErrRTSPConnect)
	}
	medi, forma, codec, decode, params, randomAccess, err := pickVideo(desc)
	if err != nil {
		return fail(err)
	}
	if _, err := c.Setup(desc.BaseURL, medi, 0, 0); err != nil {
		return fail(ErrRTSPConnect)
	}
	dec, err := startDecoder(s.FFmpeg, codec, s.Start)
	if err != nil {
		return fail(err)
	}
	// The access units go to FFmpeg from a goroutine of their own, so the
	// client's callback never waits on FFmpeg.
	aus := make(chan []byte, 256)
	var firstWrite time.Time
	var fwMu sync.Mutex
	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		au, err := decode(pkt)
		if err != nil || len(au) == 0 {
			return
		}
		if !params.started {
			if !randomAccess(au) {
				return // FFmpeg starts at a random access point
			}
			params.started = true
			au = append(append([][]byte{}, params.sets...), au...)
		}
		b, err := h264.AnnexB(au).Marshal() // H.265's Annex B is the same
		if err != nil {
			return
		}
		select {
		case aus <- b:
		default: // FFmpeg is behind: the frame diff tolerates a dropped access unit
		}
	})
	if _, err := c.Play(nil); err != nil {
		dec.kill()
		return fail(ErrRTSPConnect)
	}
	go func() {
		defer func() { _ = dec.in.Close() }()
		for {
			select {
			case b := <-aus:
				fwMu.Lock()
				if firstWrite.IsZero() {
					firstWrite = s.now()
				}
				fwMu.Unlock()
				if _, err := dec.in.Write(b); err != nil {
					return
				}
			case <-ctx.Done():
				return
			case <-dec.done:
				return
			}
		}
	}()
	go func() {
		_ = c.Wait()
		dec.kill()
	}()
	frames := make(chan Frame, 4)
	first := make(chan struct{})
	go func() {
		defer close(frames)
		defer stop()
		defer c.Close()
		defer dec.kill()
		for n := 0; ; n++ {
			buf := make([]byte, FrameSize)
			if _, err := io.ReadFull(dec.out, buf); err != nil {
				return
			}
			at := s.now()
			fwMu.Lock()
			t0 := firstWrite
			fwMu.Unlock()
			// FFmpeg stamps each access unit with its wall clock as it reads it,
			// and fps=2 keeps the one nearest each half second from the first:
			// frame n is the access unit received about t0 + n × 0.5 s.
			if !t0.IsZero() {
				s.mu.Lock()
				s.samples.add(at.Sub(t0.Add(time.Duration(n) * frameInterval)))
				s.mu.Unlock()
			}
			if n == 0 {
				close(first)
			}
			select {
			case frames <- Frame{AtMs: at.UnixMilli(), Gray: buf}:
			case <-ctx.Done():
				return
			}
		}
	}()
	wait := s.FirstFrame
	if wait <= 0 {
		wait = 30 * time.Second
	}
	select {
	case <-first:
		return frames, nil
	case <-time.After(wait):
	case <-ctx.Done():
	}
	c.Close()
	dec.kill()
	for range frames { //nolint:revive // drained until the reader ends
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, ErrNoFrames
}

// streamParams are a track's parameter sets, which FFmpeg needs before the
// first random access point.
type streamParams struct {
	sets    [][]byte
	started bool
}

// pickVideo picks desc's first H.264 or H.265 video track.
func pickVideo(desc *description.Session) (*description.Media, format.Format, string, func(*rtp.Packet) ([][]byte, error),
	*streamParams, func([][]byte) bool, error,
) {
	for _, medi := range desc.Medias {
		if medi.Type != description.MediaTypeVideo {
			continue
		}
		for _, forma := range medi.Formats {
			switch f := forma.(type) {
			case *format.H264:
				d, err := f.CreateDecoder()
				if err != nil {
					return nil, nil, "", nil, nil, nil, ErrRTSPCodec
				}
				decode := func(pkt *rtp.Packet) ([][]byte, error) {
					au, err := d.Decode(pkt)
					if errors.Is(err, rtph264.ErrMorePacketsNeeded) {
						return nil, nil
					}
					return au, err
				}
				return medi, forma, CodecH264, decode, &streamParams{sets: nonEmpty(f.SPS, f.PPS)}, h264.IsRandomAccess, nil
			case *format.H265:
				d, err := f.CreateDecoder()
				if err != nil {
					return nil, nil, "", nil, nil, nil, ErrRTSPCodec
				}
				decode := func(pkt *rtp.Packet) ([][]byte, error) {
					au, err := d.Decode(pkt)
					if errors.Is(err, rtph265.ErrMorePacketsNeeded) {
						return nil, nil
					}
					return au, err
				}
				return medi, forma, CodecH265, decode, &streamParams{sets: nonEmpty(f.VPS, f.SPS, f.PPS)}, h265.IsRandomAccess, nil
			}
		}
	}
	return nil, nil, "", nil, nil, nil, ErrRTSPCodec
}

// nonEmpty drops the empty parameter sets.
func nonEmpty(sets ...[]byte) [][]byte {
	var out [][]byte
	for _, s := range sets {
		if len(s) > 0 {
			out = append(out, s)
		}
	}
	return out
}
