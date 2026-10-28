package mi1

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// testImageSize is the tests' image size: a raw scan reads the whole image,
// which the race detector slows down.
const testImageSize = 16 << 20

// fakeImageTools stands in for mkfs.ext4, losetup and mount, so that no
// root is needed: the image file itself is the loop device, and the mount
// point stays a plain folder. It records each call by the image or the
// folder it was given.
type fakeImageTools struct {
	mu                                      sync.Mutex
	formatted, attached, mounted, unmounted []string
	detached, flushed                       []string
	formatOrder                             []string // "format <image>" and "attach <image>", in order
}

func (f *fakeImageTools) note(list *[]string, s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*list = append(*list, s)
}

func (f *fakeImageTools) format(_ context.Context, image string) error {
	f.note(&f.formatted, image)
	f.note(&f.formatOrder, "format "+image)
	return nil
}

func (f *fakeImageTools) attach(_ context.Context, image string) (string, error) {
	f.note(&f.attached, image)
	f.note(&f.formatOrder, "attach "+image)
	return image, nil
}

func (f *fakeImageTools) mount(_ context.Context, device, _ string) error {
	f.note(&f.mounted, device)
	return nil
}

func (f *fakeImageTools) unmount(_ context.Context, dir string) error {
	f.note(&f.unmounted, dir)
	return nil
}

func (f *fakeImageTools) detach(_ context.Context, device string) error {
	f.note(&f.detached, device)
	return nil
}

func (f *fakeImageTools) flush(dir string) error {
	f.note(&f.flushed, dir)
	return nil
}

func (f *fakeImageTools) calls() (formatted, attached, mounted, unmounted, detached []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.formatted), slices.Clone(f.attached), slices.Clone(f.mounted), slices.Clone(f.unmounted), slices.Clone(f.detached)
}

// TestHub04_LabImage_FreshPerEncryptedRun (G08): on a Linux lab kill9
// shard every encrypted run records on a fresh image of its own, which its
// raw scans read. A clear run that recorded on an image leaves its ftyp and
// frames there, in live files and in freed blocks: an encrypted run's raw
// scan of that reused image fails, while the fresh image that the driver
// creates for it scans clean. The driver asks for exactly one new image per
// encrypted run (created, formatted, attached and mounted, then unmounted,
// detached and deleted once the run is reported) and none for clear runs,
// which record in a plain folder.
func TestHub04_LabImage_FreshPerEncryptedRun(t *testing.T) {
	sps := []byte{0x67, 0x4d, 0x40, 0x1f, 0xda, 0x02, 0x80, 0xbf, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00}
	needles := [][]byte{sps}
	tools := &fakeImageTools{}
	images := &Images{Root: t.TempDir(), tools: tools, size: testImageSize}
	ctx := t.Context()

	first, err := images.Prepare(ctx, "lab-linux-kill9-01-1", true)
	if err != nil {
		t.Fatalf("Prepare of an encrypted run: %v", err)
	}
	if found, err := first.ScanRaw(needles); err != nil || len(found) != 0 {
		t.Fatalf("the fresh image's raw scan = %+v, %v; want clean", found, err)
	}
	// A clear run records on that same image: its frames reach the image's
	// blocks, and its deletion frees them without clearing them.
	image := tools.formatted[0]
	clearRun := append(append([]byte("ftypmp42"), bytes.Repeat([]byte{0}, 4096)...), sps...)
	f, err := os.OpenFile(image, os.O_WRONLY, 0) //nolint:gosec // the test's own image
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(clearRun, testImageSize/2); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if found, err := first.ScanRaw(needles); err != nil || len(found) == 0 {
		t.Errorf("an encrypted run's raw scan of the reused image = %+v, %v; want the clear run's frames found", found, err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(image); !os.IsNotExist(err) {
		t.Errorf("the released image is still there: %v", err)
	}

	second, err := images.Prepare(ctx, "lab-linux-kill9-03-1", true)
	if err != nil {
		t.Fatalf("Prepare of the next encrypted run: %v", err)
	}
	if found, err := second.ScanRaw(needles); err != nil || len(found) != 0 {
		t.Errorf("the next encrypted run's fresh image = %+v, %v; want clean", found, err)
	}
	for _, id := range []string{"lab-linux-kill9-02-1", "lab-linux-kill9-04-1"} {
		v, err := images.Prepare(ctx, id, false)
		if err != nil {
			t.Fatalf("Prepare of the clear run %s: %v", id, err)
		}
		if found, err := v.ScanRaw(needles); err != nil || found != nil {
			t.Errorf("the clear run %s's raw scan = %+v, %v; want none: it has no image", id, found, err)
		}
		if info, err := os.Stat(v.DataDir()); err != nil || !info.IsDir() {
			t.Errorf("the clear run %s's data folder: %v", id, err)
		}
	}
	if err := second.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	formatted, attached, mounted, unmounted, detached := tools.calls()
	if len(formatted) != 2 || formatted[0] == formatted[1] || len(attached) != 2 || len(mounted) != 2 ||
		len(unmounted) != 2 || len(detached) != 2 {
		t.Errorf("images: formatted %v, attached %v, mounted %v, unmounted %v, detached %v; want one new image for each of the 2 encrypted runs",
			formatted, attached, mounted, unmounted, detached)
	}
	if want := []string{"format " + formatted[0], "attach " + formatted[0], "format " + formatted[1], "attach " + formatted[1]}; !slices.Equal(tools.formatOrder, want) {
		t.Errorf("calls %v, want each image formatted before it is attached", tools.formatOrder)
	}

	t.Run("the driver", func(t *testing.T) {
		r := newRig(t, "lab-linux")
		d := r.driver(t)
		tools := &fakeImageTools{}
		d.Volumes = &Images{Root: d.Work, tools: tools, size: testImageSize}
		if err := d.RunSet(t.Context(), SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true}); err != nil {
			t.Fatalf("RunSet: %v\nscreen:\n%s", err, r.out.String())
		}
		formatted, attached, mounted, unmounted, detached := tools.calls()
		if len(formatted) != 1 || len(attached) != 1 || len(mounted) != 1 || len(unmounted) != 1 || len(detached) != 1 {
			t.Errorf("a smoke run's encrypted run and clear run: formatted %v, attached %v, mounted %v, unmounted %v, detached %v; want one image",
				formatted, attached, mounted, unmounted, detached)
		}
		if len(tools.flushed) != 2 {
			t.Errorf("the encrypted run's image was flushed and scanned %d times, want 2: after the stop and after the reconcile", len(tools.flushed))
		}
		for _, rep := range r.f.runReports() {
			if !rep.Hub04Clear {
				t.Errorf("run %s: HUB-04 not clear\nscreen:\n%s", rep.RunID, r.out.String())
			}
		}
		if leftovers, _ := filepath.Glob(filepath.Join(d.Work, "*", imageFile)); len(leftovers) != 0 {
			t.Errorf("images left after the runs: %v", leftovers)
		}
	})
}

// TestLabImage_RealLoopDevice is the image of an encrypted lab run with
// the system's mkfs.ext4, losetup and mount, through sudo as the lab runner
// has it: the run's data folder is on the image, writable by recspike's
// user, and Release leaves no loop device and no image. It skips where sudo
// asks for a password or there are no loop devices, unless
// RECSPIKE_REQUIRE_LOOP=1.
func TestLabImage_RealLoopDevice(t *testing.T) {
	if err := exec.Command("sudo", "-n", "losetup", "--find").Run(); err != nil {
		if os.Getenv("RECSPIKE_REQUIRE_LOOP") == "1" {
			t.Fatalf("RECSPIKE_REQUIRE_LOOP=1, but sudo -n losetup --find: %v", err)
		}
		t.Skipf("no loop device through sudo -n: %v", err)
	}
	images := &Images{Root: t.TempDir(), size: testImageSize}
	v, err := images.Prepare(t.Context(), "lab-linux-kill9-01-1", true)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = v.Release()
		}
	})
	sps := []byte{0x67, 0x4d, 0x40, 0x1f, 0xda, 0x02, 0x80, 0xbf, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00}
	if found, err := v.ScanRaw([][]byte{sps}); err != nil || len(found) != 0 {
		t.Fatalf("the fresh image's raw scan = %+v, %v; want clean", found, err)
	}
	segment := filepath.Join(v.DataDir(), "rec", "cam01", "main", "segment.mp4")
	if err := os.MkdirAll(filepath.Dir(segment), 0o750); err != nil {
		t.Fatalf("the data folder on the image is not writable: %v", err)
	}
	if err := os.WriteFile(segment, append([]byte("clear frames: "), sps...), 0o600); err != nil {
		t.Fatal(err)
	}
	// Written, not yet synced: the scan flushes the file system first.
	if found, err := v.ScanRaw([][]byte{sps}); err != nil || len(found) != 1 {
		t.Errorf("the raw scan after a clear frame reached the image = %+v, %v; want it found", found, err)
	}
	device := v.(*imageVolume).device
	released = true
	if err := v.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if out, _ := exec.Command("losetup", "--list", "--noheadings", "--output", "NAME").Output(); bytes.Contains(out, []byte(device+"\n")) {
		t.Errorf("%s is still attached after Release", device)
	}
	if _, err := os.Stat(v.(*imageVolume).image); !os.IsNotExist(err) {
		t.Errorf("the image is still there after Release: %v", err)
	}
}

// TestLabImage_ReleasedWhenTheRunIsNotSaved: once Prepare has mounted an
// encrypted run's image, a run that fails before the run state holds it
// releases the image, as the run's report would. The next attempt has the
// same run id, and its fresh image must not collide with the leftover
// (O_EXCL): it records.
func TestLabImage_ReleasedWhenTheRunIsNotSaved(t *testing.T) {
	r := newRig(t, "lab-linux")
	d := r.driver(t)
	tools := &fakeImageTools{}
	d.Volumes = &Images{Root: d.Work, tools: tools, size: testImageSize}
	// A folder where the run's identity file goes: writeRun fails.
	runDir := filepath.Join(d.Work, ResultsSet(SetLabLinuxKill9, true)+"-01-1")
	block := filepath.Join(runDir, "ri_g.key")
	if err := os.MkdirAll(block, 0o700); err != nil {
		t.Fatal(err)
	}
	o := SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true}
	if err := d.RunSet(t.Context(), o); err == nil {
		t.Fatalf("RunSet with the identity file blocked = nil, want writeRun's error\nscreen:\n%s", r.out.String())
	}
	formatted, attached, mounted, unmounted, detached := tools.calls()
	if len(formatted) != 1 || len(attached) != 1 || len(mounted) != 1 || len(unmounted) != 1 || len(detached) != 1 {
		t.Errorf("the failed run's image: formatted %v, attached %v, mounted %v, unmounted %v, detached %v; want it released",
			formatted, attached, mounted, unmounted, detached)
	}
	if _, err := os.Stat(filepath.Join(runDir, imageFile)); !os.IsNotExist(err) {
		t.Errorf("the failed run's image is still there: %v", err)
	}

	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	if err := d.RunSet(t.Context(), o); err != nil {
		t.Fatalf("RunSet again: %v\nscreen:\n%s", err, r.out.String())
	}
	if reports := r.f.runReports(); len(reports) != smokeRuns || reports[0].RunID != filepath.Base(runDir) {
		t.Errorf("reports %+v: want the smoke's %d runs, the first as %s", reports, smokeRuns, filepath.Base(runDir))
	}
}
