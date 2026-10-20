package conf

// The configuration tests of patch MTX-2 (hub/third_party/PATCHES.md): a
// path's recordAgeRecipients holds exactly two age1 X25519 recipients, RI_g
// then RK, and an error about it never quotes what it rejects, which may be a
// secret key pasted in the wrong place (C22).

import (
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
)

// mtx2Config is a configuration whose path cam01_main records, with the YAML
// value recipients as its recordAgeRecipients and the path keys in extra.
func mtx2Config(recipients, extra string) []byte {
	return []byte("paths:\n" +
		"  cam01_main:\n" +
		"    record: yes\n" +
		"    recordAgeRecipients: " + recipients + "\n" +
		extra)
}

func mtx2Identity(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	return id
}

// TestMTX2_RejectsSecretKeyInRecipients: a secret key among the recipients,
// one recipient, three recipients, a secret key instead of a list and RI_g as
// RK each fail the load, with an error that contains none of the keys and no
// secret key prefix.
func TestMTX2_RejectsSecretKeyInRecipients(t *testing.T) {
	riG, rk, third := mtx2Identity(t), mtx2Identity(t), mtx2Identity(t)
	riGs, rks, thirds := riG.Recipient().String(), rk.Recipient().String(), third.Recipient().String()
	keys := []string{riG.String(), rk.String(), riGs, rks, thirds}

	// The same file with RI_g and RK loads, so the failures below are about
	// the recipients.
	loaded, _, err := Load(createTempFile(t, mtx2Config("["+riGs+", "+rks+"]", "")), nil, nil)
	require.NoError(t, err)
	scope, err := loaded.Paths["cam01_main"].RecordAgeScope()
	require.NoError(t, err)
	require.NotNil(t, scope)
	recipients, err := scope.Strings()
	require.NoError(t, err)
	require.Equal(t, [2]string{riGs, rks}, recipients)

	for _, tc := range []struct {
		name, recipients string
		// about is in the error, so that the load fails for this reason.
		about string
	}{
		{"a secret key as RI_g", "[" + riG.String() + ", " + rks + "]", "recordAgeRecipients"},
		{"a secret key as RK", "[" + riGs + ", " + rk.String() + "]", "recordAgeRecipients"},
		{"one recipient", "[" + riGs + "]", "recordAgeRecipients"},
		{"three recipients", "[" + riGs + ", " + rks + ", " + thirds + "]", "recordAgeRecipients"},
		// Upstream's JSON decoder names the field's type, as for any key.
		{"a secret key instead of a list", riG.String(), "[]string"},
		{"RI_g as RK", "[" + riGs + ", " + riGs + "]", "recordAgeRecipients"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(createTempFile(t, mtx2Config(tc.recipients, "")), nil, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.about, "the load failed for another reason")
			for _, key := range keys {
				require.NotContains(t, err.Error(), key, "the error quotes a key")
			}
			require.NotContains(t, err.Error(), "AGE-SECRET-KEY-", "the error holds secret key material")
		})
	}
}

// TestMTX2_RecipientsNeedFMP4: recordAgeRecipients with recordFormat mpegts
// fails the load, since MTX-2 encrypts fMP4 segments only and an MPEG-TS
// recording would be written in clear (HUB-04).
func TestMTX2_RecipientsNeedFMP4(t *testing.T) {
	riGs, rks := mtx2Identity(t).Recipient().String(), mtx2Identity(t).Recipient().String()
	recipients := "[" + riGs + ", " + rks + "]"

	_, _, err := Load(createTempFile(t, mtx2Config(recipients, "    recordFormat: fmp4\n")), nil, nil)
	require.NoError(t, err)

	_, _, err = Load(createTempFile(t, mtx2Config(recipients, "    recordFormat: mpegts\n")), nil, nil)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "recordAgeRecipients"), "error: %v", err)
}

// TestMTX2_NoRecipientsRecordsInClear: without recordAgeRecipients, or with an
// empty list, which Load cannot tell apart, the path records in clear, in
// either format.
func TestMTX2_NoRecipientsRecordsInClear(t *testing.T) {
	for _, tc := range []struct{ name, config string }{
		{"no key", "paths:\n  cam01_main:\n    record: yes\n"},
		{"an empty list", string(mtx2Config("[]", ""))},
		{"an empty list, MPEG-TS", string(mtx2Config("[]", "    recordFormat: mpegts\n"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, _, err := Load(createTempFile(t, []byte(tc.config)), nil, nil)
			require.NoError(t, err)
			scope, err := loaded.Paths["cam01_main"].RecordAgeScope()
			require.NoError(t, err)
			require.Nil(t, scope)
		})
	}
}
