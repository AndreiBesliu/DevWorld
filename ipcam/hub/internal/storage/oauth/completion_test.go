package oauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
)

// heldSealer is a sealer whose first Seal of label waits, once it has
// told entered, until release closes; with fail set, every Seal of label
// fails instead.
type heldSealer struct {
	crypto.Sealer
	label   string
	fail    error
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newHeldSealer(inner crypto.Sealer, label string) *heldSealer {
	return &heldSealer{Sealer: inner, label: label, entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldSealer) Seal(ctx context.Context, label string, plain []byte) ([]byte, error) {
	if label == h.label {
		if h.fail != nil {
			return nil, h.fail
		}
		h.once.Do(func() {
			close(h.entered)
			<-h.release
		})
	}
	return h.Sealer.Seal(ctx, label, plain)
}

// deviceGrants counts the device code redemptions that reached the fake's
// token endpoints.
func (e *env) deviceGrants() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, f := range e.tokenForms {
		if f.Get("grant_type") == grantDeviceCode && f.Get("device_code") != "" {
			n++
		}
	}
	return n
}

// beginDevice begins web × Google's device login of destID on s and
// approves it at the fake.
func (e *env) beginDevice(s *Store, destID string) BeginResult {
	e.t.Helper()
	begin, err := s.Begin(e.t.Context(), BeginRequest{DestID: destID, Provider: storage.KindGoogleDrive, Platform: PlatformWeb})
	if err != nil {
		e.t.Fatal(err)
	}
	if begin.Plan != PlanB1 {
		e.t.Fatalf("web × Google began plan %q, want B1", begin.Plan)
	}
	if err := e.fake.ApproveDevice(begin.UserCode, "acct-1"); err != nil {
		e.t.Fatal(err)
	}
	return begin
}

// TestDeviceStatus_ConcurrentStatusDuringCompletion (Task 9A review I1): a
// Status or DeviceStatus that comes while another one stores the approved
// device login is pending; it never redeems the used device code again,
// so it never reports the provider's invalid_grant as failed. Once the
// login is stored, every Status reports it completed.
func TestDeviceStatus_ConcurrentStatusDuringCompletion(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	held := newHeldSealer(e.sealer, loginPrefix+"g-web")
	s := e.store(func(c *Config) { c.Sealer = held })
	begin := e.beginDevice(s, "g-web")
	type answer struct {
		status string
		c      Completion
		err    error
	}
	first := make(chan answer, 1)
	go func() {
		st, c, err := s.Status(context.Background(), begin.State)
		first <- answer{st, c, err}
	}()
	select {
	case <-held.entered: // the first poll has the tokens and seals the login
	case <-time.After(10 * time.Second):
		t.Fatal("the first Status did not reach the login's sealing")
	}
	e.clk.Add(time.Minute) // a poll is due again
	st, c, err := s.Status(t.Context(), begin.State)
	if st != StatusPending || err != nil || c.DestID != "g-web" {
		t.Errorf("Status while the login is stored = %s, %+v, %v; want pending, naming g-web", st, c, err)
	}
	if _, err := s.DeviceStatus(t.Context(), begin.State); !errors.Is(err, ErrAuthorizationPending) {
		t.Errorf("DeviceStatus while the login is stored = %v, want ErrAuthorizationPending", err)
	}
	close(held.release)
	var got answer
	select {
	case got = <-first:
	case <-time.After(10 * time.Second):
		t.Fatal("the first Status did not return")
	}
	if got.status != StatusCompleted || got.err != nil || got.c.IssuedAt.IsZero() {
		t.Fatalf("the first Status = %s, %+v, %v; want completed", got.status, got.c, got.err)
	}
	for range 2 {
		if st, c, err := s.Status(t.Context(), begin.State); st != StatusCompleted || err != nil || c != got.c {
			t.Errorf("Status after the completion = %s, %+v, %v; want completed, %+v", st, c, err, got.c)
		}
	}
	if n := e.deviceGrants(); n != 1 {
		t.Errorf("the device code was redeemed %d times, want once", n)
	}
	if _, err := s.Refresh(t.Context(), "g-web"); err != nil {
		t.Errorf("Refresh of the stored login: %v", err)
	}
}

// TestDeviceStatus_StoreFailureEndsTheLogin: an approved device login that
// the hub fails to store ends failed, reported as such by every later
// Status, which never redeems the used device code again.
func TestDeviceStatus_StoreFailureEndsTheLogin(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	held := newHeldSealer(e.sealer, loginPrefix+"g-web")
	held.fail = errors.New("the sealer is broken")
	s := e.store(func(c *Config) { c.Sealer = held })
	begin := e.beginDevice(s, "g-web")
	for i := range 3 {
		st, c, err := s.Status(t.Context(), begin.State)
		if st != StatusFailed || err == nil || errors.Is(err, storage.ErrTransient) || c.DestID != "g-web" {
			t.Errorf("Status %d = %s, %+v, %v; want failed, naming g-web", i+1, st, c, err)
		}
		e.clk.Add(time.Minute)
	}
	if n := e.deviceGrants(); n != 1 {
		t.Errorf("the device code was redeemed %d times, want once", n)
	}
}

// TestCompletion_CallerGoneWhileStoring: a login that the provider
// approved is stored even when its caller (a control request) goes away
// while the hub stores it: the code or device code is used up, so a
// cancelled store would lose the login.
func TestCompletion_CallerGoneWhileStoring(t *testing.T) {
	for _, plan := range []string{PlanA, PlanB1} {
		t.Run(plan, func(t *testing.T) {
			e := newEnv(t, fakeoauth.Options{})
			held := newHeldSealer(e.sealer, loginPrefix+"g-windows")
			s := e.store(func(c *Config) { c.Sealer = held })
			ctx, cancel := context.WithCancel(t.Context())
			begin, err := s.Begin(ctx, BeginRequest{DestID: "g-windows", Provider: storage.KindGoogleDrive, Platform: PlatformWindows,
				LoopbackPort: 53123, Plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			if plan == PlanB1 {
				if err := e.fake.ApproveDevice(begin.UserCode, "acct-1"); err != nil {
					t.Fatal(err)
				}
				go func() {
					_, err := s.DeviceStatus(ctx, begin.State)
					done <- err
				}()
			} else {
				redirect, err := e.fake.Authorize(begin.AuthURL)
				if err != nil {
					t.Fatal(err)
				}
				code := codeOf(t, redirect, begin.State)
				go func() {
					_, err := s.Complete(ctx, begin.State, code)
					done <- err
				}()
			}
			select {
			case <-held.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the completion did not reach the login's sealing")
			}
			cancel()
			close(held.release)
			if err := <-done; err != nil {
				t.Errorf("the completion whose caller went away: %v", err)
			}
			if st, c, err := s.Status(t.Context(), begin.State); st != StatusCompleted || err != nil {
				t.Errorf("Status = %s, %+v, %v; want completed", st, c, err)
			}
			if _, ok, err := s.Login(t.Context(), "g-windows"); !ok || err != nil {
				t.Errorf("the login's row: ok %v, %v; want stored", ok, err)
			}
		})
	}
}
