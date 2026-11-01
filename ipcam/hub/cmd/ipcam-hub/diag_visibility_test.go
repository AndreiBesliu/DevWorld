package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// The MI6 pairs of the fixture and the client types of their logins.
var visibilityPairs = map[string]string{"g-android": "android", "g-windows": "desktop", "g-web": "tv"}

// TestDiagVisibility_MatrixCellAndHideMode (P21): with the Desktop client's
// files hidden from the TV client (HideFrom), the desktop→tv cell is
// listed: false, read: false, and every other judged cell of the hub's
// clients is listed and read; a same-client cell is self. The diag creates
// nothing under state/ but SQLite's own files, and the rotated refresh
// tokens it stored keep the hub's logins alive.
func TestDiagVisibility_MatrixCellAndHideMode(t *testing.T) {
	f := newOAuthFixture(t)
	f.logIn(t, "g-android", oauth.PlatformAndroid, "acct-1")
	f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
	f.logIn(t, "g-web", oauth.PlatformWeb, "acct-1")
	f.drive.HideFrom(clientDesktop, clientTV)
	stateDir := filepath.Join(f.dataDir, "state")
	files := dirNames(t, stateDir)
	for _, creator := range []string{"g-android", "g-windows", "g-web"} {
		for _, reader := range []string{"g-android", "g-windows", "g-web"} {
			code, lines, stderr := runDiag(t, "visibility", "--config", f.config, "--creator", creator, "--reader", reader)
			if code != exitOK || len(lines) != 1 {
				t.Fatalf("%s→%s: diag visibility = %d, %v, %s", creator, reader, code, lines, stderr)
			}
			l := lines[0]
			self := creator == reader
			want := creator != "g-windows" || reader != "g-web"
			if l["schema"] != "ipcam.diag.visibility/1" || l["provider"] != "GOOGLE_DRIVE" || l["creator"] != visibilityPairs[creator] ||
				l["reader"] != visibilityPairs[reader] || l["self"] != self || l["listed"] != want || l["read"] != want {
				t.Errorf("%s→%s: %v, want self %v, listed and read %v", creator, reader, l, self, want)
			}
		}
	}
	// One probe file per creator, kept by its ID: the cells made no more.
	var probes []string
	for _, o := range f.drive.Objects() {
		if strings.HasPrefix(o.Name, "vis-") {
			probes = append(probes, o.Name)
			if string(o.Data) != "ipcam-visibility" {
				t.Errorf("%s holds %q, want the 16 fixed bytes", o.Name, o.Data)
			}
		}
	}
	slices.Sort(probes)
	if want := []string{"vis-android.txt", "vis-desktop.txt", "vis-tv.txt"}; !slices.Equal(probes, want) {
		t.Errorf("probe files %q, want %q", probes, want)
	}
	for _, name := range dirNames(t, stateDir) {
		if !slices.Contains(files, name) && name != "catalog.db-wal" && name != "catalog.db-shm" {
			t.Errorf("diag visibility made state/%s", name)
		}
	}
	for ref := range visibilityPairs {
		if _, err := f.store.Refresh(t.Context(), ref); err != nil {
			t.Errorf("the hub's refresh of %s after the diag: %v", ref, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "hub.lock")); !os.IsNotExist(err) {
		t.Errorf("diag visibility took the hub lock (%v)", err)
	}
}

// TestDiagVisibility_AccountsDiffer (P21, P45): a creator and a reader of
// two accounts stop the matrix: exit 5, the accounts-differ error, and no
// permission ID in the output.
func TestDiagVisibility_AccountsDiffer(t *testing.T) {
	f := newOAuthFixture(t)
	f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
	f.logIn(t, "g-android", oauth.PlatformAndroid, "acct-2")
	code, lines, stderr := runDiag(t, "visibility", "--config", f.config, "--creator", "g-windows", "--reader", "g-android")
	if code != exitAccountsDiffer || len(lines) != 1 || lines[0]["error"] != "accounts-differ" || lines[0]["schema"] != "ipcam.diag.visibility/1" {
		t.Fatalf("diag visibility = %d, %v, %s; want exit 5 and accounts-differ", code, lines, stderr)
	}
	for _, acct := range []string{"acct-1", "acct-2"} {
		id := fakegdrive.PermissionID(acct)
		if strings.Contains(stderr, id) {
			t.Errorf("stderr holds a permission ID")
		}
		for k, v := range lines[0] {
			if s, ok := v.(string); ok && strings.Contains(s, id) {
				t.Errorf("the line's %s holds a permission ID", k)
			}
		}
	}
}
