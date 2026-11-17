//go:build !windows

package main

// defaultOpenBrowser is nil outside Windows: the Linux hubs (a Raspberry Pi, a
// server) serve the phone, and nobody reads a browser on them.
var defaultOpenBrowser openBrowserFunc
