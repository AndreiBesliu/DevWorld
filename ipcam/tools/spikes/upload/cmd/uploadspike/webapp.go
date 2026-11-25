// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"embed"
	"io/fs"
	"net"
	"strconv"
)

// webappFiles is the video app's web build, which the web pairs' logins
// open in the PC's browser (Task 12): no other program of the upload
// release serves it. The repository holds only webapp/.gitkeep (all: so
// that a dot file counts); a release build copies the app's dist/ into
// webapp/, as spikehub's build does into its web/.
//
//go:embed all:webapp
var webappFiles embed.FS

// appFS is the app's tree, for the handoff server's /app/.
func appFS() fs.FS {
	sub, err := fs.Sub(webappFiles, "webapp")
	if err != nil {
		panic(err) // the embed's own folder: never fails
	}
	return sub
}

// webAppURLs are the app's addresses for a web login: the loopback one
// when the browser runs on the hub's device, else each LAN address.
func webAppURLs(remote bool, port int, addrs []net.IP) []string {
	p := strconv.Itoa(port)
	if !remote {
		return []string{"http://localhost:" + p + "/app/"}
	}
	urls := make([]string, 0, len(addrs))
	for _, ip := range addrs {
		urls = append(urls, "http://"+net.JoinHostPort(ip.String(), p)+"/app/")
	}
	return urls
}
