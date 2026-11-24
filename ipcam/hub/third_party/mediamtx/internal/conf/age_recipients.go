package conf

import (
	"errors"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf/env"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf/jsonwrapper"
)

// AgeRecipients is the value of recordAgeRecipients (MTX-2,
// hub/third_party/PATCHES.md): RI_g then RK, as age1 strings, which
// Path.RecordAgeScope checks. Path holds it behind a pointer, nil when the key
// is absent, so that an empty list, null or an empty variable is not taken
// for an absent key and cannot record in clear.
type AgeRecipients []string

var (
	_ jsonwrapper.NullRejecter = AgeRecipients(nil)
	_ env.Unmarshaler          = (*AgeRecipients)(nil)
)

// RejectsJSONNull implements jsonwrapper.NullRejecter: null fails the load
// instead of leaving the pointer nil.
func (AgeRecipients) RejectsJSONNull() {}

// UnmarshalEnv implements env.Unmarshaler: one variable holds a
// comma-separated list. An empty variable is an empty list, which validation
// refuses.
func (r *AgeRecipients) UnmarshalEnv(_ string, v string) error {
	if r == nil {
		// The loader calls this for an unset key when another variable
		// starts with its name, as an indexed list would.
		return errors.New("'recordAgeRecipients' takes a comma-separated list in one variable")
	}
	if v == "" {
		*r = AgeRecipients{}
		return nil
	}
	*r = strings.Split(v, ",")
	return nil
}
