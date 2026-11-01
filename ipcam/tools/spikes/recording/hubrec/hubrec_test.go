package hubrec

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
)

// The test binary stands in for ipcam-hub when spiketest.FakeHubEnv is set
// (spiketest.FakeHub).
func TestMain(m *testing.M) {
	if os.Getenv(spiketest.FakeHubEnv) == "1" {
		os.Exit(spiketest.FakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// leftIn is every recording folder and identity file left under root.
func leftIn(t *testing.T, root string) []string {
	t.Helper()
	var left []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && (d.Name() == IdentityFile || d.Name() == RecordingDir || strings.HasSuffix(path, ".mp4.age")) {
			left = append(left, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return left
}

// TestRecord_WhereWorkFindsAndRemovesIt (C01, PH0-22): Record runs one
// encrypted cam01_main job of the segment's length through ipcam-hub, for
// two recipients, and returns its first closed segment with the file of
// RI_g's identity, readable by its owner only, which decrypts it. A
// probe's Work finds the newest run's recording by its name (MI9 plays
// MI4's 5-min segment so), and removes every recording and identity while
// it keeps the logs; the next run's Start removes the ones before it.
func TestRecord_WhereWorkFindsAndRemovesIt(t *testing.T) {
	dir := t.TempDir()
	hub, jobs := spiketest.InstallFakeHub(t, dir)
	w := Work{Root: filepath.Join(dir, "work", "mi4"), Probe: "MI4"}
	record := func(at time.Time) Segment {
		t.Helper()
		run, err := w.Start(at)
		if err != nil {
			t.Fatal(err)
		}
		seg, err := Record(t.Context(), Options{Hub: hub, CameraURL: "rtsp://192.0.2.1:8554/cam01_main", Run: run, Name: FiveMin, SegmentSecs: 300})
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(run, FiveMin, IdentityFile); seg.Identity != want ||
			!strings.HasPrefix(seg.Path, filepath.Join(run, FiveMin, RecordingDir)+string(filepath.Separator)) {
			t.Errorf("Record = %+v, want the segment under %s and the identity %s", seg, filepath.Join(run, FiveMin, RecordingDir), want)
		}
		return seg
	}

	first := record(time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC))
	if got := spiketest.HubJobs(t, jobs); len(got) != 1 || got[0].Cam != Camera || got[0].Profile != Profile || got[0].SegmentSecs != 300 ||
		len(got[0].Recipients) != 2 || got[0].RestreamURL != "rtsp://192.0.2.1:8554/cam01_main" {
		t.Errorf("ipcam-hub's jobs: %+v; want one encrypted cam01_main job of 300 s for two recipients", got)
	}
	id, err := ReadIdentity(first.Identity)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := age.Decrypt(f, id)
	if err == nil {
		_, err = io.Copy(io.Discard, r)
	}
	_ = f.Close()
	if err != nil {
		t.Errorf("RI_g's identity does not decrypt the segment: %v", err)
	}
	if info, err := os.Stat(first.Identity); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("the identity file: %v, %v; want 0600", info, err)
	}
	if got, ok := w.Latest(FiveMin); !ok || got != first {
		t.Errorf("Latest(%s) = %+v, %v; want %+v", FiveMin, got, ok, first)
	}
	if _, ok := w.Latest("2min"); ok {
		t.Error("Latest found a recording that no run made")
	}

	second := record(time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC))
	if _, err := os.Stat(filepath.Join(filepath.Dir(first.Identity), "ipcam-hub.log")); err != nil {
		t.Errorf("the first run's log: %v, want it kept", err)
	}
	if left := leftIn(t, filepath.Dir(filepath.Dir(first.Identity))); len(left) != 0 {
		t.Errorf("the second run's Start left the first run's %q", left)
	}
	if got, ok := w.Latest(FiveMin); !ok || got != second {
		t.Errorf("Latest(%s) = %+v, %v; want the newest run's %+v", FiveMin, got, ok, second)
	}

	if err := w.RemoveRecordings(); err != nil {
		t.Fatal(err)
	}
	if left := leftIn(t, w.Root); len(left) != 0 {
		t.Errorf("RemoveRecordings left %q", left)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(second.Identity), "ipcam-hub.log")); err != nil {
		t.Errorf("the second run's log: %v, want it kept", err)
	}
	if got, ok := w.Latest(FiveMin); ok {
		t.Errorf("Latest found %+v after the removal", got)
	}
}

// TestWork_EndSaysWhenTheIdentityStays (PH0-22): when a run's recording and
// RI_g's identity cannot be removed, End shows a Romanian line that names
// the folder to delete by hand, and a run that would otherwise succeed
// fails with the removal's error; a run that failed keeps its own.
func TestWork_EndSaysWhenTheIdentityStays(t *testing.T) {
	for _, runErr := range []error{nil, errors.New("the run's own error")} {
		t.Run(fmt.Sprint(runErr), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "mi9")
			w := Work{Root: root, Probe: "MI9", RemoveAll: func(path string) error {
				if filepath.Base(path) == IdentityFile {
					return &fs.PathError{Op: "unlinkat", Path: path, Err: fs.ErrPermission}
				}
				return os.RemoveAll(path)
			}}
			run, err := w.Start(time.Now())
			if err != nil {
				t.Fatal(err)
			}
			key := filepath.Join(run, FiveMin, IdentityFile)
			if err := os.MkdirAll(filepath.Join(run, FiveMin, RecordingDir), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(key, []byte("AGE-SECRET-KEY-1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var said []string
			err = runErr
			w.End(&err, func(format string, args ...any) { said = append(said, fmt.Sprintf(format, args...)) })
			line := "Nu am putut șterge înregistrarea probei și cheia ei: ștergeți de mână dosarul " + root + "."
			if len(said) != 1 || said[0] != line {
				t.Errorf("End said %q, want «%s»", said, line)
			}
			switch {
			case runErr == nil && (err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), root)):
				t.Errorf("End made the run's error %v, want the removal's, which names %s", err, root)
			case runErr != nil && err != runErr: //nolint:errorlint // the very error the run had
				t.Errorf("End made the run's error %v, want its own: %v", err, runErr)
			}
			if _, err := os.Stat(filepath.Join(run, FiveMin, RecordingDir)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the recording: %v, want it removed even though its identity stays", err)
			}
		})
	}
}

// TestRecord_TakesOnlyAFullLengthSegment (mis-6): a segment that closed
// early (a source drop of more than 10 s, a restart of MediaMTX) is not
// the probe's: Record waits, up to one more segment length, for the next
// segment of the full length, and returns that one. When the next one
// closes early too, it fails with a Romanian line that says how long the
// segment lasted and asks to start the probe again.
func TestRecord_TakesOnlyAFullLengthSegment(t *testing.T) {
	t.Run("the first segment closes after 80 s", func(t *testing.T) {
		dir := t.TempDir()
		hub, _ := spiketest.InstallFakeHub(t, dir)
		t.Setenv(spiketest.FakeHubSegmentsEnv, "80,300")
		run := filepath.Join(dir, "run")
		seg, err := Record(t.Context(), Options{Hub: hub, CameraURL: "rtsp://192.0.2.1:8554/cam01_main", Run: run, Name: FiveMin, SegmentSecs: 300})
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(run, FiveMin, RecordingDir, "rec", Camera, Profile, "2026-10-29", "120120_000000.mp4.age"); seg.Path != want {
			t.Errorf("Record = %s, want the full segment after the short one, %s", seg.Path, want)
		}
	})
	t.Run("the next one closes early too", func(t *testing.T) {
		dir := t.TempDir()
		hub, _ := spiketest.InstallFakeHub(t, dir)
		t.Setenv(spiketest.FakeHubSegmentsEnv, "80,90")
		start := time.Now()
		_, err := Record(t.Context(), Options{Hub: hub, CameraURL: "rtsp://192.0.2.1:8554/cam01_main", Run: filepath.Join(dir, "run"),
			Name: FiveMin, SegmentSecs: 300})
		if err == nil || !strings.HasPrefix(err.Error(), "segmentul s-a închis după 90 s, nu după 5 min: porniți din nou proba") {
			t.Errorf("Record = %v, want the Romanian line of a segment that closed after 90 s", err)
		}
		if waited := time.Since(start); waited > time.Minute {
			t.Errorf("Record waited %v for a segment that could no longer be full", waited)
		}
	})
}
