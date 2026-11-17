package mi5

import (
	"encoding/json"
	"errors"
	"testing"
)

// reachLines are diag reach's lines: provider v4 and v6, LAN v4 and v6,
// loopback v4 and v6.
func reachLines(p4, p6, lan, loop string) []json.RawMessage {
	line := func(target, family, err string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"schema": "ipcam.diag.reach/1", "target": target, "family": family, "ok": err == "",
			"blocked": err != "" && err != "refused", "err": err})
		return b
	}
	return []json.RawMessage{line("provider", "v4", p4), line("provider", "v6", p6), line("lan", "v4", lan), line("lan", "v6", "no-address"),
		line("loopback", "v4", loop), line("loopback", "v6", loop)}
}

func TestVerifyBlocked_AbortsWhenProviderReachableOverIPv6(t *testing.T) {
	res, err := judgeReach(reachLines("timeout", "", "", ""))
	if !errors.Is(err, ErrProviderReachV6) || err.Error() != "Furnizorul e încă accesibil prin IPv6; blocarea nu e completă." || res.ProviderBlockedV6 {
		t.Errorf("v6 open: %+v, %v", res, err)
	}
	if _, err := judgeReach(reachLines("", "denied", "", "")); !errors.Is(err, ErrProviderReachV4) {
		t.Errorf("v4 open: %v", err)
	}
	for _, kinds := range [][2]string{{"timeout", "no-address"}, {"denied", "unreachable"}} {
		if res, err := judgeReach(reachLines(kinds[0], kinds[1], "", "")); err != nil || !res.Lan || !res.Loopback {
			t.Errorf("%v: %+v, %v; want blocked", kinds, res, err)
		}
	}
}

func TestVerifyBlocked_RefusedCountsAsReachable(t *testing.T) {
	if _, err := judgeReach(reachLines("refused", "timeout", "", "")); !errors.Is(err, ErrProviderReachV4) {
		t.Errorf("a refused provider: %v; want reachable", err)
	}
}

func TestVerifyBlocked_AbortsWhenLanOrLoopbackFails(t *testing.T) {
	for name, lines := range map[string][]json.RawMessage{
		"LAN":      reachLines("timeout", "timeout", "timeout", ""),
		"loopback": reachLines("timeout", "timeout", "", "denied"),
	} {
		if _, err := judgeReach(lines); !errors.Is(err, ErrBlockCutLAN) || err.Error() != "Blocarea a tăiat și rețeaua locală; proba s-a oprit." {
			t.Errorf("%s down: %v", name, err)
		}
	}
}
