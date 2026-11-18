package health

import (
	"bufio"
	"bytes"
	"strings"
)

// PiModel is the Raspberry Pi model that the hub runs on, and its row of
// §4.8's table (C24).
type PiModel struct {
	// Raw is the model as the board names it, such as "Raspberry Pi 4 Model
	// B Rev 1.5"; Revision is its revision code, such as "c03115".
	Raw      string `json:"raw"`
	Revision string `json:"revision"`
	// Tier is "H1", "H1b" or "H1c" for a supported model, "unsupported" for
	// an armv6 board, and "unknown" for a model that is not in the table.
	Tier string `json:"tier"`
	// Supported is true for H1, H1b and H1c; Known is false for "unknown".
	Supported bool `json:"supported"`
	Known     bool `json:"known"`
	// RecommendedMin and RecommendedMax are the recommended camera count
	// (O16), 0 for an unsupported or unknown model.
	RecommendedMin int `json:"recommendedMin"`
	RecommendedMax int `json:"recommendedMax"`
	// Source is where Raw comes from: "cpuinfo" in production (T39); the
	// spike may set "device-tree" (G32). ParsePiModel leaves it empty.
	Source string `json:"source"`
}

// The tiers of §4.8.
const (
	TierH1          = "H1"
	TierH1b         = "H1b"
	TierH1c         = "H1c"
	TierUnsupported = "unsupported"
	TierUnknown     = "unknown"
)

// piModelTable is §4.8's table in match order: a model matches the first
// row with a prefix that its name starts with, followed by a space or
// nothing. "Zero 2 W" comes before "Zero", and "Compute Module 3 Plus"
// before "Compute Module 3".
var piModelTable = []struct {
	prefixes []string
	tier     string
	min, max int
}{
	{[]string{"Raspberry Pi 5", "Raspberry Pi 500", "Raspberry Pi Compute Module 5"}, TierH1, 8, 8},
	{[]string{"Raspberry Pi 4", "Raspberry Pi 400", "Raspberry Pi Compute Module 4"}, TierH1b, 4, 4},
	{[]string{"Raspberry Pi Zero 2 W"}, TierH1c, 1, 2},
	{[]string{"Raspberry Pi 3", "Raspberry Pi 2 Model B Rev 1.1", "Raspberry Pi 2 Model B Rev 1.2",
		"Raspberry Pi Compute Module 3 Plus", "Raspberry Pi Compute Module 3"}, TierH1c, 1, 2},
	{[]string{"Raspberry Pi Model", "Raspberry Pi Zero", "Raspberry Pi Compute Module Rev"}, TierUnsupported, 0, 0},
}

// ParsePiModel applies §4.8's table to a model string, such as the Model
// line of /proc/cpuinfo or the contents of /proc/device-tree/model, without
// the spaces and NUL bytes around it. It reads no file. Any other board is
// "unknown", with Known false and no number.
func ParsePiModel(raw, revision string) PiModel {
	const cutset = " \t\r\n\x00"
	m := PiModel{Raw: strings.Trim(raw, cutset), Revision: strings.Trim(revision, cutset), Tier: TierUnknown}
	for _, row := range piModelTable {
		for _, p := range row.prefixes {
			if m.Raw == p || strings.HasPrefix(m.Raw, p+" ") {
				m.Tier, m.Known, m.Supported = row.tier, true, row.tier != TierUnsupported
				m.RecommendedMin, m.RecommendedMax = row.min, row.max
				return m
			}
		}
	}
	return m
}

// ReadPiModel reads the Model and Revision lines of cpuinfo (T39), which a
// container sees too, and applies ParsePiModel; Source is "cpuinfo". It
// keeps no other line: never Serial. Without a Model line, as on a mainline
// kernel or another board, the model is "unknown" with an empty Raw. When
// the file cannot be read, it returns that model and the error.
func ReadPiModel(cpuinfoPath string) (PiModel, error) {
	var raw, revision string
	b, err := readFile(cpuinfoPath)
	if err == nil {
		s := bufio.NewScanner(bytes.NewReader(b))
		for s.Scan() {
			key, value, ok := strings.Cut(s.Text(), ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "Model":
				raw = value
			case "Revision":
				revision = value
			}
		}
		err = s.Err()
	}
	m := ParsePiModel(raw, revision)
	m.Source = "cpuinfo"
	return m, err
}
