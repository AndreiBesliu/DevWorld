//go:build !windows

package main

import "testing"

// spikehub opens a browser only on the owner's Windows PC: never on Linux or
// a Raspberry Pi, which serve phones and have no one at a screen.
func TestOpenBrowser_NeverOutsideWindows(t *testing.T) {
	if defaultOpenBrowser != nil {
		t.Error("a default browser opener is set outside Windows")
	}
}
