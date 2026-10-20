# Phase 0 (c1) Recorder Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Pe scurt pentru proprietar.** Construim o probă care verifică, pe dispozitivele dvs., că hub-ul nu pierde înregistrări la o pană de curent și că duce numărul de camere pe care îl promitem:
- pe PC-ul cu Windows 11 și pe Raspberry Pi-ul dvs. (hub-ul citește singur ce model e);
- celălalt dispozitiv joacă rol de „martor” (controlor): trimite camere simulate și măsoară exact momentul opririi;
- măsurăm cât se pierde la oprire, dacă înregistrarea repornește singură după un blocaj, câte camere duce dispozitivul o oră întreagă, cât de repede se derulează o înregistrare criptată și redarea prin conexiunea directă.

Ce aveți de făcut: porniți un program pe fiecare dispozitiv, iar într-un interval de timp ales de dvs. îl lăsați să repornească dispozitivele automat de câteva ori. La final, de 5 ori pe fiecare dispozitiv, scoateți curentul când vă spune programul și reporniți. Totul e gratuit și folosește doar dispozitivele dvs. La final iese un raport cu verdictul pentru porțile G10–G13 și G18.

**Goal:** Measure MI1, MI2, MI3, MI4 and MI9 on the owner's Windows 11 PC (host H3) and the owner's Raspberry Pi (any supported model, §4.8), using the other device as the T43 controller, and produce the Phase 0 (c) recorder-durability report with verdicts for gates G10, G11, G12, G13 and G18.

**Architecture:**
- **Production-bound code (written for real, §3.5, §16.1).** The MediaMTX fork is vendored under `hub/third_party/mediamtx` with `PATCHES.md` and the three patches MTX-1 (fsync per part), MTX-2 (`age` encryption at write) and MTX-3 (segment-closed hook). The `recorder.Recorder` adaptor (`hub/internal/recorder`) and the `crypto.Recording` `age` writer/reader (`hub/internal/crypto`) are the real hub modules, scoped to the subset these gates exercise.
- **`reccontroller`.** The T43 controller runs on the *other* device. It publishes pre-coded simulated cameras over RTSP into the host's MediaMTX, logs the send time of each access unit (its own clock), and receives a 50 ms UDP liveness beacon from the host under test. The stop moment is the arrival of the last beacon. Loss is computed from the controller's own send log keyed by AU index, so NTP skew on the host cannot corrupt it.
- **`recspike` harness.** Orchestrates the power-cut, stability, capacity, `age` random-access and data-channel playback runs, and writes the `MeasureResult` JSON the report consumes. Harness-only; lives under `tools/spikes/recording/` and is deleted after the report.
- **The report.** A Go generator turns the results into the Romanian report and the G10–G13 and G18 verdicts.

**Tech Stack:**
- Go (newest `go1.27.x` whose toolchain is ≥ 14 days old), MediaMTX v1.21.2 (MIT, vendored fork), `github.com/bluenviron/gortsplib/v5` v5.6.7, `github.com/bluenviron/mediacommon/v2`, `github.com/pion/webrtc/v4` v4.2.24, `filippo.io/age` v1.3.2
- `coturn` 4.14+ and `netem` as lab-only tools for the emulated TURN-TLS/443 path (MI9); `notmyfault64` (Sysinternals, unshipped test tool) on the PC; `echo b > /proc/sysrq-trigger` on the Pi
- ffmpeg with libx264 (CI/lab only, for pre-coded test media), QEMU/KVM (TST-09)

**Spec:** `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`:
- §16.4 (MI1–MI4, MI9; test hosts; controller T43; real power cuts), §16.5 gates G10–G13, G18 and PH0-30…32
- §4.2 (go2rtc/MediaMTX integration; MTX-1/2/3; fallback recorder on gortsplib/mediacommon), §4.4 (recorder, crypto interfaces), §4.5 (segment lifecycle, startup reconcile), §4.8 (hardware tiers, per-model Pi counts, T40 SD policy), §4.9 (disk layout)
- §7.6 CTR-12 (data-channel playback, G18), §8.5 (playback, RAM buffer plan B), §10.2 (age format, agerange), §10.1 ENC-03, ENC-08
- §16.9 (Faza 0-bis deferrals), decisions O11, O13, O14, O16, T39, T40, T41, T43; requirements HUB-04, HUB-07, HUB-30…34, STO-02 (playback of open segment only), SUC-05, ENC-08, CTR-12, FLW-15

**Prerequisites:** The video spike plan (`2026-10-18-phase0b-video-spike.md`) Task 1 has created `ipcam/go.work`, `ipcam/tools/go.mod` (with `go tool` directives for `golangci-lint`, `govulncheck`, `go-licenses`) and the Go CI patterns; this plan adds `./hub` and `./tools/spikes/recording` to that workspace and reuses those lint/license/CI patterns. MI4 part 3 (`agerange` in the AND-1 WebView, ENC-08) reuses the video spike app and Android shell (video spike Tasks 9–10). MI9 reuses the video spike `session` package for the Pion PeerConnection and `rpc`/`bulk` data-channel plumbing (video spike Task 5); it adds only a bulk playback measurement on top. Phase 0 (a) is not required by this plan.

## Global Constraints

- **Repo rules.** Touch only `ipcam/**` and `.github/workflows/ipcam-*.yml`; every push/PR trigger has `paths:` limited to the files it builds plus its own workflow file. Code, identifiers, comments and commits are in English. Owner-facing text (guide, report summary, on-screen controller prompts) is Romanian.
- **Zero cost (O13, PH0-32).** No paid service and no payment details. Only the owner's devices: a Windows 11 PC, a Raspberry Pi of unknown model (the harness reads and reports the model via `/proc/cpuinfo`, HUB-34; an armv6 board moves all Pi measurements to Faza 0-bis, §16.9), and DIGI home internet. Only GitHub-hosted runners (free on the public repo, O12) and QEMU/KVM for the lab runs. No smart plug (T43). H1 (Pi 5 + SSD) and H2 (N100) are Faza 0-bis, only if the owner buys them — list them, do not plan their execution.
- **Owner-approved destructive runs (T43).** Automated hard stops run on the owner's devices **only inside a time window the owner picks**, because they reboot the devices repeatedly: `kill -9`, Pi `echo b > /proc/sysrq-trigger` (sudo, test Pi only), PC `notmyfault64 /crash` bugcheck with Windows auto-reboot on. Plus **5 real power cuts per device, done by hand** when the harness tells the owner (random moment 3–12 min after boot). No other destructive action runs unprompted.
- **Secrets and privacy (PH0-22).** The owner's camera credentials, RTSP URLs and any storage tokens never enter the repo, logs, results or report. Results published to the public repo are redacted per §16.2.3 (no IPs, MACs, serials, SSIDs, camera names, URLs or credentials). Real cameras appear as `real-1`, `real-2`.
- **Toolchains.** Go: newest `go1.27.x` whose `golang.org/toolchain` is ≥ 14 days old (check `Time` with `go list -m -json`). Every other dependency: newest version ≥ 14 days old. Lockfiles committed; `-mod=readonly` in CI.
- **Licenses.** Allowlist: MIT, BSD-2/3, Apache-2.0, ISC, MPL-2.0. MediaMTX v1.21.2, gortsplib v5.6.7, mediacommon v2, Pion v4.2.24 and `filippo.io/age` v1.3.2 are all MIT/BSD-3. ffmpeg with libx264, `coturn` and QEMU are used only in CI/lab to drive the spike; none is shipped and none is in a release asset.
- **Spike-only shortcuts.** None of these may appear in `hub/**`, and each is marked `// SPIKE-ONLY` with a reference to this plan: the plain-RTSP controller publish, the UDP liveness beacon, the frame send-time log, the capacity/stability orchestration, and the data-channel bulk measurement harness. Production recorder/crypto/MediaMTX-patch code carries no such marker.
- **MediaMTX recording shape (§4.2, §4.5).** fMP4 segments of **5 min** with **1 s parts**; fsync on each part close (MTX-1); segment closes at the first keyframe after 300 s or on codec/resolution change; `.part` renamed atomically with directory fsync. When the local disk resolves to "encrypted", the segment is written straight to `.mp4.age` (MTX-2) and video never touches the disk in clear (HUB-04). MTX-2 aligns each part end to a 64 KiB `age` chunk boundary with an fMP4 `free` box, only when padding ≤ 10 % of the part.
- **Thresholds, exactly as in §16.4 / §16.5:**

  | Code | Threshold |
  |---|---|
  | MI1 (G10, HUB-30, FLW-15, SUC-05) | per 20-run automated set and per 5-real-cut set: median loss ≤ 1 s, max ≤ 2 s; reconciliation without intervention; 0 unreadable files; HUB-04 = 0. Encrypted streams < 512 kbps: max = max(1 part, 64 KiB ÷ bitrate) (HUB-07). Half of automated runs encrypted; one run per 20-set at 256 kbps encrypted; real cuts 1, 3 and 5 encrypted. Statistical: 0 failures in ≥ 40 automated per OS → < ≈ 7 %; 0 in 10 real → < ≈ 26 % |
  | MI2 (G11, HUB-31) | 20 × `kill -9` of the recorder host + 72 h with the host's recommended camera count (8 on PC; the model's number on Pi): recording resumes ≤ 10 s; 0 host crashes in 72 h. On Pi with SD card, the 72 h run also exercises card surveillance (T40) |
  | MI3 (G12, HUB-32) | host's recommended cameras (§4.8): 8 on H3, the model's number on the owner's Pi; the tier's live sessions + 1 playback + 1 export + encrypted upload (10 Mbps; on H1b/H1c, the camera throughput) in parallel for 1 h: §4.8 budgets met and total CPU ≤ 60 %; on failure the recommended number drops, the model stays supported (minimum 1, O16) |
  | MI4 (G13, HUB-33, ENC-08) | 1000 random reads, each ≤ 50 ms on the test Pi; seek in a 5-min encrypted segment → first frame ≤ 1.5 s; encryption throughput ≥ 50 MB/s on Pi 5 and, on other models, ≥ 10 × recommended-camera throughput (≥ 20 MB/s Pi 4, ≥ 10 MB/s H1c); `agerange` ≥ 12 Mbps in the AND-1 WebView; identical bytes to `age` CLI and `rage` (ENC-06) |
  | MI9 (G18, CTR-12) | on a 4 Mbps recording: a bulk channel sustains ≥ 8 Mbps on LAN and ≥ 5 Mbps through TURN-TLS/443 at 50 ms RTT, and a timeline seek shows the first frame ≤ 1.5 s |

- **Plan B per gate (written into §2.2 on failure, PH0-31).** G10 → own recorder on gortsplib/mediacommon behind `Recorder`, retested before Faza 1. G11 → reverse ingest (MediaMTX pulls the camera). G12 → recommended number drops (min 1). G13 → (seek) whole-segment decrypt into a ≤ 256 MiB RAM playback buffer, else 2-min encrypted segments; (throughput) recommended number drops; format unchanged. G18 → hub plays the recording as a 1× WebRTC track in the same session, seek via RPC.
- **Faza 0-bis leftovers to list, not execute (§16.9).** H1 (Pi 5) and H2 (N100) measurements; `agerange` on iOS-1 (MI4 part 3 iOS path); any Pi measurement if the owner's Pi is armv6.

## Review Focus

1. **The owner performs a real power cut outside the window, or reboots before the harness says to.** Expected: the controller flags a cut whose boot-to-cut time is outside [3 min, 12 min] or that arrives with no preceding "cut now" prompt as `invalid`, excludes it from the verdict, and the guide tells the owner to redo it (the 5 valid cuts per device are what counts). *Owner: Task 6, test `cut_outsideWindow_markedInvalid`.*
2. **The Pi has no RTC and NTP has not synced, or the clocks drift between the two devices.** Expected: the loss number comes from the controller's own send-time log keyed by AU index, never from the host's catalog wall clock, so a host-side skew leaves loss unchanged; the recorder marks affected segments `time_uncertain` until NTP syncs (§13.8). *Owner: Task 6, test `loss_usesControllerSendLog_notHostClock`.*
3. **A cut leaves a torn final part** (an fMP4 `moof` with no complete `mdat`, or an `.age` file whose last 64 KiB chunk is truncated). Expected: startup reconcile truncates to the last complete part, re-seals the final `age` chunk with the end marker so the file still decrypts with the `age` CLI, writes the lost tail as a `gap`, and if over 50 % of segments would read "lost" it stops with an alert (§4.5). *Owner: Task 5, test `Reconcile_TornAgeTail_StaysDecryptable`.*
4. **The SD card wears out or remounts read-only during the 72 h Pi run (T40).** Expected: `health` moves the "card SD" row to DEGRADED then FAILED and raises the alert in ≤ 60 s; recording stops with a `disk_full`/`card` gap, never silent loss. *Owner: Task 8, test `SdCard_IoErrors_RaiseAlertThenStop`.*
5. **The host PC is a laptop whose battery holds power, so pulling the plug does not cut it.** Expected: the guide instructs a forced shutdown (hold power ≥ 10 s) and the result records `shutdown=forced`; the controller still fixes the stop moment from the last liveness beacon, so the loss number is valid either way. *Owner: Task 6, test `stopMoment_isLastBeacon_regardlessOfShutdownKind`.*

---

### Task 1: `hub` module, MediaMTX vendoring, PATCHES.md and recorder CI

**Files:**
- Modify: `ipcam/go.work` (add `./hub`, `./tools/spikes/recording`)
- Create: `ipcam/hub/go.mod` (module `github.com/AndreiBesliu/DevWorld/ipcam/hub`), `ipcam/hub/.golangci.yml`, `ipcam/hub/doc.go`
- Create: `ipcam/hub/third_party/mediamtx/` (vendored subset of MediaMTX v1.21.2 needed to build the recorder path), `ipcam/hub/third_party/mediamtx/PATCHES.md` (base `v1.21.2`, one section per patch, rebase note), `ipcam/hub/third_party/mediamtx/SOURCE.md` (upstream commit + SHA-256)
- Create: `ipcam/tools/spikes/recording/go.mod` (module `github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording`), `.golangci.yml`, `doc.go`
- Create: `.github/workflows/ipcam-spike-recording.yml`

**Interfaces:**
- Produces: `cd ipcam && go build ./hub/... ./tools/spikes/recording/...`, `go tool golangci-lint run ./hub/... ./tools/spikes/recording/...` and `go test ./hub/... ./tools/spikes/recording/...` work; the CI job `recorder` runs lint, `go test -race`, `govulncheck` and the license check on both module trees.

- [ ] **Step 1:** Create `hub/go.mod` and the spike module; add both to `go.work`. Vendor only the MediaMTX packages the recorder path compiles (muxer/recorder, fMP4 writer), with `SOURCE.md` recording the upstream tag and digest. Lint config mirrors the video spike (`version: "2"`; `govet, staticcheck, errcheck, gosec, revive`).
- [ ] **Step 2:** Write `PATCHES.md` with three empty sections MTX-1/MTX-2/MTX-3 (each: what, why, files touched, test, rebase status), filled by Tasks 3–4.
- [ ] **Step 3:** Write `ipcam-spike-recording.yml`: paths `ipcam/hub/**`, `ipcam/tools/spikes/recording/**`, `ipcam/go.work` and the workflow file; `concurrency` cancel-in-progress; actions pinned to commit SHAs; `setup-go` with `go-version-file`; job `recorder` runs lint, `go test -race`, govulncheck, licenses.
- [ ] **Step 4: Verify.** `cd ipcam && go build ./hub/... ./tools/spikes/recording/... && go tool golangci-lint run ./hub/... ./tools/spikes/recording/... && go run github.com/rhysd/actionlint/cmd/actionlint@latest ../.github/workflows/ipcam-spike-recording.yml` — Expected: all succeed.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add hub module, vendored MediaMTX and recorder spike CI"`

### Task 2: `crypto.Recording` — `age` writer and random-access reader

**Files:**
- Create: `ipcam/hub/internal/crypto/{recording.go,keyscope.go,recording_test.go}`
- Create: `ipcam/hub/internal/crypto/testdata/` (golden byte vectors shared with TS `agerange`)

**Interfaces:**
- Produces (exact, §4.4, §10.2):

```go
package crypto
type KeyScope struct { RecordingIdentity age.Recipient; Recovery age.Recipient } // RI_g + RK public X25519 recipients (T20)
type Recording interface {
    NewWriter(dst io.Writer, scope KeyScope) (io.WriteCloser, error)            // age STREAM, ChaCha20-Poly1305, 64 KiB chunks
    OpenReaderAt(src io.ReaderAt, size int64) (io.ReaderAt, int64, error)        // random access (E6); returns plaintext size
}
func NewRecording(ids ...age.Identity) Recording // ids used by OpenReaderAt; nil ids => header-only (write path)
```

The writer encrypts for exactly the two recipients in `KeyScope` (§10.2, T20). `OpenReaderAt` uses `age.DecryptReaderAt` (present in v1.3.2), caches the file key via `ExtractHeader`/`DecryptHeader` + `NewInjectedFileKeyIdentity`, and authenticates size through the final chunk. Chunk *i* of the plaintext starts at `len(header) + 16 + i·(65536+16)`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRecording_RoundTrip`: write N bytes, read them back whole; bytes equal.
  - `TestRecording_RandomRangesMatchStream`: 1000 random ranges read via `OpenReaderAt` equal the same ranges from a full streaming decrypt (ENC-07).
  - `TestRecording_DecryptsWithAgeCLIAndRage` (requires `age` and `rage`; skipped if absent): a file written with only `RK` as identity decrypts with both tools to the same bytes (ENC-06).
  - `TestRecording_TwoRecipientsOnly`: the header lists exactly `RI_g` and `RK`; a third recipient is rejected.
- [ ] **Step 2: Run.** `go test ./hub/internal/crypto/...` — Expected: FAIL.
- [ ] **Step 3: Implement** over `filippo.io/age` v1.3.2.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add crypto age recording writer and random-access reader"`

### Task 3: MTX-1 (fsync per part) and MTX-3 (segment-closed hook) patches

**Files:**
- Modify: `ipcam/hub/third_party/mediamtx/` (muxer part-closer and recorder), `PATCHES.md` (fill MTX-1, MTX-3)
- Create: `ipcam/hub/third_party/mediamtx/patch_test.go`

**Interfaces:**
- Produces: on each part close, `Sync()` is called on the part file and the segment directory before the next part opens; on segment finalize, a MediaMTX-layer `SegmentComplete{Path, StartMs, EndMs}` is delivered over the MTX-3 hook (over `runOnRecordSegmentComplete`). Task 5's `recorder` maps this to its own `recorder.SegmentClosed`.

- [ ] **Step 1: Write the failing tests** (inject a file/dir wrapper recording `Sync` calls):
  - `TestMTX1_FsyncOnEachPartClose`: writing 3 parts yields a part-file `Sync` and a directory `Sync` per part, before the next part's first write.
  - `TestMTX1_AtomicRenameWithDirFsync`: the `.part` → final rename is followed by a directory `Sync`.
  - `TestMTX3_HookFiresWithFinalPath`: finalizing a segment delivers one `SegmentComplete` whose `Path` is the renamed final file and whose `EndMs − StartMs` ≈ segment duration.
- [ ] **Step 2: Run.** `go test ./hub/third_party/mediamtx/...` — Expected: FAIL.
- [ ] **Step 3: Implement** both patches; record them in `PATCHES.md` with the exact files touched and the rebase note (re-apply on each adopted MediaMTX release).
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MediaMTX MTX-1 fsync and MTX-3 segment hook patches"`

### Task 4: MTX-2 (`age` encryption at write) patch

**Files:**
- Modify: `ipcam/hub/third_party/mediamtx/` (fMP4 part writer), `PATCHES.md` (fill MTX-2)
- Create: `ipcam/hub/third_party/mediamtx/mtx2_test.go`

**Interfaces:**
- Consumes: `crypto.Recording.NewWriter` (Task 2).
- Produces: when a recording job carries `age` recipients, each segment is written straight through `crypto.NewWriter` to `<name>.mp4.age`; clear video never reaches the disk (HUB-04). At each part fsync, all complete 64 KiB `age` chunks are flushed; the remainder stays in RAM. The part end is aligned to a chunk boundary with an fMP4 `free` box only when the padding is ≤ 10 % of the part. Writes are strictly sequential (no rewrite of the file).

- [ ] **Step 1: Write the failing tests.**
  - `TestMTX2_EncryptedSegmentHasNoClearFrames`: record an encrypted segment of a known NAL pattern; scanning the file finds `ftyp`/`moof` and the test NAL prefixes 0 times (HUB-04), and the file decrypts with `crypto.OpenReaderAt`.
  - `TestMTX2_PartBoundaryChunkAligned`: each part boundary lands on a 64 KiB `age` chunk boundary; the `free` padding is ≤ 10 % of the part, otherwise no padding is added.
  - `TestMTX2_StrictlySequentialWrites`: the writer performs only appends (no seek-back), verified with an append-only writer wrapper.
  - `TestMTX2_PublicKeysOnly`: the recorder is given only public recipients, never an identity.
- [ ] **Step 2: Run.** `go test ./hub/third_party/mediamtx/...` — Expected: FAIL.
- [ ] **Step 3: Implement**; record in `PATCHES.md` with the 64 KiB alignment note tying it to §4.5.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MediaMTX MTX-2 age-at-write patch"`

### Task 5: `recorder.Recorder` adaptor with crash-recovery reconcile

**Files:**
- Create: `ipcam/hub/internal/recorder/{recorder.go,adaptor.go,reconcile.go,gaps.go}` + tests

**Interfaces:**
- Consumes: Tasks 2–4; the vendored MediaMTX process.
- Produces (exact, §4.4):

```go
package recorder
type CameraID string; type Profile string // "main" | "sub"
type Job struct { Cam CameraID; Profile Profile; RestreamURL string; SegmentSecs int; PartSecs int; AgeRecipients []age.Recipient }
type SegmentClosed struct { Cam CameraID; Profile Profile; Path string; StartMs, EndMs int64; Encrypted bool }
type RecStatus struct { LastPartAgoMs int64; LastSegmentAgoMs int64; Running bool }
type Recorder interface { Apply(jobs []Job) error; Closed() <-chan SegmentClosed; Status(cam CameraID, p Profile) RecStatus }
func New(binaryPath, runDir string) (Recorder, error) // generates MediaMTX config (no camera passwords), starts & supervises the process

// Startup reconcile (§4.5): called by New before serving.
func Reconcile(recDir string, rec Recording) (ReconcileReport, error)
type ReconcileReport struct { Finalized, Truncated, LostTails, GapsWritten int; Aborted bool /* >50% would be lost */ }
```

`Apply` (re)starts MediaMTX so recording resumes ≤ 10 s after a host crash (HUB-31). `Reconcile`: a clear `.part` is truncated to the last complete `moof`+`mdat` and finalized; an `.age` tail is recovered with the recording identity, truncated to the last complete part, and the final chunk re-sealed with the end marker so the file stays valid for `age`; the lost tail becomes a `gap`; if over 50 % of segments would read "lost", it aborts with an alert (HUB-09).

- [ ] **Step 1: Write the failing tests.**
  - `TestRecorder_ResumesAfterKill`: kill the MediaMTX child; `Apply` restarts it and `Status.Running` is true again, with a new part within 10 s (fake clock).
  - `Reconcile_TornClearPart_TruncatesAndFinalizes`: a `.part` with a half-written `mdat` is truncated to the last complete part and renamed.
  - `Reconcile_TornAgeTail_StaysDecryptable` (Review Focus 3): an `.age` file with a truncated final chunk is recovered, re-sealed, and decrypts with the `age` CLI; the lost tail is one `gap` row.
  - `Reconcile_OverHalfLost_Aborts`: a `recDir` where > 50 % of segments are unreadable returns `Aborted: true` and writes no further changes.
- [ ] **Step 2: Run.** `go test ./hub/internal/recorder/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add recorder adaptor with crash-recovery reconcile"`

### Task 6: T43 controller, liveness beacon and loss computation

**Files:**
- Create: `ipcam/tools/spikes/recording/controller/{controller.go,beacon.go,sender.go,loss.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/reccontroller/main.go`
- Create: `ipcam/tools/spikes/recording/cmd/livebeacon/main.go` (runs on the host under test)

**Interfaces:**
- Consumes: the pre-coded `main`/`sub` elementary streams and the `band` package from the video spike (`tools/spikes/video/band`, Prerequisite).
- Produces:

```go
package controller
type SentAU struct { Index uint64; At time.Time }                     // controller clock
type Cut struct { BootAt, PromptAt, StopAt time.Time; ShutdownKind string /* "power" | "forced" | "sysrq" | "bugcheck" | "kill9" */
                  LastRecordedIndex uint64; Encrypted bool; BitrateKbps int }
func PublishPrecoded(ctx context.Context, rtspURL, mediaPath string, log *SendLog) error // // SPIKE-ONLY
func (l *SendLog) SendTime(index uint64) (time.Time, bool)
func StopMoment(beacons <-chan Beacon) (at time.Time, errBound time.Duration)             // last beacon; errBound ≤ 100 ms // SPIKE-ONLY
func Loss(c Cut, log *SendLog) (time.Duration, bool)                                      // StopAt − SendTime(LastRecordedIndex); false if index unknown
func Valid(c Cut) bool                                                                    // 3 min ≤ PromptAt−BootAt ≤ 12 min AND StopAt after a prompt
```

`LastRecordedIndex` is read from the reconciled catalog after reboot by counting access units with `mediacommon` (no pixel decode); the burned `band` is an ffmpeg-based cross-check only. The loss subtraction uses only the controller's clock (`StopAt` and `SendTime` both come from the controller), so host NTP skew cannot change it.

- [ ] **Step 1: Write the failing tests.**
  - `cut_outsideWindow_markedInvalid` (Review Focus 1): a cut with boot-to-prompt 2 min, or with no preceding prompt, returns `Valid == false`.
  - `loss_usesControllerSendLog_notHostClock` (Review Focus 2): injecting a 5 s skew into the host catalog timestamps leaves `Loss` unchanged.
  - `stopMoment_isLastBeacon_regardlessOfShutdownKind` (Review Focus 5): beacons stop; `StopMoment` returns the last arrival with `errBound ≤ 100 ms` for `ShutdownKind` `"power"` and `"forced"`.
  - `TestSendLog_MapsIndexToSendTime`: AU 719 maps to its recorded send time ± 1 ms.
  - `TestPublishPrecoded_NoRealUrlInLogs`: start with an injected `user:pw@` real URL; neither logs nor results contain `pw` (capture the logger).
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/controller/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** RTSP publish via gortsplib; beacon is a 50 ms UDP ticker.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add T43 controller, liveness beacon and loss computation"`

### Task 7: MI1 power-cut spike (automated + real cuts) — G10

**Files:**
- Create: `ipcam/tools/spikes/recording/mi1/{runner.go,lab.go,ownerdevice.go,realcut.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi1.go` (subcommand `recspike mi1`)
- Modify: `.github/workflows/ipcam-spike-recording.yml` (job `mi1-lab`: on the Linux runner 20 × `kill -9` + 20 × QEMU `system_reset` (TST-09), on the Windows runner 20 × `kill -9`, half encrypted, one 256 kbps encrypted run per set; uploads `phase0c-recorder-mi1` artifact)

**Interfaces:**
- Consumes: Tasks 5, 6.
- Produces `recspike mi1` with modes `--lab`, `--owner-device <pc|pi>` and `--real`:
  - `--lab`: on the GitHub runners (free), 20 × `kill -9` on Linux + 20 × `kill -9` on Windows + 20 × QEMU/KVM `system_reset` (TST-09), random cut 3–12 min after boot, half the runs encrypted, one run per 20-set at 256 kbps encrypted (HUB-07 limit).
  - `--owner-device`: 20 × `kill -9` + 20 × no-cache-flush stop (Pi `echo b > /proc/sysrq-trigger`; PC `notmyfault64 /crash` with auto-reboot), **only inside the owner-chosen window**; prints a Romanian start/stop banner naming the window.
  - `--real`: guides the owner through 5 hand cuts (random prompt 3–12 min after boot), cuts 1/3/5 encrypted; after each reboot the harness reconciles and records the run.
  - Writes `MeasureResult{code:"MI1", host, os, runs:[{kind, encrypted, bitrateKbps, lossMs, reconciled, unreadable, hub04Clear}]}`.

- [ ] **Step 1: Write the failing tests.**
  - `TestMI1_Verdict_MedianAndMax`: a run set with median 0.8 s and max 1.9 s passes; max 2.1 s fails.
  - `TestMI1_LowBitrateEncrypted_UsesHub07Limit`: a 256 kbps encrypted run is judged against `max(1 part, 64 KiB ÷ bitrate)`, not 2 s.
  - `TestMI1_HalfEncrypted`: a 20-run set has 10 encrypted runs and exactly one 256 kbps encrypted run.
  - `TestMI1_ConfidenceFromCounts`: 0 failures in 40 automated runs reports "< ≈ 7 %"; 0 in 10 real reports "< ≈ 26 %".
  - `TestMI1_RealCutRequiresWindowPrompt`: a real cut recorded without a preceding "cut now" prompt is excluded (Review Focus 1).
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/mi1/...` — Expected: FAIL.
- [ ] **Step 3: Implement**; the owner-device and real modes refuse to run outside the configured window.
- [ ] **Step 4: Run** — Expected: PASS; `ipcam-spike-recording.yml` `mi1-lab` green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI1 power-cut spike for G10"`

### Task 8: MI2 crash and 72 h stability — G11 (plus SD-card surveillance)

**Files:**
- Create: `ipcam/tools/spikes/recording/mi2/{runner.go,stability.go}` + tests
- Create: `ipcam/hub/internal/recorder/sdcard.go` + test (block-device detection and health rows, T40, HUB-17)
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi2.go`

**Interfaces:**
- Consumes: Task 5.
- Produces:

```go
// recorder (production): SD-card detection and health, T40/HUB-17
func IsSdCard(mountinfoPath, recDir string) bool          // mmcblk*, major 179, from /proc/self/mountinfo
type CardHealth int // OK, DEGRADED, FAILED
func CardStatus(io IoStats) CardHealth                    // §13.1 thresholds

// mi2 (spike)
type Mi2Result struct { Host, OS string; ResumeMs []int64 /* 20 kills */; Crashes72h int; OnSdCard bool; CardAlertAtMs *int64 }
func RunMi2(ctx context.Context, cfg Mi2Config) (Mi2Result, error)
```

- [ ] **Step 1: Write the failing tests.**
  - `TestMI2_Verdict`: 20 resumes all ≤ 10 s and 0 crashes passes; one resume 12 s fails.
  - `TestIsSdCard_DetectsMmcblk`: a `/proc/self/mountinfo` fixture with `mmcblk0p2` major 179 → true; an NVMe device → false.
  - `SdCard_IoErrors_RaiseAlertThenStop` (Review Focus 4): injected I/O errors move the card row DEGRADED→FAILED and raise the alert in ≤ 60 s; a read-only remount → FAILED and recording stops with a gap.
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/mi2/... ./hub/internal/recorder/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** The 72 h run uses the host's recommended camera count; on Pi with SD card it also samples card health.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI2 stability spike and SD-card surveillance for G11"`

### Task 9: MI3 hardware-tier capacity rig — G12 (plus Pi model report)

**Files:**
- Create: `ipcam/tools/spikes/recording/mi3/{capacity.go,budgets.go}` + tests
- Create: `ipcam/hub/internal/recorder/pimodel.go` + test (read `/proc/cpuinfo` `Model`/`Revision`, never `Serial`; recommended number per §4.8, HUB-34)
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi3.go`

**Interfaces:**
- Consumes: Task 5.
- Produces:

```go
// recorder (production): Pi model, HUB-34
type PiModel struct { Raw string; Recommended int; Known bool }
func ReadPiModel(cpuinfoPath string) PiModel // table from §4.8; unknown model => Known=false, no number

// mi3 (spike)
type Mi3Result struct { Host, OS string; Cameras int; CpuPctTotal, RamMb float64; Budgets map[string]bool; Pass bool }
func RunMi3(ctx context.Context, cfg Mi3Config) (Mi3Result, error) // live sessions + 1 playback + 1 export + 10 Mbps encrypted upload, 1 h
```

The rig runs the host's recommended cameras for 1 h with one playback, one export and an encrypted upload (10 Mbps; on H1b/H1c the camera throughput), checks the §4.8 budgets and total CPU ≤ 60 %. On failure it reports the camera count that passes (minimum 1); the model stays supported (O16).

- [ ] **Step 1: Write the failing tests.**
  - `TestReadPiModel_TableFromSpec`: "Raspberry Pi 5" → 8; "Raspberry Pi 4" → 4; "Raspberry Pi 3 Model B Rev 1.2" → 1–2; an unknown string → `Known:false`, no number; `Serial` is never read.
  - `TestMI3_Verdict_BudgetsAndCpu`: total CPU 55 % with budgets met passes; 65 % fails.
  - `TestMI3_OnFail_DropsRecommended`: a failing 8-camera run reports the largest passing count, ≥ 1.
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/mi3/... ./hub/internal/recorder/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI3 capacity rig and Pi model report for G12"`

### Task 10: MI4 `age` random-access rig — G13 (ENC-08)

**Files:**
- Create: `ipcam/tools/spikes/recording/mi4/{seek.go,throughput.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi4.go`
- Create: `ipcam/tools/spikes/video/app/src/agerange-bench.ts` + test (WebView `agerange` throughput, reuses the video spike app; marked SPIKE-ONLY)

**Interfaces:**
- Consumes: Task 2 (`crypto.OpenReaderAt`); the video spike `agerange` (`apps/packages/core/agerange`) and the video spike app WebView harness (Prerequisite).
- Produces:

```go
// mi4 (spike)
type Mi4Result struct { Host string; ReadMsP50, ReadMsP99 float64; SeekFirstFrameMs float64; EncryptMBs float64; AgeCliOK bool; Pass bool }
func RunMi4(ctx context.Context, cfg Mi4Config) (Mi4Result, error) // 1000 random reads; 5-min encrypted segment seek; encryption throughput; age CLI/rage cross-check
```
```ts
// video spike app, SPIKE-ONLY
export function benchAgerange(cipherBytes: Uint8Array, identity: string): Promise<{ mbps: number }>; // ≥ 12 Mbps target in AND-1 WebView
```

The encryption-throughput threshold is per model (≥ 50 MB/s Pi 5, ≥ 20 MB/s Pi 4, ≥ 10 MB/s H1c, derived from the recommended-camera throughput); an unmeasured model keeps its §4.8 estimate. The iOS `agerange` path (WKWebView) is Faza 0-bis — listed, not run.

- [ ] **Step 1: Write the failing tests.**
  - `TestMI4_ReadLatency`: 1000 reads with p99 ≤ 50 ms passes; p99 60 ms fails.
  - `TestMI4_SeekFirstFrame`: a seek giving first frame 1.2 s passes; 1.7 s fails and records the RAM-buffer plan B as the triggered decision.
  - `TestMI4_ThroughputPerModel`: 22 MB/s on a Pi 4 passes (≥ 20); 18 MB/s fails and drops the recommended number.
  - `agerange_bench_meets12Mbps` (Vitest): the WebView bench reports ≥ 12 Mbps on a fixture (hashed timing mocked).
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/mi4/...` and `pnpm -C ipcam --filter spike-video-app test` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI4 age random-access rig for G13"`

### Task 11: MI9 playback over the data channel — G18 (CTR-12)

**Files:**
- Create: `ipcam/tools/spikes/recording/mi9/{bulk.go,seek.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi9.go`
- Modify: `.github/workflows/ipcam-spike-recording.yml` (job `mi9`: start a local `coturn`, apply `netem` 50 ms RTT, run the LAN and relay measurements)

**Interfaces:**
- Consumes: the video spike `session` package for the Pion PeerConnection and `rpc`/`bulk` plumbing (Prerequisite); a 4 Mbps recording from the pre-coded media.
- Produces:

```go
package mi9
type Transport string // "lan" | "turns443"
type BulkResult struct { Transport Transport; ThroughputMbps float64; SeekFirstFrameMs float64; Pass bool }
func MeasureBulk(ctx context.Context, recPath string, t Transport, rttMs int) (BulkResult, error) // 20 timeline seeks
```

LAN pass ≥ 8 Mbps; `turns443` pass ≥ 5 Mbps through a local `coturn` on TLS/443 at 50 ms emulated RTT; a seek must show the first frame ≤ 1.5 s. On failure the triggered decision is the WebRTC-track plan B (§8.5), recorded for §2.2.

- [ ] **Step 1: Write the failing tests** (an in-process Pion peer; a loopback `coturn`):
  - `TestMI9_LanThroughput`: a run at 9 Mbps LAN passes; 7 Mbps fails.
  - `TestMI9_RelayThroughput`: 6 Mbps through the relay passes; 4 Mbps fails.
  - `TestMI9_SeekFirstFrame`: first frame 1.3 s passes; 1.7 s fails and records the plan B.
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/mi9/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS; `mi9` CI job green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI9 data-channel playback spike for G18"`

### Task 12: Report generator and verdicts (G10–G13, G18)

**Files:**
- Create: `ipcam/tools/spikes/recording/cmd/recreport/{main.go,metrics.go,verdict.go,render.go}` + tests and `testdata/` result sets

**Interfaces:**
- Consumes: the `MeasureResult`/`Mi*Result` JSON from Tasks 7–11.
- Produces `recreport -in results/ -out docs/research`, which writes `docs/research/<date>-phase0c-recorder-results.md` (Romanian owner summary; per host a `Cod | Valoare | Prag | Trece` table; the G10–G13 and G18 verdicts; the triggered decisions in PH0-30 format; the Pi model and resulting recommended numbers, §4.8; the Faza 0-bis leftovers) and `docs/research/phase0-data/phase0c-recorder-*.csv` (one row per sample).

```go
type Verdict string // "trece" | "pică → <plan B>"
func VerdictG10(r []Result) Verdict // median ≤1s, max ≤2s per set
func VerdictG11(r []Result) Verdict
func VerdictG12(r []Result) Verdict
func VerdictG13(r []Result) Verdict
func VerdictG18(r []Result) Verdict
```

- [ ] **Step 1: Write the failing tests.**
  - `TestG10_AllSetsPass_Trece` and `TestG10_OneRealCutFails_PlanB`.
  - `TestG11_Resume12s_PlanB`, `TestG12_Fail_DropsRecommended`, `TestG13_SeekFail_RamBuffer`, `TestG18_RelayFail_WebrtcTrack`.
  - `TestRender_NoIPSerialOrUrlInOutput`: a regex over the output finds no IPv4/IPv6 literal, no serial and no `rtsp://` URL (PH0-22).
  - `TestRender_GoldenMarkdown`: matches `testdata/golden.md`.
- [ ] **Step 2: Run.** `go test ./tools/spikes/recording/cmd/recreport/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add recorder spike report generator with G10-G13,G18 verdicts"`

### Task 13: Release assets and Romanian owner guide

**Files:**
- Create: `.github/workflows/ipcam-spike-recording-release.yml` (runs on tag `spike-recording-v*`; builds `recspike`, `reccontroller` and `livebeacon` for `windows/amd64`, `linux/arm64` and `linux/arm` (GOARM=7) with MediaMTX embedded, the pre-coded media zip, `SHA256SUMS` and a Syft SBOM; creates the GitHub Release)
- Create: `ipcam/docs/spike-recording.md` (Romanian owner guide)
- Modify: `ipcam/README.md` (link to the guide)

The guide covers, in order: (1) which device is the host and which is the controller (Pi when testing the PC and vice versa), and that both must be NTP-synced; (2) start `reccontroller` on the controller and `recspike` on the host, both from the Release; (3) choose the time window for the automated hard-stop runs and let them reboot the device repeatedly (PC bugcheck shows a blue screen each time; Pi reboots immediately); (4) the 5 real power cuts per device when prompted — desktop PC: pull the plug; laptop: hold power ≥ 10 s (forced, noted in the report); Pi: pull the power cable, then reboot; (5) optional: add your own RTSP cameras; (6) zip the `results` folder and attach it in the chat or put it in `ipcam-teste/` on Google Drive. It includes a time estimate per device (the automated set reboots the device dozens of times; the 5 real cuts ≈ 2.5 h of the owner's time, T43), a one-line description of each measurement, and what to do when something fails (Review Focus 1, 4, 5).

- [ ] **Step 1:** Write the workflow; validate with `actionlint`. Push tag `spike-recording-v0.1.0`; check the assets attach and `SHA256SUMS` verifies.
- [ ] **Step 2:** Write the guide; `grep` the built binaries for each Romanian on-screen label the guide quotes, to confirm it exists.
- [ ] **Step 3: Commit** — `git commit -m "ipcam: add recorder spike release and owner guide"`

### Task 14: Owner run, report and decisions (PH0-30, PH0-31, PH0-32)

**Files:**
- Create: `ipcam/docs/research/<date>-phase0c-recorder-results.md` and `ipcam/docs/research/phase0-data/phase0c-recorder-*.csv` (from `recreport`)
- Modify: `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`: §2.2 for triggered technical decisions; §4.8 for the owner's Pi model and resulting recommended numbers. Each change references its gate.

- [ ] **Step 1:** Send the owner the Release link and the guide link in Romanian, with the time estimate and the reminder to pick the hard-stop window.
- [ ] **Step 2:** When the results arrive (chat attachment or Drive `ipcam-teste/`), put them in a scratch directory. Run `recreport` and inspect the output for any IP, serial, SSID or RTSP URL before committing (PH0-22).
- [ ] **Step 3:** Write the triggered decisions into §2.2, each referencing its gate: G10 fail → own gortsplib/mediacommon recorder behind `Recorder`; G11 fail → reversed ingest; G12 outcome → recommended numbers per model (also §4.8); G13 fail → RAM-buffer / 2-min segments; G18 fail → WebRTC-track playback. Record the owner's Pi model and the measured recommended numbers.
- [ ] **Step 4:** List the services, accounts and devices used, each with its `OWN` row, to confirm zero cost (PH0-32: OWN-14 PC, OWN-15 the owner's Pi, OWN-18 CI). List the Faza 0-bis leftovers with gate, estimated cost and deadline (§16.9): H1 (Pi 5), H2 (N100), the iOS `agerange` path, and all Pi measurements if the Pi turns out to be armv6.
- [ ] **Step 5: Commit and open a PR** for the owner's approval — `git commit -m "ipcam: add Phase 0 (c) recorder spike results and decisions"`
