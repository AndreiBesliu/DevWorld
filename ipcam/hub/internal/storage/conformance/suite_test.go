package conformance_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/conformance"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	fakegraph "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/graph"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/onedrive"
)

const kib = 1 << 10

// ciOptions are the suite's options in CI (g2-12): 20 kills, the default
// object size of 3 × PartSize + 7 bytes.
var ciOptions = conformance.Options{KillOffsets: 20, Seed: 1}

// resumeChecks are the resume checks that each driver runs by name (P24).
var resumeChecks = []string{"Resume/KillAtRandomOffset", "Resume/CompleteSessionDetected"}

// wantResumeChecks fails t unless report ran both resume checks and skipped
// neither.
func wantResumeChecks(t *testing.T, report conformance.Report) {
	t.Helper()
	for _, name := range resumeChecks {
		if !slices.Contains(report.Ran, name) {
			t.Errorf("Report.Ran = %q, want it to hold %s", report.Ran, name)
		}
		if slices.Contains(report.Skipped, name) {
			t.Errorf("Report.Skipped = %q holds %s", report.Skipped, name)
		}
	}
}

// TestConformance_Local: the local driver passes the suite (STO-01), its
// resume checks included; it is the suite's baseline.
func TestConformance_Local(t *testing.T) {
	root := t.TempDir()
	report := conformance.Run(t, func(t *testing.T) storage.Destination {
		d, err := local.New(root)
		if err != nil {
			t.Fatalf("local.New: %v", err)
		}
		return d
	}, ciOptions)
	wantResumeChecks(t, report)
}

// TestConformance_FakeDrive: the Drive driver passes the suite against
// fakegdrive, with PartSize 256 KiB (g2-12). Each new driver shares the
// folder store, as ipcam-hub's share the catalog after a restart.
func TestConformance_FakeDrive(t *testing.T) {
	fake := fakegdrive.New(fakegdrive.Options{})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	folders := newMemFolders()
	report := conformance.Run(t, func(t *testing.T) storage.Destination {
		d, err := gdrive.New(gdrive.Options{
			DestID: "dst-1", HubID: "H", HTTPClient: srv.Client(), Tokens: testTokens(), BaseURL: srv.URL,
			PartSize: 256 * kib, Folders: folders,
		})
		if err != nil {
			t.Fatalf("gdrive.New: %v", err)
		}
		return d
	}, ciOptions)
	wantResumeChecks(t, report)
}

// TestConformance_FakeGraph: the OneDrive personal driver passes the suite
// against fakegraph, with PartSize 320 KiB (g2-12).
func TestConformance_FakeGraph(t *testing.T) {
	fake := fakegraph.New(fakegraph.Options{})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	report := conformance.Run(t, func(t *testing.T) storage.Destination {
		d, err := onedrive.New(onedrive.Options{
			DestID: "dst-od", HubID: "H", HTTPClient: srv.Client(), Tokens: testTokens(), BaseURL: srv.URL,
			PartSize: 320 * kib,
		})
		if err != nil {
			t.Fatalf("onedrive.New: %v", err)
		}
		return d
	}, ciOptions)
	wantResumeChecks(t, report)
}

// stubHelperEnv makes TestConformance_StubHelper run the suite on a driver
// that reports ResumeNone: TestConformance_FailsOnResumeNone sets it in a
// run of this test binary.
const stubHelperEnv = "CONFORMANCE_STUB_HELPER"

// TestConformance_FailsOnResumeNone (P24): the suite fails a driver that
// reports Resumable ResumeNone, in Caps/ResumableNotNone, and skips its
// resume checks. A failing subtest always fails its parent, and a
// *testing.T cannot be faked, so the test runs its own binary's
// TestConformance_StubHelper and reads its verdicts: Caps/ResumableNotNone
// and its parents fail, and nothing else does.
func TestConformance_FailsOnResumeNone(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestConformance_StubHelper$", "-test.v", "-test.count=1") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), stubHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("the helper run ended with %v, want a non-zero exit; its output:\n%s", err, out)
	}
	var failed, skipped []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "--- FAIL: "); ok {
			failed = append(failed, strings.Fields(name)[0])
		}
		if name, ok := strings.CutPrefix(line, "--- SKIP: "); ok {
			skipped = append(skipped, strings.Fields(name)[0])
		}
	}
	slices.Sort(failed)
	wantFailed := []string{
		"TestConformance_StubHelper", "TestConformance_StubHelper/Caps", "TestConformance_StubHelper/Caps/ResumableNotNone",
	}
	if !slices.Equal(failed, wantFailed) {
		t.Errorf("the helper's failed tests %q, want %q; its output:\n%s", failed, wantFailed, out)
	}
	for _, name := range resumeChecks {
		if !slices.Contains(skipped, "TestConformance_StubHelper/"+name) {
			t.Errorf("the helper's skipped tests %q, want them to hold %s", skipped, name)
		}
	}
}

// TestConformance_StubHelper runs the suite on the local driver as a stub
// that reports Resumable ResumeNone, when TestConformance_FailsOnResumeNone
// runs it; it skips otherwise.
func TestConformance_StubHelper(t *testing.T) {
	if os.Getenv(stubHelperEnv) != "1" {
		t.Skip("run by TestConformance_FailsOnResumeNone only")
	}
	root := t.TempDir()
	conformance.Run(t, func(t *testing.T) storage.Destination {
		d, err := local.New(root)
		if err != nil {
			t.Fatalf("local.New: %v", err)
		}
		return resumeNone{d}
	}, conformance.Options{KillOffsets: 1, Seed: 1, Size: 4 * kib})
}

// resumeNone is a Destination that reports Resumable ResumeNone, with its
// other Caps and its methods those of the Destination it holds.
type resumeNone struct{ storage.Destination }

func (r resumeNone) Capabilities() storage.Caps {
	caps := r.Destination.Capabilities()
	caps.Resumable = storage.ResumeNone
	return caps
}

// testTokens is the token source of the fakes, which accept any token.
func testTokens() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok-conformance", TokenType: "Bearer"})
}

// memFolders is a gdrive.FolderStore in memory that, as the catalog's, keeps
// the first ID stored for a path.
type memFolders struct {
	mu  sync.Mutex
	ids map[string]string
}

func newMemFolders() *memFolders { return &memFolders{ids: map[string]string{}} }

func (m *memFolders) Folder(_ context.Context, destID, p string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.ids[destID+"|"+p]
	return id, ok, nil
}

func (m *memFolders) PutFolder(_ context.Context, destID, p, remoteID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ids[destID+"|"+p]; !ok {
		m.ids[destID+"|"+p] = remoteID
	}
	return nil
}
