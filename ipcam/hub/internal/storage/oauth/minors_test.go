package oauth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
)

// The tests of the carried Task 9A review minors.

// TestOAuth_ExpiredStatesWiped (Task 9A review M6): a state that expired
// unused loses its verifier, or its device code, at the store's next
// call, whichever state that call names, not an hour later.
func TestOAuth_ExpiredStatesWiped(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	planA, err := s.Begin(ctx, BeginRequest{DestID: "g-android", Provider: storage.KindGoogleDrive, Platform: PlatformAndroid})
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.Begin(ctx, BeginRequest{DestID: "g-web", Provider: storage.KindGoogleDrive, Platform: PlatformWeb})
	if err != nil {
		t.Fatal(err)
	}
	if s.pendingVerifier(planA.State) == "" || s.pendingDevice(device.State) == "" {
		t.Fatal("a new state holds no verifier or device code")
	}
	e.clk.Add(device.ExpiresAt.Sub(e.clk.Now()) + time.Second)
	if _, _, err := s.Status(ctx, "another-state"); !errors.Is(err, ErrStateUnknown) {
		t.Fatalf("Status of an unknown state = %v", err)
	}
	if s.pendingVerifier(planA.State) != "" {
		t.Error("the expired plan A state kept its verifier")
	}
	if s.pendingDevice(device.State) != "" {
		t.Error("the expired device state kept its device code")
	}
	for _, state := range []string{planA.State, device.State} {
		if st, _, _ := s.Status(ctx, state); st != StatusExpired {
			t.Errorf("Status of a wiped state = %s, want expired", st)
		}
	}
}

// pendingDevice is the device code that the hub keeps for state, "" once
// wiped or for none.
func (s *Store) pendingDevice(state string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pending[state]; p != nil {
		return p.device
	}
	return ""
}

// failingSealer fails its seal number fail, counted from 1.
type failingSealer struct {
	crypto.Sealer
	fail  int32
	count atomic.Int32
}

func (f *failingSealer) Seal(ctx context.Context, label string, plain []byte) ([]byte, error) {
	if f.count.Add(1) == f.fail {
		return nil, errors.New("the sealer failed")
	}
	return f.Sealer.Seal(ctx, label, plain)
}

// TestSetClients_AllOrNothing (Task 9A review M7): SetClients replaces the
// client rows in one transaction, after it sealed them all: a seal that
// fails leaves every row as it was.
func TestSetClients_AllOrNothing(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	ctx := t.Context()
	s := e.store()
	if err := s.SetClients(ctx, Clients{Google: map[ClientType]Client{ClientAndroid: {ID: idAndroid}}}); err != nil {
		t.Fatal(err)
	}
	before, err := e.cat.SecretIDs(ctx, clientPrefix)
	if err != nil {
		t.Fatal(err)
	}
	blob, _, _, err := e.cat.Secret(ctx, "client:google:android")
	if err != nil {
		t.Fatal(err)
	}
	failing := e.store(func(c *Config) { c.Sealer = &failingSealer{Sealer: e.sealer, fail: 5} }) // the fifth of five clients
	if err := failing.SetClients(ctx, hubClients()); err == nil {
		t.Fatal("SetClients with a failing seal succeeded")
	}
	after, err := e.cat.SecretIDs(ctx, clientPrefix)
	if err != nil {
		t.Fatal(err)
	}
	again, _, _, err := e.cat.Secret(ctx, "client:google:android")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || after[0] != before[0] || string(again) != string(blob) {
		t.Errorf("after a failed SetClients the client rows are %v, want %v as they were", after, before)
	}
}
