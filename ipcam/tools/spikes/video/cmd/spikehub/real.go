package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"golang.org/x/term"
)

// The URL of a real camera is the owner's secret (PH0-22): the owner types it
// at the prompt, where it is not shown, and it is kept in memory only. It is
// never an argument or an environment variable, which process lists show, and
// never printed, logged, returned or written to a results file. The hub names
// the camera real-1, real-2 and so on, and says nothing else about it; source
// scrubs the errors it returns.

// silentCameraTimeout is how long a real camera may play without delivering an
// access unit before the hub warns that it has no decodable video.
const silentCameraTimeout = 10 * time.Second

// askFunc shows a prompt and returns one line that the owner typed, without
// echoing it. It returns io.EOF when there is no more input.
type askFunc func(prompt string) (string, error)

// connectFunc connects to a real camera; it is source.NewRTSP.
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

// addRealCameras asks for the URLs of up to maxRealCameras cameras, connects to
// each and adds it to the catalog as real-1, real-2 and so on. An empty line
// ends the prompts. A camera that does not connect is reported without its URL
// and asked for again under the same name, so that a typo costs nothing.
func addRealCameras(ctx context.Context, cat *catalog, ask askFunc, out io.Writer, connect connectFunc) error {
	for cat.real < maxRealCameras {
		id := fmt.Sprintf("real-%d", cat.real+1)
		line, err := ask(fmt.Sprintf("Adresa RTSP a camerei %s (nu se afișează ce tastați; Enter gol = gata): ", id))
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("citirea adresei camerei %s: %w", id, err)
		}
		rawURL := strings.TrimSpace(line)
		if rawURL == "" {
			return nil
		}
		src, err := connect(ctx, id, rawURL)
		if err != nil {
			_, _ = fmt.Fprintf(out, "Conectarea la camera %s a eșuat: %v\n"+
				"Verificați adresa (utilizator, parolă, port, cale) sau apăsați Enter pentru a continua fără ea.\n", id, err)
			continue
		}
		cat.addReal(src)
		_, _ = fmt.Fprintf(out, "Camera %s este conectată (%s).\n", id, src.Codec())
	}
	return nil
}

// warnWhenSilent writes a warning to out when src has delivered no access unit
// timeout after the call: a camera that answers RTSP but sends no video that
// the hub can decode otherwise stays silent, and its tile in the app stays
// black. The warning names the camera by id only. It returns at once when the
// source already has a frame, when the first one arrives, when the source ends
// (its own log says why) or when ctx is done.
func warnWhenSilent(ctx context.Context, out io.Writer, id string, src source.Source, timeout time.Duration) {
	cached, live, cancel := src.Subscribe()
	defer cancel()
	if len(cached) > 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-live:
	case <-ctx.Done():
	case <-timer.C:
		_, _ = fmt.Fprintf(out, "Atenție: camera %s răspunde, dar nu a trimis niciun cadru video decodabil în %g secunde.\n"+
			"Verificați că fluxul ei este H.264 sau H.265 (nu MJPEG și nu doar audio) și că adresa indică fluxul video.\n",
			id, timeout.Seconds())
	}
}
