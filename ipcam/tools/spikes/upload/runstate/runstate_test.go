package runstate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// lockHelperEnv makes the test binary a helper that takes the lock at its
// value, prints "locked" and holds it until its stdin ends.
const lockHelperEnv = "UPLOADSPIKE_TEST_LOCK_HELPER"

func TestMain(m *testing.M) {
	if path := os.Getenv(lockHelperEnv); path != "" {
		release, err := Lock(path)
		if err != nil {
			_, _ = os.Stdout.WriteString("error: " + err.Error() + "\n")
			os.Exit(1)
		}
		_, _ = os.Stdout.WriteString("locked\n")
		buf := make([]byte, 1)
		_, _ = os.Stdin.Read(buf)
		release()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

var t0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// TestRunState_SleepGapFromWallVsMono (P49, P53): between two heartbeats of
// one boot, the wall clock running more than 2 min beyond the monotonic
// one, which stops while the host sleeps, is a sleep gap from the last
// heartbeat to this one; an ordinary minute, or a drift under 2 min, is
// none.
func TestRunState_SleepGapFromWallVsMono(t *testing.T) {
	var s State
	if g := s.Heartbeat(t0, time.Hour, "boot-1"); g != nil {
		t.Fatalf("the first heartbeat gave %+v", g)
	}
	if g := s.Heartbeat(t0.Add(time.Minute), time.Hour+time.Minute, "boot-1"); g != nil {
		t.Errorf("a minute on both clocks gave %+v", g)
	}
	if g := s.Heartbeat(t0.Add(3*time.Minute+50*time.Second), time.Hour+2*time.Minute, "boot-1"); g != nil {
		t.Errorf("a drift of 1 min 50 s gave %+v", g)
	}
	// The host sleeps 3 h: the wall clock moves 3 h + 1 min, the monotonic
	// one 1 min.
	last := t0.Add(3*time.Minute + 50*time.Second)
	wake := last.Add(3*time.Hour + time.Minute)
	g := s.Heartbeat(wake, time.Hour+3*time.Minute, "boot-1")
	want := results.Gap{StartMs: last.UnixMilli(), EndMs: wake.UnixMilli(), Reason: "sleep"}
	if g == nil || *g != want {
		t.Fatalf("after a sleep: %+v; want %+v", g, want)
	}
	if len(s.Gaps) != 1 || s.Gaps[0] != want {
		t.Errorf("gaps %+v; want the sleep", s.Gaps)
	}
}

// TestRunState_RebootGap: a new boot id is a reboot gap from the last
// heartbeat on, whatever the clocks say; the monotonic clock restarts
// from 0 with the boot.
func TestRunState_RebootGap(t *testing.T) {
	var s State
	s.Heartbeat(t0, 5*time.Hour, "boot-1")
	at := t0.Add(10 * time.Minute)
	g := s.Heartbeat(at, 30*time.Second, "boot-2")
	want := results.Gap{StartMs: t0.UnixMilli(), EndMs: at.UnixMilli(), Reason: "reboot"}
	if g == nil || *g != want {
		t.Fatalf("after a reboot: %+v; want %+v", g, want)
	}
	if g := s.Heartbeat(at.Add(time.Minute), 90*time.Second, "boot-2"); g != nil {
		t.Errorf("the next minute gave %+v", g)
	}
}

// TestBootIDAndMono: this host's boot id is stable across two readings, and
// its monotonic clock advances.
func TestBootIDAndMono(t *testing.T) {
	a, err := BootID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := BootID()
	if err != nil || !SameBoot(a, b) {
		t.Errorf("BootID = %q then %q, %v; want one boot", a, b, err)
	}
	m1, err := Mono()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	m2, err := Mono()
	if err != nil || m2-m1 < 15*time.Millisecond {
		t.Errorf("Mono = %v then %v, %v; want it to advance", m1, m2, err)
	}
}

// TestUpdate_ConcurrentWritersKeepEachOthersFields: Update holds the run
// state's lock for its whole load-apply-write, so two writers of different
// fields lose none of each other's updates, and the file is 0600 with a
// handover token of 32 bytes in hex.
func TestUpdate_ConcurrentWritersKeepEachOthersFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", StateFile)
	var wg sync.WaitGroup
	for w := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if err := Update(path, func(s *State) error {
					s.Handoffs[[]string{"a", "b"}[w]]++
					return nil
				}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Handoffs["a"] != 50 || s.Handoffs["b"] != 50 {
		t.Errorf("handoffs %v; want 50 each", s.Handoffs)
	}
	if len(s.HandoverToken) != 64 || strings.Trim(s.HandoverToken, "0123456789abcdef") != "" {
		t.Errorf("handover token %q; want 64 hex characters", s.HandoverToken)
	}
	if info, err := os.Stat(path); err != nil || (info.Mode().Perm() != 0o600 && os.PathSeparator == '/') {
		t.Errorf("the run state: %v, %v; want 0600", info, err)
	}
	// An error of fn writes nothing.
	before, _ := os.ReadFile(path) //nolint:gosec // the test's own file
	if err := Update(path, func(s *State) error { s.Mode = "C"; return errors.New("no") }); err == nil {
		t.Error("Update ignored fn's error")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:gosec // the test's own file
		t.Error("a failed update changed the file")
	}
}

// TestLock_OneHolderAcrossProcesses: while another process holds the lock,
// Lock gives ErrLocked; once that process ends, its lock is free.
func TestLock_OneHolderAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), HubLock)
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // the test binary as the helper
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	buf := make([]byte, 7)
	if _, err := out.Read(buf); err != nil || string(buf) != "locked\n" {
		t.Fatalf("the helper said %q, %v", buf, err)
	}
	if release, err := Lock(path); !errors.Is(err, ErrLocked) {
		if release != nil {
			release()
		}
		t.Fatalf("Lock while the helper holds it: %v; want ErrLocked", err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	release, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock after the helper ended: %v", err)
	}
	if _, err := Lock(path); !errors.Is(err, ErrLocked) {
		t.Errorf("a second Lock in this process: %v; want ErrLocked (another open file)", err)
	}
	release()
}

// TestClaim_ValidWhileItsProcessLives: a claim holds while its process
// lives with the recorded start time and its time is ahead.
func TestClaim_ValidWhileItsProcessLives(t *testing.T) {
	now := time.Now()
	c, err := NewClaim(now)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Valid(now) || !c.Mine() {
		t.Errorf("a fresh claim of this process: valid %v, mine %v", c.Valid(now), c.Mine())
	}
	if c.Valid(now.Add(ClaimLife + time.Second)) {
		t.Error("a claim past its time is valid")
	}
	other := *c
	other.ProcStartMs++
	if other.Valid(now) || other.Mine() {
		t.Error("a claim of another start of this pid is valid")
	}
	var none *Claim
	if none.Valid(now) {
		t.Error("no claim is valid")
	}
}
