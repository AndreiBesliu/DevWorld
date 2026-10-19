package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"golang.org/x/term"
)

// spikehub -real is a probe of the owner's real cameras and nothing else
// (R43): for each URL that the owner types, it connects, waits for the first
// access unit that it can decode, prints what the camera sends, and closes the
// connection. It starts no HTTP server, and a real camera never enters the
// catalog, so no client can watch one through the hub.
//
// The URL of a real camera is the owner's secret (PH0-22): the owner types it
// at the prompt, where it is not shown, and it is kept in memory only. It is
// never an argument or an environment variable, which process lists show, and
// never printed, logged, returned or written to a file. The hub names the
// camera real-1, real-2 and so on; source scrubs the errors it returns.

const (
	// maxRealCameras is how many real cameras -real asks for.
	maxRealCameras = 4
	// realFrameTimeout is how long the probe waits, once a camera plays, for
	// its first access unit.
	realFrameTimeout = 10 * time.Second
	// realCloseTimeout bounds the wait for a camera's connection to close.
	realCloseTimeout = 5 * time.Second
	// unknownResolution stands for a resolution that no SPS gives.
	unknownResolution = "rezoluție necunoscută"
)

// askFunc shows a prompt and returns one line that the owner typed, without
// echoing it. It returns io.EOF when there is no more input.
type askFunc func(prompt string) (string, error)

// connectFunc connects to a real camera; it is source.NewRTSP. The camera's
// connection lasts until ctx is done, and the source ends then.
type connectFunc func(ctx context.Context, id, rawURL string) (source.Source, error)

// newTerminalAsker returns an askFunc that reads from in, a terminal, with the
// input hidden by golang.org/x/term, and writes the prompts to out. It fails
// when in is not a terminal, because a pipe would show the URL in the shell or
// leave it in a file. When ctx is done (Ctrl+C) while a prompt waits, the
// prompt returns ctx's error and the terminal is as it was, with the echo on.
func newTerminalAsker(ctx context.Context, in *os.File, out io.Writer) (askFunc, error) {
	fd := int(in.Fd()) //nolint:gosec // a file descriptor fits in an int
	if !term.IsTerminal(fd) {
		return nil, errors.New("-real cere un terminal interactiv: adresele camerelor se introduc de la tastatură, " +
			"fără afișare, și nu se citesc din fișiere, din argumente sau dintr-o conductă")
	}
	type typed struct {
		line []byte
		err  error
	}
	return func(prompt string) (string, error) {
		state, err := term.GetState(fd)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprint(out, prompt)
		// ReadPassword cannot be interrupted. When ctx ends first, the read is left
		// to the end of the process, which follows at once.
		res := make(chan typed, 1)
		go func() {
			line, err := term.ReadPassword(fd)
			res <- typed{line, err}
		}()
		select {
		case r := <-res:
			_, _ = fmt.Fprintln(out) // the Enter of the owner was not echoed either
			return string(r.line), r.err
		case <-ctx.Done():
			_ = term.Restore(fd, state)
			_, _ = fmt.Fprintln(out)
			return "", ctx.Err()
		}
	}, nil
}

// runRealProbe is spikehub -real: it probes the cameras whose URLs the owner types,
// then waits for Enter, so that the window stays open while the owner copies
// the lines.
func runRealProbe(ctx context.Context, e env) error {
	ask := e.askSecret
	if ask == nil {
		var err error
		if ask, err = newTerminalAsker(ctx, os.Stdin, e.stdout); err != nil {
			return err
		}
	}
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(e.stdout, format+"\n", args...) }
	p("")
	p("spikehub -real: verificarea camerelor dvs. reale (Faza 0 b)")
	p("")
	p("Pentru fiecare cameră, spikehub se conectează, așteaptă primul cadru video, scrie ce")
	p("format trimite camera și închide conexiunea. Nu pornește serverul de test: camerele")
	p("reale nu apar în nicio aplicație. Adresele nu se afișează și nu se salvează nicăieri.")
	p("")
	if err := probeRealCameras(ctx, ask, e.stdout, source.NewRTSP, e.realFrameTimeout); err != nil {
		return err
	}
	p("")
	if _, err := ask("Apăsați Enter pentru a închide."); err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// probeRealCameras asks for the URLs of up to maxRealCameras cameras and probes
// each, as real-1, real-2 and so on. It prints one line per attempt, then a
// summary with one line per camera, which the owner sends to the developers.
// An empty line ends the prompts. A camera that fails is asked for again under
// the same name, so that a typo costs nothing; the summary keeps the outcome
// of its last attempt.
func probeRealCameras(ctx context.Context, ask askFunc, out io.Writer, connect connectFunc, frameTimeout time.Duration) error {
	var lines []string // per camera, the outcome of its last attempt
	for n := 1; n <= maxRealCameras; {
		id := fmt.Sprintf("real-%d", n)
		typed, err := ask(fmt.Sprintf("Adresa RTSP a camerei %s (nu se afișează ce tastați; Enter gol = gata): ", id))
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("citirea adresei camerei %s: %w", id, err)
		}
		rawURL := strings.TrimSpace(typed)
		if rawURL == "" {
			break
		}
		_, _ = fmt.Fprintf(out, "Verific camera %s…\n", id)
		res, err := probeCamera(ctx, id, rawURL, connect, frameTimeout)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, res.line)
		if len(lines) < n {
			lines = append(lines, res.line)
		} else {
			lines[n-1] = res.line
		}
		if res.hint != "" {
			_, _ = fmt.Fprintln(out, res.hint)
			continue
		}
		n++
	}

	_, _ = fmt.Fprintln(out)
	if len(lines) == 0 {
		_, _ = fmt.Fprintln(out, "Nu ați introdus nicio adresă de cameră.")
		return nil
	}
	_, _ = fmt.Fprintln(out, "Rezumat (trimiteți-ne aceste rânduri; nu conțin adrese):")
	for _, line := range lines {
		_, _ = fmt.Fprintln(out, line)
	}
	return nil
}

// probeResult is what the probe of one camera found: the line to send, and for
// a camera that failed, what to check before trying again.
type probeResult struct {
	line string
	hint string
}

// The hints after a camera that failed.
const (
	hintAddress = "Verificați adresa (utilizator, parolă, port, cale) și introduceți-o din nou, " +
		"sau apăsați Enter pentru a încheia."
	hintVideo = "Verificați că fluxul camerei este H.264 sau H.265 (nu MJPEG și nu doar audio) și că adresa " +
		"indică fluxul video; introduceți-o din nou sau apăsați Enter pentru a încheia."
)

func failedProbe(id, reason, hint string) probeResult {
	return probeResult{line: fmt.Sprintf("Conectarea la camera %s a eșuat: %s.", id, strings.TrimRight(reason, ". ")), hint: hint}
}

// probeCamera connects to one camera, waits up to frameTimeout for its first
// access unit, and closes the connection before it returns. The error is
// ctx's, when ctx ends first; a camera that fails is a result, not an error.
func probeCamera(ctx context.Context, id, rawURL string, connect connectFunc, frameTimeout time.Duration) (probeResult, error) {
	camCtx, closeCamera := context.WithCancel(ctx)
	defer closeCamera()
	src, err := connect(camCtx, id, rawURL)
	if err != nil {
		if ctx.Err() != nil {
			return probeResult{}, ctx.Err()
		}
		return failedProbe(id, err.Error(), hintAddress), nil // source scrubs its errors
	}
	cached, live, unsubscribe := src.Subscribe()
	defer unsubscribe()
	first, got, ended := firstAccessUnit(ctx, cached, live, frameTimeout)

	// The source ends, and closes live, once the camera's connection is closed.
	closeCamera()
	if !drainUntilClosed(live, realCloseTimeout) {
		slog.Warn("the connection of a real camera did not close in time", "camera", id)
	}
	switch {
	case ctx.Err() != nil:
		return probeResult{}, ctx.Err()
	case got:
		profile := src.Profile()
		if profile == "" {
			profile = "necunoscut"
		}
		return probeResult{line: fmt.Sprintf("Camera %s este conectată: %s, profil %s, %s.",
			id, src.Codec(), profile, resolutionOf(src.Codec(), first))}, nil
	case ended:
		return failedProbe(id, "camera a închis conexiunea înainte de primul cadru video", hintAddress), nil
	default:
		return failedProbe(id, fmt.Sprintf("camera răspunde, dar nu a trimis niciun cadru video decodabil în %g secunde",
			frameTimeout.Seconds()), hintVideo), nil
	}
}

// firstAccessUnit returns the first access unit of a subscription: the oldest
// cached one, or the first live one within timeout. got is false when none
// came; ended is true when the source ended first.
func firstAccessUnit(ctx context.Context, cached []source.AccessUnit, live <-chan source.AccessUnit,
	timeout time.Duration,
) (au source.AccessUnit, got, ended bool) {
	if len(cached) > 0 {
		return cached[0], true, false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case au, ok := <-live:
		return au, ok, !ok
	case <-timer.C:
	case <-ctx.Done():
	}
	return source.AccessUnit{}, false, false
}

// drainUntilClosed reads ch until it is closed and reports whether that
// happened within timeout.
func drainUntilClosed(ch <-chan source.AccessUnit, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// resolutionOf reads the picture size from the SPS of an access unit: the
// first one of a source, a keyframe that carries its parameter sets. It
// returns unknownResolution when no SPS parses.
func resolutionOf(codec source.Codec, au source.AccessUnit) string {
	for _, nalu := range au.NALUs {
		if len(nalu) == 0 {
			continue
		}
		var width, height int
		switch {
		case codec == source.CodecH264 && h264.NALUType(nalu[0]&0x1f) == h264.NALUTypeSPS:
			var sps h264.SPS
			if sps.Unmarshal(nalu) != nil {
				return unknownResolution
			}
			width, height = sps.Width(), sps.Height()
		case codec == source.CodecH265 && h265.NALUType(nalu[0]>>1&0x3f) == h265.NALUType_SPS_NUT:
			var sps h265.SPS
			if sps.Unmarshal(nalu) != nil {
				return unknownResolution
			}
			width, height = sps.Width(), sps.Height()
		default:
			continue
		}
		if width <= 0 || height <= 0 {
			return unknownResolution
		}
		return fmt.Sprintf("%dx%d", width, height)
	}
	return unknownResolution
}
