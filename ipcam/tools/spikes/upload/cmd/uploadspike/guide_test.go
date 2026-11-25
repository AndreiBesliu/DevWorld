// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGuide_CoversOwnerChoices (Task 12 review): the owner's guide
// (docs/spike-upload.md) shows how to apply every answer to D1 and D3, gives
// setup the Android client's ID, names the elevated window's pause, and
// says where the data that uninstall keeps lives. The label check
// (check-guide-labels.sh) proves only that quoted texts exist.
func TestGuide_CoversOwnerChoices(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "docs", "spike-upload.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(b)
	for _, want := range []string{
		// D1: the same --drive on mi6 and mi5.
		"`--drive test+personal`", "`--drive personal`", "uploadspike mi6 --drive", "uploadspike mi5 --drive",
		// D3: B runs one camera per brand, C skips MI8.
		"uploadspike mi8 --one-per-brand", "D3-C",
		// setup: the button that the page shows, its file field, the
		// Android client's ID.
		"«" + pageSetupSave + "»<!--uploadspike-->", "«" + pageSetupFiles + "»<!--uploadspike-->",
		"«" + pageClientAndroid + "»<!--uploadspike-->", "`ipcam android` de la pasul 6",
		// The elevated copy's window and its pause.
		"«" + msgElevatedWindow + "»<!--uploadspike-->", "«" + msgPressEnterToClose + "»<!--uploadspike-->",
		// The data that uninstall keeps.
		"`" + baseDirName + "`", linuxBase,
		// B1 and the reader wait for the end of MI5 too.
		"`uploadspike mi6 --b1 …`", "`uploadspike mi6 reader …`",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("the guide lacks %q", want)
		}
	}
	if strings.Contains(guide, "Configurare automată") {
		t.Error("the guide names a button that the setup page no longer shows")
	}
}
