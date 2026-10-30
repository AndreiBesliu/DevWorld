// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package link is the protocol between recspike, on the host under test,
// and reccontroller (T43): JSON lines over TCP, which reccontroller accepts
// on Port, and the liveness beacon, 16-byte UDP datagrams that recspike sends
// to BeaconPort. Only reccontroller listens (R08); recspike connects out.
package link

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// The controller's ports: the link (TCP) and the beacon (UDP).
const (
	Port       = 7081
	BeaconPort = 7082
)

// The bounds of one message's line (security-2). The link takes any peer
// on the LAN, so a line is read only as far as its type allows: a results
// folder (results_tar) is a few MiB of envelopes in base64; a results file
// is what MI9 sends, at most 64 KiB of data in base64; the messages that
// carry WebRTC descriptions or ipcam-hub's events stay under 1 MiB; every
// other message is small.
const (
	maxTarLine     = 8 << 20
	maxResultsLine = 128 << 10
	maxLargeLine   = 1 << 20
	maxSmallLine   = 64 << 10
)

// maxLine is the bound of a line of the message type typ.
func maxLine(typ string) int {
	switch typ {
	case "results_tar":
		return maxTarLine
	case "results":
		return maxResultsLine
	case "session", "report", "mi3_offers", "mi3_answers", "mi9_offer", "mi9_answer":
		return maxLargeLine
	}
	return maxSmallLine
}

// linePrefix starts every line that Send writes: its type comes first.
var linePrefix = []byte(`{"type":"`)

// lineType is the message type that the start of line names, as Send
// writes it; ok is false while the start does not name one.
func lineType(line []byte) (typ string, ok bool) {
	rest, ok := bytes.CutPrefix(line, linePrefix)
	if !ok {
		return "", false
	}
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return "", false
	}
	return string(rest[:end]), true
}

// Conn is one end of a link. Send may be called from several goroutines;
// Receive from one at a time.
type Conn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex // serialises Send
}

// NewConn wraps an accepted or dialled connection.
func NewConn(c net.Conn) *Conn {
	return &Conn{c: c, r: bufio.NewReaderSize(c, 64<<10)}
}

// Dial connects to a controller at addr, a host:port, over IPv4.
func Dial(ctx context.Context, addr string) (*Conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp4", addr)
	if err != nil {
		return nil, err
	}
	return NewConn(c), nil
}

// Close closes the connection.
func (c *Conn) Close() error { return c.c.Close() }

// Send writes m as one line.
func (c *Conn) Send(m Message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("link: encode %s: %w", m.Type(), err)
	}
	typ, err := json.Marshal(m.Type())
	if err != nil {
		return err
	}
	// {"type":"<type>"} then, when the message has fields, a comma and the
	// fields with the closing brace of body.
	line := append([]byte(`{"type":`), typ...)
	if len(body) > len("{}") {
		line = append(line, ',')
		line = append(line, body[1:]...)
	} else {
		line = append(line, '}')
	}
	line = append(line, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.c.Write(line)
	return err
}

// Receive reads the next message.
func (c *Conn) Receive() (Message, error) {
	line, err := c.readLine()
	if err != nil {
		return nil, err
	}
	var head struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, fmt.Errorf("link: not a message: %w", err)
	}
	if head.Type == nil {
		return nil, errors.New(`link: a message without "type"`)
	}
	if typ, _ := lineType(line); typ != *head.Type || len(line) > maxLine(typ) {
		return nil, fmt.Errorf("link: a %q message that does not start with its type, or over its %d bytes", *head.Type, maxLine(*head.Type))
	}
	decode, ok := decoders[*head.Type]
	if !ok {
		return nil, fmt.Errorf("link: unknown message type %q", *head.Type)
	}
	m, err := decode(line)
	if err != nil {
		return nil, fmt.Errorf("link: %s: %w", *head.Type, err)
	}
	return m, nil
}

// readLine reads up to and including the next newline, at most the bound
// of the type that the line's start names (maxLine), or of the small
// messages until it names one.
func (c *Conn) readLine() ([]byte, error) {
	var line []byte
	for {
		chunk, err := c.r.ReadSlice('\n')
		line = append(line, chunk...)
		limit := maxSmallLine
		if typ, ok := lineType(line); ok {
			limit = maxLine(typ)
		}
		if len(line) > limit {
			return nil, fmt.Errorf("link: a message longer than %d bytes", limit)
		}
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

// PullResults asks the far side for its results folder and unpacks it into
// dir, which it creates; files already there with other names stay.
func (c *Conn) PullResults(dir string) error {
	if err := c.Send(PullResults{}); err != nil {
		return err
	}
	m, err := c.Receive()
	if err != nil {
		return err
	}
	tarball, ok := m.(ResultsTar)
	if !ok {
		return fmt.Errorf("link: got %s, want results_tar", m.Type())
	}
	return Untar(dir, tarball.Data)
}

// SendResultsTar answers PullResults with the results folder dir; a folder
// that does not exist yet is sent empty.
func (c *Conn) SendResultsTar(dir string) error {
	data, err := TarDir(dir)
	if err != nil {
		return err
	}
	return c.Send(ResultsTar{Data: data})
}

// TarDir packs the regular files under dir, by their slash paths relative to
// it, in lexical order. A lock file (*.lock, the run state's) is no result,
// and Windows keeps a locked one unreadable: it stays behind.
func TarDir(dir string) ([]byte, error) {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == dir {
				return fs.SkipAll
			}
			return err
		}
		if !d.Type().IsRegular() || strings.HasSuffix(d.Name(), ".lock") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // a file of the results folder
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(data))}
		if err := w.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("link: pack %s: %w", filepath.Base(dir), err)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Untar unpacks a TarDir archive from the far side into dir, which it
// creates. Every entry must be a regular file or a folder with a local
// path; the files are written through an os.Root, so that nothing lands
// outside dir.
func Untar(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	r := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("link: unpack: %w", err)
		}
		name := filepath.FromSlash(hdr.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("link: unpack: %q is not a path inside the results folder", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if parent := path.Dir(hdr.Name); parent != "." {
				if err := root.MkdirAll(filepath.FromSlash(parent), 0o750); err != nil {
					return err
				}
			}
			if err := writeEntry(root, name, r); err != nil {
				return err
			}
		default:
			return fmt.Errorf("link: unpack: %q is neither a file nor a folder", hdr.Name)
		}
	}
}

func writeEntry(root *os.Root, name string, r io.Reader) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// RunHash is FNV-64a of a run id: the first half of the run's beacons.
func RunHash(runID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(runID)) // a hash.Hash never fails to write
	return h.Sum64()
}

// BeaconPacket is one beacon of a run: RunHash(runID) and seq, big-endian.
func BeaconPacket(runID string, seq uint64) []byte {
	return binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(make([]byte, 0, 16), RunHash(runID)), seq)
}

// ParseBeacon reads a beacon datagram; ok is false for any other datagram.
func ParseBeacon(p []byte) (run, seq uint64, ok bool) {
	if len(p) != 16 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint64(p[:8]), binary.BigEndian.Uint64(p[8:]), true
}
