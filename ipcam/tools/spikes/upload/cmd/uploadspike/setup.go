// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/sdconfirm"
)

// The Google client types of Phase 0, in the order that setup asks them.
var googleTypes = []string{"android", "desktop", "tv", "web"}

// client is an OAuth client as the control endpoint takes it; Secret only
// for Desktop and TV.
type client struct {
	ID     string `json:"id"`
	Secret string `json:"secret,omitempty"`
}

// clients is the control endpoint's Clients.
type clients struct {
	Google    map[string]client `json:"google"`
	Microsoft *client           `json:"microsoft,omitempty"`
}

// normalize drops the secrets that the public clients do not take: the
// Android client's and the Web client's (the web reader's token comes
// from Google Identity Services, P21), as their downloaded JSON may carry
// one.
func (c clients) normalize() {
	for _, t := range []string{"android", "web"} {
		if cl, ok := c.Google[t]; ok {
			cl.Secret = ""
			c.Google[t] = cl
		}
	}
}

// check reports what the clients lack: each Google type's ID, and the
// Desktop and TV secrets.
func (c clients) check() error {
	var missing []string
	for _, t := range googleTypes {
		cl, ok := c.Google[t]
		if !ok || cl.ID == "" {
			missing = append(missing, t)
			continue
		}
		if (t == "desktop" || t == "tv") && cl.Secret == "" {
			missing = append(missing, t+" (secret)")
		}
	}
	if len(missing) > 0 {
		return ownerError(fmt.Sprintf(msgSetupClientsWrong, strings.Join(missing, ", ")))
	}
	return nil
}

// runSetup gives the hub the OAuth clients (P46): on the PC through the
// setup page, on the Pi (and with --terminal) in the terminal, secrets
// without echo; on Linux it first asks the SD-card confirmation when rec/
// is on an SD card and the run state has no answer yet (P02). The clients
// go to the hub's control endpoint, which seals them; nothing is kept on
// disk. Then the hub goes back to the autostart. Another command's
// handover, while it waits for the clients or once they are in, gets the
// interactive holder's answer (Handover): the scan, the hub's stop, the
// lock's release and exit 0.
func runSetup(ctx context.Context, e *env, args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	terminal := flags.Bool("terminal", false, "")
	if err := cli.ParseFlags(flags, args, e.out, usage); err != nil {
		return err
	}
	ctx, stop := signalContext(ctx)
	defer stop()
	inTerminal := *terminal || runtime.GOOS != "windows"
	if runtime.GOOS == "linux" {
		if err := confirmSdCard(ctx, e); err != nil {
			return err
		}
	}
	h, err := acquire(ctx, e, "uploadspike setup")
	if err != nil {
		return err
	}
	defer h.giveBack()
	if err := h.startHub(ctx); err != nil {
		return err
	}
	if !inTerminal {
		h.setupHandler = h.setupPageHandler()
	}
	if err := h.serve(ctx); err != nil {
		return err
	}
	if inTerminal {
		err = h.setupTerminal(ctx)
	} else {
		err = h.setupPage(ctx)
	}
	switch {
	case errors.Is(err, errHandedOver):
		h.handBack(ctx)
		return nil
	case err != nil:
		return err
	}
	e.printf(msgSetupSaved)
	select {
	case <-h.handoverRequested():
		h.handBack(ctx)
	default:
	}
	return nil
}

// setupTerminal asks the clients in the terminal and gives them to the
// hub; a handover while it asks ends it with errHandedOver, a signal with
// ctx's error.
func (h *holder) setupTerminal(ctx context.Context) error {
	type answer struct {
		c   clients
		err error
	}
	got := make(chan answer, 1)
	go func() {
		c, err := readClients(h.e)
		got <- answer{c, err}
	}()
	select {
	case a := <-got:
		if a.err != nil {
			return a.err
		}
		return saveClients(ctx, h, a.c)
	case <-h.handoverRequested():
		h.e.restoreTerminal()
		return errHandedOver
	case <-ctx.Done():
		h.e.restoreTerminal()
		return ctx.Err()
	}
}

// saveClients hands the clients to the hub and keeps their IDs that the
// web reader needs in the run state (no secret).
func saveClients(ctx context.Context, h *holder, c clients) error {
	c.normalize()
	if err := c.check(); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := h.ctl.SetClients(ctx, raw); err != nil {
		return err
	}
	return runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		s.WebClientID = c.Google["web"].ID
		s.EntraClientID = ""
		if c.Microsoft != nil {
			s.EntraClientID = c.Microsoft.ID
		}
		return nil
	})
}

// confirmSdCard asks the SD-card confirmation once per device: when diag
// host reports rec/ on an SD card and the run state has no answer.
func confirmSdCard(ctx context.Context, e *env) error {
	st, err := runstate.Load(e.statePath())
	if err != nil || st.Sd != nil {
		return err
	}
	host, err := hostinfo.Read(ctx, e.hubBin, recDir(e))
	if err != nil {
		return err
	}
	if host.Storage != "sd" {
		return nil
	}
	e.printf(setupSdWarnFirst)
	conf, ok := sdconfirm.Ask(e.in, e.out, e.now)
	if !ok {
		return exitError(1)
	}
	return runstate.Update(e.statePath(), func(s *runstate.State) error {
		s.Sd = &conf
		return nil
	})
}

// recDir is the folder whose storage diag host reports: the hub's rec/,
// or, before the hub made it, the nearest folder above it that exists.
func recDir(e *env) string {
	for d := filepath.Join(e.dataDir(), "rec"); ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || filepath.Dir(d) == d {
			return d
		}
	}
}

// readClients asks the clients in the terminal: for each Google type the
// pasted JSON file that Google gave at its creation, or its ID and then,
// for Desktop and TV, its secret without echo; then the Microsoft client's
// ID, empty when A5 did not pass.
func readClients(e *env) (clients, error) {
	c := clients{Google: map[string]client{}}
	e.printf(setupIntro)
	for _, t := range googleTypes {
		_, _ = fmt.Fprintf(e.out, setupClientID, t)
		line, err := readAnswer(e.in)
		if err != nil && line == "" {
			return c, ownerError(fmt.Sprintf(msgSetupClientsWrong, t))
		}
		cl, isJSON := parseClientJSON(line)
		if !isJSON {
			cl = client{ID: strings.TrimSpace(line)}
		}
		if (t == "desktop" || t == "tv") && cl.Secret == "" {
			_, _ = fmt.Fprintf(e.out, setupSecret, t)
			secret, err := readSecret(e)
			if err != nil && secret == "" {
				return c, ownerError(fmt.Sprintf(msgSetupClientsWrong, t+" (secret)"))
			}
			cl.Secret = strings.TrimSpace(secret)
		}
		c.Google[t] = cl
	}
	_, _ = fmt.Fprint(e.out, setupMicrosoft)
	if line, _ := readAnswer(e.in); strings.TrimSpace(line) != "" {
		c.Microsoft = &client{ID: strings.TrimSpace(line)}
	}
	return c, nil
}

// parseClientJSON reads a Google client JSON ({"installed": …} or {"web":
// …}), if line is one.
func parseClientJSON(line string) (client, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return client{}, false
	}
	var f map[string]struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if json.Unmarshal([]byte(line), &f) != nil {
		return client{}, false
	}
	for _, k := range []string{"installed", "web"} {
		if v, ok := f[k]; ok {
			return client{ID: v.ClientID, Secret: v.ClientSecret}, true
		}
	}
	return client{}, false
}

// readAnswer reads one answer from in, a byte at a time: a line, or a JSON
// object that spans several lines, read until its braces close.
func readAnswer(in io.Reader) (string, error) {
	var b strings.Builder
	depth, started := 0, false
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n == 1 {
			c := buf[0]
			switch c {
			case '{':
				depth, started = depth+1, true
			case '}':
				depth--
			}
			if c == '\n' && (!started || depth <= 0) {
				return strings.TrimRight(b.String(), "\r"), nil
			}
			b.WriteByte(c)
		}
		if err != nil {
			return strings.TrimRight(b.String(), "\r"), err
		}
	}
}

// readSecret reads a secret without echo from a terminal, else a line.
// While it reads, the terminal's state is kept, so that a handover or a
// signal that ends setup mid-secret gives the owner's terminal its echo
// back (restoreTerminal).
func readSecret(e *env) (string, error) {
	if f, ok := e.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) { //nolint:gosec // a file descriptor
		fd := int(f.Fd()) //nolint:gosec // a file descriptor
		if old, err := term.GetState(fd); err == nil {
			e.keepTerminal(func() { _ = term.Restore(fd, old) })
			defer e.keepTerminal(nil)
		}
		b, err := term.ReadPassword(fd)
		_, _ = fmt.Fprintln(e.out)
		return string(b), err
	}
	return readAnswer(e.in)
}

// keepTerminal records how to give the terminal its state back while a
// secret is read without echo; nil once the read ended.
func (e *env) keepTerminal(restore func()) {
	e.termMu.Lock()
	defer e.termMu.Unlock()
	e.termRestore = restore
}

// restoreTerminal gives the terminal its state back if a secret's read
// without echo is cut.
func (e *env) restoreTerminal() {
	e.termMu.Lock()
	defer e.termMu.Unlock()
	if e.termRestore != nil {
		e.termRestore()
		e.termRestore = nil
	}
}

// setupPageHandler serves the setup page and takes its clients.
func (h *holder) setupPageHandler() http.HandlerFunc {
	h.setupDone = make(chan struct{}, 1)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			renderPage(w, "setup.html", pageData{Title: pageSetupTitle, Lines: googleTypes})
			return
		}
		var c clients
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			writeError(w, http.StatusBadRequest, pageSetupBadBody)
			return
		}
		if err := c.check(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := saveClients(r.Context(), h, c); err != nil {
			h.e.printf("%s %v", msgError, err)
			writeError(w, http.StatusBadGateway, pageSetupHubRefused)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
		select {
		case h.setupDone <- struct{}{}:
		default:
		}
	}
}

// setupPage opens the setup page in the browser and returns once the
// page's clients are in the hub.
func (h *holder) setupPage(ctx context.Context) error {
	openBrowser(h.e, fmt.Sprintf("http://localhost:%d/setup", h.e.port))
	h.e.printf(msgSetupWaitPage)
	select {
	case <-h.setupDone:
		return nil
	case <-h.handoverRequested():
		return errHandedOver
	case <-ctx.Done():
		return ctx.Err()
	}
}

// writeError answers a JSON error.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
