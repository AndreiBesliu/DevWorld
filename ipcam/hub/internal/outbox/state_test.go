package outbox

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// destStates returns the states that DestState reported for dest, in order.
func (h *harness) destStates(dest string) []DestState {
	var states []DestState
	for _, e := range h.obs.of("DestState") {
		if e.Dest == dest {
			states = append(states, e.State)
		}
	}
	return states
}

// lastAlert returns the alert of the last DestState reported for dest.
func (h *harness) lastAlert(dest string) string {
	var alert string
	for _, e := range h.obs.of("DestState") {
		if e.Dest == dest {
			alert = e.Alert
		}
	}
	return alert
}

// TestOutbox_RetryAfterHonoured (P26): a destination that asks to wait 90 s
// at the first failure is called again 90 s later, not after Backoff(1)'s
// 2 s.
func TestOutbox_RetryAfterHonoured(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindOneDrivePersonal, h.clk.Now)
	var once sync.Once
	f.setFault(func(_ context.Context, c fakeCall) error {
		var err error
		if c.Method == "BeginUpload" {
			once.Do(func() { err = fmt.Errorf("fake: 429: %w", storage.ErrRateLimited{RetryAfter: 90 * time.Second}) })
		}
		return err
	})
	p := h.enqueue(h.segment("a", 0, randomBytes(100, 1)), "dst-a", catalog.ClassContinuous)
	q := h.newQueue(map[string]storage.Destination{"dst-a": f}, func(o *Options) { o.Rand = func() float64 { return 0.999999 } })
	h.run(q, time.Second, 5*time.Minute, h.verified(p))

	begins := f.callsOf("BeginUpload")
	if len(begins) != 2 || begins[1].At.Sub(begins[0].At) != 90*time.Second {
		t.Fatalf("BeginUpload calls %+v, want two, 90 s apart", begins)
	}
	fails := h.obs.of("Failed")
	if len(fails) != 1 || fails[0].Reason != "rate_limited" || fails[0].Attempts != 1 ||
		fails[0].P.NextAttemptAtMs != begins[0].At.Add(90*time.Second).UnixMilli() {
		t.Errorf("Failed calls %+v, want one, rate_limited, due again 90 s after the first call", fails)
	}
}

// TestOutbox_RateLimitIsPerDestination (§9.9): a rate-limited destination
// waits; the others go on uploading meanwhile.
func TestOutbox_RateLimitIsPerDestination(t *testing.T) {
	h := newHarness(t)
	a := newFakeDest(storage.KindOneDrivePersonal, h.clk.Now)
	b := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
	var once sync.Once
	a.setFault(func(context.Context, fakeCall) error {
		var err error
		once.Do(func() { err = storage.ErrRateLimited{RetryAfter: 10 * time.Minute} })
		return err
	})
	var ps []catalog.Placement
	for i := range 3 {
		s := h.segment(fmt.Sprintf("s%d", i), time.Duration(i)*time.Minute, randomBytes(fakePart+i, byte(i)))
		ps = append(ps, h.enqueue(s, "dst-a", catalog.ClassContinuous), h.enqueue(s, "dst-b", catalog.ClassContinuous))
	}
	q := h.newQueue(map[string]storage.Destination{"dst-a": a, "dst-b": b})
	h.run(q, 10*time.Second, 15*time.Minute, h.verified(ps...))

	// Calls at the same instant as the first were made before its answer.
	first := a.callsOf("")[0].At
	for _, c := range a.callsOf("")[1:] {
		if c.At.After(first) && c.At.Before(first.Add(10*time.Minute)) {
			t.Errorf("dst-a was called %s after its rate limit: %+v", c.At.Sub(first), c)
		}
	}
	var bDone time.Time
	for _, e := range h.obs.of("Verified") {
		if e.Dest == "dst-b" && e.At.After(bDone) {
			bDone = e.At
		}
	}
	if bDone.IsZero() || !bDone.Before(first.Add(10*time.Minute)) {
		t.Errorf("dst-b's last placement was verified %s after dst-a's rate limit, want within its 10 min", bDone.Sub(first))
	}
}

// TestOutbox_DailyCapPauses24h (§9.9): Drive's daily cap pauses the
// destination for its ResetAfter, then one probe upload goes first, and its
// success makes the destination OK again.
func TestOutbox_DailyCapPauses24h(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
	var once sync.Once
	f.setFault(func(_ context.Context, c fakeCall) error {
		var err error
		if c.Method == "PutChunk" {
			once.Do(func() {
				err = fmt.Errorf("fake: 403 uploadLimitExceeded: %w", storage.ErrDailyCap{ResetAfter: 24 * time.Hour})
			})
		}
		return err
	})
	var ps []catalog.Placement
	for i := range 2 {
		ps = append(ps, h.enqueue(h.segment(fmt.Sprintf("s%d", i), time.Duration(i)*time.Minute, randomBytes(2*fakePart, byte(i))), "dst-a", catalog.ClassContinuous))
	}
	q := h.newQueue(map[string]storage.Destination{"dst-a": f})
	h.run(q, 10*time.Minute, 26*time.Hour, h.verified(ps...))

	if states := h.destStates("dst-a"); !slices.Equal(states, []DestState{StatePausedDailyCap, StateOK}) {
		t.Errorf("DestState %v, want PAUSED_DAILY_CAP, then OK", states)
	}
	capped := h.obs.of("DestState")[0]
	if capped.Alert != "Plafonul zilnic Google Drive a fost atins; urcarea se reia peste 24 h." {
		t.Errorf("the daily cap's alert is %q", capped.Alert)
	}
	if fails := h.obs.of("Failed"); len(fails) != 1 || fails[0].Reason != "daily_cap" || fails[0].P.State != catalog.PlacementPending {
		t.Errorf("Failed calls %+v, want one, daily_cap, left pending", fails)
	}
	calls := f.callsOf("")
	var after []fakeCall
	for _, c := range calls {
		if c.Method == "PutChunk" && c.At.After(capped.At) && c.At.Before(capped.At.Add(24*time.Hour)) {
			t.Errorf("a PutChunk %s into the pause: %+v", c.At.Sub(capped.At), c)
		}
		if !c.At.Before(capped.At.Add(24 * time.Hour)) {
			after = append(after, c)
		}
	}
	// One probe: the first upload after the pause commits before the next
	// one begins, and only its success makes the destination OK.
	commit := slices.IndexFunc(after, func(c fakeCall) bool { return c.Method == "Commit" })
	if len(after) == 0 || after[0].Method != "Resume" && after[0].Method != "BeginUpload" || commit < 0 ||
		slices.ContainsFunc(after[:commit], func(c fakeCall) bool { return c.Key != after[0].Key }) {
		t.Errorf("the calls after the pause %+v, want one upload committed before any other begins", after)
	}
	ok := h.obs.of("DestState")[1]
	if ok.At.Before(after[commit].At) {
		t.Errorf("OK at %s, before the probe's commit at %s", ok.At, after[commit].At)
	}
}

// TestOutbox_SessionExpiredRestartsFrom0 (§9.9): an expired session is
// aborted and the upload starts again from 0 in a new one, without a
// failure.
func TestOutbox_SessionExpiredRestartsFrom0(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
	data := randomBytes(3*fakePart+1, 1)
	p := h.enqueue(h.segment("a", 0, data), "dst-a", catalog.ClassContinuous)
	f.setFault(func(_ context.Context, c fakeCall) error {
		if c.Method == "PutChunk" && c.Session == "s1" && c.Off == 2*fakePart {
			return fmt.Errorf("fake: 404 on the session: %w", storage.ErrSessionExpired)
		}
		return nil
	})
	q := h.newQueue(map[string]storage.Destination{"dst-a": f})
	h.run(q, time.Second, 5*time.Minute, h.verified(p))

	if aborts := f.callsOf("Abort"); len(aborts) != 1 || aborts[0].Session != "s1" {
		t.Errorf("Abort calls %+v, want one, of the old session", aborts)
	}
	if n := f.count("BeginUpload"); n != 2 {
		t.Errorf("%d BeginUpload calls, want 2", n)
	}
	var sent int64
	var firstOff int64 = -1
	for _, c := range f.callsOf("PutChunk") {
		if c.Session != "s2" {
			continue
		}
		if firstOff < 0 {
			firstOff = c.Off
		}
		sent += c.N
	}
	if firstOff != 0 || sent != int64(len(data)) {
		t.Errorf("the new session's first chunk at %d with %d bytes sent, want 0 and %d", firstOff, sent, len(data))
	}
	if fails := h.obs.of("Failed"); len(fails) != 0 {
		t.Errorf("Failed calls %+v, want none", fails)
	}
}

// TestOutbox_AuthExpiredBlocks (P26): an expired sign-in blocks the
// destination with its alert, and nothing is sent until Unblock.
func TestOutbox_AuthExpiredBlocks(t *testing.T) {
	h := newHarness(t)
	drive := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
	onedrive := newFakeDest(storage.KindOneDrivePersonal, h.clk.Now)
	var mu sync.Mutex
	blocked := true
	fault := func(_ context.Context, c fakeCall) error {
		mu.Lock()
		defer mu.Unlock()
		if blocked && c.Method == "BeginUpload" {
			return fmt.Errorf("fake: 401 invalid_grant: %w", storage.ErrAuthExpired)
		}
		return nil
	}
	drive.setFault(fault)
	onedrive.setFault(fault)
	s := h.segment("a", 0, randomBytes(fakePart+1, 1))
	ps := []catalog.Placement{h.enqueue(s, "dst-g", catalog.ClassContinuous), h.enqueue(s, "dst-o", catalog.ClassContinuous)}
	q := h.newQueue(map[string]storage.Destination{"dst-g": drive, "dst-o": onedrive})
	h.run(q, 10*time.Second, 11*time.Minute, h.after(10*time.Minute))

	for dest, alert := range map[string]string{"dst-g": "Reconectează Google Drive", "dst-o": "Reconectează OneDrive"} {
		if q.State(dest) != StateBlockedAuth || h.lastAlert(dest) != alert {
			t.Errorf("%s: %s with %q, want BLOCKED_AUTH with %q", dest, q.State(dest), h.lastAlert(dest), alert)
		}
	}
	for _, f := range []*fakeDest{drive, onedrive} {
		if f.count("PutChunk") != 0 || f.count("BeginUpload") != 1 {
			t.Errorf("%s: %d BeginUpload and %d PutChunk calls while blocked, want 1 and 0", f.kind, f.count("BeginUpload"), f.count("PutChunk"))
		}
	}
	for _, p := range ps {
		if got := h.placementOf(p.ObjectID, p.DestinationID); got.State != catalog.PlacementPending {
			t.Errorf("a blocked destination's placement is %+v, want pending", got)
		}
	}

	mu.Lock()
	blocked = false
	mu.Unlock()
	q.Unblock("dst-g")
	q.Unblock("dst-o")
	h.run(q, 10*time.Second, 5*time.Minute, h.verified(ps...))
	for _, dest := range []string{"dst-g", "dst-o"} {
		if states := h.destStates(dest); !slices.Equal(states, []DestState{StateBlockedAuth, StateOK}) || q.State(dest) != StateOK {
			t.Errorf("%s: DestState %v, now %s; want BLOCKED_AUTH, OK", dest, states, q.State(dest))
		}
	}
}

// quotaAt is a quota of 1000 bytes with used of them used and inBin in the
// recycle bin.
func quotaAt(used, inBin int64) storage.Quota {
	return storage.Quota{Total: 1000, Used: used, Free: 1000 - used, InBin: inBin}
}

// TestOutbox_QuotaFullKeepsLocal (§9.8 step 4, P26): a quota read at 98.5 %
// pauses the destination with the copies kept on the hub, a read below 98 %
// resumes it, and an upload over 1 GiB reads the quota before it starts.
func TestOutbox_QuotaFullKeepsLocal(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
	f.caps.Quota = storage.QuotaAccount
	f.setQuota(quotaAt(500, 0))
	var reads atomic.Int32
	q := h.newQueue(map[string]storage.Destination{"dst-a": f}, func(o *Options) {
		o.ReadSpool = func(path string, off int64, p []byte) (int, error) {
			reads.Add(1)
			return readSpool(path, off, p)
		}
	})
	h.run(q, 10*time.Second, 15*time.Minute, func() bool {
		if h.clk.Now().Sub(h.start) >= 14*time.Minute {
			f.setQuota(quotaAt(985, 0))
		}
		return q.State("dst-a") == StateQuotaFull
	})
	quotaReads := f.callsOf("Quota")
	if len(quotaReads) != 2 || quotaReads[0].At != h.start || quotaReads[1].At != h.start.Add(15*time.Minute) {
		t.Errorf("quota reads %+v, want at the start and 15 min later", quotaReads)
	}
	if h.lastAlert("dst-a") != "Destinația e plină; copiile rămân pe hub." {
		t.Errorf("QUOTA_FULL's alert is %q", h.lastAlert("dst-a"))
	}

	data := randomBytes(fakePart+3, 1)
	s := h.segment("a", 0, data)
	ps := []catalog.Placement{h.enqueue(s, "dst-a", catalog.ClassContinuous), h.enqueue(h.segment("b", time.Minute, data), "dst-a", catalog.ClassEventClip)}
	info, err := os.Stat(h.spoolPath(s.Path))
	if err != nil {
		t.Fatal(err)
	}
	h.run(q, 10*time.Second, 15*time.Minute, h.after(29*time.Minute))
	if n := f.count("BeginUpload") + f.count("PutChunk"); n != 0 || reads.Load() != 0 {
		t.Errorf("%d upload calls and %d spool reads while QUOTA_FULL, want none", n, reads.Load())
	}
	for _, p := range ps {
		if got := h.placementOf(p.ObjectID, "dst-a"); got.State != catalog.PlacementPending {
			t.Errorf("a placement while QUOTA_FULL is %+v, want pending", got)
		}
	}
	if now, err := os.Stat(h.spoolPath(s.Path)); err != nil || now.Size() != info.Size() || !now.ModTime().Equal(info.ModTime()) {
		t.Errorf("the spool file changed while QUOTA_FULL: %v, %v", now, err)
	}

	f.setQuota(quotaAt(970, 0))
	h.run(q, 10*time.Second, 5*time.Minute, h.verified(ps...))
	if states := h.destStates("dst-a"); !slices.Equal(states, []DestState{StateQuotaFull, StateOK}) {
		t.Errorf("DestState %v, want QUOTA_FULL, then OK", states)
	}

	// An upload over 1 GiB reads the quota before it begins, between the
	// 15-min reads.
	h.clk.Advance(time.Minute)
	f.setQuota(quotaAt(985, 0))
	readsBefore := len(f.callsOf("Quota"))
	begins := f.count("BeginUpload")
	big := h.enqueue(h.segmentRow("big", 2*time.Minute, 3<<29), "dst-a", catalog.ClassContinuous)
	h.run(q, time.Second, 5*time.Minute, func() bool { return q.State("dst-a") == StateQuotaFull })
	if got := f.callsOf("Quota")[readsBefore:]; len(got) != 1 || got[0].At.Sub(h.start)%(15*time.Minute) == 0 {
		t.Errorf("quota reads for the 1.5 GiB upload %+v, want one, between the 15-min reads", got)
	}
	if n := f.count("BeginUpload"); n != begins {
		t.Errorf("the 1.5 GiB upload began (%d BeginUpload calls, were %d)", n, begins)
	}
	if got := h.placementOf(big.ObjectID, "dst-a"); got.State != catalog.PlacementPending {
		t.Errorf("the 1.5 GiB placement is %+v, want pending", got)
	}
}

// TestOutbox_RecycleQuota_PausesKeepLocal (Review Focus 4, §9.8 step 7): a
// recycle-bin destination at 98.5 % with bytes in its bin pauses with the
// copies on the hub and the bin's alert, and deletes nothing; with an empty
// bin the alert is the generic one.
func TestOutbox_RecycleQuota_PausesKeepLocal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inBin int64
		alert string
	}{
		{"bytes in the bin", 50, "Spațiul se eliberează după golirea coșului (max. 30 de zile)."},
		{"empty bin", 0, "Destinația e plină; copiile rămân pe hub."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f := newFakeDest(storage.KindOneDrivePersonal, h.clk.Now)
			f.caps.Quota, f.caps.Delete = storage.QuotaAccount, storage.DeleteRecycleBin
			f.setQuota(quotaAt(985, tc.inBin))
			var ps []catalog.Placement
			for i := range 2 {
				ps = append(ps, h.enqueue(h.segment(fmt.Sprintf("s%d", i), time.Duration(i)*time.Minute, randomBytes(100, byte(i))), "dst-od", catalog.ClassContinuous))
			}
			q := h.newQueue(map[string]storage.Destination{"dst-od": f})
			h.run(q, 10*time.Second, 31*time.Minute, h.after(30*time.Minute))

			if q.State("dst-od") != StateQuotaFull || h.lastAlert("dst-od") != tc.alert {
				t.Errorf("%s with %q, want QUOTA_FULL with %q", q.State("dst-od"), h.lastAlert("dst-od"), tc.alert)
			}
			if n := f.count("Delete"); n != 0 {
				t.Errorf("%d Delete calls, want 0", n)
			}
			if n := f.count("BeginUpload"); n != 0 {
				t.Errorf("%d BeginUpload calls, want 0", n)
			}
			for _, p := range ps {
				if got := h.placementOf(p.ObjectID, "dst-od"); got.State != catalog.PlacementPending {
					t.Errorf("a placement is %+v, want pending", got)
				}
			}
		})
	}
}

// TestOutbox_OutageRecoversWithoutAction (§9.9, P26): 6 h without the
// destination, then it answers again: every placement is verified and the
// destination is OK, after UNREACHABLE from the 9th failure in a row to the
// first success, with nobody calling Unblock.
func TestOutbox_OutageRecoversWithoutAction(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
	outageEnd := h.start.Add(6 * time.Hour)
	f.setFault(func(_ context.Context, c fakeCall) error {
		if c.At.Before(outageEnd) {
			return fmt.Errorf("fake: dial tcp: i/o timeout: %w", storage.ErrTransient)
		}
		return nil
	})
	var ps []catalog.Placement
	for i := range 3 {
		ps = append(ps, h.enqueue(h.segment(fmt.Sprintf("s%d", i), time.Duration(i)*time.Minute, randomBytes(fakePart+i, byte(i))), "dst-a", catalog.ClassContinuous))
	}
	// One worker, so that the failures, and so n, come one at a time.
	q := h.newQueue(map[string]storage.Destination{"dst-a": f}, func(o *Options) {
		o.Rand = func() float64 { return 0.5 }
		o.Workers = 1
	})
	h.run(q, time.Minute, 8*time.Hour, h.verified(ps...))

	if states := h.destStates("dst-a"); !slices.Equal(states, []DestState{StateUnreachable, StateOK}) || q.State("dst-a") != StateOK {
		t.Fatalf("DestState %v, now %s; want UNREACHABLE, then OK", states, q.State("dst-a"))
	}
	events := h.obs.of("")
	unreachable := slices.IndexFunc(events, func(e obsEvent) bool { return e.Kind == "DestState" && e.State == StateUnreachable })
	failsBefore := 0
	for _, e := range events[:unreachable] {
		if e.Kind == "Failed" && e.Reason == "transient" {
			failsBefore++
		}
	}
	if failsBefore != unreachableAfter {
		t.Errorf("UNREACHABLE after %d failures, want %d", failsBefore, unreachableAfter)
	}
	if alert := events[unreachable].Alert; alert != "Destinația nu răspunde; se reîncearcă." {
		t.Errorf("UNREACHABLE's alert is %q", alert)
	}
	firstVerified := slices.IndexFunc(events, func(e obsEvent) bool { return e.Kind == "Verified" })
	ok := slices.IndexFunc(events, func(e obsEvent) bool { return e.Kind == "DestState" && e.State == StateOK })
	if firstVerified < 0 || ok < firstVerified || events[ok].At != events[firstVerified].At || events[ok].At.Before(outageEnd) {
		t.Errorf("OK at %s (event %d), first verification at %s (event %d); want OK on the first success, after the outage",
			events[ok].At, ok, events[firstVerified].At, firstVerified)
	}
	for _, e := range events[unreachable+1 : ok] {
		if e.Kind == "Failed" && e.Attempts > 0 {
			wait := time.Duration(e.P.NextAttemptAtMs-e.At.UnixMilli()) * time.Millisecond
			if wait != Backoff(unreachableAfter, 0.5) {
				t.Errorf("while UNREACHABLE a retry %s later, want the cap, %s", wait, Backoff(unreachableAfter, 0.5))
				break
			}
		}
	}
}

// TestOutbox_BusyDestinationHoldsNoOther (§9.9): each destination has its
// own workers, so a backlog of one destination's clips, ahead in the queue's
// order, does not keep another destination's placement waiting.
func TestOutbox_BusyDestinationHoldsNoOther(t *testing.T) {
	h := newHarness(t)
	a := newFakeDest(storage.KindLocal, h.clk.Now)
	b := newFakeDest(storage.KindLocal, h.clk.Now)
	bCommitted := make(chan struct{})
	var once sync.Once
	b.setFault(func(_ context.Context, c fakeCall) error {
		if c.Method == "Commit" {
			once.Do(func() { close(bCommitted) })
		}
		return nil
	})
	var held atomic.Int32
	a.setFault(func(ctx context.Context, c fakeCall) error {
		if c.Method != "PutChunk" {
			return nil
		}
		select {
		case <-bCommitted:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			held.Add(1)
			return storage.ErrTransient
		}
	})
	var ps []catalog.Placement
	for i := range 4 {
		ps = append(ps, h.enqueue(h.segment(fmt.Sprintf("clip%d", i), time.Duration(i)*time.Minute, randomBytes(100, byte(i))), "dst-a", catalog.ClassEventClip))
	}
	ps = append(ps, h.enqueue(h.segment("cont", 0, randomBytes(100, 9)), "dst-b", catalog.ClassContinuous))
	q := h.newQueue(map[string]storage.Destination{"dst-a": a, "dst-b": b})
	h.run(q, time.Second, 5*time.Minute, h.verified(ps...))
	if n := held.Load(); n != 0 {
		t.Errorf("dst-a's uploads waited 10 s for dst-b's %d times: dst-b's placement waited behind dst-a's", n)
	}
}
