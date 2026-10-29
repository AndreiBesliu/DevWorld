package mi9

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// lockedBuffer is a log that the hub side's goroutines write and the test
// reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureLog sends slog's default logger to a buffer for the test.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	log := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(log, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return log
}

// bulkStopped is the hub side's English warning for a transfer that stopped.
const bulkStopped = "mi9: a bulk channel stopped"

// TestHub_StoppedTransferLogsNothing (Task 11b review, M1): when the
// measurement's wait ends during the transfer, the client stops it and
// closes its bulk channel. That is a normal stop, and the hub side logs
// nothing in English on the owner's screen. A send that fails while the
// channel is still open is logged. The test closes the hub side only once
// it has decided whether to log (stopDecided), and, when the channel is to
// be open, ends the client's wait only then, so that each case takes the
// branch it names whatever the scheduling: the channel's state, never the
// hub's own stop (Task 12 review, M10).
func TestHub_StoppedTransferLogsNothing(t *testing.T) {
	path, id, _ := testSegment(t, 10)
	for _, c := range []struct {
		name string
		// send is the hub side's send of the transfer.
		send func(ctx context.Context, ch channel, src io.ReaderAt, off, size int64, message int) error
		// logs is whether the hub side logs the stop. When it does, the
		// channel is open as the hub side decides: the client's wait ends
		// only after the decision. Otherwise it ends after a second, during
		// the transfer, and the client closes the channel.
		logs bool
	}{
		{"the client stopped it", sendHalfThenAfterClose, false},
		{"the send failed with the channel open", func(context.Context, channel, io.ReaderAt, int64, int64, int) error {
			return errors.New("the test failed the send")
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			log := captureLog(t)
			seg, err := OpenSegment(path, id)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = seg.Close() }()
			realSend, realDecided := sendBulk, stopDecided
			t.Cleanup(func() { sendBulk, stopDecided = realSend, realDecided })
			decided := make(chan struct{})
			var once sync.Once
			sendBulk = c.send
			stopDecided = func() { once.Do(func() { close(decided) }) }
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			wait := newFiredCtx(ctx)
			cl, h, err := newPair(ctx, seg, LAN, nil, options{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cl.Close() }()
			if c.logs {
				go func() {
					select {
					case <-decided:
					case <-ctx.Done():
					}
					wait.fire()
				}()
			} else {
				time.AfterFunc(time.Second, wait.fire)
			}
			_, _ = cl.Measure(wait)
			select {
			case <-decided:
			case <-ctx.Done():
				t.Fatal("the hub side did not decide whether to log its stopped send")
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			if logged := strings.Contains(log.String(), bulkStopped); logged != c.logs {
				t.Errorf("logged %q: %v, want %v; the log:\n%s", bulkStopped, logged, c.logs, log.String())
			}
		})
	}
}

// sendHalfThenAfterClose sends the first half of the range, waits until
// the client has closed the bulk channel, then sends the rest, which fails
// as a send after the client's stop does.
func sendHalfThenAfterClose(ctx context.Context, ch channel, src io.ReaderAt, off, size int64, message int) error {
	if err := send(ctx, ch, src, off, size/2, message); err != nil {
		return err
	}
	dc, ok := ch.(*webrtc.DataChannel)
	if !ok {
		return errors.New("the bulk channel is not Pion's")
	}
	for dc.ReadyState() == webrtc.DataChannelStateOpen {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return send(ctx, ch, src, off+size/2, size-size/2, message)
}
