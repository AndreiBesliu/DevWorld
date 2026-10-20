// Package hub is the root of the ipcam hub module: hubd (spec §3.5) and its
// internal packages, which become production code.
//
// The MediaMTX fork in third_party/mediamtx is a nested module of its own,
// outside the Go workspace; it is built and tested with GOWORK=off from its
// directory. Its patches are listed in third_party/PATCHES.md.
package hub
