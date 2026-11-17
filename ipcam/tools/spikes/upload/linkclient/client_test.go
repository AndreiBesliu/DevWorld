package linkclient

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"testing"
)

// fakeController accepts one link on 127.0.0.1, records the hello line,
// and answers with lines.
func fakeController(t *testing.T, lines ...string) (addr string, hello chan string) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	hello = make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		line, _ := bufio.NewReader(c).ReadString('\n')
		hello <- line
		for _, l := range lines {
			_, _ = c.Write([]byte(l + "\n"))
		}
	}()
	return ln.Addr().String(), hello
}

// sessionJSON is a session line as c1's link sends it, with its extra
// fields.
func sessionJSON() string {
	return `{"type":"session","rtspBase":"rtsp://127.0.0.1:8554","user":"u0123456789abcdef","pass":"` + "SENT" + `INEL-pass-0123456789abcdef",` +
		`"streams":["cam01_main","cam01_sub","cam02_sub"],"beaconPort":7082,"needles":["AAEC"],"sinkPort":7083,"sinkCert":"MIIB"}`
}

// TestLinkClient_HelloSession (g3-16): the hello line is exactly c1's, over
// tcp4, and a session line with c1's extra fields decodes.
func TestLinkClient_HelloSession(t *testing.T) {
	addr, hello := fakeController(t, `{"type":"note","text":"x"}`, sessionJSON())
	s, err := Dial(t.Context(), addr, "pc")
	if err != nil {
		t.Fatal(err)
	}
	if got := <-hello; got != `{"type":"hello","role":"host","host":"pc","version":"c1"}`+"\n" {
		t.Errorf("hello %q", got)
	}
	if s.RTSPBase != "rtsp://127.0.0.1:8554" || s.User != "u0123456789abcdef" || !strings.HasSuffix(s.Pass, "pass-0123456789abcdef") ||
		!slices.Equal(s.Streams, []string{"cam01_main", "cam01_sub", "cam02_sub"}) {
		t.Errorf("session %#v", s)
	}
	addr, _ = fakeController(t)
	if _, err := Dial(t.Context(), addr, "pc"); err == nil {
		t.Error("a link closed before the session: no error")
	}
}

// TestLinkClient_NeverLogsCredentials: the session's String, GoString and
// %v, the standard logger and Dial's errors never carry the credentials.
func TestLinkClient_NeverLogsCredentials(t *testing.T) {
	addr, _ := fakeController(t, sessionJSON())
	var logged bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(old) })
	s, err := Dial(t.Context(), addr, "pi")
	if err != nil {
		t.Fatal(err)
	}
	addr, _ = fakeController(t, `{"type":"session","user":"u0123456789abcdef","pass":"`+"SENT"+`INEL-pass-0123456789abcdef"}`)
	_, badErr := Dial(t.Context(), addr, "pi")
	out := strings.Join([]string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s", s, s, s, s), logged.String(), fmt.Sprint(badErr)}, "\n")
	for _, secret := range []string{s.Pass, s.User, "SENT" + "INEL"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q appears in:\n%s", secret, out)
		}
	}
}
