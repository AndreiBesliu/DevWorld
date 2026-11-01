package main

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDiagReach_FamiliesAndTargets (P15, P56): diag reach prints one line
// per probe. Its own loopback listeners answer, over IPv4 and, where the host
// has IPv6 loopback, IPv6; TEST-NET-3 on port 9, which no transparent proxy
// takes, is blocked within its 5-s timeout, with no IPv6 address; a closed
// port of 127.0.0.1 refuses, which is no block; a listener's port answers;
// and the LAN line names no address.
func TestDiagReach_FamiliesAndTargets(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	closed, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := closed.Addr().String()
	_ = closed.Close()
	open := ln.Addr().String()
	start := time.Now()
	code, lines, stderr := runDiag(t, "reach", "--hosts", "203.0.113.1:9,"+closedAddr+","+open, "--lan", open)
	if code != exitOK {
		t.Fatalf("diag reach = %d (%s), want 0", code, stderr)
	}
	if took := time.Since(start); took > 8*time.Second {
		t.Errorf("diag reach took %v; its probes run at once, 5 s each at most", took)
	}
	find := func(target, host, family string) map[string]any {
		t.Helper()
		for _, l := range lines {
			if l["schema"] != "ipcam.diag.reach/1" {
				t.Errorf("line %v has no reach schema", l)
			}
			if l["target"] == target && (host == "" || l["host"] == host) && l["family"] == family {
				return l
			}
		}
		t.Fatalf("no %s line of %s over %s in %v", target, host, family, lines)
		return nil
	}
	want := func(l map[string]any, ok, blocked bool, errText ...string) {
		t.Helper()
		if l["ok"] != ok || l["blocked"] != blocked || (len(errText) > 0 && !containsAny(l["err"], errText)) {
			t.Errorf("line %v, want ok %v, blocked %v, err in %q", l, ok, blocked, errText)
		}
	}
	testNet := find("provider", "203.0.113.1", "v4")
	want(testNet, false, true, "timeout", "unreachable")
	if ms := testNet["ms"].(float64); ms > 5500 {
		t.Errorf("the blocked probe took %v ms, want its 5-s timeout at most", ms)
	}
	if testNet["port"] != float64(9) {
		t.Errorf("line %v, want port 9", testNet)
	}
	want(find("provider", "203.0.113.1", "v6"), false, true, "no-address")
	closedHost, closedPort, _ := net.SplitHostPort(closedAddr)
	refused := find("provider", closedHost, "v4")
	want(refused, false, false, "refused")
	if refused["port"] != float64(mustAtoi(t, closedPort)) {
		t.Errorf("line %v, want port %s", refused, closedPort)
	}
	answered := 0
	for _, l := range lines {
		if l["target"] == "provider" && l["host"] == "127.0.0.1" && l["port"] == float64(mustAtoi(t, open[strings.LastIndex(open, ":")+1:])) && l["family"] == "v4" {
			want(l, true, false)
			answered++
		}
	}
	if answered != 1 {
		t.Errorf("%d lines of the listener's port, want 1", answered)
	}
	lan := find("lan", "", "v4")
	want(lan, true, false)
	line, _ := json.Marshal(lan)
	if _, ok := lan["host"]; ok || strings.Contains(string(line), "127.0.0.1") {
		t.Errorf("the LAN line %s names its address", line)
	}
	want(find("loopback", "127.0.0.1", "v4"), true, false)
	if v6, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		_ = v6.Close()
		want(find("loopback", "::1", "v6"), true, false)
	} else {
		t.Logf("no IPv6 loopback here (%v): its probe is not checked", err)
	}

	for _, bad := range [][]string{{"reach"}, {"reach", "--hosts", "a:b:c:d"}, {"reach", "--hosts", "h:99999"}} {
		if code, _, _ := runDiag(t, bad...); code != exitFatal {
			t.Errorf("diag %v = %d, want a usage error, exit 1", bad, code)
		}
	}
}

func containsAny(v any, texts []string) bool {
	s, _ := v.(string)
	for _, text := range texts {
		if s == text {
			return true
		}
	}
	return false
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
