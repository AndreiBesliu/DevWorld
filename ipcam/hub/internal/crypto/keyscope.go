package crypto

import (
	"errors"
	"fmt"

	"filippo.io/age"
)

// KeyScope holds the two recipients of every recording (§10.2, T20): RI_g,
// the recording identity of the current generation, and RK, the recovery
// key. Both are public keys; package recorder and MediaMTX only ever see
// these (§4.3).
type KeyScope struct{ RecordingIdentity, Recovery *age.X25519Recipient }

// ParseKeyScope parses RI_g and RK, each one "age1…" X25519 recipient. It
// rejects anything else: an empty string, a list, a hybrid or SSH recipient,
// or a secret key. Its errors never quote the input, which may be a secret
// key pasted in the wrong place.
func ParseKeyScope(riG, rk string) (KeyScope, error) {
	ri, err := parseX25519Recipient("RI_g", riG)
	if err != nil {
		return KeyScope{}, err
	}
	r, err := parseX25519Recipient("RK", rk)
	if err != nil {
		return KeyScope{}, err
	}
	return KeyScope{RecordingIdentity: ri, Recovery: r}, nil
}

func parseX25519Recipient(name, s string) (*age.X25519Recipient, error) {
	r, err := age.ParseX25519Recipient(s)
	if err != nil {
		// age's error quotes s; this one does not.
		return nil, fmt.Errorf("crypto: %s is not one age1 X25519 recipient", name)
	}
	return r, nil
}

// Strings returns the two recipients as "age1…" strings, RI_g first, then RK.
func (s KeyScope) Strings() [2]string {
	return [2]string{s.RecordingIdentity.String(), s.Recovery.String()}
}

// recipients returns RI_g and RK in header order.
func (s KeyScope) recipients() ([]age.Recipient, error) {
	if s.RecordingIdentity == nil || s.Recovery == nil {
		return nil, errors.New("crypto: the key scope needs both RI_g and RK")
	}
	return []age.Recipient{s.RecordingIdentity, s.Recovery}, nil
}
