//go:build windows

package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The opener hands the address to the shell's URL handler, the default
// browser, through rundll32 from the system folder. The test builds the
// command and does not start it.
func TestOpenBrowser_UsesTheFileProtocolHandler(t *testing.T) {
	if defaultOpenBrowser == nil {
		t.Fatal("no default browser opener on Windows")
	}
	cmd, err := browserCommand("http://127.0.0.1:7080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Base(cmd.Path), "rundll32.exe") || !filepath.IsAbs(cmd.Path) {
		t.Errorf("path %q, want rundll32.exe from the system folder", cmd.Path)
	}
	if want := []string{cmd.Path, "url.dll,FileProtocolHandler", "http://127.0.0.1:7080"}; !slices.Equal(cmd.Args, want) {
		t.Errorf("args %q, want %q", cmd.Args, want)
	}
}
