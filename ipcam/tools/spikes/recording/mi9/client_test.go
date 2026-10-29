package mi9

import (
	"context"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// firedCtx is a measurement's wait whose deadline passes when the test
// fires it, at a chosen point of a call that the hub side holds.
type firedCtx struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newFiredCtx(parent context.Context) *firedCtx {
	return &firedCtx{Context: parent, done: make(chan struct{})}
}

func (c *firedCtx) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *firedCtx) Done() <-chan struct{} { return c.done }

func (c *firedCtx) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *firedCtx) fire() { c.once.Do(func() { close(c.done) }) }

// holdCall makes the hub side serve a measurement's bulk ranges up to its
// nth (1 is the whole transfer, 2 the first seek), then send only the
// first keep(size) bytes of that one and hold it until the hub side
// closes. fire, when set, ends the measurement's wait hold after the hold
// began.
func holdCall(t *testing.T, n int64, keep func(size int64) int64, hold time.Duration, fire func()) {
	t.Helper()
	realSend := sendBulk
	t.Cleanup(func() { sendBulk = realSend })
	var calls atomic.Int64
	sendBulk = func(ctx context.Context, ch channel, src io.ReaderAt, off, size int64, message int) error {
		if calls.Add(1) != n {
			return realSend(ctx, ch, src, off, size, message)
		}
		if err := realSend(ctx, ch, src, off, keep(size), message); err != nil {
			return err
		}
		if fire != nil {
			time.AfterFunc(hold, fire)
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

// TestMeasure_CutDuringTheSeeksKeepsThem (G27, G25): when the measurement's
// wait ends during the seeks, Measure keeps what it measured, as Partial:
// the whole transfer's throughput, the seeks timed by then, and the seek
// under way, here the third:
//   - its IDR had not arrived 2 s into it: its time so far, a lower bound
//     over 1.5 s, is the leg's last seek (LastSeekCut), a measured failure
//     that JudgeBulk and Judge fail, «pică», not «fără verdict»;
//   - its IDR had arrived, the rest of its part had not: its time to the
//     IDR is a seek like the others, and the leg, with 3 of 20 seeks all
//     within 1.5 s, has no verdict;
//   - the wait ended 0.1 s into it: that time decides nothing, so it is
//     dropped, and the leg, with 2 of 20 seeks, has no verdict.
func TestMeasure_CutDuringTheSeeksKeepsThem(t *testing.T) {
	path, id, _ := testSegment(t, 10)
	for _, c := range []struct {
		name    string
		keep    func(size int64) int64
		hold    time.Duration
		seeks   int
		cut     bool
		verdict results.Verdict
	}{
		{"its IDR not there 2 s into it", func(int64) int64 { return 0 }, 2 * time.Second, 3, true, results.Fail},
		{"its IDR there, its part not", func(size int64) int64 { return size - 1 }, 2 * time.Second, 3, false, results.Incomplete},
		{"the wait ended 0.1 s into it", func(int64) int64 { return 0 }, 100 * time.Millisecond, 2, false, results.Incomplete},
	} {
		t.Run(c.name, func(t *testing.T) {
			seg, err := OpenSegment(path, id)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = seg.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			wait := newFiredCtx(ctx)
			holdCall(t, 4, c.keep, c.hold, wait.fire)
			cl, h, err := newPair(ctx, seg, LAN, nil, options{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cl.Close(); _ = h.Close() }()

			r, err := cl.Measure(wait)
			if err != nil {
				t.Fatalf("Measure cut during its seeks = %v, want what it measured", err)
			}
			r.Transport = LAN
			p := r.Partial
			if p != nil {
				t.Logf("Measure: %s Mbps, seeks %v ms, partial %+v", mbps(r.ThroughputMbps), r.SeekMs, *p)
			}
			if p == nil || p.ReceivedBytes != seg.Size() || p.SizeBytes != seg.Size() || p.ElapsedMs <= 0 {
				t.Fatalf("Measure = %+v, partial %+v; want the whole transfer of %d bytes, partial", r, p, seg.Size())
			}
			if want := float64(seg.Size()*8) / p.ElapsedMs / 1e3; math.Abs(r.ThroughputMbps-want) > want/1e3 {
				t.Errorf("throughput %v Mbps, want %v: the whole transfer's", r.ThroughputMbps, want)
			}
			if len(r.SeekMs) != c.seeks || p.LastSeekCut != c.cut {
				t.Fatalf("seeks %v, LastSeekCut %v; want %d seeks, LastSeekCut %v", r.SeekMs, p.LastSeekCut, c.seeks, c.cut)
			}
			if last := r.SeekMs[len(r.SeekMs)-1]; c.cut && last < float64(c.hold.Milliseconds()) {
				t.Errorf("the cut seek's time is %v ms, want at least the %v it was held", last, c.hold)
			}
			if !c.cut && slices.Max(r.SeekMs) > MaxSeekMs {
				t.Errorf("seeks %v ms on the loopback, want each within %d ms", r.SeekMs, MaxSeekMs)
			}

			pass, reasons := JudgeBulk(r)
			want := string(results.Incomplete)
			if c.verdict == results.Fail {
				want = "pică → " + PlanB
			}
			if pass || len(reasons) < 2 || reasons[len(reasons)-1] != want {
				t.Errorf("JudgeBulk = %v %q, want «%s» last", pass, reasons, want)
			}
			if c.cut && !strings.Contains(strings.Join(reasons, "\n"), "nici după") {
				t.Errorf("JudgeBulk's reasons %q do not say the seek had not arrived «nici după» its time", reasons)
			}
			j := Judge([]results.Envelope{pcLAN, mi9Env("pi", LANSet, Leg(r, SourceLAN)), relay, chrome})
			if j.Verdict != c.verdict {
				t.Errorf("Judge = %q, reasons %q; want %q", j.Verdict, j.Reasons, c.verdict)
			}
		})
	}
}
