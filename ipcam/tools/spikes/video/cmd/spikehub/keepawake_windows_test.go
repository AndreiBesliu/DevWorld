//go:build windows

package main

import "testing"

// The request and its release work on Windows; a second release does nothing.
func TestKeepAwake_RequestsAndReleasesOnWindows(t *testing.T) {
	if defaultKeepAwake == nil {
		t.Fatal("no keep-awake request on Windows")
	}
	release, err := defaultKeepAwake()
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
}
