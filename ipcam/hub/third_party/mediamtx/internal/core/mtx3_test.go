package core

// The core test of patch MTX-3 (hub/third_party/PATCHES.md): the command that
// runOnRecordSegmentComplete starts gets the segment's final path, its start
// in Unix milliseconds, its duration and the MediaMTX path name, on top of the
// environment of the MediaMTX process. The hook command is this test binary,
// run as TestMTX3_HookHelperProcess, which writes what it sees to a file.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	amp4 "github.com/abema/go-mp4"
	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/recordstore"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/test"
)

const (
	// mtx3HookReportEnv names the file where TestMTX3_HookHelperProcess writes
	// its report. Its presence is what makes that test act as the hook. The
	// names avoid the MTX_ prefix, which MediaMTX reads as configuration.
	mtx3HookReportEnv = "MTX3_HOOK_REPORT"
	// mtx3ParentEnv is set in the MediaMTX process and must reach the hook.
	mtx3ParentEnv = "MTX3_PARENT_VARIABLE"
)

// mtx3HookReport is what the hook command saw when it ran.
type mtx3HookReport struct {
	Env []string
	// SegmentExists and PartExists say whether MTX_SEGMENT_PATH and
	// MTX_SEGMENT_PATH + ".part" existed at that moment.
	SegmentExists bool
	PartExists    bool
}

// TestMTX3_HookHelperProcess is the hook command of TestMTX3_HookEnvironment.
// Run in any other way, it does nothing.
func TestMTX3_HookHelperProcess(t *testing.T) {
	reportPath := os.Getenv(mtx3HookReportEnv)
	if reportPath == "" {
		return
	}

	segmentPath := os.Getenv("MTX_SEGMENT_PATH")
	_, segmentErr := os.Stat(segmentPath)
	_, partErr := os.Stat(segmentPath + ".part")
	report, err := json.Marshal(mtx3HookReport{
		Env:           os.Environ(),
		SegmentExists: segmentErr == nil,
		PartExists:    partErr == nil,
	})
	if err == nil {
		err = os.WriteFile(reportPath, report, 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	// Exit before the testing package prints its summary into MediaMTX's stdout.
	os.Exit(0)
}

// yamlSingleQuoted quotes s as a YAML single-quoted scalar, in which
// backslashes are literal, as in Windows paths.
func yamlSingleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// TestMTX3_HookEnvironment: the segment-complete command gets MTX_PATH,
// MTX_SEGMENT_PATH (the final path, which exists when the command runs, while
// the .part no longer does), MTX_SEGMENT_START (the segment's start in Unix
// milliseconds, decimal), MTX_SEGMENT_DURATION (seconds, as upstream formats
// it) and a variable of the MediaMTX process's environment.
func TestMTX3_HookEnvironment(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	// The command line quotes the path with single quotes (shellquote rules).
	require.NotContains(t, exe, "'")

	recordDir := t.TempDir()
	reportPath := filepath.Join(t.TempDir(), "hook.json")
	t.Setenv(mtx3HookReportEnv, reportPath)
	t.Setenv(mtx3ParentEnv, "kept from the parent environment")

	recordPath := filepath.Join(recordDir, "%path", "%Y-%m-%d_%H-%M-%S-%f")
	hookCmd := "'" + exe + "' -test.run=TestMTX3_HookHelperProcess"

	segmentDir := filepath.Join(recordDir, "cam01_main")
	var before, after time.Time

	func() {
		// Only the RTSP server, on TCP and loopback: the other servers would open
		// more ports, and MoQ would write a certificate into the working directory.
		p, ok := newInstance(t, "logLevel: warn\n"+
			"rtmp: no\n"+
			"hls: no\n"+
			"webrtc: no\n"+
			"srt: no\n"+
			"moq: no\n"+
			"rtspTransports: [tcp]\n"+
			"rtspAddress: 127.0.0.1:8554\n"+
			"pathDefaults:\n"+
			"  record: yes\n"+
			"  recordPath: "+yamlSingleQuoted(recordPath)+"\n"+
			"paths:\n"+
			"  cam01_main:\n"+
			"    runOnRecordSegmentComplete: "+yamlSingleQuoted(hookCmd)+"\n")
		require.True(t, ok)
		defer p.Close() // stops the recorder, which closes the segment, and waits for the hook

		media0 := test.UniqueMediaH264()
		tcp := gortsplib.ProtocolTCP
		source := gortsplib.Client{Protocol: &tcp}
		err = source.StartRecording("rtsp://127.0.0.1:8554/cam01_main",
			&description.Session{Medias: []*description.Media{media0}})
		require.NoError(t, err)
		defer source.Close()

		before = time.Now()
		for i := range 4 {
			err = source.WritePacketRTP(media0, &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					Marker:         true,
					PayloadType:    96,
					SequenceNumber: 1123 + uint16(i),
					Timestamp:      45343 + 90000*uint32(i),
					SSRC:           563423,
				},
				Payload: []byte{5},
			})
			require.NoError(t, err)
		}

		// The segment file appears when its first part is written.
		require.Eventually(t, func() bool {
			parts, _ := filepath.Glob(filepath.Join(segmentDir, "*.mp4.part"))
			return len(parts) == 1
		}, 10*time.Second, 10*time.Millisecond, "no segment was created")
		after = time.Now()
	}()

	raw, err := os.ReadFile(reportPath)
	require.NoError(t, err, "the hook did not run")
	var report mtx3HookReport
	require.NoError(t, json.Unmarshal(raw, &report))

	env := map[string]string{}
	for _, kv := range report.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}

	require.Equal(t, "cam01_main", env["MTX_PATH"])
	require.Equal(t, "kept from the parent environment", env[mtx3ParentEnv])

	// The final path: in the path's directory, the only file left there, and
	// already renamed when the hook ran.
	segmentPath := env["MTX_SEGMENT_PATH"]
	require.Equal(t, segmentDir, filepath.Dir(segmentPath))
	require.True(t, strings.HasSuffix(segmentPath, ".mp4"), segmentPath)
	require.True(t, report.SegmentExists, "%s did not exist when the hook ran", segmentPath)
	require.False(t, report.PartExists, "%s.part still existed when the hook ran", segmentPath)
	entries, err := os.ReadDir(segmentDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, filepath.Base(segmentPath), entries[0].Name())

	// The start in Unix milliseconds: the start that the file name encodes (in
	// microseconds), received while the test was sending.
	startText := env["MTX_SEGMENT_START"]
	require.Regexp(t, regexp.MustCompile(`^[0-9]+$`), startText)
	startMs, err := strconv.ParseInt(startText, 10, 64)
	require.NoError(t, err)
	var named recordstore.Path
	require.True(t, named.Decode(recordPath+".mp4", segmentPath))
	// MTX-4: the name is in UTC, which Decode, without %z, reads as local time.
	n := named.Start
	nameStart := time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), n.Minute(), n.Second(), n.Nanosecond(), time.UTC)
	require.Equal(t, nameStart.UnixMilli(), startMs, "MTX_SEGMENT_START against the file name")
	require.GreaterOrEqual(t, startMs, before.Add(-time.Second).UnixMilli())
	require.LessOrEqual(t, startMs, after.Add(time.Second).UnixMilli())

	// The duration in seconds, as upstream formats it, equal to the file's mvhd.
	content, err := os.ReadFile(segmentPath)
	require.NoError(t, err)
	boxes, err := amp4.ExtractBoxWithPayload(bytes.NewReader(content), nil,
		amp4.BoxPath{amp4.BoxTypeMoov(), amp4.BoxTypeMvhd()})
	require.NoError(t, err)
	require.Equal(t, 1, len(boxes))
	mvhd := boxes[0].Payload.(*amp4.Mvhd)
	require.Equal(t, uint32(1000), mvhd.Timescale)
	require.NotZero(t, mvhd.DurationV0)
	duration := time.Duration(mvhd.DurationV0) * time.Millisecond
	require.Equal(t, strconv.FormatFloat(duration.Seconds(), 'f', -1, 64), env["MTX_SEGMENT_DURATION"])
}
