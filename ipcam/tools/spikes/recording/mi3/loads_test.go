package mi3

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// fakeTime is a clock that sleeping moves.
type fakeTime struct{ now time.Time }

func (c *fakeTime) Now() time.Time          { return c.now }
func (c *fakeTime) Sleep(d time.Duration)   { c.now = c.now.Add(d) }
func (c *fakeTime) since(t time.Time) int64 { return c.now.Sub(t).Milliseconds() }

// TestLoads_UploadPacedToTarget (G20): the upload goes out at 10 Mbps on H3
// and H1, and at the cameras' total throughput, 4 Mbps each, on H1b and
// H1c; the paced body takes the time its bytes need at that rate, and no
// less.
func TestLoads_UploadPacedToTarget(t *testing.T) {
	cases := []struct {
		tier    string
		cameras int
		bps     int64
		ms      int64 // to send 2,500,000 bytes
	}{
		{"H3", 8, 10_000_000, 2000},
		{"H1", 8, 10_000_000, 2000},
		{"H1b", 4, 16_000_000, 1250},
		{"H1c", 2, 8_000_000, 2500},
	}
	for _, c := range cases {
		if got := UploadBps(c.tier, c.cameras); got != c.bps {
			t.Errorf("UploadBps(%s, %d) = %d, want %d", c.tier, c.cameras, got, c.bps)
			continue
		}
		clock := &fakeTime{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
		start := clock.now
		body := newPaced(bytes.NewReader(make([]byte, 2_500_000)), c.bps, clock)
		buf := make([]byte, 100_000)
		var sent int64
		for {
			n, err := body.Read(buf)
			sent += int64(n)
			// Never ahead of the rate: what went out by now fits in the time.
			if ahead := sent*8*1000/c.bps - clock.since(start); ahead > 0 {
				t.Fatalf("%s: %d bytes out after %d ms, %d ms ahead of %d bit/s", c.tier, sent, clock.since(start), ahead, c.bps)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if sent != 2_500_000 || clock.since(start) != c.ms {
			t.Errorf("%s: %d bytes in %d ms, want 2,500,000 in %d ms", c.tier, sent, clock.since(start), c.ms)
		}
	}
}

// TestLoads_RunUntilTheirEnd (G20): the playback reads the encrypted
// segment at 1×, the export decrypts and re-encrypts it at full speed, the
// upload encrypts the clear one and PUTs it over TLS into the sink, each in
// a loop until its context ends, which is no error; each reports the bytes
// it moved, and the sink counted what the upload sent.
func TestLoads_RunUntilTheirEnd(t *testing.T) {
	fx, err := makeFixture(t.TempDir(), 256<<10)
	if err != nil {
		t.Fatal(err)
	}
	cert, der, err := controller.NewSinkCertificate()
	if err != nil {
		t.Fatal(err)
	}
	sink := controller.NewSink()
	srv := httptest.NewUnstartedServer(sink)
	srv.TLS = controller.SinkServerTLS(cert)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	specs := map[string]LoadSpec{
		LoadPlayback: {Segment: fx.segment, Identity: fx.identity},
		LoadExport:   {Segment: fx.segment, Identity: fx.identity},
		LoadUpload:   {Plain: fx.plain, URL: srv.URL + "/sink/", SinkCert: der, Bps: 8_000_000},
	}
	for kind, spec := range specs {
		ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
		started := false
		report, err := RunLoad(ctx, kind, spec, func(LoadReport) error {
			started = true
			return nil
		})
		cancel()
		if err != nil || !started || report.Bytes <= 0 || report.Seconds < 1 {
			t.Errorf("%s: %+v, started %v, %v; want bytes moved until its end", kind, report, started, err)
		}
		t.Logf("%s: %d bytes in %.1f s", kind, report.Bytes, report.Seconds)
		switch kind {
		case LoadPlayback: // 4 Mbps: 750,000 bytes in 1.5 s, and the reads' rounding
			if report.Bytes > 800_000 {
				t.Errorf("the playback read %d bytes in 1.5 s, faster than 1×", report.Bytes)
			}
		case LoadExport: // full speed: the segment several times over
			if report.Bytes < 2*256<<10 {
				t.Errorf("the export moved %d bytes in 1.5 s", report.Bytes)
			}
		case LoadUpload: // 8 Mbps: a 256 KiB segment's PUT takes ≈ 0.26 s
			if got := sink.Bytes("upload-1") + sink.Bytes("upload-2"); got <= 0 || got > report.Bytes {
				t.Errorf("the sink counted %d bytes of the first PUTs, the upload sent %d", got, report.Bytes)
			}
		}
	}
	if _, err := RunLoad(t.Context(), "stream", LoadSpec{}, nil); err == nil {
		t.Error("an unknown load ran")
	}
}
