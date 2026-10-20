package conf

// The configuration tests of patch MTX-2 (hub/third_party/PATCHES.md): a
// path's recordAgeRecipients is either absent, and the path records in clear,
// or exactly two age1 X25519 recipients, RI_g then RK. Any other value, an
// empty list or null included, fails the load, with an error that never
// quotes what it rejects, which may be a secret key pasted in the wrong place
// (C22).

import (
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf/jsonwrapper"
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

// mtx2DefaultsConfig is a configuration whose pathDefaults hold the YAML value
// recipients as recordAgeRecipients, and whose path cam01_main records with
// the path keys in extra.
func mtx2DefaultsConfig(recipients, extra string) []byte {
	return []byte("pathDefaults:\n" +
		"  recordAgeRecipients: " + recipients + "\n" +
		"paths:\n" +
		"  cam01_main:\n" +
		"    record: yes\n" +
		extra)
}

func mtx2Identity(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	return id
}

// mtx2RequireScope requires that path cam01_main of loaded encrypts to
// exactly riGs then rks.
func mtx2RequireScope(t *testing.T, loaded *Conf, riGs, rks string) {
	t.Helper()
	scope, err := loaded.Paths["cam01_main"].RecordAgeScope()
	require.NoError(t, err)
	require.NotNil(t, scope)
	recipients, err := scope.Strings()
	require.NoError(t, err)
	require.Equal(t, [2]string{riGs, rks}, recipients)
}

// mtx2RequireRefusal requires that err is a refusal of recordAgeRecipients
// that quotes none of keys and no secret key prefix.
func mtx2RequireRefusal(t *testing.T, err error, about string, keys []string) {
	t.Helper()
	require.Error(t, err)
	require.Contains(t, err.Error(), about, "the load failed for another reason")
	for _, key := range keys {
		require.NotContains(t, err.Error(), key, "the error quotes a key")
	}
	require.NotContains(t, err.Error(), "AGE-SECRET-KEY-", "the error holds secret key material")
}

// TestMTX2_RejectsSecretKeyInRecipients: a secret key among the recipients,
// one recipient, three recipients, a secret key instead of a list, RI_g as RK,
// an empty list and null each fail the load, in a path or in pathDefaults,
// with an error that contains none of the keys and no secret key prefix.
func TestMTX2_RejectsSecretKeyInRecipients(t *testing.T) {
	riG, rk, third := mtx2Identity(t), mtx2Identity(t), mtx2Identity(t)
	riGs, rks, thirds := riG.Recipient().String(), rk.Recipient().String(), third.Recipient().String()
	keys := []string{riG.String(), rk.String(), riGs, rks, thirds}
	pair := "[" + riGs + ", " + rks + "]"

	// The same files with RI_g and RK load, so the failures below are about
	// the recipients.
	loaded, _, err := Load(createTempFile(t, mtx2Config(pair, "")), nil, nil)
	require.NoError(t, err)
	mtx2RequireScope(t, loaded, riGs, rks)
	loaded, _, err = Load(createTempFile(t, mtx2DefaultsConfig(pair, "")), nil, nil)
	require.NoError(t, err)
	mtx2RequireScope(t, loaded, riGs, rks)

	for _, tc := range []struct {
		name   string
		config []byte
		// about is in the error, so that the load fails for this reason.
		about string
	}{
		{"a secret key as RI_g", mtx2Config("["+riG.String()+", "+rks+"]", ""), "recordAgeRecipients"},
		{"a secret key as RK", mtx2Config("["+riGs+", "+rk.String()+"]", ""), "recordAgeRecipients"},
		{"one recipient", mtx2Config("["+riGs+"]", ""), "recordAgeRecipients"},
		{"three recipients", mtx2Config("["+riGs+", "+rks+", "+thirds+"]", ""), "recordAgeRecipients"},
		// Upstream's JSON decoder names the field's type, as for any key.
		{"a secret key instead of a list", mtx2Config(riG.String(), ""), "AgeRecipients"},
		{"RI_g as RK", mtx2Config("["+riGs+", "+riGs+"]", ""), "recordAgeRecipients"},
		{"an empty list", mtx2Config("[]", ""), "recordAgeRecipients"},
		{"an empty list, MPEG-TS", mtx2Config("[]", "    recordFormat: mpegts\n"), "recordAgeRecipients"},
		{"an empty list over recipients in pathDefaults",
			[]byte("pathDefaults:\n  recordAgeRecipients: " + pair + "\n" + string(mtx2Config("[]", ""))),
			"recordAgeRecipients"},
		{"null", mtx2Config("null", ""), "recordAgeRecipients"},
		// A block list whose entries were deleted: YAML reads the key as null.
		{"no value", mtx2Config("", ""), "recordAgeRecipients"},
		{"an empty list in pathDefaults", mtx2DefaultsConfig("[]", ""), "recordAgeRecipients"},
		{"null in pathDefaults", mtx2DefaultsConfig("null", ""), "recordAgeRecipients"},
		{"one recipient in pathDefaults", mtx2DefaultsConfig("["+riGs+"]", ""), "recordAgeRecipients"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(createTempFile(t, tc.config), nil, nil)
			mtx2RequireRefusal(t, err, tc.about, keys)
		})
	}
}

// TestMTX2_RejectsEmptyRecipientsFromTheAPI: a path that the control API adds
// with an empty list fails validation, and one with null fails to decode, as
// in a file.
func TestMTX2_RejectsEmptyRecipientsFromTheAPI(t *testing.T) {
	riGs, rks := mtx2Identity(t).Recipient().String(), mtx2Identity(t).Recipient().String()
	keys := []string{riGs, rks}

	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"two recipients", `{"record": true, "recordAgeRecipients": ["` + riGs + `", "` + rks + `"]}`, true},
		{"an empty list", `{"record": true, "recordAgeRecipients": []}`, false},
		{"null", `{"record": true, "recordAgeRecipients": null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, _, err := Load("", nil, nil)
			require.NoError(t, err)

			// As the API's path handlers do.
			var p OptionalPath
			err = jsonwrapper.Unmarshal([]byte(tc.body), &p)
			if err == nil {
				err = loaded.AddPath("cam01_main", &p)
				require.NoError(t, err)
				err = loaded.Validate(nil)
			}
			if tc.ok {
				require.NoError(t, err)
				mtx2RequireScope(t, loaded, riGs, rks)
				return
			}
			mtx2RequireRefusal(t, err, "recordAgeRecipients", keys)
		})
	}
}

// TestMTX2_RecipientsFromEnv: the environment sets recordAgeRecipients as one
// comma-separated variable, for a path or for pathDefaults. An empty variable
// is an empty list, so it fails the load as in a file, even over recipients
// that the file sets, and so does an indexed variable, which the loader does
// not read as a list.
func TestMTX2_RecipientsFromEnv(t *testing.T) {
	riGs, rks := mtx2Identity(t).Recipient().String(), mtx2Identity(t).Recipient().String()
	keys := []string{riGs, rks}
	// The environment cannot name a path with an underscore, so the file
	// declares the path that the variables of a path address.
	file := "paths:\n  cam01:\n    record: yes\n"

	for _, tc := range []struct {
		name, key, value string
		ok               bool
	}{
		{"a path", "MTX_PATHS_CAM01_RECORDAGERECIPIENTS", riGs + "," + rks, true},
		{"pathDefaults", "MTX_PATHDEFAULTS_RECORDAGERECIPIENTS", riGs + "," + rks, true},
		{"an empty variable for a path", "MTX_PATHS_CAM01_RECORDAGERECIPIENTS", "", false},
		{"an empty variable for pathDefaults", "MTX_PATHDEFAULTS_RECORDAGERECIPIENTS", "", false},
		{"one recipient", "MTX_PATHS_CAM01_RECORDAGERECIPIENTS", riGs, false},
		{"an indexed variable for a path", "MTX_PATHS_CAM01_RECORDAGERECIPIENTS_0", riGs, false},
		{"an indexed variable for pathDefaults", "MTX_PATHDEFAULTS_RECORDAGERECIPIENTS_0", riGs, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			loaded, _, err := Load(createTempFile(t, []byte(file)), nil, nil)
			if !tc.ok {
				mtx2RequireRefusal(t, err, "recordAgeRecipients", keys)
				return
			}
			require.NoError(t, err)
			scope, err := loaded.Paths["cam01"].RecordAgeScope()
			require.NoError(t, err)
			require.NotNil(t, scope)
			recipients, err := scope.Strings()
			require.NoError(t, err)
			require.Equal(t, [2]string{riGs, rks}, recipients)
		})
	}

	t.Run("an empty variable over the file's recipients", func(t *testing.T) {
		t.Setenv("MTX_PATHS_CAM01_RECORDAGERECIPIENTS", "")
		withRecipients := file + "    recordAgeRecipients: [" + riGs + ", " + rks + "]\n"
		_, _, err := Load(createTempFile(t, []byte(withRecipients)), nil, nil)
		mtx2RequireRefusal(t, err, "recordAgeRecipients", keys)
	})
}

// TestMTX2_CloneKeepsRecipients: Conf.Clone and Path.Clone copy the
// recipients, into a list of their own, so that the clone still encrypts.
func TestMTX2_CloneKeepsRecipients(t *testing.T) {
	riGs, rks := mtx2Identity(t).Recipient().String(), mtx2Identity(t).Recipient().String()
	loaded, _, err := Load(createTempFile(t, mtx2Config("["+riGs+", "+rks+"]", "")), nil, nil)
	require.NoError(t, err)
	original := loaded.Paths["cam01_main"]

	for _, tc := range []struct {
		name  string
		clone *Path
	}{
		{"Conf.Clone", loaded.Clone().Paths["cam01_main"]},
		{"Path.Clone", original.Clone()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, original, tc.clone)
			require.NotNil(t, tc.clone.RecordAgeRecipients)
			require.NotSame(t, original.RecordAgeRecipients, tc.clone.RecordAgeRecipients)

			cloned := Conf{Paths: map[string]*Path{"cam01_main": tc.clone}}
			mtx2RequireScope(t, &cloned, riGs, rks)

			(*tc.clone.RecordAgeRecipients)[0] = rks
			mtx2RequireScope(t, loaded, riGs, rks)
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

// TestMTX2_NoRecipientsRecordsInClear: without recordAgeRecipients, in the
// path and in pathDefaults, the path records in clear, in either format. A
// path without the key takes pathDefaults' recipients.
func TestMTX2_NoRecipientsRecordsInClear(t *testing.T) {
	for _, tc := range []struct{ name, config string }{
		{"no key", "paths:\n  cam01_main:\n    record: yes\n"},
		{"no key, MPEG-TS", "paths:\n  cam01_main:\n    record: yes\n    recordFormat: mpegts\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, _, err := Load(createTempFile(t, []byte(tc.config)), nil, nil)
			require.NoError(t, err)
			require.Nil(t, loaded.Paths["cam01_main"].RecordAgeRecipients)
			scope, err := loaded.Paths["cam01_main"].RecordAgeScope()
			require.NoError(t, err)
			require.Nil(t, scope)
		})
	}

	t.Run("no key in the path, recipients in pathDefaults", func(t *testing.T) {
		riGs, rks := mtx2Identity(t).Recipient().String(), mtx2Identity(t).Recipient().String()
		loaded, _, err := Load(createTempFile(t, mtx2DefaultsConfig("["+riGs+", "+rks+"]", "")), nil, nil)
		require.NoError(t, err)
		mtx2RequireScope(t, loaded, riGs, rks)
	})
}
