// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// The rules of §7.6 for bulk channels: the hub opens bulk/<call_id>,
// reliable and ordered, at most four at once, for a call's bytes. §7.6 has
// the hub's close mark their end; here the client closes the channel once
// it has every byte (the reply names the range's length), and the hub side
// only after closeGrace: when the hub side reset the stream right after
// its last byte, Chrome now and then lost the bytes of the next bulk
// channel, which opened while that reset was under way (1 local run in 7,
// Chrome 155).
const (
	// MaxBulkChannels is how many bulk channels a session holds open at
	// once.
	MaxBulkChannels = 4
	// maxMessage bounds a message: 64 KiB, or less when the peer's
	// max-message-size is smaller.
	maxMessage = 64 << 10
	// highWater and lowWater are the sender's backpressure: it pauses with
	// more than 1 MiB buffered and resumes once the buffer is down to
	// 256 KiB (bufferedamountlow).
	highWater = 1 << 20
	lowWater  = 256 << 10
)

// errTooManyBulk refuses a call that would open a fifth bulk channel; the
// client reads it as resource_exhausted.
var errTooManyBulk = errors.New("resource_exhausted: 4 bulk channels are open")

// messageSize is a bulk message's largest size: min(64 KiB, the negotiated
// max-message-size).
func messageSize(maxMessageSize uint32) int {
	return int(min(maxMessage, maxMessageSize))
}

// bulkSet counts a session's open bulk channels.
type bulkSet struct {
	mu   sync.Mutex
	open int
}

// acquire takes a bulk channel, or refuses a fifth with errTooManyBulk.
func (b *bulkSet) acquire() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open >= MaxBulkChannels {
		return errTooManyBulk
	}
	b.open++
	return nil
}

// release gives a bulk channel back once its call has ended.
func (b *bulkSet) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.open--
}

// channel is what the sender needs of a data channel; a Pion DataChannel
// has it.
type channel interface {
	Send([]byte) error
	BufferedAmount() uint64
	SetBufferedAmountLowThreshold(uint64)
	OnBufferedAmountLow(func())
}

// send sends the n bytes of src at off over ch in messages of at most size
// bytes, in order. Before each message it pauses while more than highWater
// bytes are buffered, until bufferedamountlow says that the buffer is down
// to lowWater; it gives up when ctx ends.
func send(ctx context.Context, ch channel, src io.ReaderAt, off, n int64, size int) error {
	low := make(chan struct{}, 1)
	ch.SetBufferedAmountLowThreshold(lowWater)
	ch.OnBufferedAmountLow(func() {
		select {
		case low <- struct{}{}:
		default:
		}
	})
	buf := make([]byte, size)
	for end := off + n; off < end; {
		for ch.BufferedAmount() > highWater {
			select {
			case <-low:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		chunk := buf[:min(int64(size), end-off)]
		if err := readFullAt(src, chunk, off); err != nil {
			return fmt.Errorf("read the plaintext at %d: %w", off, err)
		}
		if err := ch.Send(chunk); err != nil {
			return err
		}
		off += int64(len(chunk))
	}
	return nil
}
