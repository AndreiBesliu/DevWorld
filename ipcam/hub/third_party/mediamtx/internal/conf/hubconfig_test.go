package conf

// TestMTX0_AcceptsHubGeneratedConfig: the configuration that ipcam-hub
// generates (hub/internal/recorder.GenerateConfig) loads through this fork's
// own loader and validation, with the values that the hub means. A check of
// the YAML alone could not catch a key that MediaMTX rejects or reads
// otherwise.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	hubrecorder "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

func TestMTX0_AcceptsHubGeneratedConfig(t *testing.T) {
	riG, rk := mtx2Identity(t), mtx2Identity(t)
	riGs, rks := riG.Recipient().String(), rk.Recipient().String()
	scope, err := crypto.ParseKeyScope(riGs, rks)
	require.NoError(t, err)

	recDir := filepath.Join(t.TempDir(), "rec")
	hook := []string{filepath.Join(t.TempDir(), "ipcam hub", "ipcam-hub"), "hook", "segment-complete"}
	clear := hubrecorder.NewJob("cam1", "main", "rtsp://127.0.0.1:8554/cam1/main", nil)
	padded := hubrecorder.NewJob("cam1", "sub", "rtsp://127.0.0.1:8554/cam1/sub", &scope)
	unpadded := hubrecorder.NewJob("cam2", "main", "rtsp://127.0.0.1:8554/cam2/main", &scope)
	unpadded.PadLowBitrate = false
	unpadded.SegmentSecs = 10

	for name, jobs := range map[string][]hubrecorder.Job{
		"a clear job":                    {clear},
		"a scoped job with padding on":   {padded},
		"a scoped job with padding off":  {unpadded},
		"the three jobs in one":          {clear, padded, unpadded},
		"no job, before the first Apply": nil,
	} {
		t.Run(name, func(t *testing.T) {
			generated, err := hubrecorder.GenerateConfig(jobs, hook, recDir)
			require.NoError(t, err)
			loaded, _, err := Load(createTempFile(t, generated), nil, nil)
			require.NoError(t, err)

			for server, on := range map[string]bool{
				"rtsp": loaded.RTSP, "rtmp": loaded.RTMP, "hls": loaded.HLS, "webrtc": loaded.WebRTC, "srt": loaded.SRT,
				"moq": loaded.MoQ, "playback": loaded.Playback, "api": loaded.API, "metrics": loaded.Metrics, "pprof": loaded.PPROF,
			} {
				require.False(t, on, "the %s server is on", server)
			}
			require.Len(t, loaded.Paths, len(jobs))
			for _, j := range jobs {
				p, ok := loaded.Paths[string(j.Cam)+"/"+string(j.Profile)]
				require.True(t, ok, "no path for %s/%s", j.Cam, j.Profile)
				require.Equal(t, j.RestreamURL, p.Source)
				require.NotNil(t, p.RTSPTransport.Protocol, "rtspTransport is automatic")
				require.Equal(t, gortsplib.ProtocolTCP, *p.RTSPTransport.Protocol)
				require.True(t, p.Record)
				require.Equal(t, filepath.ToSlash(recDir)+"/%path/%Y-%m-%d/%H%M%S_%f", p.RecordPath)
				require.Equal(t, RecordFormatFMP4, p.RecordFormat)
				require.Equal(t, Duration(time.Second), p.RecordPartDuration)
				require.Equal(t, Duration(time.Duration(j.SegmentSecs)*time.Second), p.RecordSegmentDuration)
				require.Equal(t, Duration(0), p.RecordDeleteAfter, "MediaMTX's cleaner would delete recordings")

				ageScope, err := p.RecordAgeScope()
				require.NoError(t, err)
				if j.Scope == nil {
					require.Nil(t, ageScope, "a clear job records encrypted")
				} else {
					require.NotNil(t, ageScope, "a scoped job records in clear")
					recipients, err := ageScope.Strings()
					require.NoError(t, err)
					require.Equal(t, [2]string{riGs, rks}, recipients)
					require.Equal(t, j.PadLowBitrate, p.RecordAgePadLowBitrate)
				}

				args, err := shellquote.Split(p.RunOnRecordSegmentComplete)
				require.NoError(t, err)
				require.Equal(t, hook, args, "MediaMTX would run another hook command")
			}
		})
	}
}
