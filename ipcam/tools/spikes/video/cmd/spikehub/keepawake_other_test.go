//go:build !windows

package main

import (
	"strings"
	"testing"
)

// Only Windows sleeps under the owner's hub in this spike (R49): elsewhere
// spikehub asks nothing of the OS and the banner says nothing about sleep.
func TestKeepAwake_DoesNothingOutsideWindows(t *testing.T) {
	if defaultKeepAwake != nil {
		t.Error("the default keep-awake request is set outside Windows")
	}
	h := startHub(t, harnessOptions{})
	if banner := h.stdout.String(); strings.Contains(banner, "repaus") {
		t.Errorf("banner talks about sleep outside Windows:\n%s", banner)
	}
}
