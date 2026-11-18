//go:build windows

package main

import (
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// defaultOpenBrowser opens a page in the default browser on Windows.
var defaultOpenBrowser openBrowserFunc = openWindowsBrowser

// browserCommand is `rundll32 url.dll,FileProtocolHandler <url>`, which hands
// the address to the program registered for http, the default browser. The
// rundll32 of the system folder runs, never one found on the PATH.
func browserCommand(url string) (*exec.Cmd, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	//nolint:gosec // the hub's own loopback address, http://127.0.0.1:<port>
	return exec.Command(filepath.Join(system, "rundll32.exe"), "url.dll,FileProtocolHandler", url), nil
}

// openWindowsBrowser starts the browser command and does not wait for the
// browser; a goroutine collects rundll32 when it exits.
func openWindowsBrowser(url string) error {
	cmd, err := browserCommand(url)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
