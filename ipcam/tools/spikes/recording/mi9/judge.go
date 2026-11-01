// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"fmt"
	"math"
	"slices"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The sources of MI9's legs (G26): where each figure was measured, which
// decides what it may judge.
const (
	// SourceLAN is the owner's LAN, each device as host
	// (`recspike mi9 --lan`): the LAN throughput and seeks.
	SourceLAN = "lan"
	// SourceRelay is CI's relay through coturn on TLS/443 at 50 ms RTT: the
	// relay throughput and seeks.
	SourceRelay = "ci-relay"
	// SourceChrome is CI's page in Chrome: the first frame shown after a
	// seek.
	SourceChrome = "ci-chrome"
	// SourceLoopback is CI's loopback leg: information only.
	SourceLoopback = "ci-loopback"
)

// The thresholds of G18 (CTR-12).
const (
	MinLANMbps   = 8
	MinRelayMbps = 5
	MaxSeekMs    = 1500
)

// PlanB is G18's plan B: the hub plays the recording as a WebRTC track at
// 1× in the same session, and a seek is an RPC.
const PlanB = "redare ca track WebRTC 1×, salt prin RPC"

// JudgeBulk is G25's rule for one measurement: on the LAN the bulk channel
// sustains at least 8 Mbps, through TURN-TLS/443 at least 5; the slowest of
// the 20 seeks brings its first IDR within 1.5 s, and on Chrome the slowest
// shows its first frame within 1.5 s. A measured failure decides even when
// other parts are missing (G27): its reasons, in Romanian, end with the
// plan B. Without one, a missing part (no seek, fewer than 20 seeks all
// within 1.5 s, as a measurement that its wait ended may have, or no frame
// shown on Chrome) leaves no verdict: the reasons end with «fără verdict».
func JudgeBulk(r BulkResult) (pass bool, reasons []string) {
	var failed, missing bool
	fail := func(format string, args ...any) {
		reasons, failed = append(reasons, fmt.Sprintf(format, args...)), true
	}
	lack := func(reason string) { reasons, missing = append(reasons, reason), true }
	switch r.Transport {
	case LAN:
		if r.ThroughputMbps < MinLANMbps {
			fail("canalul bulk a dus %s Mbps pe LAN (pragul ≥ %d Mbps)", mbps(r.ThroughputMbps), MinLANMbps)
		}
	case TURNS443:
		if r.ThroughputMbps < MinRelayMbps {
			fail("canalul bulk a dus %s Mbps prin TURN-TLS/443 (pragul ≥ %d Mbps)", mbps(r.ThroughputMbps), MinRelayMbps)
		}
	}
	if len(r.SeekMs) == 0 {
		lack("lipsesc salturile")
	} else {
		switch worst, cut := slowest(r.SeekMs, r.Partial); {
		case worst > MaxSeekMs && cut:
			fail("un salt nu adusese primul cadru întreg (IDR) nici după %s ms, când proba s-a încheiat (limita ≤ %d ms)", ms(worst), MaxSeekMs)
		case worst > MaxSeekMs:
			fail("un salt a adus primul cadru întreg (IDR) în %s ms (limita ≤ %d ms)", ms(worst), MaxSeekMs)
		case len(r.SeekMs) < Seeks:
			lack(fmt.Sprintf("s-au măsurat doar %d din %d de salturi", len(r.SeekMs), Seeks))
		}
	}
	if r.Transport == Chrome {
		switch {
		case len(r.DisplayMs) == 0:
			lack("lipsește primul cadru afișat după salt")
		case slices.Max(r.DisplayMs) > MaxSeekMs:
			fail("primul cadru după un salt s-a afișat în %s ms (limita ≤ %d ms)", ms(slices.Max(r.DisplayMs)), MaxSeekMs)
		}
	}
	switch {
	case failed:
		return false, append(reasons, "pică → "+PlanB)
	case missing:
		return false, append(reasons, string(results.Incomplete))
	}
	return true, nil
}

// slowest is the slowest of seekMs, which must hold one, and whether it is
// the seek that the measurement's wait cut (partial's LastSeekCut), timed
// until then: a lower bound.
func slowest(seekMs []float64, partial *results.Mi9Partial) (worst float64, cut bool) {
	worst = slices.Max(seekMs)
	return worst, partial != nil && partial.LastSeekCut && seekMs[len(seekMs)-1] == worst
}

// Leg is r as an envelope's leg from source.
func Leg(r BulkResult, source string) results.Mi9Leg {
	return results.Mi9Leg{Transport: string(r.Transport), ThroughputMbps: r.ThroughputMbps, SeekMs: r.SeekMs, DisplayMs: r.DisplayMs,
		RTTMs: r.RTTMs, Source: source, Partial: r.Partial}
}

// leg is a leg with the host of its envelope.
type leg struct {
	results.Mi9Leg
	host string
}

// verdict collects the parts of G18.
type verdict struct {
	j               results.Judgement
	failed, missing bool
}

func (v *verdict) row(code, value, limit, source string, pass *bool) {
	v.j.Rows = append(v.j.Rows, results.Row{Code: code, Value: value, Limit: limit, Source: source, Pass: pass})
}

// open records why l, a LAN leg of an owner device, leaves G18 without a
// verdict, as its part of the final collection (results.OpenPart).
func (v *verdict) open(l leg, why string) {
	if l.Source == SourceLAN && (l.host == "pc" || l.host == "pi") {
		v.j.AddOpen(results.Label("MI9", LANSet, l.host), why)
	}
}

// legRow is a row of l: its source is the leg's, its table its host's.
func (v *verdict) legRow(l leg, code, value, limit string, pass *bool) {
	v.j.Rows = append(v.j.Rows, results.Row{Code: code, Value: value, Limit: limit, Source: l.Source, Host: l.host, Pass: pass})
}

// Judge is G18 (CTR-12, G26), each part decided only by the source that
// measures it: the LAN throughput (≥ 8 Mbps) by every lan leg of the
// owner's devices, at least one needed; the relay throughput (≥ 5 Mbps) by
// every ci-relay leg; the seeks (the slowest first IDR within 1.5 s) by
// every lan and ci-relay leg; the first frame shown after a seek (within
// 1.5 s) by the ci-chrome legs' DisplayMs alone. CI's loopback leg is
// information only. A part with no deciding leg, or a deciding leg without
// its 20 seeks and none over 1.5 s (one that its wait ended may have fewer),
// is incomplete; a measured failure decides all the same (G27), with PlanB.
// A leg's rows name its Source and, as Row.Host, its envelope's host; a leg
// that its wait ended has a row that says how far it got (PartialText).
func Judge(envs []results.Envelope) results.Judgement {
	v := &verdict{j: results.Judgement{Gate: "G18"}}
	var lan, relay, chrome, loop []leg
	for _, e := range envs {
		if e.Code != "MI9" || e.MI9 == nil {
			continue
		}
		for _, l := range e.MI9.Legs {
			switch l.Source {
			case SourceLAN:
				lan = append(lan, leg{l, e.Host})
			case SourceRelay:
				relay = append(relay, leg{l, e.Host})
			case SourceChrome:
				chrome = append(chrome, leg{l, e.Host})
			case SourceLoopback:
				loop = append(loop, leg{l, e.Host})
			}
		}
	}
	v.throughput(lan, "MI9 LAN", SourceLAN, MinLANMbps, "lipsește proba MI9 pe LAN (recspike mi9 --lan pe PC și pe Pi)")
	v.throughput(relay, "MI9 TURN-TLS/443 la 50 ms RTT", SourceRelay, MinRelayMbps, "lipsește proba MI9 prin TURN-TLS/443 din CI")
	v.seeks(append(slices.Clone(lan), relay...))
	v.display(chrome)
	for _, l := range loop {
		v.legRow(l, "MI9 loopback CI (informativ)", fmt.Sprintf("%s Mbps; salt max %s ms", mbps(l.ThroughputMbps), maxOf(l.SeekMs)), "", nil)
	}
	switch {
	case v.failed:
		v.j.Verdict, v.j.PlanB = results.Fail, PlanB
	case v.missing:
		v.j.Verdict = results.Incomplete
	default:
		v.j.Verdict = results.Pass
	}
	return v.j
}

// throughput judges the throughput of each of legs, measured at source,
// against limit; a leg that its wait ended (Partial) says how far it got.
func (v *verdict) throughput(legs []leg, part, source string, limit int, missing string) {
	limitText := fmt.Sprintf("≥ %d Mbps", limit)
	if len(legs) == 0 {
		v.row(part+": debit canal bulk", "lipsește", limitText, source, nil)
		v.j.Reasons = append(v.j.Reasons, missing)
		v.missing = true
		return
	}
	for _, l := range legs {
		ok := l.ThroughputMbps >= float64(limit)
		v.legRow(l, fmt.Sprintf("%s (%s): debit canal bulk", part, l.host), fmt.Sprintf("%s Mbps (RTT %s ms)", mbps(l.ThroughputMbps), ms(l.RTTMs)),
			limitText, results.RowPass(ok, true))
		if l.Partial != nil {
			r := BulkResult{SeekMs: l.SeekMs, Partial: l.Partial}
			v.legRow(l, fmt.Sprintf("%s (%s): proba încheiată de așteptare (G27)", part, l.host), PartialText(r), "", nil)
		}
		if !ok {
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("%s: %s: canalul bulk a dus %s Mbps (pragul %s)", l.host, part, mbps(l.ThroughputMbps), limitText))
			v.failed = true
		}
	}
}

// seeks judges each deciding leg's slowest seek: one over 1.5 s decides,
// even the seek that the leg's wait cut, timed until then (LastSeekCut). A
// leg without seeks, or with fewer than Seeks all within 1.5 s, as one
// that its wait ended (Partial) may have, leaves the part missing.
func (v *verdict) seeks(legs []leg) {
	const code, limit = "MI9 salt: primul cadru întreg (IDR) primit", "fiecare ≤ 1500 ms"
	if len(legs) == 0 {
		v.row(code, "lipsește", limit, "", nil)
		v.j.Reasons = append(v.j.Reasons, "lipsesc salturile pe LAN și prin TURN-TLS/443")
		v.missing = true
		return
	}
	for _, l := range legs {
		rowCode := fmt.Sprintf("%s (%s, %s)", code, l.Source, l.host)
		if len(l.SeekMs) == 0 {
			v.legRow(l, rowCode, "lipsește", limit, nil)
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("%s (%s): lipsesc salturile", l.host, l.Source))
			v.missing = true
			v.open(l, "lipsesc salturile")
			continue
		}
		worst, cut := slowest(l.SeekMs, l.Partial)
		switch {
		case worst > MaxSeekMs && cut:
			v.legRow(l, rowCode, fmt.Sprintf("max ≥ %s ms (%d salturi)", ms(worst), len(l.SeekMs)), limit, results.RowPass(false, true))
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("%s (%s): un salt nu adusese primul IDR nici după %s ms, când proba s-a încheiat (limita ≤ 1500 ms)",
				l.host, l.Source, ms(worst)))
			v.failed = true
		case worst > MaxSeekMs:
			v.legRow(l, rowCode, fmt.Sprintf("max %s ms (%d salturi)", ms(worst), len(l.SeekMs)), limit, results.RowPass(false, true))
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("%s (%s): un salt a adus primul IDR în %s ms (limita ≤ 1500 ms)", l.host, l.Source, ms(worst)))
			v.failed = true
		case len(l.SeekMs) < Seeks:
			v.legRow(l, rowCode, fmt.Sprintf("max %s ms (%d din %d de salturi)", ms(worst), len(l.SeekMs), Seeks), limit, nil)
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("%s (%s): s-au măsurat doar %d din %d de salturi", l.host, l.Source, len(l.SeekMs), Seeks))
			v.missing = true
			v.open(l, fmt.Sprintf("s-au măsurat doar %d din %d de salturi", len(l.SeekMs), Seeks))
		default:
			v.legRow(l, rowCode, fmt.Sprintf("max %s ms (%d salturi)", ms(worst), len(l.SeekMs)), limit, results.RowPass(true, true))
		}
	}
}

// display judges the first frame shown after each seek, from Chrome alone.
func (v *verdict) display(legs []leg) {
	const code, limit = "MI9 Chrome: primul cadru afișat după salt", "fiecare ≤ 1500 ms"
	measured := false
	for _, l := range legs {
		if len(l.DisplayMs) == 0 {
			continue
		}
		measured = true
		worst := slices.Max(l.DisplayMs)
		ok := worst <= MaxSeekMs
		v.legRow(l, code, fmt.Sprintf("max %s ms (%d salturi)", ms(worst), len(l.DisplayMs)), limit, results.RowPass(ok, true))
		if !ok {
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("Chrome: primul cadru după un salt s-a afișat în %s ms (limita ≤ 1500 ms)", ms(worst)))
			v.failed = true
		}
	}
	if !measured {
		v.row(code, "lipsește", limit, SourceChrome, nil)
		v.j.Reasons = append(v.j.Reasons, "lipsește proba MI9 în Chrome din CI")
		v.missing = true
	}
}

// maxOf is the largest of figures, "–" for none.
func maxOf(figures []float64) string {
	if len(figures) == 0 {
		return "–"
	}
	return ms(slices.Max(figures))
}

// ms is a time in ms to a tenth, as few decimals as it needs: "318", "50,5".
func ms(v float64) string { return results.Decimal(math.Round(v*10)/10, -1) }

func mbps(v float64) string { return results.Decimal(v, 1) }
