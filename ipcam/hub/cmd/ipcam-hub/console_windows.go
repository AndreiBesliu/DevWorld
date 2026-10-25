package main

import "golang.org/x/sys/windows"

var allocConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AllocConsole")

// ensureConsole gives ipcam-hub a console of its own when it was started
// without one (DETACHED_PROCESS, or a parent with none): MediaMTX inherits
// it, and the recorder's clean stop of MediaMTX, a Ctrl+Break to its process
// group, works only between processes that share a console. Without it,
// every stop would kill MediaMTX and leave its segments as .part files.
// AllocConsole fails when the hub already has a console, the usual case,
// and then nothing changes. Go keeps the standard handles that it read at
// start, so the events still go to the pipe that the parent reads.
func ensureConsole() { _, _, _ = allocConsole.Call() }
