package crypto_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// ENC-06's tools come from the environment, as the recorder job sets it:
// RECSPIKE_AGE_BINS lists age binaries (separated like PATH), RECSPIKE_RAGE_BIN
// is the rage binary, and RECSPIKE_REQUIRE_AGE_TOOLS=1 makes their absence a
// failure instead of a skip.
const (
	envAgeBins          = "RECSPIKE_AGE_BINS"
	envRageBin          = "RECSPIKE_RAGE_BIN"
	envRequireAgeTools  = "RECSPIKE_REQUIRE_AGE_TOOLS"
	maxAnnotationLength = 3500 // bytes of a notice line after annotate.sh's escaping (C32)
)

// ageTool is a binary that decrypts age files.
type ageTool struct {
	name string // as its --version says, e.g. "age v1.0.0" or "rage 0.12.1"
	path string
}

// ageTools returns the age binaries and rage, or nil when the environment
// names none and does not require them. It fails the test when they are
// required and missing, or when a named binary does not run.
func ageTools(t *testing.T) []ageTool {
	t.Helper()
	ageBins := filepath.SplitList(os.Getenv(envAgeBins))
	rage := os.Getenv(envRageBin)
	if len(ageBins) == 0 || rage == "" {
		if os.Getenv(envRequireAgeTools) == "1" {
			t.Fatalf("%s=1, but %s or %s is empty", envRequireAgeTools, envAgeBins, envRageBin)
		}
		t.Logf("no age CLI check: set %s and %s", envAgeBins, envRageBin)
		return nil
	}
	tools := make([]ageTool, 0, len(ageBins)+1)
	for _, path := range append(ageBins, rage) {
		out, err := exec.CommandContext(t.Context(), path, "--version").Output() //nolint:gosec // the tool comes from the job's environment
		if err != nil {
			t.Fatalf("%s --version: %v", path, err)
		}
		name := strings.TrimSpace(string(out))
		if path != rage {
			name = "age " + name
		}
		tools = append(tools, ageTool{name: name, path: path})
	}
	return tools
}

// writeIdentityFile writes an identity file that holds only id.
func writeIdentityFile(t *testing.T, id *age.X25519Identity) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recovery.key")
	content := fmt.Sprintf("# public key: %s\n%s\n", id.Recipient(), id)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// decryptWithTool decrypts file with the tool and the identity file alone.
func decryptWithTool(t *testing.T, tool ageTool, identityFile, file string) ([]byte, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), tool.path, "--decrypt", "-i", identityFile, file) //nolint:gosec // the tool comes from the job's environment
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// enc06Result is ENC-06's notice in the recorder job (C32).
type enc06Result struct {
	Code  string   `json:"code"`
	Tools []string `json:"tools"`
	Files int      `json:"files"`
	Pass  bool     `json:"pass"`
}

// annotationLength is the length of line once annotate.sh has escaped it:
// each "%" becomes "%25" (a single line has no line break to escape).
func annotationLength(line string) int {
	return len(line) + 2*strings.Count(line, "%")
}

// TestRecording_DecryptsWithAgeCLIAndRage is ENC-06: every golden file and a
// resealed torn file decrypt, with each age binary and with rage, to the same
// bytes, using only the RK identity file. It prints one "enc06: <json>" line,
// which the ENC-06 step of the recorder job turns into a notice.
func TestRecording_DecryptsWithAgeCLIAndRage(t *testing.T) {
	tools := ageTools(t)
	if tools == nil {
		t.Skipf("no age binaries (%s, %s)", envAgeBins, envRageBin)
	}
	riG, rk := goldenIdentities(t)
	identityFile := writeIdentityFile(t, rk)

	type check struct {
		path string
		want []byte
	}
	var checks []check
	for _, size := range goldenSizes {
		checks = append(checks, check{filepath.Join(goldenDir, goldenName(size)), pattern(size)})
	}
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", goldenScope(t))
	keep := torn.plaintext[:tornChunks*crypto.ChunkSize-1000]
	if _, err := openTorn(t, torn.path, riG).Reseal(t.Context(), int64(len(keep)), nil); err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	checks = append(checks, check{torn.path, keep})

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.name)
		for _, c := range checks {
			got, err := decryptWithTool(t, tool, identityFile, c.path)
			switch {
			case err != nil:
				t.Errorf("%s %s: %v", tool.name, filepath.Base(c.path), err)
			case !bytes.Equal(got, c.want):
				t.Errorf("%s %s: %d bytes that differ from the %d expected", tool.name, filepath.Base(c.path), len(got), len(c.want))
			}
		}
	}

	result, err := json.Marshal(enc06Result{Code: "ENC-06", Tools: names, Files: len(checks), Pass: !t.Failed()})
	if err != nil {
		t.Fatal(err)
	}
	line := "enc06: " + string(result)
	if n := annotationLength(line); n > maxAnnotationLength {
		t.Errorf("the enc06 line is %d bytes once escaped, over %d", n, maxAnnotationLength)
	}
	fmt.Println(line)
}
