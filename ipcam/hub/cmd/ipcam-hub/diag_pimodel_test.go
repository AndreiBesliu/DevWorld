package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
)

// TestDiagPimodel_RawUsesSameTableAndReadsNoFile (G32, T39): every model of
// health's TestReadPiModel_EverySpecRow gives, through diag pimodel --raw,
// the tier and numbers that ReadPiModel gives for it from cpuinfo: the
// spike's device-tree fallback uses §4.8's one table. diag pimodel reads
// nothing from the host: neither diag host's probe nor any file that health
// reads (the reference cpuinfo below is read before health's reads fail).
func TestDiagPimodel_RawUsesSameTableAndReadsNoFile(t *testing.T) {
	defer setHostProbe(func(string) hostFacts { t.Error("diag pimodel read the host"); return hostFacts{} })()
	readNothing := func(name string) ([]byte, error) {
		t.Errorf("diag pimodel read %s", name)
		return nil, errors.New("this test reads no file")
	}
	for _, raw := range []string{
		"Raspberry Pi 5 Model B Rev 1.0", "Raspberry Pi 500 Rev 1.0", "Raspberry Pi Compute Module 5 Rev 1.0",
		"Raspberry Pi 4 Model B Rev 1.5", "Raspberry Pi 400 Rev 1.1", "Raspberry Pi Compute Module 4 Rev 1.1",
		"Raspberry Pi 3 Model B Rev 1.2", "Raspberry Pi 3 Model B Plus Rev 1.3", "Raspberry Pi 3 Model A Plus Rev 1.1",
		"Raspberry Pi Zero 2 W Rev 1.0", "Raspberry Pi 2 Model B Rev 1.1", "Raspberry Pi 2 Model B Rev 1.2",
		"Raspberry Pi Compute Module 3 Rev 1.0", "Raspberry Pi Compute Module 3 Plus Rev 1.0",
		"Raspberry Pi Model B Rev 2", "Raspberry Pi Zero Rev 1.3", "Raspberry Pi Zero W Rev 1.1", "Raspberry Pi Compute Module Rev 1.0",
		"Banana Pi M5",
	} {
		t.Run(raw, func(t *testing.T) {
			cpuinfo := filepath.Join(t.TempDir(), "cpuinfo")
			if err := os.WriteFile(cpuinfo, []byte("processor\t: 0\nModel\t\t: "+raw+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			want, err := health.ReadPiModel(cpuinfo)
			if err != nil {
				t.Fatal(err)
			}
			want.Source = ""

			restore := health.SetReadFile(readNothing)
			code, line := diag(t, "diag", "pimodel", "--raw", raw)
			restore()
			if code != exitOK {
				t.Fatalf("diag pimodel exited %d", code)
			}
			var got struct {
				Schema  string         `json:"schema"`
				PiModel health.PiModel `json:"piModel"`
			}
			dec := json.NewDecoder(strings.NewReader(line))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("diag pimodel printed %q: %v", line, err)
			}
			if got.Schema != "ipcam.diag.pimodel/1" || got.PiModel != want {
				t.Errorf("diag pimodel = %+v, want schema ipcam.diag.pimodel/1 and %+v", got, want)
			}
		})
	}
}
