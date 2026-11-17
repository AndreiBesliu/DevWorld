// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package linkclient is a copied minimal client of c1's link protocol
// (g3-16): uploadspike says hello as the host to `reccontroller --local`
// and reads one session line, which gives it the RTSP base, the per-start
// credentials and the stream names of the simulated cameras that MI5
// records. JSON lines carry the type inside the object, as c1's link
// does, so the copy interoperates with the c1 reccontroller that ships in
// c2's bundle. It never imports c1's code (P01) and never logs a
// credential.
package linkclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// LocalAddr is reccontroller --local's link.
const LocalAddr = "127.0.0.1:7081"

// maxLine bounds a line of the link, as c1's.
const maxLine = 256 << 20

// helloTimeout bounds the hello and the session's line.
const helloTimeout = 30 * time.Second

// Dial says hello to the controller at addr, over tcp4, as the host
// hostPseudonym, and reads one session line; the connection then closes.
// Its errors carry no credential.
func Dial(ctx context.Context, addr, hostPseudonym string) (Session, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp4", addr)
	if err != nil {
		return Session{}, fmt.Errorf("linkclient: dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(helloTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	line, err := json.Marshal(hello{Type: "hello", Role: "host", Host: hostPseudonym, Version: "c1"})
	if err != nil {
		return Session{}, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return Session{}, fmt.Errorf("linkclient: hello: %w", err)
	}
	return readSession(bufio.NewReaderSize(conn, 64<<10))
}

// readSession reads lines until the session's, each at most maxLine.
func readSession(r *bufio.Reader) (Session, error) {
	for {
		line, err := readLine(r)
		if err != nil {
			return Session{}, err
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &head) != nil {
			return Session{}, errors.New("linkclient: a line that is not a JSON object")
		}
		if head.Type != "session" {
			continue
		}
		var s sessionLine
		if json.Unmarshal(line, &s) != nil || s.RTSPBase == "" || len(s.Streams) == 0 {
			return Session{}, errors.New("linkclient: a session line without its base or streams")
		}
		return Session{RTSPBase: s.RTSPBase, User: s.User, Pass: s.Pass, Streams: s.Streams}, nil
	}
}

// readLine reads one line of at most maxLine bytes.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := r.ReadSlice('\n')
		buf.Write(chunk)
		if buf.Len() > maxLine {
			return nil, fmt.Errorf("linkclient: a line over %d bytes", maxLine)
		}
		switch {
		case err == nil:
			return bytes.TrimRight(buf.Bytes(), "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return nil, errors.New("linkclient: the controller closed the link before the session")
		default:
			return nil, fmt.Errorf("linkclient: read: %w", err)
		}
	}
}
