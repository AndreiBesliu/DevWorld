# Phase 0 (b) Video Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Pe scurt pentru proprietar.** Construim o probă care măsoară, pe dispozitivele dvs., dacă tehnologia aleasă pentru aplicații (varianta B: o singură aplicație web, împachetată pentru Android și Windows) afișează fluent mai multe camere deodată:
- pe Samsung Galaxy A24 (4 și 9 camere);
- pe PC, în Chrome, Edge și Firefox;
- în aplicația de Windows (16 camere).

Camerele sunt simulate: fiecare imagine poartă un cod pe care proba îl citește ca să măsoare întârzierea exactă. Puteți adăuga și camerele dvs. reale. La final iese un raport cu verdictul: merge (GO), merge cu 4 camere pe telefon (GO condiționat) sau trecem aplicațiile de telefon pe Flutter (NO-GO).

Ce aveți de făcut: porniți un program pe PC, instalați o aplicație de test pe telefon și apăsați pe rând scenariile. Durează în total aproximativ 3–4 ore, în una sau mai multe sesiuni. Totul e gratuit. iPhone-ul și testele prin internetul mobil vin în Faza 0-bis.

**Goal:** Measure MV1–MV10 and MV12–MV15 and gates G5, G6, G7 (shell origins and `http://localhost`), G9 and G19 on the owner's Galaxy A24, the owner's Windows 11 PC (Chrome, Edge, Firefox, Tauri/WebView2) and CI. The output is the Phase 0 (b) report with the A3 verdict for Android, web and Windows.

**Architecture:**
- **`spikehub`.** A Go spike hub in `tools/spikes/video`. It plays pre-encoded simulated cameras (and, optionally, the owner's own RTSP cameras) into a single Pion PeerConnection per viewer. The client offers N+1 `recvonly` video transceivers plus an `rpc` data channel, and the hub binds cameras to slots over that channel (A7, §6.3). The hub serves the spike web app, a 10-minute fMP4 HLS recording and a results endpoint over plain HTTP on the LAN; this shortcut is for the spike only.
- **The production player.** `@ipcam/player` is written as the production package (APP-22).
- **The measuring app.** A framework-free TypeScript spike app runs the scenarios and computes the metrics. It is the same bundle in Chrome/Edge/Firefox (`http://localhost`), in Capacitor on Android and in Tauri on Windows.
- **The report.** A Go generator turns the results into the report and the verdicts.

**Tech Stack:**
- Go 1.27, Pion WebRTC v4 (`github.com/pion/webrtc/v4`), `github.com/bluenviron/gortsplib/v5` and `github.com/bluenviron/mediacommon/v2`
- Node 24 LTS, pnpm 10, TypeScript (strict), Vite, Vitest, ESLint, Playwright, hls.js, hash-wasm
- Capacitor 8.5.3 (Android) with Kotlin plugins; Tauri 2.12.4 (Rust) with the `sysinfo` crate
- ffmpeg (CI only, for media generation)

**Spec:** `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`:
- §16.3 (device matrix, MV1–MV15, GO/NO-GO)
- §16.5 (G5, G6, G7, G9, G19), §16.9 (deferred parts), PH0-30…32
- §6.1–§6.6, APP-01, APP-05, APP-22, APP-30, APP-32, HUB-06
- A3, A6, A7, T29, T41, T42, O13, O14

## Global Constraints

- **Repo rules.** Touch only `ipcam/**` and `.github/workflows/ipcam-*.yml`; every push/PR trigger has `paths:` limited to the files it builds plus its own workflow file. Code, identifiers, comments and commits are in English. Owner-facing text (app labels, guide, report summary) is Romanian, with English labels in the app.
- **Zero cost (O13, PH0-32).** No paid service and no payment details. Builds stay unsigned or debug-signed. The APK is sideloaded. Only GitHub-hosted runners are used; the repo is public, so they are free. Excluded and deferred to Faza 0-bis (§16.9): remote over mobile data, TURN (MV1 remote, MV2 remote, MV11), Chrome from a public origin (the Chrome part of MV12), iOS (iOS-1, iOS-2), the device farm (AND-2).
- **Toolchains.**
  - Go: newest `go1.27.x` whose `golang.org/toolchain` version is at least 14 days old (check `Time` with `go list -m -json`).
  - Node: 24 LTS. pnpm 10 with `minimumReleaseAge: 10080` in `pnpm-workspace.yaml` (7 days, §11.10).
  - Every other dependency: the newest version at least 14 days old. Lockfiles committed; `--frozen-lockfile` and `-mod=readonly` in CI.
- **TypeScript.** `strict`, `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes`; `any` is forbidden (ESLint `@typescript-eslint/no-explicit-any: error`). `@ipcam/player` must not import React, Capacitor or Tauri (APP-22, enforced by ESLint `no-restricted-imports`).
- **Native code budgets (APP-01, MV14).** At most 400 lines per plugin per platform; Rust under 300 lines. Counted by `tools/scripts/count-native-lines.sh`, which fails CI when a budget is exceeded.
- **Licenses.** Allowlist: MIT, BSD-2/3, Apache-2.0, ISC, MPL-2.0. ffmpeg with libx264/libx265 is used only in CI to generate test media. It is never shipped and never in a release asset.
- **Simulated media (§16.3, A6), all as Annex-B elementary streams with `GOP = fps` and no B-frames, 60 s loops:**
  - `sub`: H.264 Main@3.1, 640×360, 12 fps, 512 kbps
  - `main`: H.264 Main@4.0, 1920×1080, 20 fps, 4 Mbps
  - `main-high51`: H.264 High@5.1, 3840×2160, 20 fps, 8 Mbps, 20 s
  - `main-h265`: H.265 Main@4.1, 1920×1080, 20 fps, 2 Mbps

  The recording is HLS fMP4, VOD, 10 min, 4 s segments, made from `main` by `-c copy`.
- **Band (frame code).** Two rows of 48 square blocks along the top edge of every frame: block side 16 px at ≥ 1080 lines, 8 px at 360 lines, white = 1. Row 1 holds 8 bits camera index, 16 bits frame index within the loop, 16 bits loop counter mod 65536 and 8 bits CRC-8/SMBUS of the previous 40 bits. Row 2 is the bitwise inverse of row 1, for robustness. The test vectors in `tools/spikes/video/band/vectors.json` are the single source of truth for Go and TypeScript.
- **Thresholds, exactly as in §16.3:**

  | Code | Threshold |
  |---|---|
  | MV1 | LAN p90 ≤ 1.5 s over 20 starts |
  | MV2 | LAN p95 < 1 s over 100 samples |
  | MV3 | fps ≥ 90 % of source and dropped ≤ 2 % over 10 min |
  | MV4 | fullscreen (main stream) always hardware; all 9 grid tiles hardware on AND-1 |
  | MV5 | Android thermal ≤ MODERATE; fps not below MV3 because of heat; battery ≤ 15 %/h |
  | MV6 | 16 tiles ≤ 25 % CPU total; 1 main 1080p ≤ 10 % |
  | MV7 | 30 min with paging and layout changes: memory growth ≤ 10 %, 0 unprovoked player restarts |
  | MV8 | Android Activity PiP appears and video advances for 5 min uninterrupted |
  | MV9 | 2 min in background, then live resumes in ≤ 3 s without an app restart |
  | MV10 | HLS start ≤ 2 s; seek ≤ 1.5 s |
  | MV12 | WebView2 and Android WebView from shell origins: selected ICE pair is not relay; prompt yes/no recorded |
  | MV13 | Chrome, Edge and Firefox at `http://localhost`: both ends of the pair on private LAN addresses (`host` or `prflx`), no relay |
  | MV14 | Kotlin lines for PiP plus local network ≤ 400 per plugin |
  | MV15 | Argon2id `m=64 MiB, t=3, p=4`, 10 runs, ≤ 3 s on AND-1 |

- **GO/NO-GO (§16.3) for AND-1 in Phase 0 (LAN critical only, T41).**
  - Critical: MV1, MV2, MV3 with 4 tiles, MV9, MV10; plus MV3, MV4 and MV5 with 9 tiles.
  - All pass → `GO provizoriu (fără iOS)`.
  - Only the 9-tile items fail while 4 tiles pass → `GO condiționat`. The phone cap changes only with the owner's approval.
  - Anything else → `NO-GO mobil`, which triggers T29.
  - WIN-1 cannot cause NO-GO: if MV6 fails, the desktop grid drops to 9.
  - Web: if MV12 or MV13 fails → relay on the LAN plus a recommendation to use the native app.
- **Spike-only shortcuts.** None of these may appear in `apps/packages/**`, and each is marked `// SPIKE-ONLY` with a reference to this plan:
  - plain HTTP signaling on the LAN, port `7080`;
  - the Android `spike` flavour with cleartext traffic and `androidScheme: "http"`;
  - a 1-pixel camera capture held only during MV4, so that Chrome exposes `decoderImplementation`/`powerEfficientDecoder` (production uses `mediaCapabilities`, §6.5);
  - the `spike-metrics` Tauri feature.
- **Data minimisation (PH0-22).** Results contain device model, OS/WebView version, browser and ICE candidate *types*. They never contain IP addresses, SSIDs, serial numbers or the owner's RTSP URLs or credentials. Real-camera URLs are typed by the owner at hub start, kept in memory only, and identified in results as `real-1`, `real-2` and so on.

## Review Focus

1. **The phone cannot reach the hub.** Typical causes: guest Wi-Fi or AP isolation, a different subnet, or the Windows firewall set to "Public". The app must show the URL it tried, the error class (timeout or refused) and the Romanian hint "același Wi-Fi, nu rețeaua pentru oaspeți; permiteți spikehub în firewall la «Rețele private»". *Owner: Task 9, test `connectError_showsTriedUrlAndHint`.*
2. **The phone screen turns off or the app is backgrounded in the middle of a 10–30 min scenario.** Expected: a wake lock is held during scenarios. A `visibilitychange` to hidden marks the run `interrupted`; interrupted runs are excluded from verdicts and the app offers to repeat them (MV8 and MV9 excepted, since they background on purpose). *Owner: Task 9, test `scenario_visibilityHidden_marksInterrupted`.*
3. **Device clock wrong or drifting.** Glass-to-glass latency must not use the device wall clock. It uses the data-channel clock offset estimate (minimum-RTT sample of 20 pings, re-estimated every 30 s). A sample is flagged `unreliable` when RTT > 50 ms or the offset moves by more than 20 ms between estimates. *Owner: Task 8, test `clockOffset_jitterySamples_minRttWins`.*
4. **The hub PC is busy** (owner working, antivirus scan), so frame pacing jitters and client numbers look worse than they are. Expected: the hub reports pacing jitter (p99 inter-frame send deviation) per run, and the report flags runs above 20 ms. *Owner: Task 4, test `TestPacer_ReportsJitter`; Task 13, test `TestReport_FlagsHubJitter`.*
5. **Results upload fails** because the hub was closed or the Wi-Fi dropped. Expected: results are written to `localStorage` first and retried on next launch, and the run is never lost. *Owner: Task 9, test `results_uploadFails_keptAndRetried`.*

---

### Task 1: Go spike module and Go CI

**Files:**
- Create: `ipcam/go.work` (uses `./tools`, `./tools/spikes/video`), `ipcam/tools/go.mod` with `tool` directives (`golangci-lint` v2, `govulncheck`, `go-licenses`), `ipcam/tools/tools.go`
- Create: `ipcam/tools/spikes/video/go.mod` (module `github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video`), `ipcam/tools/spikes/video/.golangci.yml`, `ipcam/tools/spikes/video/doc.go`
- Create: `ipcam/tools/licenses/allowlist.txt`, `ipcam/tools/scripts/check-licenses.sh`
- Create: `.github/workflows/ipcam-spikes.yml`

**Interfaces:**
- Produces: `cd ipcam && go tool golangci-lint run ./tools/spikes/video/...` and `go test ./tools/spikes/video/...` work, and the CI job `go` runs both plus `govulncheck` and the license check.

- [ ] **Step 1:** Create the modules and the lint config (`version: "2"`; `govet, staticcheck, errcheck, gosec, revive`).
- [ ] **Step 2:** Write `ipcam-spikes.yml`:
  - Paths: `ipcam/tools/spikes/video/**`, `ipcam/tools/**`, `ipcam/go.work` and the workflow file. `concurrency` with cancel-in-progress. Actions pinned to commit SHAs. `setup-go` with `go-version-file`.
  - Job `go`: lint, `go test -race ./tools/spikes/video/...`, govulncheck, licenses.
- [ ] **Step 3: Verify.** `cd ipcam && go build ./tools/... && go tool golangci-lint run ./tools/spikes/video/... && go run github.com/rhysd/actionlint/cmd/actionlint@latest ../.github/workflows/ipcam-spikes.yml` — Expected: all succeed.
- [ ] **Step 4: Commit** — `git commit -m "ipcam: add Go spike workspace and CI"`

### Task 2: JS workspace and apps CI

**Files:**
- Create: `ipcam/package.json` (private; scripts `typecheck`, `lint`, `test`, `build`), `ipcam/pnpm-workspace.yaml` (packages `apps/packages/*`, `apps/mobile`, `apps/desktop`, `tools/spikes/video/app`; `minimumReleaseAge: 10080`), `ipcam/tsconfig.base.json`, `ipcam/eslint.config.js`, `ipcam/vitest.workspace.ts`
- Create: `.github/workflows/ipcam-apps.yml`

**Interfaces:**
- Produces: `pnpm -C ipcam typecheck|lint|test|build` and the ESLint rule from Global Constraints for `apps/packages/player/**`. `tsconfig.base.json` sets `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `"target": "ES2022"` and `"module": "ESNext"`.

- [ ] **Step 1:** Write the configs.
- [ ] **Step 2:** Write `ipcam-apps.yml`. Paths: `ipcam/apps/**`, `ipcam/tools/spikes/video/app/**`, `ipcam/package.json`, `ipcam/pnpm-*.yaml`, `ipcam/tsconfig.base.json`, `ipcam/eslint.config.js` and the workflow file. Steps: `pnpm install --frozen-lockfile`, typecheck, lint, test, build.
- [ ] **Step 3: Verify.** `pnpm -C ipcam install && pnpm -C ipcam lint && pnpm -C ipcam typecheck` — Expected: succeed with no packages yet (empty workspace OK).
- [ ] **Step 4: Commit** — `git commit -m "ipcam: add pnpm workspace and apps CI"`

### Task 3: Band format and media generator

**Files:**
- Create: `ipcam/tools/spikes/video/band/{band.go,band_test.go,vectors.json}`
- Create: `ipcam/tools/spikes/video/cmd/mediagen/main.go` and `mediagen_test.go`
- Create: `ipcam/tools/spikes/video/media/README.md`
- Modify: `.github/workflows/ipcam-spikes.yml` (job `media`: `apt-get install ffmpeg`, run mediagen, upload artifact `spike-video-media`)

**Interfaces:**
- Produces:

```go
package band
type Code struct{ Camera uint8; Frame uint16; Loop uint16 }
func Encode(c Code) [48]bool                 // row 1; row 2 is the inverse
func Decode(row1, row2 [48]bool) (Code, bool) // false if CRC fails or rows disagree
func Paint(frame []byte, width, height int, c Code) // I420 luma plane; block side 16 px if height >= 1080 else 8 px
```

- `mediagen -out DIR` writes the four elementary streams and the HLS folder from Global Constraints. It produces raw I420 frames (moving gradient, band, large printed frame number) and pipes them to `ffmpeg -f rawvideo -pix_fmt yuv420p … -c:v libx264|libx265` with the exact profile, level, fps, GOP and bitrate. After encoding it runs `ffprobe`, and writes `manifest.json` with each file's codec, profile, level, width, height, fps, GOP, bitrate and frame count.

- [ ] **Step 1: Write the failing tests.**
  - `TestBand_Vectors`: every vector in `vectors.json` (at least 20, including camera 0 and 255, frame 0 and 65535, loop wrap) encodes to the stored bits and decodes back.
  - `TestBand_RejectsCorruptedBit`: flip any single bit and `Decode` returns false.
  - `TestPaintDecode_RoundTripAtBothSizes`: paint into 1920×1080 and 640×360 luma buffers, read the blocks back by averaging each block's center 50 %, and the result equals the input.
  - `TestMediagen_ManifestMatchesSpec` (requires ffmpeg; skipped if absent): the manifest values equal Global Constraints within ±5 % bitrate and exact for the rest, and `has_b_frames == 0`.
- [ ] **Step 2: Run.** `go test ./tools/spikes/video/band/... ./tools/spikes/video/cmd/mediagen/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** CRC-8/SMBUS: polynomial `0x07`, init `0x00`, no reflection, no xorout.
- [ ] **Step 4: Run.** Same command — Expected: PASS. Then `go run ./tools/spikes/video/cmd/mediagen -out /tmp/media && ls /tmp/media` shows 4 `.h264`/`.h265` files, `hls/` and `manifest.json`.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike frame band and media generator"`

### Task 4: `spikehub` sources: looped simulators, pacing, GOP cache, optional RTSP

**Files:**
- Create: `ipcam/tools/spikes/video/source/{annexb.go,sim.go,pacer.go,gop.go,rtsp.go}` + tests

**Interfaces:**
- Consumes: the media files from Task 3.
- Produces:

```go
type Codec string // "H264" | "H265"
type AccessUnit struct{ NALUs [][]byte; IDR bool; Frame uint16; Loop uint16; PTS time.Duration }
type Source interface {
	ID() string; Codec() Codec; Profile() string /* e.g. "4d0028" */; FPS() int
	Subscribe() (cached []AccessUnit, live <-chan AccessUnit, cancel func()) // cached = from last IDR
	SentLog() <-chan SentRecord                                               // only simulators
	Jitter() time.Duration                                                    // p99 send deviation since start
}
type SentRecord struct{ Camera uint8; Frame, Loop uint16; At time.Time } // hub clock
func ReadAnnexB(path string) ([]AccessUnit, error)
func NewSim(id string, camera uint8, aus []AccessUnit, fps int, phase int, clock Clock) Source
func NewRTSP(ctx context.Context, id string, url string) (Source, error) // owner's camera; url in memory only
type Clock interface{ Now() time.Time; NewTicker(d time.Duration) Ticker }
```

- [ ] **Step 1: Write the failing tests.**
  - `TestReadAnnexB_SplitsAccessUnits`: the `sub` file yields 720 AUs, and the IDR flag is on every 12th starting at 0.
  - `TestSim_PacesAtFPS`: with a fake clock, 60 ticks at 12 fps emit 60 AUs and `SentRecord.At` advances by 83.3 ms ±1 ms.
  - `TestSim_LoopCounterIncrements`: after 720 frames, `Loop` becomes 1.
  - `TestSim_PhaseOffsetsDifferPerCamera`.
  - `TestGOP_SubscribeStartsAtLastIDR`: subscribing mid-GOP returns `cached[0].IDR == true` and the cached frames up to the current one.
  - `TestPacer_ReportsJitter` (Review Focus 4): a fake clock with 30 ms injected delays gives `Jitter()` ≥ 30 ms.
  - `TestRTSP_ReadsFromLocalServer`: a gortsplib test server publishing the `sub` file; `NewRTSP` yields AUs with H264 codec. The URL never appears in any log output (capture the logger and assert).
- [ ] **Step 2: Run.** `go test ./tools/spikes/video/source/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** RTSP depacketisation uses gortsplib's `format.H264`/`format.H265` decoders.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike hub camera sources"`

### Task 5: `spikehub` WebRTC session: slots, binding, data channel protocol

**Files:**
- Create: `ipcam/tools/spikes/video/session/{session.go,slots.go,codecs.go,proto.go}` + tests

**Interfaces:**
- Consumes: `source.Source`.
- Produces:

```go
type Session struct{ /* *webrtc.PeerConnection, slots, sources map, dc */ }
func NewAPI(mdns bool) (*webrtc.API, error)   // SettingEngine: MulticastDNSModeQueryOnly when mdns; H264 (all offered profile-level-ids) + H265 registered
func Answer(api *webrtc.API, offerSDP string, sources map[string]source.Source) (*Session, string, error) // N recvonly video m-lines → N slots
func (s *Session) Close() error
func (s *Session) SelectedPair() (local, remote string /* candidate types only */, ok bool)
```

Data-channel `rpc` messages are JSON, one per message, with fields in this order:
- client → hub:
  - `{"t":"bind","slot":3,"cam":"sim-04","profile":"sub","epoch":7}`
  - `{"t":"unbind","slot":3}`
  - `{"t":"ping","c0":123.4}`
- hub → client:
  - `{"t":"bound","slot":3,"epoch":7,"codec":"H264","profile":"4d0028"}`
  - `{"t":"error","slot":3,"epoch":7,"code":"CODEC_UNSUPPORTED"}`
  - `{"t":"pong","c0":123.4,"h":1729260000123.5}` (`h` = hub Unix ms)
  - `{"t":"sent","cam":4,"r":[[frame,loop,hubMs],…]}`, every 500 ms, simulators only
  - `{"t":"pair","local":"host","remote":"prflx"}` when ICE selects a pair
  - `{"t":"jitter","ms":3.1}` every 10 s

Binding rules:
- A slot writes the source's cached GOP, then live frames, through a `TrackLocalStaticSample` created at answer time. A rebind switches the source on the next IDR of the new source, so no mixed GOPs reach the decoder.
- H.264 profile matching: answer with the client's `profile-level-id` whose profile matches the source. A High@5.1 source with no High offered → `CODEC_UNSUPPORTED`.
- H.265 is sent only if offered; otherwise `CODEC_UNSUPPORTED` (APP-05 fallback happens client-side).

- [ ] **Step 1: Write the failing tests** (an in-process Pion client peer in the test):
  - `TestAnswer_CreatesSlotPerRecvonlyLine`: an offer with 5 recvonly lines plus `rpc` gives 5 slots.
  - `TestBind_FirstPacketStartsWithIDR`: after `bind`, the first RTP payload received on that transceiver depacketises to an IDR AU, and a `bound` message carries the same epoch.
  - `TestRebind_SwitchesOnNextIDR`: depacketised AUs on the slot change camera index (band decode on known frames is not needed; use the AU `Frame` sequence of the second source) only at an IDR.
  - `TestBind_H265NotOfferedReturnsError`, and `TestBind_HighProfileNotOfferedReturnsError` (offer with only `42e01f`).
  - `TestPingPong_EchoesClientTime`.
  - `TestSentLog_BatchesEvery500ms` (fake clock).
  - `TestSelectedPair_TypesOnly`: the `pair` message contains no IP literal (regex check).
  - `TestMDNS_CoexistsWithExistingListener` (G7, Linux): a UDP socket bound to `:5353` with `SO_REUSEADDR` exists before `NewAPI(true)`; a session still answers, and resolves a `.local` host candidate published by the test. When the mDNS listener cannot start, `NewAPI` returns a usable API without mDNS and a `MDNSUnavailable` flag, which the banner and every results file report.
- [ ] **Step 2: Run.** `go test ./tools/spikes/video/session/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS (with `-race`).
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike hub WebRTC session with slot binding"`

### Task 6: `spikehub` HTTP server and binary

**Files:**
- Create: `ipcam/tools/spikes/video/cmd/spikehub/{main.go,server.go,banner.go,results.go}` + tests
- Create: `ipcam/tools/spikes/video/cmd/spikehub/web/.gitkeep` (release builds copy the app's `dist/` here; `//go:embed web`)

**Interfaces:**
- Consumes: Tasks 4 and 5.
- Produces the binary `spikehub`, with flags `-media DIR` (default `./media`), `-sims 16`, `-port 7080`, `-results DIR` (default `./results`), `-mdns=true` and `-real` (interactive: prompts for up to 4 RTSP URLs with hidden input via `golang.org/x/term`). It serves:
  - `GET /` and `/assets/*`: the embedded spike app
  - `GET /spike/v1/cameras` → `[{"id":"sim-01","camera":1,"codec":"H264","profiles":["sub","main"]}, …, {"id":"mx-h265","camera":17,"codec":"H265","profiles":["main","sub"]}, {"id":"mx-high51","camera":18,"codec":"H264","profileLevel":"640033","profiles":["main","sub"]}, {"id":"real-1",…}]` (`-sims N` sets the number of `sim-*` cameras, each with `sub` and `main`; the two codec-matrix cameras are always present; their `main` uses `main-h265` / `main-high51` and their `sub` uses the standard `sub` stream, so the G9 fallback stays on the same camera)
  - `POST /spike/v1/offer` (SDP in, SDP out; `Content-Type: application/sdp`)
  - `GET /media/hls/*` (the VOD; `Accept-Ranges`)
  - `POST /spike/v1/results` (JSON ≤ 2 MiB → `results/<device>-<yyyymmdd-hhmmss>-<scenario>.json`; the device and scenario fields are sanitised to `[a-z0-9-]{1,40}`)

  CORS allows exactly the origins `http://localhost:7080`, `http://localhost`, `http://tauri.localhost` and `https://tauri.localhost`.

  On start it prints a Romanian banner: the LAN URLs (`http://<ip>:7080`) for each private interface, the instruction for the phone, and the firewall hint from Review Focus 1.

- [ ] **Step 1: Write the failing tests** (`httptest`):
  - `TestCameras_ListsSimsAndCodecs`: with `-sims 16`, the list has `sim-01…sim-16` (profiles `sub` and `main`), `mx-h265` and `mx-high51` (profiles `main` and `sub`), with codecs and profile levels per Global Constraints.
  - `TestOffer_ReturnsSDPAnswer`.
  - `TestResults_SanitisesNameAndSize`: device `../../x` is stored as `x`; 3 MiB → 413.
  - `TestCORS_AllowsOnlyListedOrigins`: `http://evil.example` gets no `Access-Control-Allow-Origin`.
  - `TestHLS_ServesRanges`.
  - `TestBanner_ListsPrivateURLsAndFirewallHint`: the output contains `Rețele private`.
  - `TestRealURL_NotLoggedNorReturned`: start with an injected real URL containing `user:pw@`; neither `/spike/v1/cameras` nor the logs contain `pw`.
- [ ] **Step 2: Run** — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS. Then `go build -o /tmp/spikehub ./tools/spikes/video/cmd/spikehub && /tmp/spikehub -media /tmp/media -sims 4 &` and `curl -s localhost:7080/spike/v1/cameras` lists the cameras.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spikehub HTTP server and binary"`

### Task 7: `@ipcam/player` (production subset: WebRTC slot live and HLS recordings)

**Files:**
- Create: `ipcam/apps/packages/player/{package.json,tsconfig.json,src/index.ts,src/player.ts,src/webrtc.ts,src/hls.ts,src/stats.ts,src/timers.ts,NOTICE}` + `src/*.test.ts`

**Interfaces:**
- Produces the API from spec §6.2. Exact for this plan:

```ts
export type Transport = 'webrtc' | 'mse' | 'hls' | 'snapshot';
export interface PlayerSource { kind: 'live' | 'recording'; cameraId: string; profile: 'main' | 'sub'; from?: Date; url?: string /* recording: HLS playlist URL (spike); production uses a ReadRange loader (CTR-12) */ }
export interface MediaLink { webrtcTrack(slot: number): Promise<MediaStreamTrack>; byteStream(call: 'mse' | 'segment', req: Uint8Array): Promise<ReadableStream<Uint8Array>> }
export interface Player {
  attach(video: HTMLVideoElement, link: MediaLink): void;
  play(src: PlayerSource, prefer?: Transport[]): Promise<void>;
  seek(t: Date): Promise<void>;
  stop(): void;
  readonly stats: { transport: Transport; codec: string; framesDecoded: number; latencyMs?: number; hardware?: boolean };
  on(e: 'first-frame' | 'transport-changed' | 'error', cb: (d: unknown) => void): () => void;
}
export function createPlayer(opts?: { now?: () => number; setTimer?: (fn: () => void, ms: number) => () => void; slot?: number; hlsFactory?: () => HlsLike }): Player;
```

The fallback timers from §6.2 are exact: no first frame within 4 s, or `framesDecoded` stalled for 3 s, emits `error` with `{reason: 'NO_FIRST_FRAME' | 'STALLED'}`. The spike handles the error itself. MSE and snapshot fallbacks are not implemented in this plan; `play` with `prefer` containing only those rejects with `'UNSUPPORTED_TRANSPORT'`. `first-frame` fires from `requestVideoFrameCallback`, and on Firefox, which lacks it, from the first `timeupdate` with `currentTime > 0`. Attribution for video-rtc.js (MIT) goes in `NOTICE`.

- [ ] **Step 1: Write the failing tests** (Vitest; fake video element and fake track; injected timers):
  - `play_webrtc_attachesTrackAndEmitsFirstFrame`
  - `noFirstFrameIn4s_emitsErrorNoFirstFrame`
  - `framesStalled3s_emitsErrorStalled`
  - `recording_usesHlsFactoryAndSeeks`: `seek` calls `hls.media.currentTime` with the right offset.
  - `stop_detachesAndClearsTimers`
  - `onUnsubscribe_stopsCallbacks`
  - An ESLint fixture test proving that importing `react` from `apps/packages/player/src` fails lint.
- [ ] **Step 2: Run.** `pnpm -C ipcam --filter @ipcam/player test` — Expected: FAIL.
- [ ] **Step 3: Implement.** hls.js is a dependency; `hlsFactory` defaults to `() => new Hls({ lowLatencyMode: false })`.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add @ipcam/player with WebRTC and HLS"`

### Task 8: Spike app core: session, band reader, clock sync, metrics

**Files:**
- Create: `ipcam/tools/spikes/video/app/{package.json,tsconfig.json,vite.config.ts,index.html}`
- Create: `ipcam/tools/spikes/video/app/src/{session.ts,band.ts,clock.ts,metrics.ts,percentile.ts,device.ts}` + `*.test.ts`

**Interfaces:**
- Consumes: `@ipcam/player` (Task 7), the hub protocol (Task 5) and `band/vectors.json` (Task 3).
- Produces:

```ts
export interface SpikeSession { slots: number; bind(slot: number, cam: string, profile: 'sub' | 'main'): Promise<{ epoch: number; codec: string }>; unbind(slot: number): void;
  track(slot: number): MediaStreamTrack; pc: RTCPeerConnection; onSent(cb: (cam: number, recs: [number, number, number][]) => void): void;
  pair(): Promise<{ local: string; remote: string }>; close(): void }
export function openSession(hubUrl: string, slots: number): Promise<SpikeSession>; // offer: slots+1 recvonly video transceivers + dc "rpc"
export function readBand(video: HTMLVideoElement, canvas: OffscreenCanvas | HTMLCanvasElement): { camera: number; frame: number; loop: number } | null;
export function decodeBits(row1: boolean[], row2: boolean[]): { camera: number; frame: number; loop: number } | null;
export class ClockSync { constructor(ping: () => Promise<{ c0: number; c1: number; h: number }>); estimate(): Promise<{ offsetMs: number; rttMs: number; reliable: boolean }> }
export function glassToGlassMs(displayPerfMs: number, sentHubMs: number, offsetMs: number, timeOrigin: number): number;
export interface TileStats { framesDecoded: number; framesDropped: number; fps: number; decoder?: string; powerEfficient?: boolean }
export function sampleStats(pc: RTCPeerConnection): Promise<Map<string /* trackId */, TileStats>>;
export function p(values: number[], q: number): number; // nearest-rank percentile
```

- [ ] **Step 1: Write the failing tests** (Vitest):
  - `decodeBits_vectors`: the same vectors as the Go test, read from `../../band/vectors.json`.
  - `decodeBits_crcFail_returnsNull`
  - `clockOffset_jitterySamples_minRttWins` (Review Focus 3): 20 samples with RTT 5–80 ms; the offset comes from the minimum-RTT sample. RTT > 50 ms or an offset change > 20 ms → `reliable: false`.
  - `glassToGlass_knownValues`
  - `p_nearestRank`: `p([1..20], 0.9) == 18`.
  - `openSession_offerHasSlotsPlusOneRecvonlyAndRpc`: a fake `RTCPeerConnection` records `addTransceiver` calls (7 for slots=6) and `createDataChannel('rpc')`.
  - `sampleStats_mapsInboundRtp`: a fake `getStats` report.
- [ ] **Step 2: Run.** `pnpm -C ipcam --filter spike-video-app test` — Expected: FAIL.
- [ ] **Step 3: Implement.** `readBand` draws the top 40 px strip to a canvas and averages the luma (BT.601) of the central 50 % of each block, with threshold 128.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike app session, band reader and metrics"`

### Task 9: Spike app scenarios, UI and results

**Files:**
- Create: `ipcam/tools/spikes/video/app/src/{main.ts,ui.ts,i18n.ts,scenarios/*.ts,results.ts,wakelock.ts}` + `*.test.ts`

**Interfaces:**
- Consumes: Task 8, plus the native facades from Tasks 10 and 11 through `window.spikeNative?: { metrics(): Promise<NativeMetrics>; enterPip?(): Promise<void>; onPip?(cb: (on: boolean) => void): void; keepAwake?(on: boolean): Promise<void> }`, where `NativeMetrics = { thermal?: 'NONE'|'LIGHT'|'MODERATE'|'SEVERE'|'CRITICAL'|'EMERGENCY'|'SHUTDOWN'; batteryPct?: number; memMb?: number; cpuPctTotal?: number; cpuPctApp?: number; model?: string; os?: string; webview?: string; gpu?: string }`. Without native code, the facade is undefined and the dependent measurements are marked `n/a`. The MV4 capture (SPIKE-ONLY) is web code in `src/decoderinfo.ts`: `captureForDecoderInfo(): Promise<() => void>` holds a 1-pixel `getUserMedia` video track while S3 samples `decoderImplementation`/`powerEfficientDecoder`, and returns `n/a` when no camera or no permission is available.
- Produces: scenario runners that write `ScenarioResult` JSON:

```ts
export interface ScenarioResult { scenario: 'S1'|'S2'|'S3-4'|'S3-9'|'S3-16'|'S4'|'S5'|'S6'|'S7'|'S8'|'S9'|'S10';
  device: { platform: 'android'|'windows-tauri'|'chrome'|'edge'|'firefox'; model?: string; os?: string; webview?: string; ua: string };
  startedAt: string; durationS: number; interrupted: boolean; hubJitterMs: number[]; samples: Record<string, number[]>; notes: string[] }
```

| Scenario | Measures | What it does |
|---|---|---|
| S1 | MV1 | bind `sub` 20 times on slot 0: bind → first frame of the matching epoch, with 2 s pause |
| S2 | MV2 | 100 latency samples on `sub` and 100 on `main` |
| S3-4 / S3-9 / S3-16 | MV3, MV4, MV5, MV6 | grid of 4 / 9 / 16 `sub` tiles for 10 min. Stats every 2 s; native metrics every 10 s. MV4 uses `captureForDecoderInfo` from `src/decoderinfo.ts` (SPIKE-ONLY; the screen explains why the camera light turns on). `main` fullscreen for 2 min at the end |
| S4 | MV7 | 30 min of paging (4 → 9 → 4 every 60 s) and fullscreen toggles every 90 s; memory every 30 s; count of `error` events not caused by the scenario |
| S5 | MV9 | guided: "Apăsați butonul Acasă, așteptați 2 minute, reveniți" (Press Home, wait 2 minutes, come back); measures time from `visibilitychange` to visible until the next first frame |
| S6 | MV8 (Android) | enter PiP on `sub`; count `requestVideoFrameCallback` callbacks over 5 min while in PiP; pass if no gap > 2 s |
| S7 | MV10 | HLS: 20 starts and 20 seeks to random offsets in the 10-min VOD |
| S8 | MV15 | Argon2id via hash-wasm, `m=65536 KiB, t=3, p=4`, 32-byte output, 10 runs |
| S9 | G9 | bind `mx-h265`, then `mx-high51` (profile `main`): record `bound` or `error`, first frame within 4 s, and fallback to `sub` on failure; the result per path is `ok`, `fallback` or `unsupported-profile` |
| S10 | MV12 / MV13 | record `pair` types, plus whether a permission prompt appeared (the user answers yes/no in the UI) |

- [ ] **Step 1: Write the failing tests** (Vitest with fake session, fake time and fake `document.visibilityState`):
  - `S1_collects20FirstFrameSamples`
  - `scenario_visibilityHidden_marksInterrupted` (Review Focus 2; not for S5 or S6)
  - `results_uploadFails_keptAndRetried` (Review Focus 5: fake `fetch` rejects; the item stays in `localStorage` under key `spike.pending`; the next `flush()` succeeds and clears it)
  - `connectError_showsTriedUrlAndHint` (Review Focus 1)
  - `S9_h265NotOffered_fallsBackToSub`
  - `S8_runsTenTimes` (hash-wasm mocked)
  - `i18n_roIsDefault_enAvailable`
- [ ] **Step 2: Run** — Expected: FAIL.
- [ ] **Step 3: Implement.** The UI is a start page: hub URL field (pre-filled with `location.origin` on web; remembered on mobile) → device check → list of scenarios with "Pornește" buttons and per-scenario status. While running, a wake lock is held (`navigator.wakeLock` where available, otherwise the native facade).
- [ ] **Step 4: Run** — Expected: PASS. Then `pnpm -C ipcam --filter spike-video-app build` produces `dist/`.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike scenarios, UI and resilient results upload"`

### Task 10: Android shell (Capacitor) with PiP and spike metrics plugins

**Files:**
- Create: `ipcam/apps/mobile/{package.json,capacitor.config.ts,android/**}` (Capacitor 8.5.3, `appId: "ro.ipcam.spike"`, temporary and never published, OPN-01)
- Create: `ipcam/apps/mobile/plugins/pip/{android/src/main/java/ro/ipcam/pip/PipPlugin.kt,src/index.ts,package.json}`
- Create: `ipcam/tools/spikes/video/android-metrics/{android/src/main/java/ro/ipcam/spike/MetricsPlugin.kt,src/index.ts,package.json}` (spike-only)
- Create: `ipcam/tools/scripts/count-native-lines.sh`
- Create: `.github/workflows/ipcam-mobile.yml`

**Interfaces:**
- Consumes: the spike app `dist/` (Task 9) as `webDir` (`../../tools/spikes/video/app/dist`).
- Produces `window.spikeNative = { metrics, enterPip, onPip, keepAwake }`, installed by `tools/spikes/video/app/src/native-android.ts` (types from Task 9):
  - `enterPip()`: Activity PiP with aspect ratio 16:9; `onPip(cb)` fires on entering and leaving PiP
  - `metrics()` fills `thermal` (from `PowerManager.getCurrentThermalStatus`), `batteryPct`, `memMb` (PSS), `model`, `os` (SDK level) and `webview` (WebView package version)
  - `keepAwake(on)` (`FLAG_KEEP_SCREEN_ON`)
  - MV14 counts the Kotlin of `apps/mobile/plugins/pip` only; the spike metrics plugin is excluded as SPIKE-ONLY, and the `local-network` plugin (Phase 4) is reported as "not measured in Phase 0".
- Android build flavour `spike` (SPIKE-ONLY): `server.androidScheme: "http"`, a `network_security_config.xml` with `cleartextTrafficPermitted="true"`, and the `CAMERA` permission for the MV4 capture. The `release` flavour keeps `false` (§6.7). `minSdk 29`, `targetSdk 36`.

- [ ] **Step 1: Write the failing tests.**
  - Kotlin unit test `PipPluginTest.aspectRatio16by9`.
  - `MetricsPluginTest.thermalStatusMapsAllLevels` (map from `PowerManager.THERMAL_STATUS_*`).
  - TS facade test `nativeAndroid_installsSpikeNative`.
  - `count-native-lines.sh` test: the fixture over 400 lines exits 1.
- [ ] **Step 2: Run** `pnpm -C ipcam --filter @ipcam/mobile test && (cd ipcam/apps/mobile/android && ./gradlew testSpikeDebugUnitTest)` — Expected: FAIL.
- [ ] **Step 3: Implement.** Then write `ipcam-mobile.yml`:
  - Paths: `ipcam/apps/mobile/**`, `ipcam/apps/packages/**`, `ipcam/tools/spikes/video/app/**`, `ipcam/tools/spikes/video/android-metrics/**` and the workflow file.
  - Job `android`: JDK 21, Android SDK 36, build `assembleSpikeDebug`, unit tests, line count.
  - Job `emulator` (AND-3, `reactivecircus/android-emulator-runner`, API 36, x86_64, KVM): start `spikehub` on the runner with 4 sims, install the APK, launch with intent extra `autorun=S1&hub=http://10.0.2.2:7080` and wait ≤ 5 min for a results file with `S1` and 20 samples.
- [ ] **Step 4: Run** the tests locally — Expected: PASS. CI `ipcam-mobile.yml` is green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add Android spike shell with PiP and metrics plugins"`

### Task 11: Windows shell (Tauri) with spike metrics

**Files:**
- Create: `ipcam/apps/desktop/{package.json,src-tauri/Cargo.toml,src-tauri/tauri.conf.json,src-tauri/src/main.rs,src-tauri/src/spike_metrics.rs}`
- Create: `.github/workflows/ipcam-windows.yml`

**Interfaces:**
- Consumes: the spike app `dist/` as `frontendDist`.
- Produces:
  - The Tauri command `spike_metrics() -> { cpu_pct_total: f32, cpu_pct_app: f32, rss_mb: f32, webview2_procs: u32, cpu_model: String, gpu: String }`. It sums the app process and its `msedgewebview2.exe` children via `sysinfo` and sits behind cargo feature `spike-metrics`, on by default only in the spike build profile.
  - CLI args `--autorun S1 --hub http://localhost:7080` for CI.
  - `window.spikeNative.metrics` is wired to the command in `tools/spikes/video/app/src/native-tauri.ts`, mapping to `cpuPctTotal`, `cpuPctApp`, `memMb` and `gpu` (Task 9 `NativeMetrics`).
  - The CSP allows `connect-src http://localhost:7080 http://*:7080`.
- Rust total under 300 lines (APP-01), checked by `count-native-lines.sh`.

- [ ] **Step 1: Write the failing tests.**
  - `cargo test`: `spike_metrics_sums_children` with an injected process list.
  - TS: `nativeTauri_installsSpikeNative`.
- [ ] **Step 2: Run** `cd ipcam/apps/desktop/src-tauri && cargo test --features spike-metrics` — Expected: FAIL.
- [ ] **Step 3: Implement.** Then write `ipcam-windows.yml` (`windows-latest`, WIN-2):
  - Paths: `ipcam/apps/desktop/**`, `ipcam/apps/packages/**`, `ipcam/tools/spikes/video/**` and the workflow file.
  - Build the spike app.
  - `tauri build --no-bundle` plus an NSIS bundle, both unsigned.
  - Start `spikehub.exe -sims 4`, run the app with `--autorun S1`, and assert that a results file with 20 S1 samples appears within 5 min.
- [ ] **Step 4: Run** — Expected: PASS locally for `cargo test`; CI green on Windows.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add Windows spike shell with metrics"`

### Task 12: Browser end-to-end test in CI (HUB-06 and G9 fallback)

**Files:**
- Create: `ipcam/tools/spikes/video/e2e/{playwright.config.ts,spike.spec.ts}`
- Modify: `.github/workflows/ipcam-spikes.yml` (job `e2e`: download the media artifact, build the app, embed it, run `spikehub -sims 4`, run Playwright with `channel: 'chrome'` and also with Firefox)

**Interfaces:**
- Consumes: Tasks 3–9.

- [ ] **Step 1: Write the failing tests.**
  - `S1 p90 first frame ≤ 1500 ms` (HUB-06, Chrome; 20 starts).
  - `band decodes on ≥ 95 % of sampled frames` (Chrome).
  - `4-tile grid 60 s: fps ≥ 90 % of 12 and dropped ≤ 2 %` (Chrome).
  - `S9: H.265 not decodable on runner → fallback to sub, labelled` (APP-05 negative branch).
  - `Firefox × mx-high51 → "profil video nesuportat" label and sub fallback` (APP-05).
  - `S10: selected pair is host or prflx` (MV13 on the runner).
- [ ] **Step 2: Run** `pnpm -C ipcam exec playwright test -c tools/spikes/video/e2e` — Expected: FAIL before wiring, PASS after.
- [ ] **Step 3: Implement** the missing glue (the `?autorun=` URL parameter in the app, the test hooks `window.__spikeResults`).
- [ ] **Step 4: Run** — Expected: PASS locally (Chrome installed) and in CI.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike browser end-to-end tests"`

### Task 13: Report generator and verdicts

**Files:**
- Create: `ipcam/tools/spikes/video/cmd/spikereport/{main.go,metrics.go,verdict.go,render.go}` + tests and `testdata/` result sets

**Interfaces:**
- Consumes: the `ScenarioResult` JSON files (Task 9).
- Produces `spikereport -in results/ -out docs/research`, which writes:
  - `docs/research/<date>-phase0b-results.md`. It contains a Romanian summary for the owner, then per device a table of `Cod | Valoare | Prag | Trece`, then the verdicts for AND-1, WIN-1 and web and the triggered decisions (PH0-30 format), plus a list of Faza 0-bis leftovers (iOS rows, MV11, the Chrome part of MV12, AND-2).
  - `docs/research/phase0-data/phase0b-*.csv` with one row per sample.

```go
type Verdict string // "GO provizoriu (fără iOS)" | "GO condiționat" | "NO-GO mobil" | "GO" | "plan B: grilă desktop 9" | "plan B: relay în LAN + aplicația nativă"
func ComputeMV(results []Result) map[string]MV     // MV{Code, Value float64, Unit string, Threshold string, Pass bool, Device string}
func AndroidVerdict(mv map[string]MV) Verdict        // rule from Global Constraints
func WindowsVerdict(mv map[string]MV) Verdict
func WebVerdict(mv map[string]MV) Verdict
```

- [ ] **Step 1: Write the failing tests.**
  - `TestAndroidVerdict_AllPass_GOProvizoriu`.
  - `TestAndroidVerdict_Only9TileFails_GOConditionat`.
  - `TestAndroidVerdict_MV9Fails_NOGO`.
  - `TestWindowsVerdict_MV6Fails_PlanB9`.
  - `TestWebVerdict_MV13Relay_PlanB`.
  - `TestComputeMV_ExcludesInterruptedRuns`.
  - `TestReport_FlagsHubJitter` (Review Focus 4: runs with p99 jitter > 20 ms are flagged).
  - `TestRender_NoIPOrSerialInOutput`: a regex over the output finds no IPv4/IPv6 literal.
  - `TestRender_GoldenMarkdown`: matches `testdata/golden.md`.
- [ ] **Step 2: Run** `go test ./tools/spikes/video/cmd/spikereport/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add spike report generator with A3 verdicts"`

### Task 14: Release assets and owner guide

**Files:**
- Create: `.github/workflows/ipcam-spike-release.yml`. It runs on tag `spike-video-v*`. It builds `spikehub` for `windows/amd64`, `linux/arm64` and `linux/arm` (GOARM=7) with the app embedded, the media zip, `ipcam-spike.apk` (spike debug) and the Tauri NSIS installer (unsigned). It writes `SHA256SUMS`, generates an SBOM with Syft and creates the GitHub Release.
- Create: `ipcam/docs/spike-video.md` (Romanian owner guide)
- Modify: `ipcam/README.md` (link to the guide)

The guide covers, in order:
1. Download from the Release page.
2. Unpack and run `spikehub.exe`. Allow it on **Rețele private** in the Windows firewall prompt. Read the phone URL from the window.
3. On the PC: open Chrome at `http://localhost:7080`, run S1, S2, S3-9, S3-16, S4, S7, S9 and S10; repeat in Edge and Firefox (S1, S2, S3-9, S9, S10).
4. Install `ipcam-desktop` and run S3-16, S4 and S10.
5. On the Galaxy A24: allow installing from the browser, install `ipcam-spike.apk`, connect to the same Wi-Fi (not guest), enter the URL, then run S1, S2, S3-4, S3-9 (with the camera-light explanation for MV4), S4, S5, S6, S7, S8, S9 and S10. Keep the phone plugged in only for S4; S3 runs on battery.
6. Optional: start `spikehub -real` and type the RTSP URLs of your own cameras.
7. Zip the `results` folder and attach it in the chat or put it in `ipcam-teste/` on Google Drive.

It also includes a time estimate per device, what each scenario does in one line, and what to do when something fails (Review Focus 1–2).

- [ ] **Step 1:** Write the workflow and validate it with `actionlint`. Push tag `spike-video-v0.1.0` on the branch, check that all assets are attached and `SHA256SUMS` verifies.
- [ ] **Step 2:** Write the guide. Check it against the actual binaries: run the guide's steps on the Linux runner equivalents (hub plus Chrome) to make sure every UI label quoted in the guide exists (`grep` the built app for each quoted label).
- [ ] **Step 3: Commit** — `git commit -m "ipcam: add video spike release and owner guide"`

### Task 15: Owner run, report and decisions (PH0-30, PH0-31, PH0-32)

**Files:**
- Create: `ipcam/docs/research/<date>-phase0b-results.md` and `ipcam/docs/research/phase0-data/phase0b-*.csv` (from `spikereport`)
- Modify: `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`: §2.2 for triggered technical decisions; §2.1 only for an owner-approved A6 change. Each change references its gate.

- [ ] **Step 1:** Send the owner the Release link and the guide link in Romanian, with the time estimate.
- [ ] **Step 2:** When the results arrive (chat attachment or Drive `ipcam-teste/`), put them in a scratch directory. Run `spikereport` and inspect the output for any IP, SSID or URL before committing (PH0-22).
- [ ] **Step 3:** Write the triggered decisions:
  - `GO provizoriu` → none, plus the note "lucru de interfață specific telefonului așteaptă verdictul iOS".
  - `GO condiționat` → ask the owner about the phone cap (A6) and record the answer.
  - `NO-GO mobil` → T29: open a new brainstorming/plan for `apps/mobile-flutter`.
  - Windows plan B → §2.2 "grilă desktop 9".
  - Web plan B → §2.2 "relay în LAN pentru web".
  - G19 fail → §2.2 Argon2id `m=46 MiB, t=1, p=1`.
  - G9 outcomes → §2.2.
- [ ] **Step 4:** List the services and accounts used, each with its `OWN` row, to confirm zero cost (PH0-32). List the Faza 0-bis leftovers with gate, estimated cost and deadline (§16.9).
- [ ] **Step 5: Commit and open a PR** for the owner's approval — `git commit -m "ipcam: add Phase 0 (b) video spike results and decisions"`
