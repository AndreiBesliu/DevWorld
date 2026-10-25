package hostinfo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The test binary stands in for ipcam-hub when fakeEnv is set (TestMain):
// it appends its arguments as one JSON line to the file named by fakeArgs,
// and answers "diag host" with fakeHost and "diag pimodel" with
// fakePimodel, exiting with fakeExit (default 0).
const (
	fakeEnv     = "HOSTINFO_FAKE_HUB"
	fakeArgs    = "HOSTINFO_FAKE_ARGS"
	fakeHost    = "HOSTINFO_FAKE_HOST"
	fakePimodel = "HOSTINFO_FAKE_PIMODEL"
	fakeExit    = "HOSTINFO_FAKE_EXIT"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		os.Exit(fakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeHub(args []string) int {
	if path := os.Getenv(fakeArgs); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's file
		if err != nil {
			return 9
		}
		b, _ := json.Marshal(args)
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	switch {
	case len(args) >= 2 && args[0] == "diag" && args[1] == "host":
		fmt.Println(os.Getenv(fakeHost))
	case len(args) >= 2 && args[0] == "diag" && args[1] == "pimodel":
		fmt.Println(os.Getenv(fakePimodel))
	default:
		fmt.Fprintln(os.Stderr, "usage")
		return 1
	}
	if code := os.Getenv(fakeExit); code != "" {
		fmt.Fprintln(os.Stderr, "ipcam-hub: failed")
		var n int
		_, _ = fmt.Sscan(code, &n)
		return n
	}
	return 0
}

// The outputs of diag host and diag pimodel, as ipcam-hub prints them.
const (
	windowsHost = `{"schema":"ipcam.diag.host/1","os":"windows","arch":"amd64","physicalCores":6,"logicalCpus":12,"ramMb":32622,"piModel":null,"storage":"ssd","writeCache":"on","recommendedCameras":8,"cores6Plus":true}`
	pi4Host     = `{"schema":"ipcam.diag.host/1","os":"linux","arch":"arm64","physicalCores":4,"logicalCpus":4,"ramMb":3794,"piModel":{"raw":"Raspberry Pi 4 Model B Rev 1.5","revision":"d03115","tier":"H1b","supported":true,"known":true,"recommendedMin":4,"recommendedMax":4,"source":"cpuinfo"},"storage":"sd","writeCache":"unknown","recommendedCameras":4,"cores6Plus":false}`
	// No Model line in cpuinfo, as on a mainline kernel (G32).
	noModelHost    = `{"schema":"ipcam.diag.host/1","os":"linux","arch":"arm64","physicalCores":4,"logicalCpus":4,"ramMb":3794,"piModel":{"raw":"","revision":"","tier":"unknown","supported":false,"known":false,"recommendedMin":0,"recommendedMax":0,"source":"cpuinfo"},"storage":"ssd","writeCache":"on","recommendedCameras":0,"cores6Plus":false}`
	pi4Pimodel     = `{"schema":"ipcam.diag.pimodel/1","piModel":{"raw":"Raspberry Pi 4 Model B Rev 1.5","revision":"","tier":"H1b","supported":true,"known":true,"recommendedMin":4,"recommendedMax":4,"source":""}}`
	unknownPimodel = `{"schema":"ipcam.diag.pimodel/1","piModel":{"raw":"Acme Board 7","revision":"","tier":"unknown","supported":false,"known":false,"recommendedMin":0,"recommendedMax":0,"source":""}}`
)

// fake makes the test binary the ipcam-hub that Read runs, answering diag
// host with host, and returns the file of the arguments it was run with.
func fake(t *testing.T, host string) string {
	t.Helper()
	args := filepath.Join(t.TempDir(), "args.jsonl")
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeArgs, args)
	t.Setenv(fakeHost, host)
	return args
}

// calls reads the argument lists that the fake ipcam-hub was run with.
func calls(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // the test's file
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out [][]string
	s := bufio.NewScanner(f)
	for s.Scan() {
		var args []string
		if err := json.Unmarshal(s.Bytes(), &args); err != nil {
			t.Fatal(err)
		}
		out = append(out, args)
	}
	return out
}

func TestHostInfo_ReadsDiagHost(t *testing.T) {
	args := fake(t, windowsHost)
	rec := filepath.Join(t.TempDir(), "rec")
	var out strings.Builder
	info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: rec, In: strings.NewReader("5\n"), Out: &out})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := Info{OS: "windows", Arch: "amd64", PhysicalCores: 6, LogicalCPUs: 12, RAMMb: 32622, Storage: "ssd", WriteCache: "on", RecommendedCameras: 8, Cores6Plus: true}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("Read = %+v, want %+v", info, want)
	}
	if got := calls(t, args); !reflect.DeepEqual(got, [][]string{{"diag", "host", "--rec", rec}}) {
		t.Errorf("ipcam-hub ran with %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("Read asked %q on a PC", out.String())
	}

	t.Run("a known Pi", func(t *testing.T) {
		args := fake(t, pi4Host)
		info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: rec, DeviceTree: filepath.Join(t.TempDir(), "absent")})
		if err != nil {
			t.Fatal(err)
		}
		wantPi := &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Revision: "d03115", Tier: "H1b", Source: "cpuinfo", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4}
		if !reflect.DeepEqual(info.PiModel, wantPi) || info.RecommendedCameras != 4 || info.Storage != "sd" {
			t.Errorf("Read = %+v (model %+v)", info, info.PiModel)
		}
		if got := calls(t, args); len(got) != 1 {
			t.Errorf("ipcam-hub ran %d times, want once: the model is known", len(got))
		}
	})

	for name, c := range map[string]struct{ host, exit string }{
		"a failing ipcam-hub": {windowsHost, "1"},
		"not JSON":            {"ipcam-hub 0.1", ""},
		"another schema":      {strings.Replace(windowsHost, "ipcam.diag.host/1", "ipcam.diag.host/2", 1), ""},
	} {
		t.Run(name, func(t *testing.T) {
			fake(t, c.host)
			t.Setenv(fakeExit, c.exit)
			if info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: rec}); err == nil {
				t.Errorf("Read = %+v, nil error", info)
			}
		})
	}
	t.Run("no ipcam-hub", func(t *testing.T) {
		if _, err := Read(t.Context(), Options{Hub: filepath.Join(t.TempDir(), "ipcam-hub"), RecDir: rec}); err == nil {
			t.Error("Read = nil error")
		}
	})
}

// TestHostInfo_UnknownModelAsksAndDefaultsTo1 (C24): when the model stays
// unknown, the owner gives the camera count on screen; Enter, or the end of
// the input, means 1. The count is an estimate.
func TestHostInfo_UnknownModelAsksAndDefaultsTo1(t *testing.T) {
	for _, c := range []struct {
		name, in string
		want     int
		reasks   int
	}{
		{"Enter", "\n", 1, 0},
		{"end of input", "", 1, 0},
		{"a number", " 3 \n", 3, 0},
		{"wrong answers first", "doi\n0\n17\n2\n", 2, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake(t, noModelHost)
			var out strings.Builder
			info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: t.TempDir(), DeviceTree: filepath.Join(t.TempDir(), "absent"), In: strings.NewReader(c.in), Out: &out})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			m := info.PiModel
			if m == nil || !m.Estimate || m.Known || m.RecommendedMin != c.want || m.RecommendedMax != c.want || info.RecommendedCameras != c.want {
				t.Errorf("Read = %+v (model %+v), want an estimate of %d", info, m, c.want)
			}
			if got := strings.Count(out.String(), "Câte camere"); got != 1 {
				t.Errorf("the prompt shows %d times in %q, want once", got, out.String())
			}
			if got := strings.Count(out.String(), "Scrieți un număr de la 1 la 16"); got != c.reasks {
				t.Errorf("%d corrections in %q, want %d", got, out.String(), c.reasks)
			}
		})
	}

	t.Run("no input: no prompt", func(t *testing.T) {
		fake(t, noModelHost)
		info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: t.TempDir(), DeviceTree: filepath.Join(t.TempDir(), "absent")})
		if err != nil {
			t.Fatal(err)
		}
		if m := info.PiModel; m == nil || m.Estimate || info.RecommendedCameras != 0 {
			t.Errorf("Read = %+v (model %+v), want the unknown model and no count", info, m)
		}
	})
}
