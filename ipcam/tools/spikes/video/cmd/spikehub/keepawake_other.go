//go:build !windows

package main

// defaultKeepAwake is nil outside Windows: the Linux hubs (a Raspberry Pi, a
// server) do not sleep on their own, and the spike asks nothing of them.
var defaultKeepAwake keepAwakeFunc
