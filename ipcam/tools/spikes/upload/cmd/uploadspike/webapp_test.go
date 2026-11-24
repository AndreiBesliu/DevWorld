package main

import (
	"io/fs"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
)

// TestWebApp_EmbeddedTree (Task 12): uploadspike embeds the video app's
// web build from webapp/, which the repository holds only as a placeholder
// (a release build copies the app's dist/ there), so the handoff server
// gets the folder's tree.
func TestWebApp_EmbeddedTree(t *testing.T) {
	app := appFS()
	if _, err := fs.Stat(app, ".gitkeep"); err != nil {
		t.Errorf("the embedded app has no .gitkeep at its root: %v", err)
	}
}

// TestWebApp_URLs (Task 12): the web pairs' logins name the app's address:
// on the PC, where the hub runs, the loopback one; when the hub runs on
// the Pi, each of its LAN addresses.
func TestWebApp_URLs(t *testing.T) {
	if got := webAppURLs(false, 7091, nil); !slices.Equal(got, []string{"http://localhost:7091/app/"}) {
		t.Errorf("local: %v", got)
	}
	addrs := []net.IP{net.IPv4(192, 168, 1, 20), net.IPv4(10, 0, 0, 5)}
	want := []string{"http://192.168.1.20:7091/app/", "http://10.0.0.5:7091/app/"}
	if got := webAppURLs(true, 7091, addrs); !slices.Equal(got, want) {
		t.Errorf("remote: %v; want %v", got, want)
	}
}

// TestWebApp_PromptsNameNoOtherProgram (Task 12): the web logins' prompts
// point at the app's address that uploadspike prints, never at spikehub,
// which the upload release does not ship.
func TestWebApp_PromptsNameNoOtherProgram(t *testing.T) {
	for _, plan := range []string{mi6.PlanA, mi6.PlanB1} {
		for _, provider := range []string{mi6.ProviderDrive, mi6.ProviderOneDrive} {
			p := loginPrompt(mi6.PairDef{Dest: "x-web", Provider: provider, Platform: "web"}, plan)
			if strings.Contains(p, "spikehub") {
				t.Errorf("%s %s: %q names spikehub", plan, provider, p)
			}
		}
	}
	if !strings.Contains(msgWebApp, "%s") {
		t.Errorf("msgWebApp %q has no address", msgWebApp)
	}
}
