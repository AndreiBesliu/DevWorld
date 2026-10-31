package outbox

import (
	"bytes"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestOutbox_VerifyHashAndSize (P34): a copy counts only when the
// provider's size and hash match the spool file's. A mismatch deletes the
// remote object and uploads again from 0 after Backoff, at most 3 times.
func TestOutbox_VerifyHashAndSize(t *testing.T) {
	t.Run("hash mismatch every time", func(t *testing.T) {
		h := newHarness(t)
		f := newFakeDest(storage.KindGoogleDrive, h.clk.Now)
		f.caps.Hashes = []storage.HashKind{storage.HashMD5}
		f.commitRef = func(ref storage.ObjectRef) storage.ObjectRef {
			ref.Hash = "0123456789abcdef0123456789abcdef"
			return ref
		}
		p := h.enqueue(h.segment("a", 0, randomBytes(fakePart+1, 1)), "dst-a", catalog.ClassContinuous)
		q := h.newQueue(map[string]storage.Destination{"dst-a": f})
		h.run(q, 10*time.Second, 2*time.Hour, h.after(time.Hour))

		got := h.placementOf(p.ObjectID, "dst-a")
		if got.State != catalog.PlacementFailed || got.NextAttemptAtMs != 0 || got.Attempts != 3 {
			t.Errorf("after 3 mismatches the placement is %+v, want failed, never due again, 3 attempts", got)
		}
		fails := h.obs.of("Failed")
		if len(fails) != 3 {
			t.Fatalf("Failed calls %+v, want 3", fails)
		}
		for i, e := range fails {
			if e.Reason != "hash_mismatch" || e.Attempts != i+1 {
				t.Errorf("Failed call %d: %s, %d attempts; want hash_mismatch, %d", i, e.Reason, e.Attempts, i+1)
			}
		}
		if n := len(h.obs.of("Verified")); n != 0 {
			t.Errorf("%d Verified calls, want none", n)
		}
		if b, d, o := f.count("BeginUpload"), f.count("Delete"), len(f.objectMap()); b != 3 || d != 3 || o != 0 {
			t.Errorf("%d uploads, %d deletes, %d objects; want 3, 3 and none", b, d, o)
		}
		if h.hasSession(p.ID) {
			t.Error("a session row is left")
		}
	})

	t.Run("size mismatch, then a good retry", func(t *testing.T) {
		h := newHarness(t)
		f := newFakeDest(storage.KindOneDrivePersonal, h.clk.Now)
		f.caps.Hashes = []storage.HashKind{storage.HashQuickXor}
		first := true
		f.commitRef = func(ref storage.ObjectRef) storage.ObjectRef {
			if first {
				first = false
				ref.Size--
			}
			return ref
		}
		data := randomBytes(2*fakePart+7, 1)
		p := h.enqueue(h.segment("a", 0, data), "dst-a", catalog.ClassContinuous)
		q := h.newQueue(map[string]storage.Destination{"dst-a": f})
		h.run(q, time.Second, 10*time.Minute, h.verified(p))

		if d := f.count("Delete"); d != 1 {
			t.Errorf("%d Delete calls, want 1", d)
		}
		objects := f.objectMap()
		if len(objects) != 1 {
			t.Fatalf("%d objects, want 1", len(objects))
		}
		for key, got := range objects {
			if !bytes.Equal(got, data) {
				t.Errorf("object %s holds %d bytes, not the segment's %d", key, len(got), len(data))
			}
		}
		chunks := f.callsOf("PutChunk")
		var again []fakeCall
		for _, c := range chunks {
			if c.Session == "s2" {
				again = append(again, c)
			}
		}
		if len(again) == 0 || again[0].Off != 0 {
			t.Errorf("the upload after the mismatch sent %+v, want it from 0", again)
		}
		fails, verified := h.obs.of("Failed"), h.obs.of("Verified")
		if len(fails) != 1 || fails[0].Reason != "hash_mismatch" || len(verified) != 1 {
			t.Errorf("Failed %+v and %d Verified, want one hash_mismatch and one verified", fails, len(verified))
		}
		if v := verified[0]; v.Ref.HashKind != storage.HashQuickXor || v.P.RemoteHash != v.Ref.Hash || v.P.RemoteRef != v.Ref.ID {
			t.Errorf("Verified %+v, want the QuickXor ref recorded in the placement", v)
		}
	})
}
