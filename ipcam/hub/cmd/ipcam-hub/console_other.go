//go:build !windows

package main

// ensureConsole does nothing outside Windows: MediaMTX's clean stop is a
// signal, which needs no console. Tests replace it.
var ensureConsole = func() {}
