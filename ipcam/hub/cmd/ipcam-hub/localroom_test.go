package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
)

// localFullPercent is the quota use at which the outbox holds a destination
// full (outbox's quotaFullPercent).
const localFullPercent = 98

// requireLocalRoom stops a process test whose LOCAL destination is on the
// filesystem of TMPDIR when that filesystem is too full for it (Task 7b
// review M8): the LOCAL destination's quota is its filesystem's, so at 98 %
// the outbox holds it QUOTA_FULL and pauses its uploads, and the test would
// sit until its timeout with a message that does not name the cause. With
// any variable named *_REQUIRE_* set to 1, as CI sets them, the test fails
// at once; otherwise it is skipped. Either way the message names the cause.
func requireLocalRoom(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	d, err := local.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	q, err := d.Quota(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if msg := localRoomProblem(q, dir); msg != "" {
		if requireSet(os.Environ()) {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
}

// localRoomProblem says why a LOCAL destination at dir, whose quota is q,
// would pause its uploads, or "" when it would not.
func localRoomProblem(q storage.Quota, dir string) string {
	if q.Unknown || q.Total <= 0 || q.Used*100 < q.Total*localFullPercent {
		return ""
	}
	return fmt.Sprintf("the filesystem of %s is %d %% full: a LOCAL destination there reads QUOTA_FULL at %d %% and pauses its uploads; "+
		"free some space or set TMPDIR to another filesystem", dir, q.Used*100/q.Total, localFullPercent)
}

// requireSet reports whether env sets a variable named *_REQUIRE_* to 1.
func requireSet(env []string) bool {
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); strings.Contains(k, "_REQUIRE_") && v == "1" {
			return true
		}
	}
	return false
}

// TestRequireLocalRoom_NamesTheCause (Task 7b review M8): a filesystem at
// 98 % or more is too full for a LOCAL destination, with a message that
// names the cause, and a *_REQUIRE_* variable set to 1 makes it a failure.
func TestRequireLocalRoom_NamesTheCause(t *testing.T) {
	if msg := localRoomProblem(storage.Quota{Total: 100, Used: 97, Free: 3}, "/tmp"); msg != "" {
		t.Errorf("97 %% used: %q, want no problem", msg)
	}
	msg := localRoomProblem(storage.Quota{Total: 100, Used: 98, Free: 2}, "/tmp")
	if !strings.Contains(msg, "98 % full") || !strings.Contains(msg, "QUOTA_FULL") || !strings.Contains(msg, "TMPDIR") {
		t.Errorf("98 %% used: %q, want the cause and the remedy", msg)
	}
	if msg := localRoomProblem(storage.Quota{Unknown: true}, "/tmp"); msg != "" {
		t.Errorf("an unknown quota: %q, want no problem", msg)
	}
	for env, want := range map[string]bool{
		"RECSPIKE_REQUIRE_MEDIAMTX=1": true, "SPIKE_REQUIRE_MEDIA=1": true, "SPIKE_REQUIRE_MEDIA=0": false, "PATH=/bin": false,
	} {
		if got := requireSet([]string{"HOME=/root", env}); got != want {
			t.Errorf("requireSet with %s = %v, want %v", env, got, want)
		}
	}
}
