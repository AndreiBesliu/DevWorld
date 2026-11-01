package main

import (
	"path/filepath"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
)

// TestCameraStream_NoneIsAnError: record takes hubrec's camera, cam01_main,
// from the media folder's streams, and a list without it is an error, not
// a panic.
func TestCameraStream_NoneIsAnError(t *testing.T) {
	want := hubrec.Camera + "_" + hubrec.Profile
	if s, err := cameraStream(controller.Streams(t.TempDir())); err != nil || s.Path != want {
		t.Errorf("cameraStream of the media folder's streams = %+v, %v; want %s", s, err, want)
	}
	if s, err := cameraStream([]controller.Stream{{Path: "cam02_main"}}); err == nil {
		t.Errorf("cameraStream without %s = %+v, want an error", want, s)
	}
}

// TestRecordingPlace_TrailingSeparator: -out names the recording's own
// folder, with or without a trailing separator; either spelling gives the
// same run folder and name, so the recording never lands in out/out.
func TestRecordingPlace_TrailingSeparator(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mi9-rec")
	run, name := recordingPlace(out)
	if filepath.Join(run, name) != out {
		t.Errorf("recordingPlace(%q) = %q, %q; want the recording in %s", out, run, name, out)
	}
	spellings := []string{out + "/", out + "/."}
	if filepath.Separator != '/' {
		spellings = append(spellings, out+string(filepath.Separator))
	}
	for _, spelling := range spellings {
		if r, n := recordingPlace(spelling); r != run || n != name {
			t.Errorf("recordingPlace(%q) = %q, %q; want %q, %q as for %q", spelling, r, n, run, name, out)
		}
	}
}
