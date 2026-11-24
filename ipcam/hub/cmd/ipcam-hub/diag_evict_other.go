//go:build !linux

package main

import "os"

// dropFileCache does nothing where the system offers no unprivileged way to
// drop a file's pages: there the benches' reads are warm, which MI4 reports
// (G22).
func dropFileCache(*os.File) error { return nil }
