package controller

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// play reads path from the server at addr over TCP, as MediaMTX does, and
// returns the access units it receives. The client stops when the test ends.
func play(t *testing.T, addr, path, user, pass string) <-chan [][]byte {
	t.Helper()
	aus, err := tryPlay(t, addr, path, user, pass)
	if err != nil {
		t.Fatalf("play %s: %v", path, err)
	}
	return aus
}

// tryPlay is play, returning the error of DESCRIBE, SETUP or PLAY.
func tryPlay(t *testing.T, addr, path, user, pass string) (<-chan [][]byte, error) {
	t.Helper()
	raw := "rtsp://" + addr + "/" + path
	if user != "" {
		raw = "rtsp://" + url.UserPassword(user, pass).String() + "@" + addr + "/" + path
	}
	u, err := base.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	tcp := gortsplib.ProtocolTCP
	c := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &tcp}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	desc, _, err := c.Describe(u)
	if err != nil {
		return nil, err
	}
	var forma *format.H264
	medi := desc.FindFormat(&forma)
	if medi == nil {
		t.Fatal("the stream has no H.264 track")
	}
	dec, err := forma.CreateDecoder()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		return nil, err
	}
	aus := make(chan [][]byte, 4096)
	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		au, err := dec.Decode(pkt)
		if err != nil {
			return // more packets are needed
		}
		owned := make([][]byte, len(au))
		for i, n := range au {
			owned[i] = bytes.Clone(n)
		}
		select {
		case aus <- owned:
		default:
			t.Error("the test client fell behind")
		}
	})
	if _, err := c.Play(nil); err != nil {
		return nil, err
	}
	return aus, nil
}

// receive returns the next access unit, failing the test after 10 s.
func receive(t *testing.T, aus <-chan [][]byte) [][]byte {
	t.Helper()
	select {
	case au := <-aus:
		return au
	case <-time.After(10 * time.Second):
		t.Fatal("no access unit within 10 s")
		return nil
	}
}

// newestIndex returns the newest index of stream in the send log.
func newestIndex(l *SendLog, stream string) (uint64, bool) {
	newest, found := uint64(0), false
	for i := uint64(0); ; i++ {
		if _, ok := l.SendTime(stream, i); !ok {
			if found {
				return newest, true
			}
			if i > 1<<16 {
				return 0, false
			}
			continue
		}
		newest, found = i, true
	}
}

func testCredentials(t *testing.T) Credentials {
	t.Helper()
	creds, err := NewCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return creds
}

// serveMain serves main.h264 as cam01_main on a free loopback port.
func serveMain(t *testing.T, creds Credentials, sendLog *SendLog) *Server {
	t.Helper()
	streams := []Stream{{Path: "cam01_main", File: mediaFile(t, "main.h264"), FPS: 20, Camera: 1}}
	srv, err := ServePrecoded(t.Context(), "127.0.0.1:0", streams, creds, sendLog, source.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// TestServePrecoded_StampsEveryAU (C15): every access unit that a session
// receives carries its index in an SEI, the indexes of a session increase by
// one, each is in the send log, and behind the SEI is that frame of the file.
func TestServePrecoded_StampsEveryAU(t *testing.T) {
	creds := testCredentials(t)
	sendLog := NewSendLog()
	srv := serveMain(t, creds, sendLog)
	file, err := source.ReadAnnexB(mediaFile(t, "main.h264"))
	if err != nil {
		t.Fatal(err)
	}

	aus := play(t, srv.Addr().String(), "cam01_main", creds.User, creds.Pass)
	var indexes []uint64
	for i := range 100 {
		au := receive(t, aus)
		index, ok := ReadIndex(au)
		if !ok {
			t.Fatalf("access unit %d carries no index", i)
		}
		if i > 0 && index != indexes[i-1]+1 {
			t.Fatalf("access unit %d has index %d after %d", i, index, indexes[i-1])
		}
		indexes = append(indexes, index)
		want := file[index%uint64(len(file))].NALUs
		if len(au) != len(want)+1 || !slices.EqualFunc(au[1:], want, bytes.Equal) {
			t.Errorf("index %d: the access unit behind the SEI is not frame %d of the file", index, index%uint64(len(file)))
		}
	}
	// The simulator hands a frame to the sessions before its SentRecord
	// reaches the send log, which records them in order: once the last
	// index is there, every earlier one must be too.
	last := indexes[len(indexes)-1]
	waitUntil(t, "the send log has the last index", func() bool {
		_, ok := sendLog.SendTime("cam01_main", last)
		return ok
	})
	for _, index := range indexes {
		if _, ok := sendLog.SendTime("cam01_main", index); !ok {
			t.Errorf("index %d is not in the send log", index)
		}
	}
}

// TestServePrecoded_NewSessionStartsAtCachedIDR: a session that starts in
// the middle of a GOP gets the GOP from its keyframe at once, as a camera
// with a GOP cache sends it, so MediaMTX starts its first part without
// waiting for the next keyframe.
func TestServePrecoded_NewSessionStartsAtCachedIDR(t *testing.T) {
	creds := testCredentials(t)
	sendLog := NewSendLog()
	srv := serveMain(t, creds, sendLog)

	for session := range uint64(2) {
		// Join 10 frames into a GOP (main.h264 has a keyframe every 20).
		waitUntil(t, "the simulator is in the middle of a GOP", func() bool {
			n, ok := newestIndex(sendLog, "cam01_main")
			return ok && n%20 == 10 && n > 20*(session+1)
		})
		aus := play(t, srv.Addr().String(), "cam01_main", creds.User, creds.Pass)
		// The newest frame sent when PLAY returned: a session that waited for
		// the next keyframe would start after it.
		atPlay, _ := newestIndex(sendLog, "cam01_main")
		first := receive(t, aus)
		index, ok := ReadIndex(first)
		if !ok {
			t.Fatalf("session %d: the first access unit carries no index", session)
		}
		if !h264.IsRandomAccess(first) || index%20 != 0 {
			t.Errorf("session %d: the first access unit (index %d) is not a keyframe", session, index)
		}
		if index > atPlay {
			t.Errorf("session %d: the first index %d came after PLAY (newest %d then); want the cached keyframe", session, index, atPlay)
		}
		for i := uint64(1); i <= 25; i++ {
			if got, _ := ReadIndex(receive(t, aus)); got != index+i {
				t.Fatalf("session %d: index %d after %d", session, got, index+i-1)
			}
		}
	}
}

// writeSynthStream writes n synthetic H.264 access units as an Annex B file,
// for the tests that need no real media.
func writeSynthStream(t *testing.T, n, gop int) string {
	t.Helper()
	var b bytes.Buffer
	for _, au := range synthAUs(n, gop) {
		for _, nalu := range au.NALUs {
			b.Write([]byte{0, 0, 0, 1})
			b.Write(nalu)
		}
	}
	path := filepath.Join(t.TempDir(), "synth.h264")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestServePrecoded_NoCredentialInLogs (C15, PH0-22): reccontroller's RTSP
// credentials appear in no log line and no error, also when a client logs in
// with a wrong password or without any, and they print as redacted.
func TestServePrecoded_NoCredentialInLogs(t *testing.T) {
	logs := captureLogs(t)
	creds := testCredentials(t)
	other := testCredentials(t)
	if creds.User == other.User || creds.Pass == other.Pass || len(creds.Pass) < 32 {
		t.Fatalf("NewCredentials gave %d-character passwords that repeat; want random ones", len(creds.Pass))
	}
	streams := []Stream{{Path: "cam01_main", File: writeSynthStream(t, 40, 20), FPS: 20, Camera: 1}}
	srv, err := ServePrecoded(t.Context(), "127.0.0.1:0", streams, creds, NewSendLog(), source.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	addr := srv.Addr().String()

	// flip changes the first character of a secret.
	flip := func(s string) string {
		if s[0] == 'a' {
			return "b" + s[1:]
		}
		return "a" + s[1:]
	}
	var texts []string
	for name, login := range map[string][2]string{
		"wrong password": {creds.User, flip(creds.Pass)},
		"wrong user":     {flip(creds.User), creds.Pass},
		"no credentials": {"", ""},
	} {
		if _, err := tryPlay(t, addr, "cam01_main", login[0], login[1]); err == nil {
			t.Errorf("%s: the server let the client play", name)
		} else {
			texts = append(texts, err.Error())
		}
	}
	if _, err := tryPlay(t, addr, "cam42_main", creds.User, creds.Pass); err == nil {
		t.Error("an unknown path played")
	} else {
		texts = append(texts, err.Error())
	}
	receive(t, play(t, addr, "cam01_main", creds.User, creds.Pass))

	slog.Info("credentials", "creds", creds)
	texts = append(texts, fmt.Sprint(creds), fmt.Sprintf("%+v", creds), fmt.Sprintf("%#v", creds))
	srv.Close()

	captured := logs.String()
	if !strings.Contains(captured, "cam01_main") {
		t.Errorf("the server logged nothing about the refused logins:\n%s", captured)
	}
	all := strings.Join(append(texts, captured), "\n")
	for name, secret := range map[string]string{"user": creds.User, "password": creds.Pass} {
		if strings.Contains(all, secret) {
			t.Errorf("the %s appears in the logs or errors:\n%s", name, all)
		}
	}
}
