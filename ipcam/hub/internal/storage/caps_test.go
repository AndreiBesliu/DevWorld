package storage_test

import (
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestCaps_ZeroValueIsConservative (P24): a field that a driver leaves unset
// claims the least. No resume, a delete that may only move the object to a
// recycle bin that still takes quota, playback through the hub's proxy only,
// no quota reading and no provider hash.
func TestCaps_ZeroValueIsConservative(t *testing.T) {
	var caps storage.Caps
	if caps.Resumable != storage.ResumeNone {
		t.Errorf("Resumable = %d, want ResumeNone (%d)", caps.Resumable, storage.ResumeNone)
	}
	if caps.Delete != storage.DeleteRecycleBin {
		t.Errorf("Delete = %d, want DeleteRecycleBin (%d)", caps.Delete, storage.DeleteRecycleBin)
	}
	if caps.Playback != storage.LinkProxyOnly {
		t.Errorf("Playback = %d, want LinkProxyOnly (%d)", caps.Playback, storage.LinkProxyOnly)
	}
	if caps.Quota != storage.QuotaNone {
		t.Errorf("Quota = %d, want QuotaNone (%d)", caps.Quota, storage.QuotaNone)
	}
	if len(caps.Hashes) != 0 {
		t.Errorf("Hashes = %v, want none", caps.Hashes)
	}
}

// TestDeleteOutcome_ZeroValueIsConservative: an outcome that a driver leaves
// unset claims that the object may still take quota, never that its space is
// free.
func TestDeleteOutcome_ZeroValueIsConservative(t *testing.T) {
	var outcome storage.DeleteOutcome
	if outcome != storage.DeletedRecycled {
		t.Errorf("the zero DeleteOutcome = %d, want DeletedRecycled (%d)", outcome, storage.DeletedRecycled)
	}
}
