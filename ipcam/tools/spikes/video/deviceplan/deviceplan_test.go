package deviceplan

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The table of post-release task A, requirement 2, as the owner approved it.
func TestPlan_IsTheTableOfTheAutomaticRun(t *testing.T) {
	want := []Device{
		{ID: "chrome", Label: "Chrome", Platform: "chrome", PC: true, Steps: []string{"S10", "S1", "S2", "S3-9", "S3-16", "S4", "S7", "S9"}},
		{ID: "edge", Label: "Edge", Platform: "edge", PC: true, Steps: []string{"S10", "S1", "S2", "S3-9", "S9"}},
		{ID: "firefox", Label: "Firefox", Platform: "firefox", PC: true, Steps: []string{"S10", "S1", "S2", "S3-9", "S9"}},
		{ID: "ipcam-desktop", Label: "ipcam-desktop", Platform: "windows-tauri", PC: true, Steps: []string{"S10", "S1", "S2", "S3-16", "S4", "S7", "S9"}},
		{ID: "phone", Label: "Galaxy A54", Platform: "android", Steps: []string{
			"S10", "S1", "S2", "S3-9", Pause, "S3-4", "S4", "S5", "S6", "S7", "S8", "S9",
		}},
	}
	got := Devices()
	if len(got) != len(want) {
		t.Fatalf("%d devices, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ID != w.ID || g.Label != w.Label || g.Platform != w.Platform || g.PC != w.PC || !slices.Equal(g.Steps, w.Steps) {
			t.Errorf("device %d = %+v, want %+v", i, g, w)
		}
	}
	if PauseMinutes() != 10 {
		t.Errorf("the pause lasts %d min, want 10", PauseMinutes())
	}
}

func TestPlan_ScenariosLeaveOutThePause(t *testing.T) {
	phone, ok := ByPlatform("android")
	if !ok {
		t.Fatal("no device for the android platform")
	}
	want := []string{"S10", "S1", "S2", "S3-9", "S3-4", "S4", "S5", "S6", "S7", "S8", "S9"}
	if got := phone.Scenarios(); !slices.Equal(got, want) {
		t.Errorf("phone scenarios = %v, want %v", got, want)
	}
	if _, ok := ByPlatform("safari"); ok {
		t.Error("a platform outside the table has a device")
	}
}

// Every step names a scenario of the app (results.ts SCENARIO_IDS) or the pause, at most once.
func TestPlan_StepsAreKnownScenarios(t *testing.T) {
	known := []string{"S1", "S2", "S3-4", "S3-9", "S3-16", "S4", "S5", "S6", "S7", "S8", "S9", "S10", Pause}
	for _, d := range Devices() {
		seen := map[string]bool{}
		for _, step := range d.Steps {
			if !slices.Contains(known, step) {
				t.Errorf("%s: unknown step %q", d.ID, step)
			}
			if seen[step] {
				t.Errorf("%s: step %q twice", d.ID, step)
			}
			seen[step] = true
		}
		if d.Steps[0] != "S10" {
			t.Errorf("%s starts with %s: S10 must run before any camera access", d.ID, d.Steps[0])
		}
	}
}

func TestPlan_Version(t *testing.T) {
	if Version() != "spike-video-v0.1.3" {
		t.Errorf("version %q", Version())
	}
}

// guideRow is a row of the guide's «Cât durează» table.
var guideRow = regexp.MustCompile(`^\| (PC|Telefon): ([^|]+?) \| ([^|]*) \|`)

var guidePause = regexp.MustCompile(`^pauză de (\d+) min$`)

// The owner's guide lists, per device, the scenarios in the order of the
// automatic run: the table here is its source, and the two must agree.
func TestPlan_TheGuideAgrees(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "..", "docs", "spike-video.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows := map[string][]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m := guideRow.FindStringSubmatch(scanner.Text())
		if m == nil || strings.TrimSpace(m[3]) == "" {
			continue
		}
		var steps []string
		for _, part := range strings.Split(m[3], ",") {
			part = strings.TrimSpace(part)
			if p := guidePause.FindStringSubmatch(part); p != nil {
				if minutes, _ := strconv.Atoi(p[1]); minutes != PauseMinutes() {
					t.Errorf("the guide's pause is %s min, the table's %d", p[1], PauseMinutes())
				}
				part = Pause
			}
			steps = append(steps, part)
		}
		rows[m[1]+": "+m[2]] = steps
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, d := range Devices() {
		where := "PC"
		if !d.PC {
			where = "Telefon"
		}
		label := where + ": " + d.Label
		steps, ok := rows[label]
		if !ok {
			t.Errorf("the guide has no row «%s»", label)
			continue
		}
		if !slices.Equal(steps, d.Steps) {
			t.Errorf("the guide's row «%s» lists %v, the table %v", label, steps, d.Steps)
		}
		delete(rows, label)
	}
	for label := range rows {
		t.Errorf("the guide's row «%s» is not in the table", label)
	}
}

// guideDuration is a duration in the guide's «Cât durează» table: «20 min», «1 oră și 35 min».
var guideDuration = regexp.MustCompile(`(?:(\d+) (?:oră|ore)(?: și (\d+) min)?|(\d+) min)`)

// guideTotal is the guide's estimate of the whole: «Aproximativ 3 ore și 30 de minute».
var guideTotal = regexp.MustCompile(`Aproximativ (\d+) (?:oră|ore)(?: și (\d+) de minute)?`)

// The time that the guide asks for in «Ce aveți nevoie» is the sum of its
// «Cât durează» table, so the owner plans the sessions on one number.
func TestPlan_TheGuidesTotalIsItsTable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "spike-video.md"))
	if err != nil {
		t.Fatal(err)
	}
	minutes := func(m []string) int {
		hours, _ := strconv.Atoi(m[1])
		rest, _ := strconv.Atoi(m[2])
		if len(m) > 3 && m[3] != "" {
			rest, _ = strconv.Atoi(m[3])
		}
		return hours*60 + rest
	}
	sum, rows, inTable := 0, 0, false
	total := -1
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "## "):
			inTable = line == "## Cât durează"
		case total < 0 && guideTotal.MatchString(line):
			total = minutes(guideTotal.FindStringSubmatch(line))
		case inTable && strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Unde |"):
			cells := strings.Split(line, "|")
			if len(cells) < 5 {
				t.Errorf("the table row %q has no duration column", line)
				continue
			}
			m := guideDuration.FindStringSubmatch(cells[3])
			if m == nil {
				t.Errorf("the table row %q names no duration", line)
				continue
			}
			sum += minutes(m)
			rows++
		}
	}
	if rows == 0 || total < 0 {
		t.Fatalf("the guide has %d rows in «Cât durează» and a total of %d min; want both", rows, total)
	}
	if total != sum {
		t.Errorf("the guide asks for %d h %d min, its table adds up to %d h %d min", total/60, total%60, sum/60, sum%60)
	}
}
