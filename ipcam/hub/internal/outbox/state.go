package outbox

import (
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// DestState is a destination's state (§9.3 L1510, §9.7).
type DestState string

// The destination states that the queue sets (P26).
const (
	StateOK DestState = "OK"
	// StateBlockedAuth: the provider refused the sign-in; nothing is sent
	// until Unblock.
	StateBlockedAuth DestState = "BLOCKED_AUTH"
	// StateQuotaFull: the quota is at 98 % or the provider said it is full.
	// Every class waits on the hub (PAUSE_KEEP_LOCAL; DELETE_OLDEST and
	// ALERT_ONLY come with the rules engine) until a quota read below 98 %.
	StateQuotaFull DestState = "QUOTA_FULL"
	// StatePausedDailyCap: Drive's daily upload cap; nothing is sent for its
	// ResetAfter, then one probe upload.
	StatePausedDailyCap DestState = "PAUSED_DAILY_CAP"
	// StateUnreachable: the destination keeps failing while the backoff is at
	// its cap. It is retried, and is OK again on the first chunk it confirms.
	StateUnreachable DestState = "UNREACHABLE"
)

// The owner's alerts of the states (§9.7–§9.9).
const (
	alertReconnectDrive    = "Reconectează Google Drive"
	alertReconnectOneDrive = "Reconectează OneDrive"
	alertReconnect         = "Reconectează destinația"
	alertQuotaInBin        = "Spațiul se eliberează după golirea coșului (max. 30 de zile)."
	alertQuotaFull         = "Destinația e plină; copiile rămân pe hub."
	alertDailyCap          = "Plafonul zilnic Google Drive a fost atins; urcarea se reia peste 24 h."
	alertUnreachable       = "Destinația nu răspunde; se reîncearcă."
)

const (
	// unreachableAfter is the failures in a row from which the backoff is at
	// its cap, and the destination shows UNREACHABLE.
	unreachableAfter = 9
	// quotaFullPercent is the quota use at which a destination is full
	// (§9.8 step 4).
	quotaFullPercent = 98
	// largeUpload is the size above which an upload reads the quota before
	// it starts (§9.8 step 4).
	largeUpload = 1 << 30
	// defaultDailyCapPause is the pause of a daily cap that names no
	// ResetAfter (§9.9).
	defaultDailyCapPause = 24 * time.Hour
)

// destination is one destination of the queue and its state. Its fields
// after caps are guarded by Queue.mu.
type destination struct {
	id   string
	d    storage.Destination
	caps storage.Caps

	state       DestState
	alert       string
	failures    int           // failed uploads in a row: Backoff's n
	waitUntil   time.Time     // no call before (Backoff, Retry-After)
	pausedUntil time.Time     // the end of PAUSED_DAILY_CAP
	quota       storage.Quota // the last quota read
	quotaAt     time.Time     // when the last quota read started; zero before the first
	reading     bool          // a quota read is in flight
	busy        int           // uploads in flight
	// reported and reportedAlert are what Observer.DestState last heard.
	reported      DestState
	reportedAlert string
}

func newDestination(id string, d storage.Destination) *destination {
	return &destination{id: id, d: d, caps: d.Capabilities(), state: StateOK, reported: StateOK}
}

func (d *destination) set(s DestState, alert string) {
	d.state, d.alert = s, alert
}

// slots is how many uploads d may start at now, with workers per
// destination: none while a quota read is in flight, during a wait, or while
// blocked or full; one, the probe, at the end of a daily-cap pause.
func (d *destination) slots(now time.Time, workers int) int {
	if d.reading || now.Before(d.waitUntil) {
		return 0
	}
	switch d.state {
	case StateBlockedAuth, StateQuotaFull:
		return 0
	case StatePausedDailyCap:
		if now.Before(d.pausedUntil) || d.busy > 0 {
			return 0
		}
		return 1
	}
	return max(0, workers-d.busy)
}

// paused reports whether an upload in flight must stop before its next call:
// d is blocked, full, in a daily-cap pause or in a wait.
func (d *destination) paused(now time.Time) bool {
	switch {
	case d.state == StateBlockedAuth, d.state == StateQuotaFull:
		return true
	case d.state == StatePausedDailyCap && now.Before(d.pausedUntil):
		return true
	}
	return now.Before(d.waitUntil)
}

// quotaDue reports whether d's quota is to be read at now: it has one, no
// read is in flight, its sign-in is not blocked, it is not in a wait, and
// every QuotaEvery since the last read.
func (d *destination) quotaDue(now time.Time, every time.Duration) bool {
	return d.caps.Quota != storage.QuotaNone && !d.reading && d.state != StateBlockedAuth && !now.Before(d.waitUntil) &&
		(d.quotaAt.IsZero() || now.Sub(d.quotaAt) >= every)
}

// quotaRead takes a quota that was read: at 98 % or more d is full; below,
// a full d is OK again.
func (d *destination) quotaRead(q storage.Quota) {
	d.quota = q
	full := !q.Unknown && q.Total > 0 && q.Used*100 >= q.Total*quotaFullPercent
	switch {
	case full && d.state != StateBlockedAuth:
		d.full()
	case !full && d.state == StateQuotaFull:
		d.set(StateOK, "")
	}
}

// full makes d QUOTA_FULL. The alert names the recycle bin when the bin
// holds bytes and occupies quota (§9.8 step 7): deleting would free nothing.
func (d *destination) full() {
	alert := alertQuotaFull
	if d.caps.Delete == storage.DeleteRecycleBin && d.quota.InBin > 0 {
		alert = alertQuotaInBin
	}
	d.set(StateQuotaFull, alert)
}

// blockAuth makes d BLOCKED_AUTH, with the alert of its provider.
func (d *destination) blockAuth() {
	alert := alertReconnect
	switch d.d.Kind() {
	case storage.KindGoogleDrive:
		alert = alertReconnectDrive
	case storage.KindOneDrivePersonal, storage.KindOneDriveBusiness:
		alert = alertReconnectOneDrive
	}
	d.set(StateBlockedAuth, alert)
}

// capped pauses d for resetAfter from now (§9.9).
func (d *destination) capped(now time.Time, resetAfter time.Duration) {
	if resetAfter <= 0 {
		resetAfter = defaultDailyCapPause
	}
	d.pausedUntil = now.Add(resetAfter)
	d.set(StatePausedDailyCap, alertDailyCap)
}

// failure counts one more failure in a row at now and makes d wait
// max(Backoff(n, r), retryAfter) before its next call, and returns that
// wait. A failure of reaching the destination, from the backoff's cap on,
// makes it UNREACHABLE.
func (d *destination) failure(now time.Time, r float64, retryAfter time.Duration, unreachable bool) time.Duration {
	d.failures++
	wait := max(Backoff(d.failures, r), retryAfter)
	if until := now.Add(wait); until.After(d.waitUntil) {
		d.waitUntil = until
	}
	if unreachable && d.failures >= unreachableAfter && d.state == StateOK {
		d.set(StateUnreachable, alertUnreachable)
	}
	return wait
}

// answered ends the failures in a row: d confirmed a chunk, which proves that
// it answers, so an UNREACHABLE d is OK again.
func (d *destination) answered() {
	d.failures = 0
	if d.state == StateUnreachable {
		d.set(StateOK, "")
	}
}

// success is a verified copy on d: d answered, and a daily-cap probe passed.
func (d *destination) success() {
	d.answered()
	if d.state == StatePausedDailyCap {
		d.set(StateOK, "")
	}
}
