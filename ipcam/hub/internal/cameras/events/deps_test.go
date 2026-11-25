package events

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// onvifModule is the ONVIF types' module.
const onvifModule = "github.com/kerberos-io/onvif"

// allowedOnvif are the module's packages that the hub may build (P06): its
// types and nothing of its SOAP client, whose dependencies are LGPL or
// unlicensed.
var allowedOnvif = []string{
	onvifModule + "/event",
	onvifModule + "/event/topic",
	onvifModule + "/xsd",
	onvifModule + "/xsd/iso8601",
	onvifModule + "/xsd/onvif",
	onvifModule + "/device",
}

// forbiddenModules are the modules of the ONVIF client that never enter the
// hub (P06): juju/errors is LGPL, elgs/gostrgen has no license.
var forbiddenModules = []string{"github.com/juju/errors", "github.com/elgs/gostrgen"}

// hubRoot is the hub module's directory, the one above this package's that
// holds a go.mod.
func hubRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package")
		}
		dir = parent
	}
}

// TestDeps_OnvifAllowlist (P06): go list -deps ./hub/..., tests included,
// holds no package of kerberos-io/onvif outside the six allowed, and neither
// module github.com/juju/errors nor github.com/elgs/gostrgen.
func TestDeps_OnvifAllowlist(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-test",
		"-f", "{{.ImportPath}} {{with .Module}}{{.Path}}{{end}}", "./...")
	cmd.Dir = hubRoot(t)
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list -deps: %v\n%s", err, stderr)
	}
	sawEvent := false
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg, mod := fields[0], ""
		if len(fields) > 1 {
			mod = fields[1]
		}
		if mod == onvifModule {
			if !slices.Contains(allowedOnvif, pkg) {
				t.Errorf("the hub builds %s, outside the ONVIF allowlist", pkg)
			}
			sawEvent = sawEvent || pkg == onvifModule+"/event"
		}
		if slices.Contains(forbiddenModules, mod) {
			t.Errorf("the hub builds %s of the forbidden module %s", pkg, mod)
		}
	}
	if !sawEvent {
		t.Errorf("%s/event is not among the hub's dependencies; the check saw nothing", onvifModule)
	}
}
