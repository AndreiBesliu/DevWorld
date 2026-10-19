package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Verdict is the outcome of a gate (spec 16.3, the rule of decision A3).
type Verdict string

// The verdicts. VerdictIncomplete is not one of the rule's: it is what the report says
// when the measurements that a verdict needs are not all in, so that a missing item
// never turns into a GO or into the NO-GO that triggers T29.
const (
	VerdictGOProvizoriu  Verdict = "GO provizoriu (fără iOS)"
	VerdictGOConditionat Verdict = "GO condiționat"
	VerdictNOGOMobil     Verdict = "NO-GO mobil"
	VerdictGO            Verdict = "GO"
	VerdictPlanBGrid9    Verdict = "plan B: grilă desktop 9"
	VerdictPlanBRelay    Verdict = "plan B: relay în LAN + aplicația nativă"
	VerdictIncomplete    Verdict = "fără verdict: lipsesc măsurători"
)

// The thresholds that the gates read. decisive derives from these lists, so a gate and the
// list of failures outside the gates cannot go out of step.
var (
	// G5, the items of AND-1 that decide (Global Constraints): the LAN items with 4 tiles and the
	// fullscreen decoder, and the 9-tile items. Failing one of the first is NO-GO; failing only
	// 9-tile items is GO condiționat. The fullscreen part of MV4 is not a 9-tile item: the same
	// main stream follows every grid, so fewer tiles would not fix it (report-7).
	androidCritical4 = []string{"MV1", "MV2", "MV3/4", mv4MainCode, "MV9", "MV10"}
	androidCritical9 = []string{"MV3/9", "MV4/9", "MV5/9"}
	// G6, WIN-1: MV6 and MV7 with 16 tiles (spec §16.5 G6, R45); either failing is plan B.
	windowsKeys = []string{mvKey(platformWindows, "MV6"), mvKey(platformWindows, "MV7")}
	// G7, the web gate: MV13 on the three browsers, and MV12 in the two WebViews.
	// The Chrome part of MV12 needs a public origin and waits for Faza 0-bis.
	webKeys = []string{
		mvKey("chrome", "MV13"), mvKey("edge", "MV13"), mvKey("firefox", "MV13"),
		mvKey(platformAndroid, "MV12"), mvKey(platformWindows, "MV12"),
	}
	// G19, Argon2id on AND-1.
	g19Key = mvKey(platformAndroid, "MV15")
)

// g9Keys are the codec matrices (S9) that gate G9 reads: one on each client path that the plan
// expects to run S9 (R45), in the order of the report. A browser on an unexpected OS is not one.
func g9Keys() []string {
	platforms := make([]string, 0, len(platformOrder))
	for p := range platformOrder {
		if slices.Contains(expectedCodes(p), "G9") {
			platforms = append(platforms, p)
		}
	}
	sort.Slice(platforms, func(i, j int) bool { return platformOrder[platforms[i]] < platformOrder[platforms[j]] })
	keys := make([]string, len(platforms))
	for i, p := range platforms {
		keys[i] = mvKey(p, "G9")
	}
	return keys
}

// assessment is a verdict and the thresholds behind it.
type assessment struct {
	verdict Verdict
	// failed and missing are keys of mvKey.
	failed, missing []string
	// lacking are conditions of the gate, in words, that the results do not show as met.
	lacking []string
}

// scan sorts keys into those that failed and those that have no value.
func scan(mv map[string]MV, keys []string) (failedKeys, missingKeys []string) {
	for _, key := range keys {
		v, ok := mv[key]
		switch {
		case !ok:
			missingKeys = append(missingKeys, key)
		case !v.Pass:
			failedKeys = append(failedKeys, key)
		}
	}
	return failedKeys, missingKeys
}

func deviceKeys(device string, codes []string) []string {
	keys := make([]string, len(codes))
	for i, c := range codes {
		keys[i] = mvKey(device, c)
	}
	return keys
}

func assessAndroid(mv map[string]MV) assessment {
	failed4, missing4 := scan(mv, deviceKeys(platformAndroid, androidCritical4))
	failed9, missing9 := scan(mv, deviceKeys(platformAndroid, androidCritical9))
	a := assessment{
		failed:  append(append([]string(nil), failed4...), failed9...),
		missing: append(append([]string(nil), missing4...), missing9...),
	}
	switch {
	case len(failed4) > 0:
		a.verdict = VerdictNOGOMobil
	case len(missing4) > 0:
		a.verdict = VerdictIncomplete
	case len(failed9) > 0:
		a.verdict = VerdictGOConditionat
	case len(missing9) > 0:
		a.verdict = VerdictIncomplete
	default:
		a.verdict = VerdictGOProvizoriu
	}
	return a
}

func assessWindows(mv map[string]MV) assessment {
	failedKeys, missingKeys := scan(mv, windowsKeys)
	a := assessment{failed: failedKeys, missing: missingKeys}
	switch {
	case len(failedKeys) > 0:
		a.verdict = VerdictPlanBGrid9
	case len(missingKeys) > 0:
		a.verdict = VerdictIncomplete
	default:
		a.verdict = VerdictGO
	}
	return a
}

// assessWeb is G7's verdict. Besides MV12 and MV13, spec G7 asks the hub to listen on 5353/udp
// next to the Windows DNS client: noMDNS are the S10 runs that G7 reads in which the hub said it
// could not (mdnsUnavailable). A measured relay pair still decides plan B; otherwise G7 has no
// verdict until that condition holds (report-3).
func assessWeb(mv map[string]MV, noMDNS []string) assessment {
	failedKeys, missingKeys := scan(mv, webKeys)
	a := assessment{failed: failedKeys, missing: missingKeys}
	if len(noMDNS) > 0 {
		a.lacking = append(a.lacking, fmt.Sprintf("hub-ul nu a putut asculta pe 5353/udp (mDNS) alături de clientul DNS din Windows în rulările S10 %s", codeList(noMDNS)))
	}
	switch {
	case len(failedKeys) > 0:
		a.verdict = VerdictPlanBRelay
	case len(missingKeys) > 0 || len(a.lacking) > 0:
		a.verdict = VerdictIncomplete
	default:
		a.verdict = VerdictGO
	}
	return a
}

// AndroidVerdict is the verdict of AND-1 in Phase 0 (LAN critical only, T41), from the map
// of ComputeMV. Critical are MV1, MV2, MV3 with 4 tiles, MV9 and MV10, and MV3, MV4 and MV5
// with 9 tiles. All pass: GO provizoriu (fără iOS). Only 9-tile items fail while the others
// pass: GO condiționat. Any other failure: NO-GO mobil. A failed item decides even when
// others were not measured; without a failure, a missing item leaves the verdict open.
func AndroidVerdict(mv map[string]MV) Verdict { return assessAndroid(mv).verdict }

// WindowsVerdict is the verdict of WIN-1 (G6). Windows cannot cause NO-GO: when MV6 or MV7
// with 16 tiles fails, the desktop grid drops to 9 tiles.
func WindowsVerdict(mv map[string]MV) Verdict { return assessWindows(mv).verdict }

// WebVerdict is the verdict of the web from MV12 and MV13: when one fails, the web uses a relay
// on the LAN and recommends the native app. Gate G7 also reads the hub's mDNS (assessWeb).
func WebVerdict(mv map[string]MV) Verdict { return assessWeb(mv, nil).verdict }

// Gate is a gate of Phase 0 (spec 16.5) with the decision that its result triggers, in the
// form of PH0-30: the verdict, the decision, and what changes in §2 or in the requirements.
type Gate struct {
	ID    string
	Title string
	// Verdict is set for the gates that give one (G5, G6, G7).
	Verdict Verdict
	// Result is what the gate found, in words.
	Result string
	// Failed and Missing are keys of mvKey.
	Failed, Missing []string
	// Lacking are conditions of the gate, in words, that the results do not show as met.
	Lacking  []string
	Decision string
	Change   string
}

// label writes a threshold key for people: the code, and the device when withDevice is set.
func label(key string, withDevice bool) string {
	device, code, _ := strings.Cut(key, "/")
	if !withDevice {
		return code
	}
	return fmt.Sprintf("%s (%s)", code, deviceName(device))
}

// deviceName is how the report calls a platform in running text.
func deviceName(platform string) string {
	switch platform {
	case platformAndroid:
		return "AND-1"
	case platformWindows:
		return "WIN-1"
	case "chrome":
		return "Chrome"
	case "edge":
		return "Edge"
	case "firefox":
		return "Firefox"
	}
	if unexpectedOS(platform) {
		return deviceName(basePlatform(platform)) + " pe " + osName(platform)
	}
	return platform
}

// fell is the sentence part "A picat X" or "Au picat X, Y" for the failed keys.
func fell(keys []string, withDevice bool) string {
	if len(keys) == 1 {
		return "A picat " + labels(keys, withDevice)
	}
	return "Au picat " + labels(keys, withDevice)
}

func labels(keys []string, withDevice bool) string {
	sorted := append([]string(nil), keys...)
	sortKeys(sorted)
	out := make([]string, len(sorted))
	for i, k := range sorted {
		out[i] = label(k, withDevice)
	}
	return strings.Join(out, ", ")
}

// sortKeys orders threshold keys by device and then by code, as the report does.
func sortKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		di, ci, _ := strings.Cut(keys[i], "/")
		dj, cj, _ := strings.Cut(keys[j], "/")
		if oi, oj := orderOf(platformOrder, di), orderOf(platformOrder, dj); oi != oj {
			return oi < oj
		}
		if di != dj {
			return di < dj
		}
		return codeLess(ci, cj)
	})
}

const waitsForMissing = "Fără decizie: verdictul așteaptă măsurătorile lipsă (%s). Rulați scenariile care le dau, în ghid, și repetați raportul."

// buildGates gives, for the gates G5, G6, G7, G9 and G19, the verdict and the decision it triggers.
func buildGates(ev evaluation) []Gate {
	mv := ev.MV
	return []Gate{gateG5(mv), gateG6(mv), gateG7(mv, ev.withoutMDNS()), gateG9(mv), gateG19(mv)}
}

// withoutMDNS are the S10 runs that G7 reads (the used ones of its devices) in which the hub
// reported mdnsUnavailable.
func (ev evaluation) withoutMDNS() []string {
	var files []string
	for _, r := range ev.Runs {
		if r.Status == RunUsed && r.Scenario == "S10" && r.MDNSUnavailable && slices.Contains(webKeys, mvKey(r.Device.Platform, codeOfPair(r.Device.Platform))) {
			files = append(files, r.File)
		}
	}
	return files
}

// codeOfPair is the threshold that S10 measures on a device: MV12 in the shells, MV13 in a browser.
func codeOfPair(platform string) string {
	if slices.Contains(webPlatforms, basePlatform(platform)) {
		return "MV13"
	}
	return "MV12"
}

func gateG5(mv map[string]MV) Gate {
	a := assessAndroid(mv)
	g := Gate{ID: "G5", Title: "Grila Android (AND-1)", Verdict: a.verdict, Result: string(a.verdict), Failed: a.failed, Missing: a.missing}
	switch a.verdict {
	case VerdictGOProvizoriu:
		g.Decision = "Nicio decizie tehnică nouă. Shell-ul Android rămâne subțire (spike-ul și accesul pe LAN din Faza 1, T42), iar lucrul de interfață specific telefonului (pluginurile din §6.6 marcate cu Faza 4, ecrane sau gesturi doar pentru telefon) așteaptă verdictul iOS din Faza 0-bis."
		g.Change = "Nimic în §2 sau în cerințe."
	case VerdictGOConditionat:
		g.Decision = fmt.Sprintf("Cu 4 plăci trece tot ce e critic, dar cu 9 plăci pică %s. Plafonul pe telefon (A6: maximum 9 plăci) se schimbă doar cu aprobarea dvs. Până la decizia dvs. plafonul rămâne 9, iar plăcile care nu pot decoda trec pe snapshot (§6.5).", labels(a.failed, false))
		g.Change = "O schimbare aprobată se scrie în §2.1, ca modificare a lui A6, nu în §2.2. Decizia proprietarului asupra plafonului pe telefon: de completat de proprietar."
	case VerdictNOGOMobil:
		g.Decision = fmt.Sprintf("%s. Conform regulii A3, se aplică T29: ambele aplicații mobile trec împreună pe Flutter (opțiunea C), chiar dacă a picat o singură platformă; web-ul și Windows rămân pe opțiunea B. Flutter trebuie să treacă aceeași matrice înainte să fie adoptat.", fell(a.failed, false))
		g.Change = "§2.2: Decizie tehnică (T29), cu trimitere la G5; un nou plan (brainstorming) pentru `apps/mobile-flutter`."
	case VerdictIncomplete:
		g.Decision = fmt.Sprintf(waitsForMissing, labels(a.missing, false))
		g.Change = "Nimic până atunci."
	case VerdictGO, VerdictPlanBGrid9, VerdictPlanBRelay:
	}
	return g
}

func gateG6(mv map[string]MV) Gate {
	a := assessWindows(mv)
	g := Gate{ID: "G6", Title: "Windows WebView2 (WIN-1)", Verdict: a.verdict, Result: string(a.verdict), Failed: a.failed, Missing: a.missing}
	switch a.verdict {
	case VerdictGO:
		g.Decision = "Nicio decizie: MV6 și MV7 trec cu 16 plăci, deci grila desktop rămâne la 16 plăci."
		g.Change = "Nimic în §2 sau în cerințe."
	case VerdictPlanBGrid9:
		g.Decision = fmt.Sprintf("%s: grila desktop coboară la 9 plăci (APP-30, planul B din G6). Windows nu poate da NO-GO.", fell(a.failed, false))
		g.Change = "§2.2: Decizie tehnică „grilă desktop 9”, cu trimitere la G6."
	case VerdictIncomplete:
		g.Decision = fmt.Sprintf(waitsForMissing, labels(a.missing, false))
		g.Change = "Nimic până atunci."
	case VerdictGOProvizoriu, VerdictGOConditionat, VerdictNOGOMobil, VerdictPlanBRelay:
	}
	return g
}

func gateG7(mv map[string]MV, noMDNS []string) Gate {
	a := assessWeb(mv, noMDNS)
	g := Gate{ID: "G7", Title: "Chrome LNA și mDNS (web)", Verdict: a.verdict, Result: string(a.verdict), Failed: a.failed, Missing: a.missing, Lacking: a.lacking}
	switch a.verdict {
	case VerdictGO:
		g.Decision = "Nicio decizie: perechile ICE din Chrome, Edge, Firefox și din cele două WebView-uri nu trec prin relay. MV12 în Chrome, cu origine publică, rămâne pentru Faza 0-bis."
		g.Change = "Nimic în §2 sau în cerințe."
	case VerdictPlanBRelay:
		g.Decision = fmt.Sprintf("%s: web-ul folosește relay în LAN și recomandă aplicația nativă (APP-32). Costul intră în §15.6.", fell(a.failed, true))
		g.Change = "§2.2: Decizie tehnică „relay în LAN pentru web”, cu trimitere la G7."
	case VerdictIncomplete:
		var parts []string
		if len(a.missing) > 0 {
			parts = append(parts, fmt.Sprintf(waitsForMissing, labels(a.missing, true)))
		}
		if len(a.lacking) > 0 {
			parts = append(parts, "Fără decizie: "+strings.Join(a.lacking, "; ")+", iar G7 cere și această condiție. Ce urmează (o corectură în hub sau planul B al lui G7) se decide în PR, cu aceste rulări.")
		}
		g.Decision = strings.Join(parts, " ")
		g.Change = "Nimic până atunci."
	case VerdictGOProvizoriu, VerdictGOConditionat, VerdictNOGOMobil, VerdictPlanBGrid9:
	}
	return g
}

// gateG9 reads the codec matrix of every client path that the plan expects to run S9 (spec G9:
// „pe toate căile client”). A path that fails (fallback-failed) decides plan B at once; otherwise
// G9 decides, and writes its §2.2 change, only once every expected path has run S9 (R39, report-5).
func gateG9(mv map[string]MV) Gate {
	g := Gate{ID: "G9", Title: "Matricea de codecuri"}
	var lines []string
	for _, key := range g9Keys() {
		v, ok := mv[key]
		switch {
		case !ok:
			g.Missing = append(g.Missing, key)
			continue
		case !v.Pass:
			g.Failed = append(g.Failed, key)
		}
		lines = append(lines, deviceName(v.Device)+": "+v.Text)
	}
	measured := strings.Join(lines, " · ")
	switch {
	case len(g.Failed) > 0:
		g.Result = measured
		g.Decision = "O cale nu merge nici pe substream (fallback-failed). Plan B: onboarding-ul impune mai strict H.264 pe substream, iar pe ecran complet se folosește substream-ul când fluxul principal e H.265 nedecodabil."
		g.Change = "§2.2: Decizie tehnică, cu trimitere la G9 și rezultatele de mai sus."
	case len(g.Missing) > 0:
		paths := make([]string, len(g.Missing))
		for i, key := range g.Missing {
			device, _, _ := strings.Cut(key, "/")
			paths[i] = deviceName(device)
		}
		g.Result = fmt.Sprintf("%s (S9 pe %s)", VerdictIncomplete, joinAnd(paths))
		if measured != "" {
			g.Result += "; măsurat până acum: " + measured
		}
		g.Decision = fmt.Sprintf("Fără decizie: G9 cere matricea de codecuri pe toate căile client, iar S9 lipsește pe %s. Rulați S9 acolo, în ghid, și repetați raportul.", joinAnd(paths))
		g.Change = "Nimic până atunci."
	default:
		g.Result = measured
		g.Decision = "Regula SDP și primul cadru în 4 s aleg corect pe toate căile client ale Fazei 0 (AND-1, WIN-1, Chrome, Edge, Firefox): H.265 cade pe substream cu eticheta „calitate redusă: H.265 nesuportat”, iar un profil H.264 nesuportat pe eticheta „profil video nesuportat”. Rezultatele pe cale se notează."
		g.Change = "§2.2: Decizie tehnică cu rezultatele G9 de mai sus (căile iOS rămân pentru Faza 0-bis)."
	}
	return g
}

func gateG19(mv map[string]MV) Gate {
	g := Gate{ID: "G19", Title: "Argon2id pe Android mediu (MV15)"}
	key := g19Key
	v, ok := mv[key]
	switch {
	case !ok:
		g.Result = "nemăsurat"
		g.Missing = []string{key}
		g.Decision = "Fără decizie: MV15 nu a fost măsurat pe AND-1 (S8)."
		g.Change = "Nimic până atunci."
	case v.Pass:
		g.Result = "trece: " + v.Text
		g.Decision = "Nicio decizie: parametrii impliciți Argon2id rămân cei din §10.3 (m=64 MiB, t=3, p=4)."
		g.Change = "Nimic în §2 sau în cerințe."
	default:
		g.Result = "pică: " + v.Text
		g.Failed = []string{key}
		g.Decision = "Parametrii impliciți Argon2id scad la m=46 MiB, t=1, p=1 (pragul OWASP); conturile existente trec pe ei cu AccountService.ChangeKdfParams (ENC-10)."
		g.Change = "§2.2: Decizie tehnică, cu trimitere la G19."
	}
	return g
}

// decisive says whether a gate above already turns the threshold into a decision: it is one of
// the keys the gates G5, G6, G7 and G19 read, or a codec matrix that G9 reads.
func decisive(key string) bool {
	if slices.Contains(g9Keys(), key) || key == g19Key {
		return true
	}
	for _, keys := range [][]string{deviceKeys(platformAndroid, androidCritical4), deviceKeys(platformAndroid, androidCritical9), windowsKeys, webKeys} {
		if slices.Contains(keys, key) {
			return true
		}
	}
	return false
}

// otherFailures are the thresholds that failed without a gate behind them (decisive): they
// change no verdict, so the report lists them for the owner.
func otherFailures(mv map[string]MV) []MV {
	var out []MV
	for key, v := range mv {
		if !v.Pass && !decisive(key) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if oi, oj := orderOf(platformOrder, out[i].Device), orderOf(platformOrder, out[j].Device); oi != oj {
			return oi < oj
		}
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return codeLess(out[i].Code, out[j].Code)
	})
	return out
}
