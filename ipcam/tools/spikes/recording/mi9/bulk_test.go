package mi9

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeChannel is a data channel that buffers what it is sent until the test
// drains it, as SCTP holds the bytes that the peer has not acknowledged.
type fakeChannel struct {
	mu        sync.Mutex
	buffered  uint64
	threshold uint64
	low       func()
	sent      [][]byte
	before    []uint64 // the buffered amount before each send
}

func (f *fakeChannel) Send(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.before = append(f.before, f.buffered)
	f.sent = append(f.sent, bytes.Clone(b))
	f.buffered += uint64(len(b))
	return nil
}

func (f *fakeChannel) BufferedAmount() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buffered
}

func (f *fakeChannel) SetBufferedAmountLowThreshold(th uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.threshold = th
}

func (f *fakeChannel) OnBufferedAmountLow(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.low = fn
}

// drain lets the buffer fall to n bytes, and fires bufferedamountlow as a
// browser and Pion do: when the amount falls to the threshold or below it
// from above.
func (f *fakeChannel) drain(n uint64) {
	f.mu.Lock()
	crossed := f.buffered > f.threshold && n <= f.threshold
	f.buffered = n
	low := f.low
	f.mu.Unlock()
	if crossed && low != nil {
		low()
	}
}

func (f *fakeChannel) sends() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// waitPaused waits until the sender has stopped with more than highWater
// bytes buffered, and checks that it sends nothing more for a while.
func (f *fakeChannel) waitPaused(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.BufferedAmount() <= highWater {
		if time.Now().After(deadline) {
			t.Fatalf("the sender never buffered more than %d bytes", highWater)
		}
		time.Sleep(time.Millisecond)
	}
	n := f.sends()
	time.Sleep(50 * time.Millisecond)
	if m := f.sends(); m != n {
		t.Fatalf("the sender went on with %d bytes buffered: %d sends, then %d", f.BufferedAmount(), n, m)
	}
	return n
}

// TestBulk_FollowsSection76Rules (§7.6, C19): a bulk message is at most
// min(64 KiB, the negotiated max-message-size); a fifth bulk channel open at
// once is refused; the sender pauses once more than 1 MiB is buffered, stays
// paused while the buffer drains to 512 KiB, resumes once it is below
// 256 KiB, and sends every byte, in order.
func TestBulk_FollowsSection76Rules(t *testing.T) {
	for mms, want := range map[uint32]int{262144: 65536, 65536: 65536, 65535: 65535, 16384: 16384} {
		if got := messageSize(mms); got != want {
			t.Errorf("messageSize(%d) = %d, want %d", mms, got, want)
		}
	}

	var set bulkSet
	for i := range MaxBulkChannels {
		if err := set.acquire(); err != nil {
			t.Fatalf("bulk channel %d of %d refused: %v", i+1, MaxBulkChannels, err)
		}
	}
	if err := set.acquire(); !errors.Is(err, errTooManyBulk) {
		t.Errorf("a fifth bulk channel at once: %v, want %v", err, errTooManyBulk)
	}
	set.release()
	if err := set.acquire(); err != nil {
		t.Errorf("a bulk channel after one closed: %v", err)
	}

	src := make([]byte, 5<<20)
	for i := range src {
		src[i] = byte(i * 7)
	}
	ch := &fakeChannel{}
	const size = 16384
	done := make(chan error, 1)
	go func() { done <- send(t.Context(), ch, bytes.NewReader(src), 0, int64(len(src)), size) }()

	paused := ch.waitPaused(t)
	ch.drain(512 << 10)
	time.Sleep(50 * time.Millisecond)
	if n := ch.sends(); n != paused {
		t.Fatalf("the sender resumed with 512 KiB buffered: %d sends, then %d", paused, n)
	}
	ch.drain(200 << 10)
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("send = %v", err)
			}
			ch.mu.Lock()
			defer ch.mu.Unlock()
			if ch.threshold != lowWater {
				t.Errorf("the low threshold is %d, want %d", ch.threshold, lowWater)
			}
			if b := ch.before[paused]; b > lowWater {
				t.Errorf("the first send after the pause came with %d bytes buffered, want at most %d", b, lowWater)
			}
			for i, b := range ch.before {
				if b > highWater {
					t.Errorf("send %d came with %d bytes buffered, over %d", i, b, highWater)
				}
				if len(ch.sent[i]) > size {
					t.Errorf("message %d is %d bytes, over %d", i, len(ch.sent[i]), size)
				}
			}
			if got := bytes.Join(ch.sent, nil); !bytes.Equal(got, src) {
				t.Errorf("sent %d bytes that differ from the %d of the source", len(got), len(src))
			}
			return
		case <-time.After(time.Millisecond):
			if ch.BufferedAmount() > highWater {
				ch.drain(0)
			}
		}
	}
}

// TestBulk_StopsWithItsContext: a sender paused on a full buffer gives up
// when its context ends, as when the peer goes away.
func TestBulk_StopsWithItsContext(t *testing.T) {
	ch := &fakeChannel{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- send(ctx, ch, bytes.NewReader(make([]byte, 4<<20)), 0, 4<<20, 65536) }()
	ch.waitPaused(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("send = %v, want %v", err, context.Canceled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the paused sender did not stop with its context")
	}
}
