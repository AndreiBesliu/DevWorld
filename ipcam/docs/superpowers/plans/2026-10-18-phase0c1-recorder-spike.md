# Phase 0 (c1) Recorder Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Revision v2 (2026-10-20).** v1 was written before the Phase 0 (b) video spike existed. v2 lands the accepted pre-flight rulings C01–C32, G01–G36, R01–R09 and the owner decision on pre-flight item O1 (ruling C1-R1, recorded in the spec as **O17** by 5a102e4 on `ipcam/design-spec`) in the task texts. What changed and why:
- **Process boundary (C01, C07, C08, C10).** The Go internal-package rule forbids `tools/` from importing `hub/internal`, so the spike reaches production code only through the new production binary `hub/cmd/ipcam-hub`. MI2 kills that binary. recspike sends the beacon itself, so there is no `livebeacon`.
- **MediaMTX fork (C02, C03, C30, C31, R04, R05).** The whole MediaMTX tree is vendored as a nested module outside `go.work`, built with `GOWORK=off`. `PATCHES.md` moves to `hub/third_party/`. MTX-0 records the import-path rewrite and the rpiCamera removal. Version picks follow the 14-day rule.
- **age writer (C04, C11, C21, C29).** `hub/internal/crypto` writes the age v1 payload itself, with `Flush` and tail re-sealing. Keys are exactly two public recipients plus a narrow `TailSealer`, and every interface takes `ctx`.
- **Recording correctness (C09, C12, C13, C14, C22, G05–G08, R01).** `.mp4.age.part` naming, fsyncs per OS, an external-command hook with its environment, a minimal SQLite catalog, an asynchronous verification pass, and the HUB-04 scan. Reconcile keeps the whole samples of a partly durable part.
- **O17 setting „Pierdere minimă la pană de curent” (C1-R1).** Below 512 kbps, encrypted padding is a per-camera bool on `recorder.Job`, default on. The fork's MTX-2 honours it. Every automated MI1 set (lab and owner devices) adds one 256 kbps padding-off run, judged against §4.5's unpadded bound and reported separately.
- **Controller and MI1 (C06, C15, C16, G09–G14, R03, R07, R08).** The controller serves RTSP and stamps an SEI index into each access unit. It owns every timestamp and every inbound listener. MI1 gets defined sets, failure rules, autostart across reboots, and a lab workflow split into shards.
- **Results and judging (C17, C32, G15, G27, G28, G33).** Every measurement uses one results envelope, and each gate has one `Judge`. Lab and CI results travel as annotations. The PC collects the Pi's results. Verdicts can be "incomplete" or "deferred". Redaction reuses the video spike's scanner, and the Faza 0-bis table is static.
- **New Task 7a (C23, C24, G16, G32 production part).** `ipcam-hub diag host` reports the Pi model, physical cores, OS and SD card. It lands before Task 7 because MI1 already needs the recommended camera count and the storage kind. This moves C23's `pimodel.go` and `sdcard.go` from Task 8 to Task 7a; Task 8 keeps the card monitor.
- **New Task 10a and MI4 (C18, C21, C25, C26, G21–G24).** The production TS `agerange` package. MI4 judges the maximum read time, a defined first frame, per-model throughput and an on-phone bench.
- **MI2, MI3, MI9 (C05, G17–G20, G25, G26, G36, R02, R06, C19, C20).** A card monitor with the §13.1 thresholds, a rolling 72 h window, defined MI3 loads and budgets per tier, and an own MI9 bulk harness with three legs.
- **CI, release and guide (G01–G04, G24, G29, G35, R09, C27, C28).** CI installs age and rage, gets a media job, covers its paths, follows the go-job pattern and attributes licenses. The release follows R55 (the owner creates the pre-release in the web UI). No real cameras are recorded. The guide gets a full schedule, install and uninstall, and one phone step.
- **Review Focus (C05, G30).** Test names take the `Test` prefix, and `time_uncertain` moves to Faza 1.

**Pe scurt pentru proprietar.** Construim o probă care verifică, pe dispozitivele dvs., că hub-ul nu pierde înregistrări la o pană de curent și că duce numărul de camere pe care îl promitem:
- pe PC-ul cu Windows 11 și pe Raspberry Pi-ul dvs. (hub-ul citește singur ce model e);
- celălalt dispozitiv joacă rol de „martor” (controlor): trimite camere simulate și măsoară exact momentul opririi;
- măsurăm cât se pierde la oprire, dacă înregistrarea repornește singură după un blocaj, câte camere duce dispozitivul o oră întreagă, cât de repede se derulează o înregistrare criptată și redarea prin conexiunea directă.

Ce aveți de făcut:
- **O singură dată, la început, pe fiecare dispozitiv**, instalați pornirea automată a probei. Pe PC confirmați o fereastră de administrator, descărcați unealta gratuită NotMyFault de pe pagina Microsoft și puneți pe pauză actualizările Windows. Pe Pi rulați o comandă cu `sudo`.
- **Într-un interval de timp ales de dvs.**, lăsați programul să repornească dispozitivele automat de câteva ori.
- **Tăierile de curent:** de 5 ori pe fiecare dispozitiv scoateți curentul când vă spune programul, apoi reporniți.
- **Proba de 72 h și măsurătorile scurte:** lăsați ambele dispozitive pornite 72 h, cu spațiu liber pe disc (cel puțin ≈ 29 GB pe PC; pe Pi, între ≈ 8 și ≈ 29 GB, după model — programul vă spune cifra exactă), plus încă câteva ore de măsurători pornite de dvs. (MI3, MI4, MI9).
- **Pe telefon (Galaxy A24)** instalați aplicația de test nouă și rulați o singură măsurătoare.
- **La final** strângeți rezultatele pe PC cu o comandă și dezinstalați pornirea automată cu încă una.

Totul e gratuit și folosește doar dispozitivele dvs. La final iese un raport cu verdictul pentru porțile G10–G13 și G18.

Setarea cerută de dvs. (O17, „Pierdere minimă la pană de curent”) este, în aplicație, o setare pe fiecare cameră criptată local; contează la camerele cu flux sub 512 kbps:
- **Pornită (implicit):** hub-ul completează fiecare bucată criptată, ca la o pană să se piardă tot ≤ 1 s (mediană) și ≤ 2 s (maxim). Costă spațiu pe disc numai la acea cameră: o cameră de 256 kbps ocupă ≈ 5,7 GB pe zi în loc de ≈ 2,8 GB.
- **Oprită:** discul rămâne la fel, la o pană se pot pierde până la ≈ 3–4 s, iar aplicația afișează o notă.

Proba verifică ambele variante: varianta pornită în rulările obișnuite, iar varianta oprită printr-o rulare în plus în fiecare set de rulări automate, în laborator și pe dispozitivele dvs. Nu aveți nimic de făcut în plus pentru asta.

**Goal:** Measure MI1, MI2, MI3, MI4 and MI9 on the owner's Windows 11 PC (host H3) and the owner's Raspberry Pi (any supported model, §4.8), using the other device as the T43 controller, and produce the Phase 0 (c) recorder-durability report with verdicts for gates G10, G11, G12, G13 and G18.

**Architecture:**
- **Production-bound code (written for real, §3.5, §16.1).**
  - `hub/cmd/ipcam-hub` is the Phase 0 subset of hubd under its §3.5 name. It supervises MediaMTX, reconciles at startup, keeps the catalog, writes JSON-lines events, and has `hook` and `diag` subcommands.
  - The packages `hub/internal/{crypto,recorder,catalog,health}`.
  - The MediaMTX fork is the nested module `hub/third_party/mediamtx` (MTX-0…MTX-3), built with `GOWORK=off` and documented in `hub/third_party/PATCHES.md`.
  - `apps/packages/core/agerange` (TypeScript).
  - None of it carries a SPIKE-ONLY marker.
- **`reccontroller` (T43, SPIKE-ONLY).** It runs on the other device and holds every inbound listener (R08).
  - It serves the pre-coded simulated cameras as an RTSP server and stamps every access unit with an SEI index.
  - It logs send times on its own clock, receives the 50 ms UDP beacon, schedules every cut and prompt, and computes loss.
  - NTP skew on the host cannot change the loss.
- **`recspike` (SPIKE-ONLY).** It runs on the host under test, connects out to the controller, starts `ipcam-hub`, and kills and restarts it.
  - It sends the beacon, triggers OS stops and resumes its sets after reboots (autostart).
  - It reads the last recorded SEI index, and drives the MI2, MI3, MI4 and MI9 measurements.
  - It reaches production code only by running the `ipcam-hub` binary.
- **`recreport`.** It renders the results, judged by each gate's `Judge`, into the Romanian report and the CSVs. All of `tools/spikes/recording/` is deleted after the report.

**Tech Stack:**
- **Go and the fork.** Go 1.27.2. MediaMTX v1.21.1 or v1.21.2 (picked by C31, see Global Constraints) as a vendored MIT fork.
- **Go modules in the workspace.** `filippo.io/age` v1.3.2 (BSD-3) and `modernc.org/sqlite` (newest ≥ 14 days old, BSD-3). From the video spike, at its versions: `github.com/bluenviron/gortsplib/v5` v5.6.6, `github.com/bluenviron/mediacommon/v2` v2.9.6, `github.com/pion/webrtc/v4` v4.2.22 and `github.com/pion/turn/v5` v5.1.2.
- **TypeScript.** `@noble/ciphers` 2.4.0 and `@noble/hashes` 2.4.0 (MIT). `fast-check` (MIT, tests only). `age-encryption` (typage, BSD-3, spike app only).
- **Lab-only tools.** ffmpeg with libx264, QEMU/KVM (TST-09), the `coturn/coturn` Docker image (≥ 4.14) and `tc netem`. The age CLI v1.0.0 and the newest age release, plus rage, for ENC-06.
- **Owner-downloaded tool.** `notmyfault64` (Sysinternals, never redistributed).

**Spec:** `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`:
- §16.4 (MI1–MI4, MI9; test hosts; controller T43; real power cuts), §16.5 gates G10–G13, G18 and PH0-30…32, §16.9 (Faza 0-bis)
- §4.2 (MediaMTX integration; MTX-1/2/3; fallback recorder), §4.3–§4.4 (modules, interfaces with `ctx`), §4.5 (segment lifecycle, startup reconcile), §4.6 (catalog), §4.7, §4.8 (tiers, per-model Pi counts, budgets, T40 SD policy), §4.9 (disk layout)
- §7.6 (bulk channel rules), CTR-12, §8.5 (playback, RAM buffer plan B), §10.2 (age format, agerange), ENC-06, ENC-07, ENC-08, §13.1 (SD-card row), §13.8
- Decisions O11, O13, O14, O16, T39, T40, T41, T43; requirements HUB-04, HUB-07, HUB-09, HUB-13, HUB-17, HUB-30…34, SUC-05, FLW-15, PH0-22
- **O17 (owner answer of 2026-10-20 to pre-flight item O1; ruling C1-R1 in `.superpowers/sdd/2026-10-18-phase0c1-recorder-spike/rulings.md`):** the low-bitrate padding of encrypted streams is the per-camera setting „Pierdere minimă la pană de curent”, default on. The amendment has landed on `ipcam/design-spec` as 5a102e4 (O17, SUC-05, §4.5, the §4.8 Disc row, HUB-07, the camera settings screen, TST-08, TST-09, MI1), but this branch predates it. **The controller merges `origin/ipcam/design-spec` into this branch before Task 1 is dispatched**, so every task reads the amended text. The plan follows it:
  - setting off → §4.5's bound «1 parte + 64 KiB ÷ bitrate, rotunjit în sus la părți întregi» (4,000 ms at 256 kbps);
  - one extra 256 kbps run with the setting off in **every** automated MI1 set (lab and owner devices), outside the set's median and max, judged against that bound and reported separately.

**Prerequisites (they exist in this worktree; the video spike is done):**
- **Go workspace and tooling.** `ipcam/go.work` (`go 1.27.2`; `use ./tools ./tools/spikes/video`). `ipcam/tools/go.mod` with `tool` directives for golangci-lint v2.14.0, govulncheck (x/vuln v1.8.0) and go-licenses v2.0.1.
- **Video spike packages.**
  - `tools/spikes/video/source`: `ReadAnnexB`, `NewSim` with `SentLog()` and `JitterSnapshot()`, `NewRTSP`, `Clock`.
  - `tools/spikes/video/band`: `Paint`, `Decode`.
  - `tools/spikes/video/session`: `NewAPI`, `Answer`.
  - `tools/spikes/video/redact`: `Scan`.
  - `tools/spikes/video/cmd/mediagen`.
  - `tools/spikes/video/cmd/spikehub/keepawake*.go`, which lives in package main, so it is copied, not imported.
- **Scripts.** `ipcam/tools/scripts/{annotate.sh, check-licenses.sh, third-party-notices.sh, check-guide-labels.sh, check-workflow-runs.sh}` with shell tests in `tools/scripts/test/`.
- **Workflows.** `ipcam-spikes.yml` (its go-job pattern), `ipcam-apps.yml`, `ipcam-mobile.yml` and `ipcam-spike-release.yml`. The last one is the release pattern: tag plus `workflow_dispatch` with a `publish` input, an idempotent publish, and a `ci-status` gate.
- **Spike app and APK.** `tools/spikes/video/app` (autorun scenarios S1–S10, results posted to `POST /spike/v1/results`, Romanian texts in `i18n.ts`), and its spike APK build.
- **Video guide.** `ipcam/docs/spike-video.md`, whose SmartScreen and firewall wording R08 reuses.
- **Spec amendment O17.** `origin/ipcam/design-spec` 5a102e4, merged into this branch by the controller before Task 1 (see Spec above).
- **What does not exist yet.** There is no `agerange` (Task 10a creates it) and no bulk channel or Go offerer (Task 11 writes its own harness). Phase 0 (a) is not required.

## Global Constraints

- **Repo rules.**
  - Touch only `ipcam/**` and `.github/workflows/ipcam-*.yml`. Every push or PR trigger has `paths:` covering every file its jobs build, plus its own workflow file.
  - The one edit to another plan's workflow is G03's in `ipcam-spikes.yml` (Task 1). Call it out in the PR.
  - Code, identifiers, comments and commits are in English. Owner-facing text is Romanian: the guide, the report summary, on-screen prompts and verdict strings.
- **Process boundary (C01).** Nothing under `tools/` imports `…/ipcam/hub/internal/…`. The spike uses production code only by running `ipcam-hub` and reading its JSON output. The one exception is load generators and readers (MI3 loads, the SEI reader, MI4 cross-checks, the MI9 hub-side reader that decrypts with `age.DecryptReaderAt`), which may use the public API of `filippo.io/age`.
- **Modules (C03, R04).**
  - **Workspace.** `hub` (`github.com/AndreiBesliu/DevWorld/ipcam/hub`) and `tools/spikes/recording` (`github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording`) join `go.work`.
  - **Fork.** The fork `hub/third_party/mediamtx` (module `github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx`) does **not** join it. It is built, tested, vuln-checked and license-checked from its directory with `GOWORK=off`.
  - **Sibling imports.** A module that imports a sibling declares `require <sibling> v0.0.0` plus `replace <sibling> => <relative path>` (recording → `../video`; fork → `../..`).
  - **Shared versions.** Shared dependencies stay no newer than the video module's: `tools/scripts/check-video-build-list.sh` must stay green.
  - **Tidy and lockfiles.** Each task runs `GOWORK=off go mod tidy` in every Go module whose imports it changes and commits that module's `go.mod`/`go.sum` (the Files lists name them); a task that changes a `package.json` runs `pnpm -C ipcam install` and commits `ipcam/pnpm-lock.yaml`.
- **Versions (C31, R27 pattern).**
  - **Toolchain.** Go 1.27.2 (go1.27.3 becomes eligible on 2026-11-02).
  - **MediaMTX.** If Task 1 runs before 2026-10-22, it vendors MediaMTX v1.21.1. Task 3 re-vendors v1.21.2 first when it starts on or after 2026-10-22, before any patch exists. `SOURCE.md` records the version.
  - **Fork dependencies.** The fork's gortsplib, mediacommon and webrtc are whatever that release requires, in its own `go.mod`.
  - **hub and recording.** They pin mediacommon v2.9.6, webrtc v4.2.22, gortsplib v5.6.6 and pion/turn v5.1.2: the video spike's versions, because the workspace shares one build list.
  - **Everything else.** The newest version at least 14 days old, with lockfiles committed and `-mod=readonly` and `--frozen-lockfile` in CI.
  - **Security exception (R09).** govulncheck runs in CI only, because vuln.go.dev is unreachable here. A finding there allows bumping to the fixed version before 14 days; the commit message says why.
- **Licenses.**
  - **Allowlist.** MIT, BSD-2/3, Apache-2.0, ISC, MPL-2.0.
  - **MediaMTX (G35).** Its `LICENSE` stays in the fork directory. go-licenses runs on the fork with `GOWORK=off`. The release notices name MediaMTX (MIT) and the hls.js (Apache-2.0) embedded as `hls.min.js` explicitly, because the own-prefix ignore skips the fork itself.
  - **rpiCamera (R05).** The rpiCamera source (LGPL libcamera binaries) is removed by MTX-0.
  - **Lab-only tools.** ffmpeg, QEMU, coturn, age, rage and NotMyFault are lab or owner tools. None is shipped, and none is in a release asset.
- **Zero cost (O13, PH0-32).**
  - No paid service and no payment details.
  - **Devices.** Only the owner's devices: the Windows 11 desktop PC (OWN-14), the Raspberry Pi (OWN-15, model unknown, read by `ipcam-hub diag host`), the Galaxy A24 (OWN-14) and DIGI home internet. An armv6 Pi moves every Pi measurement to Faza 0-bis (§16.9) and serves only as a controller.
  - **CI.** GitHub-hosted runners only (OWN-18), with QEMU/KVM for TST-09.
  - **Not used.** No smart plug (T43). H1 (Pi 5) and H2 (N100) are listed, not run.
- **Owner-approved destructive runs (T43, G09, G12, G14).**
  - **Automated hard stops.** `kill -9` of `ipcam-hub`, Pi `echo b > /proc/sysrq-trigger` (as root, test Pi only), and PC `notmyfault64.exe /accepteula /crash` with `CrashControl\AutoReboot = 1`. They run on the owner's devices **only inside the time window the owner picks**, at a uniform random moment in [3, 12] min after recording starts (kill9) or after boot (sysrq, bugcheck).
  - **Real cuts.** 5 per device, done by hand when prompted. They are not tied to the window.
  - **Never changed.** The harness never changes write-cache, mount or power-plan settings. It records the write-cache state.
  - **Autostart.** `recspike install` and `uninstall` run once, while the owner is present. No other destructive action runs unprompted.
- **Secrets and privacy (PH0-22).**
  - **Keys.** Each run generates an ephemeral age pair (`RI_g` and `RK`, `age.GenerateX25519Identity`). `RI_g`'s identity goes to `ipcam-hub` as a 0600 file in the run directory, and `RK` goes only to the ENC-06 check. No identity ever reaches results, logs or the MediaMTX config (C11, C22).
  - **Redaction.** Published results contain no IPs, MACs, serials, SSIDs, emails, hostnames, URLs or credentials. Hosts are the pseudonyms `pc`, `pi`, `lab-linux`, `lab-windows`, `lab-qemu`.
  - **Real cameras.** c1 records simulated cameras only (C28). The report states that real-camera recording behaviour is not covered.
- **Inbound listeners (R08).** Every LAN listener lives in `reccontroller` and binds IPv4 only:
  - RTSP `:8554`;
  - link TCP `:7081`;
  - beacon UDP `:7082`;
  - app HTTP `:7080`;
  - MI3 upload sink HTTPS `:7083`;
  - Pion UDP.

  The host side only connects out: MediaMTX pulls with `rtspTransport: tcp`. `ipcam-hub`'s hook endpoint binds `127.0.0.1` only.

  **Exception:** recspike's own Pion peers bind UDP sockets on the host, for MI3's live sessions (Task 9) and the MI9 LAN run (Task 11). Those two loads are started by the owner, and the guide's firewall step covers recspike.exe's prompt as well as reccontroller's (Task 13). On the Pi there is no firewall prompt.
- **SPIKE-ONLY.** Every non-test `.go` file under `tools/spikes/recording/` starts with `// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md`; `cmd/mi9page/web/index.html` carries the same text in an HTML comment. So do the two video-spike additions: the `AGE` scenario `tools/spikes/video/app/src/scenarios/age.ts` and `tools/spikes/video/e2e/mi9.spec.ts`. No file under `hub/**` or `apps/packages/core/**` contains `SPIKE-ONLY`. Each rule has a test owner: Task 1 (`hub/**` and the recording `.go` files), Task 10a (`apps/packages/core/**`), Task 10 (`age.ts`) and Task 11 (`mi9.spec.ts`, `index.html`). The tests that look for the marker build the needle as `"SPIKE" + "-ONLY"`, so they never match their own source.
- **MediaMTX recording shape (§4.2, §4.5, C13, C14, O17).**
  - **Segments and parts.** fMP4 segments of **5 min** with **1 s parts**. A segment closes at the first keyframe after 300 s or on a codec or resolution change.
  - **Naming.** A segment is written as `<name>.mp4.part` (clear) or `<name>.mp4.age.part` (encrypted) and renamed after `Close` to `.mp4` or `.mp4.age`.
  - **Fsync.** The segment file at every part close. At segment close, in this order: the last writes (the clear `mvhd` duration, or the final age chunk with its `free` box), one more file fsync, `Close`, the rename, then the directory fsync. So a durable final name always points at durable final bytes. The directory is also synced once after the `.part` is created. On Windows the directory sync is a no-op and the rename uses `MoveFileEx(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)`.
  - **Encrypted segments.** Written strictly sequentially through `crypto.Writer`, never in clear (HUB-04), with `Flush` at every part fsync.
  - **Padding.** A part end is padded to the 64 KiB chunk boundary with an fMP4 `free` box when the padding is ≤ 10 % of the part. When the camera's `PadLowBitrate` setting is on (default), every part **smaller than 64 KiB** is padded, however large the padding; parts of at least 64 KiB follow the 10 % rule. The rule is per part (§4.5 as amended by O17, 8d7ff23): it also catches night VBR and smart codec, and it costs nothing on a large stream. In the spec, "stream below 512 kbps" means exactly parts smaller than 64 KiB (≈ 524 kbps at 1 s parts).
  - **Final chunk.** It is never empty.
- **Thresholds, as in §16.4 / §16.5, with O17 applied to MI1:**

  | Code | Threshold |
  |---|---|
  | MI1 (G10, HUB-30, FLW-15, SUC-05) | Per 20-run automated set and per 5-real-cut set: median loss ≤ 1 s, max ≤ 2 s; reconciliation without intervention; 0 unreadable files; 0 torn fMP4 parts; HUB-04 = 0.<br>**Encrypted streams < 512 kbps follow the per-camera O17 setting „Pierdere minimă la pană de curent”.** On (default): the same ≤ 2 s max. Off: ≤ `UnpaddedLimitMs` = 1 part + 64 KiB ÷ bitrate, rounded up to whole parts (§4.5; 4,000 ms at 256 kbps). The off case is judged on one extra 256 kbps run in every automated set (lab and owner devices), outside that set's median and max, and reported separately.<br>**Composition.** Half of the automated runs are encrypted. One run per 20-set is at 256 kbps, encrypted, with the setting on. Real cuts 1, 3 and 5 are encrypted.<br>**Statistics.** 0 failures in ≥ 40 automated runs per OS → < ≈ 7 %; 0 in 10 real → < ≈ 26 %. |
  | MI2 (G11, HUB-31) | 20 × `kill -9` of `ipcam-hub` + 72 h with the host's recommended camera count (8 on PC; the model's number on Pi): recording resumes ≤ 10 s; 0 `ipcam-hub` crashes in 72 h. On Pi with SD card, the 72 h run also exercises card surveillance (T40) |
  | MI3 (G12, HUB-32) | host's recommended cameras (§4.8): 8 on H3, the model's number on the owner's Pi; the tier's live sessions + 1 playback + 1 export + encrypted upload (10 Mbps; on H1b/H1c, the camera throughput) in parallel for 1 h: §4.8 budgets met and total CPU ≤ 60 %; on failure the recommended number drops, the model stays supported (minimum 1, O16) |
  | MI4 (G13, HUB-33, ENC-08) | 1000 random reads, **each** ≤ 50 ms on the test Pi (the maximum decides, C26); seek in a 5-min encrypted segment → first frame ≤ 1.5 s; encryption throughput ≥ 50 MB/s on Pi 5 and, on other models, ≥ 10 × recommended-camera throughput (≥ 20 MB/s Pi 4, ≥ 10 MB/s H1c); `agerange` ≥ 12 Mbps in the AND-1 WebView; identical bytes to `age` CLI and `rage` (ENC-06) |
  | MI9 (G18, CTR-12) | on a 4 Mbps recording: a bulk channel sustains ≥ 8 Mbps on LAN and ≥ 5 Mbps through TURN-TLS/443 at 50 ms RTT, and a timeline seek shows the first frame ≤ 1.5 s |

- **Plan B per gate (written into §2.2 on failure, PH0-31).**
  - **G10** → own recorder on gortsplib/mediacommon behind `Recorder`, retested before Faza 1. The O17-off runs are not part of G10's verdict and have no plan B. Their result is reported only, and a failure leads to a proposal in the Task 14 PR: a Technical Decision for the owner on §4.5's bound or note.
  - **G11** → reverse ingest (MediaMTX pulls the camera).
  - **G12** → the recommended number drops (min 1).
  - **G13 (G21):**
    - a seek → whole-segment decrypt into a ≤ 256 MiB RAM buffer, then 2-min encrypted segments;
    - a read > 50 ms or an age mismatch → RAM buffer;
    - throughput → the number drops;
    - `agerange` < 12 Mbps → F6 downloads and decrypts the whole segment on that platform.

    The format is unchanged.
  - **G18** → the hub plays the recording as a 1× WebRTC track in the same session, with seek via RPC.
- **Verdicts (G27, R39).** The strings are `trece`, `pică → <plan B>`, `fără verdict: lipsesc măsurători` and `amânat în Faza 0-bis: <motiv>`. A measured failure decides even when other parts are missing.
- **Results path (C32, G15).**
  - **Annotations.** Job logs, artifacts and logs are refused here (403), but check-run annotations are readable. Every lab shard, the mi9 job and the ENC-06 step write their results as compact JSON in notice annotations (`tools/scripts/annotate.sh`). recreport reads them with `gh api repos/AndreiBesliu/DevWorld/check-runs/<id>/annotations`.
  - **Annotation limits.** GitHub cut an earlier annotation in this repo after ≈ 4,000 characters (0e9ae3d), and it keeps at most 10 notices per step and 50 per job. So:
    - every notice line is ≤ **3,500 bytes** measured after annotate.sh's escaping (`%25`, `%0A`). That holds for the lab envelopes and for the ENC-06, `mtx2-loss`, `diag host` and `mi9` lines, and the test or command that prints each line asserts it;
    - lab results go out as one envelope per run (`recspike annotate`, Task 7);
    - a step emits at most 10 notices.
  - **Owner-device results.** They reach the PC's `results/` folder: the PC pulls the Pi's results over the link at the end of each session, and once more in an explicit final step (`reccontroller pull` + `recspike push`, Task 7) that lists anything missing.
- **Environment limits.**
  - Tag pushes are refused (403) and must not be routed around (C27, R55): the owner creates releases in the web UI.
  - This container has no KVM, no coturn and no `tc`. QEMU, coturn and netem run on runners only.
  - Google Maven is blocked here, so the APK is built on runners only.
  - GitHub-hosted jobs stop at 6 h (R03).
- **Faza 0-bis leftovers to list, not execute (§16.9, G31, G33).** H1 (Pi 5) and H2 (N100) measurements, whose numbers render as "estimare, nemăsurată". `agerange` on iOS-1 (MI4 part 3, iOS path). Every Pi measurement if the owner's Pi is armv6. The report then states that the Linux part of G10 rests on the lab until a supported Pi is measured, "înainte de Faza 4". The Linux parts of G11–G13 have no lab legs, so they read `amânat în Faza 0-bis: Pi armv6` (G31's wording narrowed to what is measured).

## Review Focus

1. **The owner performs a real power cut outside the window, or before the prompt.** Expected: a cut is invalid if its boot-to-stop time is outside [3 min, 12 min], or if it does not fall within [prompt, prompt + 2 min]. An invalid cut is excluded, the guide's flow redoes it in the same slot with the same encryption flag, and the 5 valid cuts per device are what counts. *Owners:* Task 6 `TestCut_OutsideWindow_MarkedInvalid`; Task 7 `TestMI1_InvalidRealCut_IsRedoneWithSameSlot`.
2. **The Pi has no RTC and NTP has not synced, or the clocks drift between the two devices.** Expected: loss comes from the controller's send log keyed by the SEI index, never from the host's clock, so a host-side skew leaves it unchanged. After each boot, recspike waits for NTP sync (at most 10 min) before starting a run, and records the wait. `time_uncertain` is a Faza 1 catalog item (REL-11), not in c1. *Owners:* Task 6 `TestLoss_UsesControllerSendLog_NotHostClock`; Task 7 `TestNTPWait_RecordsWaitAndGivesUpAfter10Min`.
3. **A cut leaves a torn final part** (an fMP4 `moof` with an incomplete `mdat`, or an `.age` file whose last chunk is truncated or missing). Expected:
   - reconcile keeps the whole samples of the last durable part (R01) and re-seals a non-empty final age chunk, so the file decrypts with the age CLI and rage;
   - the newest final-named segment of each stream gets the same check and repair, in case its last bytes did not reach the disk;
   - the lost tail becomes one `gap` row;
   - if more than 50 % of segments would read "lost", reconcile aborts with an alert (§4.5).

   *Owners:* Task 5 `TestReconcile_TornAgeTail_StaysDecryptable` and `TestReconcile_NewestFinalFileTorn_RepairedLikePart`.
4. **The SD card wears out or remounts read-only during the 72 h Pi run (T40).** Expected: the card monitor moves DEGRADED → FAILED with the §13.1 thresholds and raises the alert in ≤ 60 s. Recording then stops with a `card` gap, never with silent loss. *Owner:* Task 8 `TestSdCard_IoErrors_RaiseAlertThenStop`.
5. **The host is a laptop whose battery holds power, so pulling the plug does not cut it.** Expected: the guide instructs a forced shutdown (hold power ≥ 10 s), and the result records `shutdownKind = forced`. The controller still fixes the stop moment from the last beacon, so the loss is valid either way. *Owner:* Task 6 `TestStopMoment_IsLastBeacon_RegardlessOfShutdownKind`.

## File structure

- `ipcam/hub/` — production module:
  - `cmd/ipcam-hub` (run, hook, diag);
  - `internal/crypto` (age writer, reader, tail sealer);
  - `internal/catalog` (SQLite);
  - `internal/recorder` (Job, config generator, supervisor, hook receiver, reconcile);
  - `internal/health` (Pi model, SD card, card monitor);
  - `third_party/PATCHES.md`;
  - `third_party/mediamtx/` (nested fork module).
- `ipcam/apps/packages/core/` — `@ipcam/core` with `agerange/` (production TS).
- `ipcam/tools/spikes/recording/` — spike module:
  - `controller` (RTSP server, SEI, send log, beacon, cut and loss rules, pacing, MI3 upload sink, app endpoints);
  - `link` (host↔controller protocol);
  - `lastindex` (last recorded SEI index);
  - `results` (envelope, verdicts);
  - `hostinfo`;
  - `hubproc` (runs `ipcam-hub`);
  - `mi1`, `mi2`, `mi3`, `mi4`, `mi9` (drivers and `Judge`s);
  - `cmd/{recspike, reccontroller, recreport, recmediagen, recspike-probe, mi9page}`.
- `.github/workflows/` — `ipcam-spike-recording.yml` (CI), `ipcam-spike-recording-lab.yml` (MI1 lab), `ipcam-spike-recording-release.yml`.

---

### Task 1: `hub` module, MediaMTX fork module, PATCHES.md, recorder CI and media

Rulings: C02, C03, C30, C31, G01, G02, G03, G04, G35, R04, R05, R09.

**Files:**
- Modify: `ipcam/go.work` (add `./hub`, `./tools/spikes/recording`; the fork stays out)
- Create: `ipcam/hub/{go.mod,.golangci.yml,doc.go,spikeonly_test.go}`
- Create: `ipcam/tools/scripts/vendor-mediamtx.sh` (downloads the tagged module, copies the whole tree, rewrites the import path, removes rpiCamera, writes the `go:generate` outputs, prints the SHA-256s)
- Create: `ipcam/hub/third_party/mediamtx/**` (whole tree: `main.go`, `internal/**`, `LICENSE`, own `go.mod`/`go.sum`, committed `internal/servers/hls/hls.min.js` and `internal/core/VERSION`), `ipcam/hub/third_party/mediamtx/SOURCE.md`, `ipcam/hub/third_party/mediamtx/mtx0_test.go`
- Create: `ipcam/hub/third_party/PATCHES.md`
- Create: `ipcam/tools/spikes/recording/{go.mod,.golangci.yml,doc.go,markers_test.go}`
- Create: `ipcam/tools/spikes/recording/cmd/recmediagen/{main.go,recmediagen_test.go}`
- Create: `ipcam/tools/scripts/check-video-build-list.sh`, `ipcam/tools/scripts/test/check-video-build-list.test.sh`
- Modify: `ipcam/tools/scripts/check-licenses.sh` (option `-C <dir>`); Create: `ipcam/tools/scripts/test/check-licenses.test.sh`
- Create: `.github/workflows/ipcam-spike-recording.yml`
- Modify: `.github/workflows/ipcam-spikes.yml` (G03)

**Interfaces:**
- Produces:
  - **Modules.** The two workspace modules and the fork module named in Global Constraints.
    - **Fork `go.mod`.** It has `replace github.com/AndreiBesliu/DevWorld/ipcam/hub => ../..`. The matching `require github.com/AndreiBesliu/DevWorld/ipcam/hub v0.0.0` arrives in Task 4, with MTX-2's first import of `hub/internal/crypto`: `go mod tidy` drops a requirement that nothing imports yet, and keeps the `replace`.
    - **Recording `go.mod`.** It has `require github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video v0.0.0` and `replace … => ../video`.
  - **Fork build.** From `ipcam/hub/third_party/mediamtx`: `GOWORK=off go build -o <bin>/mediamtx .`.
  - **`PATCHES.md`.** One MediaMTX section with entries MTX-0…MTX-3. Each entry has: what, why, files touched, test, rebase status. MTX-0 is filled here: the module-path rewrite command, the rpiCamera removal (`internal/staticsources/rpicamera` and its registration in `internal/core`), and the committed `go:generate` outputs. MTX-1…3 are left empty for Tasks 3–4.
  - **`SOURCE.md`.** Upstream tag, commit, module-zip SHA-256, the SHA-256 of `hls.min.js`, the `VERSION` content, and the `vendor-mediamtx.sh` command line.
  - **`recmediagen -out DIR`.** It writes `sub-256k.h264` (H.264 Main@3.1, 640x360, 12 fps, GOP 12, 60 s loop of 720 frames, 256 kbps two-pass, band `Camera` 5) and a manifest entry `sub-256k`. Frames are rendered as in mediagen (gradient, noise, digits, `band.Paint`); the renderer is copied, because mediagen is package main.
  - **`check-video-build-list.sh`.** In `tools/spikes/video` it compares `go list -deps -test -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' ./...` with and without `GOWORK=off`. It prints `video build list unchanged (<n> modules)` and exits 0, or names each module whose version differs and exits 1.
  - **`check-licenses.sh -C <dir> [pattern…]`.** Runs go-licenses inside `<dir>` with `GOWORK=off`, using a binary built from `tools/go.mod`. Without `-C`, behaviour is unchanged.
  - **Environment for later tests:**
    - `MEDIAMTX_BIN` (path to the built fork); `RECSPIKE_REQUIRE_MEDIAMTX=1` makes a missing binary fail instead of skip;
    - `MEDIAGEN_DIR` + `SPIKE_REQUIRE_MEDIA=1` (R19 pattern);
    - `RECSPIKE_AGE_BINS` (colon-separated paths to every age CLI to test) and `RECSPIKE_RAGE_BIN`;
    - `RECSPIKE_REQUIRE_AGE_TOOLS=1` makes missing tools fail.
  - **CI jobs in `ipcam-spike-recording.yml`:** `media` and `recorder`. Later tasks add `windows`, `mi1-smoke` and `mi9`.

- [ ] **Step 1: Write the failing tests.**
  - `TestMTX0_NoUpstreamImportPath` (fork root): no `.go` file imports `github.com/bluenviron/mediamtx/`, and the `go.mod` module line is the rewritten path.
  - `TestMTX0_NoRpiCameraSource`: `internal/staticsources/rpicamera` does not exist, no file references it, and a config with `source: rpiCamera` is rejected.
  - `TestMTX0_GeneratedFilesMatchSourceMD`: the SHA-256 of `hls.min.js` and the content of `VERSION` equal what `SOURCE.md` records.
  - `TestNoSpikeOnlyMarker` (`hub/spikeonly_test.go`): no file under `hub/` contains the marker. The needle is built as `"SPIKE" + "-ONLY"`, so the test file itself does not match.
  - `TestEverySourceFileIsMarkedSpikeOnly` (`tools/spikes/recording/markers_test.go`): every non-test `.go` file there starts with the marker line.
  - `TestMarkedNonGoFiles` (same file): every path in its list starts with the marker in that file type's comment syntax (`//` or `<!-- … -->`). The list starts empty; Task 10 adds `../video/app/src/scenarios/age.ts`, and Task 11 adds `../video/e2e/mi9.spec.ts` and `cmd/mi9page/web/index.html`. A listed file that is missing fails the test.
  - `TestRecMediagen_Sub256k` (reads `MEDIAGEN_DIR`, needs ffmpeg; `MEDIAGEN_REQUIRE_FFMPEG=1` fails instead of skipping): the manifest entry is H.264 Main@3.1, 640x360, 12 fps, GOP 12, 720 frames, bitrate 256,000 ± 10 %, and `band.Decode` reads Camera 5 at every GOP start.
  - `check-video-build-list.test.sh`: a fake `go` printing equal lists → exit 0 and `video build list unchanged (2 modules)`; differing lists → exit 1 naming the module.
  - `check-licenses.test.sh`: with `-C dir`, the fake go-licenses runs in `dir` with `GOWORK=off`; without it, the default pattern `./tools/spikes/video/...` is unchanged.
- [ ] **Step 2: Run.** `cd ipcam && go test ./hub/... ./tools/spikes/recording/... && tools/scripts/test/check-video-build-list.test.sh && tools/scripts/test/check-licenses.test.sh` — Expected: FAIL (modules, scripts and fork are missing).
- [ ] **Step 3: Implement.**
  - **Workspace.** Create the modules and add hub and recording to `go.work`.
  - **Vendoring.** Run `tools/scripts/vendor-mediamtx.sh <version>`, with the version per C31, then `cd hub/third_party/mediamtx && GOWORK=off go mod tidy`.
  - **Lint configs.** They mirror the video spike (`version: "2"`; govet, staticcheck, errcheck, gosec, revive, unused, ineffassign); the fork is never linted.
  - **`ipcam-spike-recording.yml`:**
    - **Triggers.** `push` on `branches: ['**']` and `pull_request`. `paths`: `ipcam/hub/**`, `ipcam/tools/spikes/recording/**`, `ipcam/tools/spikes/video/**`, `ipcam/tools/go.mod`, `ipcam/tools/go.sum`, `ipcam/tools/scripts/**`, `ipcam/go.work`, `ipcam/go.work.sum` and the workflow file (G03).
    - **Settings.** `permissions: contents: read`; `concurrency` with cancel-in-progress; env `GOTOOLCHAIN: local`, `GOFLAGS: -mod=readonly`. Actions are pinned to the SHAs that `ipcam-spikes.yml` uses.
    - **`media` job.** Installs ffmpeg, runs `go run ./tools/spikes/video/cmd/mediagen -out $RUNNER_TEMP/media` and `go run ./tools/spikes/recording/cmd/recmediagen -out $RUNNER_TEMP/media`, runs `TestRecMediagen` with `MEDIAGEN_REQUIRE_FFMPEG=1`, and uploads the artifact `spike-recording-media`.
    - **`recorder` job** (`needs: media`). It downloads the media and installs age v1.0.0, the newest age release ≥ 14 days old (v1.3.2 at writing) and rage (newest ≥ 14 days, 0.12.1 at writing). They come from their GitHub release archives, verified against SHA-256 values that the implementer records in the workflow (G01).
      - **Where to look them up.** The GitHub API and release pages of third-party repos are refused here (403). Versions and dates come from proxy.golang.org for age, because the CLI ships in the `filippo.io/age` module (`/filippo.io/age/@v/list`, then `/@v/<version>.info` for `Time`). For rage they come from the crates.io API (`/api/v1/crates/rage`, which needs a `User-Agent` header; each version has `created_at`).
      - **SHA-256.** Computed from the archive's `https://github.com/<owner>/<repo>/releases/download/<tag>/<file>` URL, which is reachable.

      Then it runs:
      - `go build ./hub/... ./tools/spikes/recording/...`;
      - the fork build with `GOWORK=off`, for linux/amd64 into `$RUNNER_TEMP/bin/mediamtx` and for windows/amd64 (C02);
      - `go tool golangci-lint run ./hub/... ./tools/spikes/recording/...`;
      - `tools/scripts/check-video-build-list.sh` (R04);
      - `go test -count=1 -race ./hub/... ./tools/spikes/recording/...` with `MEDIAMTX_BIN`, `RECSPIKE_REQUIRE_MEDIAMTX=1`, `MEDIAGEN_DIR`, `SPIKE_REQUIRE_MEDIA=1`, `RECSPIKE_AGE_BINS`, `RECSPIKE_RAGE_BIN` and `RECSPIKE_REQUIRE_AGE_TOOLS=1`;
      - fork tests `GOWORK=off go test -count=1 -race ./internal/recorder/... && GOWORK=off go test -count=1 -race -run '^TestMTX' ./...`;
      - govulncheck in `ipcam/hub`, in `ipcam/tools/spikes/recording` and, for the fork, `cd ipcam/tools && GOWORK=off go tool govulncheck -C ../hub/third_party/mediamtx ./...`;
      - `tools/scripts/check-licenses.sh ./hub/... ./tools/spikes/recording/...` and `tools/scripts/check-licenses.sh -C hub/third_party/mediamtx ./...`.
    - **Step pattern (G04).** Every step uses `set -o pipefail` with `tee` into `$RUNNER_TEMP`, plus a `if: failure()` step that pipes the failure lines into `tools/scripts/annotate.sh`.
  - **`ipcam-spikes.yml` (G03).** Change `go build ./tools/...` to `go build ./tools ./tools/spikes/video/...`, and add `'!ipcam/tools/spikes/recording/**'` after the includes of both triggers. Its `go tool govulncheck ./... tool` step in `ipcam/tools` stays as it is. A `./...` pattern stops at nested module boundaries even in workspace mode: with `tools/spikes/video` already in `go.work`, `go list ./...` in `ipcam/tools` lists only `…/ipcam/tools`, so that step never scans the recording module.
  - **`PATCHES.md`** with MTX-0 filled.
- [ ] **Step 4: Verify.**
  ```bash
  cd ipcam && go build ./hub/... ./tools/spikes/recording/... \
    && (cd hub/third_party/mediamtx && GOWORK=off go build -o /dev/null . && GOWORK=off go test -run '^TestMTX0' .) \
    && go tool golangci-lint run ./hub/... ./tools/spikes/recording/... \
    && go test ./hub/... ./tools/spikes/recording/... \
    && tools/scripts/check-video-build-list.sh \
    && for t in tools/scripts/test/*.test.sh; do "$t" || exit 1; done \
    && go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 ../.github/workflows/ipcam-spike-recording.yml ../.github/workflows/ipcam-spikes.yml
  ```
  Expected: every command exits 0, and the build-list check prints `video build list unchanged (26 modules)`. Push the branch: `ipcam-spike-recording` is green, and annotations show no failure.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add hub module, MediaMTX fork module and recorder spike CI"` (PR note: the minimal `ipcam-spikes.yml` path and build edit, G03).

### Task 2: `crypto` — age writer with Flush, random-access reader, tail sealer

Rulings: C04, C11, C21, C29.

**Files:**
- Create: `ipcam/hub/internal/crypto/{keyscope.go,writer.go,recording.go,tail.go}` and tests `{writer_test.go,recording_test.go,tail_test.go,cctv_test.go,agecli_test.go}`
- Create: `ipcam/hub/internal/crypto/testdata/golden/` (`*.age` files plus `vectors.json`), `ipcam/hub/internal/crypto/testdata/cctv/` (the CCTV vector files used, the module's license file and a `SOURCE` file with the module version)
- Modify: `ipcam/hub/{go.mod,go.sum}` (`filippo.io/age v1.3.2`; test requirement `c2sp.org/CCTV/age v0.0.0-20260829155415-4448f2097b2d`, the version age v1.3.2 pins)
- Modify: `.github/workflows/ipcam-spike-recording.yml` (`recorder` job: an `ENC-06` step that runs `TestRecording_DecryptsWithAgeCLIAndRage -v` and turns its `enc06: ` line into a notice annotation, C32)

**Interfaces:**
- Produces (§4.4 with `ctx`, §10.2):

```go
package crypto
const ChunkSize = 64 << 10 // plaintext bytes per age STREAM chunk
type KeyScope struct{ RecordingIdentity, Recovery *age.X25519Recipient } // RI_g and RK (T20)
func ParseKeyScope(riG, rk string) (KeyScope, error)  // accepts exactly two "age1…" X25519 recipients
func (s KeyScope) Strings() [2]string                  // RI_g first, then RK
type Writer struct{ /* … */ }
func NewWriter(dst io.Writer, scope KeyScope) (*Writer, error) // writes header + 16-byte payload nonce at once
func (w *Writer) Write(p []byte) (int, error)          // seals a full chunk as non-final only when more bytes follow it
func (w *Writer) Flush() error                          // seals every complete chunk held as non-final; the remainder (< ChunkSize) stays in RAM
func (w *Writer) Buffered() int
func (w *Writer) Close() error                          // seals the final chunk; ErrEmptyFinalChunk if nothing is held and the plaintext is non-empty
var ErrEmptyFinalChunk error
type Recording interface {
    NewWriter(ctx context.Context, dst io.Writer, scope KeyScope) (io.WriteCloser, error) // returns *Writer
    OpenReaderAt(ctx context.Context, src io.ReaderAt, size int64) (io.ReaderAt, int64, error)
}
func NewRecording(ids ...age.Identity) Recording // OpenReaderAt = age.DecryptReaderAt with the file key cached (ExtractHeader/DecryptHeader + NewInjectedFileKeyIdentity)
type Patch struct{ Off int64; Data []byte }
type TornAge interface {
    io.ReaderAt                // plaintext of the authenticated complete chunks only, decrypted on demand into RAM
    DurableSize() int64
    Complete() bool            // a valid final chunk is present
    Reseal(ctx context.Context, keep int64, patches []Patch) (cipherSize int64, err error)
}
type TailSealer interface{ OpenTorn(ctx context.Context, f *os.File) (TornAge, error) }
func NewTailSealer(riG *age.X25519Identity) TailSealer // built in ipcam-hub's main; package recorder never holds a key (§4.3)
```

- **Header (C04).** It is built from public pieces: a random 16-byte file key, `Recipient.Wrap` stanzas in the age v1 text format, and an HMAC-SHA-256 under HKDF(file key, info `header`).
- **Payload.** STREAM ChaCha20-Poly1305 with key HKDF(file key, salt = nonce, info `payload`). The chunk nonce is an 11-byte big-endian counter plus the last-chunk flag. Chunk *i* starts at `len(header) + 16 + i·65552`.
- **`Reseal`** re-encrypts in RAM every chunk from the lowest patch offset (or the last kept chunk) up to `keep`, writes the last as the final chunk (non-empty), and truncates and syncs the file. It never writes a temporary file (G08).

- [ ] **Step 1: Write the failing tests.**
  - `TestRecording_RoundTrip`: 1 MiB + 17 bytes are written and read back whole, and the bytes are equal.
  - `TestRecording_RandomRangesMatchStream`: 10,000 random ranges read through `OpenReaderAt` equal the same ranges of an `age.Decrypt` stream (ENC-07).
  - `TestRecording_FlushSealsCompleteChunks`: after writing 3·65536 + 100 bytes and calling `Flush`, the destination size is `len(header) + 16 + 3·65552` and `Buffered() == 100`.
  - `TestRecording_FlushNeverSealsPartialChunk`: `Flush` with 100 bytes held writes nothing.
  - `TestWriter_CloseWithNothingBuffered_ReturnsErrEmptyFinalChunk`: writing 2·65536 bytes, then `Flush`, then `Close` returns `ErrEmptyFinalChunk`.
  - `TestRecording_ByteCompatibleWithAge`: every golden file decrypts identically with `age.Decrypt` and `age.DecryptReaderAt`.
  - `TestRecording_DecryptsWithAgeCLIAndRage` (ENC-06): every golden file and a resealed torn file decrypt with each binary in `RECSPIKE_AGE_BINS` and with `RECSPIKE_RAGE_BIN`, using only an `RK` identity file, to the same bytes. It prints `enc06: {"code":"ENC-06","tools":[…],"files":<n>,"pass":true}` and asserts that this line is ≤ 3,500 bytes after annotate.sh's escaping. It fails when the tools are missing and `RECSPIKE_REQUIRE_AGE_TOOLS=1`.
  - `TestRecording_HeaderHasExactlyRIgAndRK` (C21): the header holds exactly two `X25519` stanzas. `DecryptHeader` succeeds with the `RI_g` identity and with the `RK` identity, and fails with any third identity. `ParseKeyScope` rejects one recipient, three recipients, a non-X25519 recipient and an `AGE-SECRET-KEY-` string, with an error that does not echo the input.
  - `TestRecording_CCTVVectors` (C21): every vector of the pinned `c2sp.org/CCTV/age` module behaves through `OpenReaderAt` with its identity the way the vector expects: success with a matching payload hash, or an error. `filippo.io/age` v1.3.2's `testkit_test.go` shows how to iterate the vectors. With `-update-cctv` it copies the files used into `testdata/cctv/`; without it, it checks that the copies are byte-equal to the module's.
  - `TestWriter_GoldenVectors`: with `-update-golden`, it writes `testdata/golden/` using injected randomness. The plaintext sizes are 1, 65535, 65536 + 8, 65537, 3·65536 + 8 and 1 MiB + 17, with `Flush` at fixed offsets. The plaintext of each file is byte `i` = `(i·31 + (i>>8)) & 0xff`. `vectors.json` has per file `{file, fileKeyHex, plaintextSize, plaintextSha256, ranges:[{offset,length,sha256}] ×64}`, and the ranges include every chunk boundary ± 1. Without the flag, it checks that the files decrypt to those values.
  - `TestRecording_ResealTail_DecryptsWithAgeCLI`: a file with 5 flushed chunks plus 300 trailing bytes of a torn sixth chunk gives `DurableSize() == 5·65536` and `Complete() == false`. `Reseal(keep = 5·65536 − 1000)` makes `age.Decrypt` return exactly those bytes, and so do the age CLIs and rage when required.
  - `TestTornAge_ResealAtChunkBoundary_RewritesLastChunkAsFinal`: `keep = 4·65536` rewrites chunk 3 as the final chunk.
  - `TestTornAge_PatchIsApplied`: a patch 200 KiB before `keep` is present in the decrypted result.
  - `TestTornAge_WritesNoTempFile` (G08): with `TMPDIR` set to an empty directory, the directory is still empty after `OpenTorn` + `Reseal`, and only the target file's inode changed.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/crypto/...` — Expected: FAIL (`undefined: NewWriter`, …).
- [ ] **Step 3: Implement.** Implement the API above in `hub/internal/crypto`. Generate the goldens with `go test ./hub/internal/crypto -run 'TestWriter_GoldenVectors|TestRecording_CCTVVectors' -update-golden -update-cctv`, then rerun without the flags.
- [ ] **Step 4: Run.** `cd ipcam && go test -count=1 -race ./hub/internal/crypto/...` — Expected: PASS. In CI, the `ENC-06` step shows a notice `{"code":"ENC-06",…,"pass":true}` naming age 1.0.0, the newest age and rage.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add crypto age writer with flush, random-access reader and tail sealer"`

### Task 3: MTX-1 (`.part`, fsync, rename) and MTX-3 (segment-closed hook)

Rulings: C13, C14, G05 (fork side), C31 (re-vendor).

**Files:**
- Modify: `ipcam/hub/third_party/mediamtx/internal/recorder/format_fmp4_segment.go` and the part writer (`.part` creation, per-part `Sync`, rename after `Close`, directory sync)
- Create: `ipcam/hub/third_party/mediamtx/internal/recorder/{syncer.go,dirsync_other.go,dirsync_windows.go,rename_other.go,rename_windows.go,mtx1_test.go,mtx1_windows_test.go,mtx3_test.go}`
- Modify: `ipcam/hub/third_party/mediamtx/internal/core/path.go` (adds `MTX_SEGMENT_START`); Create: `ipcam/hub/third_party/mediamtx/internal/core/mtx3_test.go`
- Modify: `ipcam/hub/third_party/PATCHES.md` (MTX-1, MTX-3)
- Modify: `.github/workflows/ipcam-spike-recording.yml` (job `windows`: windows-latest, `shell: bash`, fork patch tests with `GOWORK=off`, annotations on failure)

**Interfaces:**
- Consumes: the fork module (Task 1).
- Produces:
  - **Seam.** A one-function `syncer` seam (`var syncFile = func(*os.File) error`, `var syncDir = func(dir string) error`, `var renameFinal = func(from, to string) error`) that the tests replace.
  - **Naming.** Segment files `<recordPath>.mp4.part` → `.mp4`; Task 4 adds `.mp4.age.part` → `.mp4.age`.
  - **Clear duration.** For clear segments, the duration is written into `mvhd` before the rename.
  - **Segment close order.** Last write (the `mvhd` duration; Task 4 adds the final age chunk), then `syncFile`, then `Close`, then `renameFinal`, then `syncDir`. Without that last `syncFile`, the directory sync could make the final name durable while its last bytes are still in the page cache (ext4 delalloc does not force allocation on a rename to a new name).
  - **Hook.** The hook `OnSegmentComplete` fires once per segment, **after** the rename and the directory sync, with the final path and start. The external command it runs (`runOnRecordSegmentComplete`) gets `MTX_PATH`, `MTX_SEGMENT_PATH` (final path), `MTX_SEGMENT_START` (Unix milliseconds, decimal) and `MTX_SEGMENT_DURATION` (upstream format), plus the parent environment.
  - **`PATCHES.md`.** MTX-1 says it **introduces** the `.part` naming, the rename and every fsync, because upstream has none.

- [ ] **Step 1: Re-vendor if due (C31).** If today is on or after 2026-10-22 and the fork is v1.21.1, run `tools/scripts/vendor-mediamtx.sh v1.21.2` and the MTX-0 tests first, and commit `ipcam: re-vendor MediaMTX v1.21.2`.
- [ ] **Step 2: Write the failing tests** (fork, internal package tests):
  - `TestMTX1_FsyncOnEachPartClose`: writing 3 parts and closing the segment gives 4 `syncFile` calls: one per part, each before the next part's first write, and one after the last write of the close (the `mvhd` duration) and before `Close`.
  - `TestMTX1_DirSyncAtCreateAndRenameOnly` (non-Windows): exactly 2 `syncDir` calls per segment, one after the `.part` is created and one after the rename.
  - `TestMTX1_RenameAfterCloseWithFinalName`: during writing only `<name>.mp4.part` exists; after close only `<name>.mp4` exists. The recorded order of the close is last write → `syncFile` → `Close` → `renameFinal` → `syncDir`.
  - `TestMTX1_ClearDurationWrittenBeforeRename`: `mvhd` of the final file has the segment duration.
  - `TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename` (`mtx1_windows_test.go`): `syncDir` returns nil without opening a handle, and the rename seam receives `MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH`.
  - `TestMTX3_HookFiresAfterRenameWithFinalPath`: the recorded order is rename → dir sync → hook, and the hook receives the final path and a start equal to the first sample's wall time ± 1 ms.
  - `TestMTX3_HookEnvironment` (`internal/core`): the command environment has `MTX_SEGMENT_PATH` = final path, `MTX_SEGMENT_START` = Unix ms, `MTX_SEGMENT_DURATION` and `MTX_PATH`, and keeps a variable set in the parent environment.
- [ ] **Step 3: Run.** `cd ipcam/hub/third_party/mediamtx && GOWORK=off go test -count=1 -run '^TestMTX' ./...` — Expected: FAIL.
- [ ] **Step 4: Implement** both patches, and record them in `PATCHES.md` with the exact files touched and the rebase note (re-apply on each adopted MediaMTX release).
- [ ] **Step 5: Run.** The same command — Expected: PASS. The CI `recorder` and `windows` jobs are green.
- [ ] **Step 6: Commit** — `git commit -m "ipcam: add MediaMTX MTX-1 part fsync and MTX-3 segment hook patches"`

### Task 4: MTX-2 (age at write) with the O17 low-bitrate padding setting

Rulings: C04, C13, C22, R01, O17 (C1-R1).

**Files:**
- Modify: `ipcam/hub/third_party/mediamtx/internal/conf/path.go` (path fields `recordAgeRecipients`, `recordAgePadLowBitrate`)
- Create: `ipcam/hub/third_party/mediamtx/internal/recorder/agewriter.go`; Modify: `format_fmp4_segment.go`, `format_fmp4.go` (encrypted path, no seek-back)
- Create: `ipcam/hub/third_party/mediamtx/internal/recorder/{mtx2_test.go,mtx2_loss_test.go}`, `ipcam/hub/third_party/mediamtx/internal/conf/mtx2_test.go`
- Modify: `ipcam/hub/third_party/mediamtx/{go.mod,go.sum}` (`GOWORK=off go mod tidy` after the first import of `hub/internal/crypto` adds `require github.com/AndreiBesliu/DevWorld/ipcam/hub v0.0.0`)
- Modify: `ipcam/hub/third_party/PATCHES.md` (MTX-2)
- Modify: `.github/workflows/ipcam-spike-recording.yml` (`recorder` job: the `mtx2-loss: ` line of `TestMTX2_WorstCaseLoss -v` becomes a notice annotation)

**Interfaces:**
- Consumes: `crypto.ParseKeyScope`, `crypto.NewWriter`, `(*Writer).Flush/Buffered/Close`, `crypto.ChunkSize` (Task 2); MTX-1 naming and seams (Task 3).
- Produces:
  - **`recordAgeRecipients: [age1…, age1…]`.** Exactly two X25519 recipients, parsed with `crypto.ParseKeyScope`; any other value is a config error that does not echo it. The fork's MTX-2 code receives only `crypto.KeyScope`, which holds public recipients by type.
  - **`recordAgePadLowBitrate: true|false`.** Default **true** when absent (O17-A).
  - **Encrypted segments.** They are `<name>.mp4.age.part`, written strictly sequentially through `crypto.Writer`, with `Flush` right after each part is written and before MTX-1's fsync. They are renamed to `.mp4.age`. `writeDuration`'s seek-back is removed for them (C13), so playback reads durations from the catalog.
  - **Small part (per part, §4.5 as amended by O17).** A part is small when its plaintext is shorter than `crypto.ChunkSize` (65,536 bytes). No per-stream state is kept.
  - **Padding rule per part.**
    - Let `p` = distance from the part end to the next 64 KiB plaintext boundary. If 0 < p < 8, add 65536.
    - If the setting is on and the part is small, write a `free` box of size `p`.
    - Otherwise, write it only if `p ≤ 10 %` of the part.
  - **Close.** If `Buffered() == 0`, MTX-2 first writes a minimal 8-byte `free` box, so the final chunk is never empty. `Writer.Close` (the final chunk) is followed by MTX-1's close order: `syncFile`, `Close`, `renameFinal`, `syncDir`.

- [ ] **Step 1: Write the failing tests.**
  - `TestMTX2_EncryptedSegmentHasNoClearFrames` (HUB-04): an encrypted segment of the test NAL pattern contains, across its file bytes, 0 hits for `ftyp`+brand, for `moof` followed by `mfhd` 4 bytes later, and for the first 16 bytes of each SPS, PPS and IDR NAL. It decrypts with `crypto.NewRecording(id).OpenReaderAt`.
  - `TestMTX2_PartBoundaryChunkAligned_WhenPaddingAtMost10Pct`: at 4 Mbps (≈ 500 KB parts) every padded part end is ≡ 0 mod 65536 in plaintext. A synthetic part needing > 10 % padding gets none.
  - `TestMTX2_LowBitratePadding_DefaultOn`: with the key absent, a 256 kbps stream has every part end on a chunk boundary.
  - `TestMTX2_LowBitratePadding_Off`: `recordAgePadLowBitrate: false` makes 256 kbps parts follow the 10 % rule only: a part is padded only when its padding is ≤ 10 % of the part.
  - `TestMTX2_LowBitrateDecidedPerPart`: with the setting on, a 63 KiB part is aligned and a 65 KiB part follows the 10 % rule (spec HUB-07's boundary test); in a 450 kbps ±30 % stream, exactly the parts under 64 KiB are aligned; a 4 Mbps stream follows only the 10 % rule. With the setting off, the 63 KiB part follows the 10 % rule.
  - `TestMTX2_FinalChunkSyncedBeforeRename`: the final age chunk (and the 8-byte `free` box when needed) is written before the segment's last `syncFile`, which comes before `Close` and `renameFinal`.
  - `TestMTX2_FlushAtEachPartFsync`: right after each part's fsync, the file size is `len(header) + 16 + n·65552` with `n = ⌊plaintext ÷ 65536⌋`.
  - `TestMTX2_NoSeekBack` (C13, HUB-30 "strict secvențial"): an append-only file wrapper fails on any `Seek`, `WriteAt` or `Truncate`; recording and closing an encrypted segment pass through it.
  - `TestMTX2_FinalChunkNeverEmpty`: a segment whose last part ends on a chunk boundary closes with an 8-byte `free` box and decrypts with `age.Decrypt`.
  - `TestMTX2_EncryptedNamingPartThenRename`: `.mp4.age.part` exists while writing and `.mp4.age` after close.
  - `TestMTX2_RejectsSecretKeyInRecipients` (C22, `internal/conf`): an `AGE-SECRET-KEY-1…` entry, one recipient, or three recipients each give an error that does not contain the input.
  - `TestMTX2_WorstCaseLoss` (R01, O17):
    - **Setup.** The real MTX-2 part writer runs on synthetic H.264-like samples (1 s parts, 20 fps, part sizes uniform within ±30 % of the nominal bitrate, 10 min). 10,000 uniformly random cut times are drawn.
    - **Loss.** At each cut, the durable plaintext is what was flushed at the last part fsync before it. Loss = cut − PTS of the last sample wholly inside that durable plaintext (R01's whole-sample salvage).
    - **Asserts.** Padding on: median ≤ 1000 ms and max ≤ 2000 ms at 4 Mbps, at 450 kbps and at 256 kbps; disk overhead ≤ 10 % at 4 Mbps. Padding off: max ≤ 4000 ms at 256 kbps (§4.5 as amended by O17: 1 part + 64 KiB ÷ bitrate, rounded up to whole parts).
    - **Output.** It prints `mtx2-loss: <json>` with median, max and overhead for 4 Mbps, 512 kbps, 450 kbps, 384 kbps and 256 kbps × on/off, and asserts that the line is ≤ 3,500 bytes after annotate.sh's escaping. The 512 kbps figures are information only.
- [ ] **Step 2: Run.** `cd ipcam/hub/third_party/mediamtx && GOWORK=off go test -count=1 -run '^TestMTX2' ./...` — Expected: FAIL.
- [ ] **Step 3: Implement.** Record MTX-2 in `PATCHES.md` with:
  - the 64 KiB alignment rule tied to §4.5;
  - the O17 setting, its default and the per-stream bitrate rule;
  - the removed seek-back;
  - the empty-final-chunk rule and the final `syncFile` before the rename.

  Run `GOWORK=off go mod tidy` in the fork once MTX-2 imports `hub/internal/crypto`.
- [ ] **Step 4: Run.** The same command — Expected: PASS. In CI, the `mtx2-loss` notice shows that at 256 kbps the padding-on max is ≈ 1 s and the padding-off max is ≤ 4.0 s.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MediaMTX MTX-2 age-at-write patch with per-camera low-bitrate padding"`

### Task 5: `ipcam-hub` run, recorder supervisor, catalog and crash-recovery reconcile

Rulings: C01, C07, C09, C10, C11, C12, C22, G05 (hub side), G06, G07, G08, R01, O17.

**Files:**
- Create: `ipcam/hub/internal/catalog/{catalog.go,catalog_test.go}`
- Create: `ipcam/hub/internal/recorder/{job.go,config.go,supervisor.go,proc_linux.go,proc_windows.go,hook.go,status.go,reconcile.go,fmp4tail.go,verify.go}` + tests, and `rtsptest_test.go` (test RTSP server over `MEDIAGEN_DIR/main.h264` that starts each session at the cached last IDR)
- Create: `ipcam/hub/cmd/ipcam-hub/{main.go,run.go,hook.go,events.go}` + tests
- Create: `ipcam/hub/third_party/mediamtx/internal/recorder/mtx2_const_test.go` (`TestMTX2_LowBitrateMatchesRecorder`) and `ipcam/hub/third_party/mediamtx/internal/conf/hubconfig_test.go` (`TestMTX0_AcceptsHubGeneratedConfig`)
- Modify: `ipcam/hub/go.mod`, `ipcam/hub/go.sum` (`modernc.org/sqlite`: the newest ≥ 14 days old whose requirements keep `check-video-build-list.sh` green; `golang.org/x/sys v0.48.0`; `github.com/google/uuid v1.6.0`; `github.com/bluenviron/mediacommon/v2 v2.9.6`; test-only `github.com/bluenviron/gortsplib/v5 v5.6.6`)
- Modify: `ipcam/hub/third_party/mediamtx/{go.mod,go.sum}` (`GOWORK=off go mod tidy`: the two fork tests import `hub/internal/recorder`, which pulls in `modernc.org/sqlite`)
- Modify: `.github/workflows/ipcam-spike-recording.yml`: the `windows` job gets `needs: media`, downloads the media and runs `go test ./hub/...` with a windows/amd64 fork build as `MEDIAMTX_BIN`, plus `RECSPIKE_REQUIRE_MEDIAMTX=1`, `MEDIAGEN_DIR` and `SPIKE_REQUIRE_MEDIA=1`, so the Windows process tests (`ResumesAfterKill`, the Job Object, `MoveFileEx`) cannot skip

**Interfaces:**
- Consumes: Task 2 (`KeyScope`, `TailSealer`, `TornAge`, `Patch`, `NewRecording`), Tasks 3–4 (the fork binary, hook environment, config keys).
- Produces:

```go
package catalog // modernc.org/sqlite, journal_mode=WAL, synchronous=FULL, forward-only migrations (§4.6)
type GapReason string // "restart" | "hub_stopped" | "disk_full" | "card" | "camera_offline"
type Segment struct{ ID, Cam, Profile, Path string; StartMs, EndMs, Bytes int64; Encrypted bool; State string /* writing|complete|lost */; VerifiedAtMs *int64 }
type Gap struct{ Cam, Profile string; StartMs, EndMs int64 /* 0 = open */; Reason GapReason }
func Open(ctx context.Context, path string) (*DB, error)
func (db *DB) PutSegment(ctx context.Context, s Segment) error
func (db *DB) PutGap(ctx context.Context, g Gap) error
func (db *DB) CloseGap(ctx context.Context, cam, profile string, endMs int64) error
func (db *DB) ReconcileFiles(ctx context.Context, files []FileFact) (ReconcileDiff, error) // files win (§4.1 (5))

package recorder
const SmallPartBytes = 65536 // §4.5 (O17): a "stream below 512 kbps" means parts smaller than this; equals crypto.ChunkSize
type CameraID string
type Profile string // "main" | "sub"
type Job struct {
    Cam CameraID; Profile Profile; RestreamURL string
    SegmentSecs, PartSecs int
    Scope *crypto.KeyScope // nil = clear
    PadLowBitrate bool     // per-camera setting O17 (cameras.min_power_loss): true pads every encrypted part smaller than 64 KiB to the 64 KiB chunk boundary
}
func NewJob(cam CameraID, p Profile, restreamURL string, scope *crypto.KeyScope) Job // SegmentSecs 300, PartSecs 1, PadLowBitrate true
type SegmentClosed struct{ Cam CameraID; Profile Profile; Path string; StartMs, EndMs int64; Encrypted bool }
type RecStatus struct{ LastPartAgoMs, LastSegmentAgoMs int64; Running bool }
type ReconcileReport struct{ Finalized, Truncated, LostTails, GapsWritten, Unreadable, TornParts, CatalogMismatches int; Aborted bool }
type Observer interface {
    ChildRestarted(at time.Time)
    ChildLog(line string)                             // every stderr line of the MediaMTX child (Task 8's card sampler reads it)
    FirstPart(cam CameraID, p Profile, at time.Time) // first growth of a new .part after a (re)start
    Verified(r ReconcileReport)                       // Unreadable, TornParts and CatalogMismatches of the background verification
}
type Config struct{ BinaryPath, RunDir, RecDir string; Tail crypto.TailSealer; Catalog *catalog.DB; Observer Observer }
type Recorder interface {
    Apply(ctx context.Context, jobs []Job) error
    Closed(ctx context.Context) <-chan SegmentClosed
    Status(ctx context.Context, cam CameraID, p Profile) RecStatus
    Stop(ctx context.Context, cam CameraID, p Profile, reason catalog.GapReason) error
}
func New(ctx context.Context, cfg Config) (Recorder, ReconcileReport, error) // reconcile .part files first, then start and supervise MediaMTX
func Reconcile(ctx context.Context, recDir string, tail crypto.TailSealer, cat *catalog.DB) (ReconcileReport, error)
func GenerateConfig(jobs []Job, hookCmd []string, recDir string) ([]byte, error)
```

- **Generated config (`<data>/state/run/mediamtx.yml`, 0600):**
  - **Servers off.** `rtsp`, `rtmp`, `hls`, `webrtc`, `srt`, `playback`, `api`, `metrics` and `pprof` are all `false`. The 127.0.0.1 API of §4.2 is not needed in Phase 0, because `Apply` works through config reload.
  - **Pull and record.** `rtspTransport: tcp`. One path per job, named `<cam>/<profile>` (MediaMTX's `IsValidPathName` allows `/`), with `source: <RestreamURL>`, `record: yes`, `recordFormat: fmp4`, `recordPartDuration: 1s`, `recordSegmentDuration: 300s` and `recordDeleteAfter: 0s`. MediaMTX's own cleaner would otherwise delete recordings after its 24 h default, and retention belongs to hubd.
  - **Record path.** `recordPath: <recDir>/%path/%Y-%m-%d/%H%M%S_%f`. MediaMTX v1.21.2 rejects a `recordPath` without `%path` (`internal/conf/path.go`: "'recordPath' must contain %path"), and `%path` expands to `<cam>/<profile>`, so the files land in `<recDir>/<cam>/<profile>/<date>/`. The date directory is in the host's local time in Phase 0, and the catalog assigns the UUIDv7 segment id.
  - **Hook.** `runOnRecordSegmentComplete: <ipcam-hub> hook segment-complete`.
  - **Encryption.** For a scoped job, `recordAgeRecipients` holds exactly the two strings from `Scope.Strings()`, and `recordAgePadLowBitrate` follows `Job.PadLowBitrate`.
- **Supervisor (C07, C09).**
  - **Child lifetime.** The MediaMTX child is bound to `ipcam-hub`'s lifetime: on Linux with `Setpgid` + `Pdeathsig: SIGKILL`; on Windows with a Job Object `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`.
  - **Restart.** On child exit, it restarts at once, then backs off 0.5 s, 1 s and 2 s (capped). The backoff resets after 60 s of uptime, so every restart is ≤ 3 s (HUB-13).
  - **`Apply`.** It only rewrites the config; MediaMTX reloads it.
  - **`Stop`.** It removes the path and writes an open gap with the reason. `Apply` re-adding the job closes that gap.
- **Hook (G05).**
  - **Endpoint.** `ipcam-hub` serves `POST /hook/segment-complete` on `127.0.0.1:<random>`.
  - **Environment.** It gives MediaMTX `IPCAM_HOOK_ADDR` and `IPCAM_HOOK_TOKEN`, both regenerated at every start.
  - **Hook command.** `ipcam-hub hook segment-complete` posts `{path, startMs, durationMs, cam, profile, encrypted}` with `Authorization: Bearer <token>`. Cam and profile are split at the `/` of `MTX_PATH`. `encrypted` = the path ends in `.age`.
  - **Results.** Each accepted hook gives one `SegmentClosed` and one catalog row.
- **`Status`.** `LastPartAgoMs` comes from polling the open `.part` file's size every 250 ms.
- **Reconcile (§4.5, R01, C12, G07, G08).**
  - **Startup (synchronous, before serving).** For every `.part` file:
    - **Clear.** Walk the boxes (`ftyp`, `moov`, `moof`/`mdat` pairs, `free`). Keep every complete part plus the whole samples of the last partially written part, rewriting that part's `moof` sample count and `mdat` size. Write the `mvhd` duration, then rename.
    - **`.age`.** `OpenTorn`, then the same walk over the durable plaintext, then `Reseal(keep, patches)`, then rename.
    - **Newest final file.** The newest final-named segment of each `<cam>/<profile>` is checked too. That is a `.mp4.age` whose `OpenTorn` gives `Complete() == false`, or a `.mp4` whose box walk ends in a torn part. Such a file is repaired exactly as a `.part` would be, and keeps its name. This covers a disk that acknowledged the close's fsync without keeping the data.
    - **Lost tails.** Each becomes a `restart` gap.
    - **Abort.** If more than 50 % of segments would be `lost`, nothing is changed and `Aborted: true` is returned (HUB-09).
    - **Catalog.** `ReconcileFiles` makes the rows match the files.
  - **Verification (in the background, after serving starts, so resume is never blocked).** The pass runs once after startup and again after each `SegmentClosed`. Every closed segment whose row has no `VerifiedAtMs` is fully parsed (clear) or decrypted through `OpenReaderAt` (`.age`). A failure counts in `Unreadable`, and a torn fMP4 part counts in `TornParts`. The pass then re-runs `ReconcileFiles` read-only and counts rows and files that still disagree in `CatalogMismatches`, which must be 0 (G07). The result is delivered through `Observer.Verified`.
- **`ipcam-hub run --config <run.json>`.**
  - **Config.** `run.json` = `{"dataDir", "mediamtx" (default: the sibling file mediamtx[.exe]), "identityFile" ("" or the 0600 RI_g file), "sdCardConfirmed": false, "jobs": [{"cam","profile","restreamUrl","segmentSecs","recipients":[…]|null,"padLowBitrate": true|false|absent→true}]}`.
  - **Data layout.** It uses `<dataDir>/state/catalog.db`, `<dataDir>/state/run/`, `<dataDir>/rec/` and `<dataDir>/tmp/` (emptied at start).
  - **Stdout.** One JSON object per line: `reconcile`, `ready`, `recording` (FirstPart), `segment_closed`, `gap`, `child_restart`, `verify`, `segment_verified` (`{path}`, one per segment that the background verification passed), `status` (every 1 s per job) and `error`. Every event has `"type"` and `"atMs"` (host wall clock, information only).
  - **Status file.** It writes `<dataDir>/state/run/status.json` every 1 s.
  - **Exit codes.** 0 on SIGTERM or Ctrl-C, 1 fatal, 3 reconcile aborted.

- [ ] **Step 1: Write the failing tests.** Process tests read `MEDIAMTX_BIN` and `MEDIAGEN_DIR`. They skip when these are unset and fail when `RECSPIKE_REQUIRE_MEDIAMTX=1` or `SPIKE_REQUIRE_MEDIA=1` (G06).
  - `TestCatalog_OpensWALSynchronousFull`: `PRAGMA journal_mode` = `wal` and `PRAGMA synchronous` = 2.
  - `TestCatalog_ReconcileFiles_FilesWin`: a row without a file → `lost`; a file without a row → row added.
  - `TestNewJob_Defaults`: 300 / 1 / `PadLowBitrate == true`.
  - `TestGenerateConfig_ServersOffAndPullOverTCP`: the server keys above are all false, `rtspTransport: tcp`, and the per-path keys are as listed. The path name is `<cam>/<profile>`, `recordPath` contains `%path`, and `recordDeleteAfter` is `0s`.
  - `TestMTX0_AcceptsHubGeneratedConfig` (fork, `internal/conf`): the configs that `recorder.GenerateConfig` writes for a clear job and for a scoped job with the setting on and off load through the fork's own config loader and validation without error. The loaded path config has the expected `recordPath`, `recordDeleteAfter`, recipients and `recordAgePadLowBitrate`. A YAML-only check cannot catch a key that MediaMTX rejects, so this test exists.
  - `TestGenerateConfig_ScopedJobHasTwoRecipientsNoSecret` (C11): exactly two `age1` strings and no `AGE-SECRET-KEY-`.
  - `TestGenerateConfig_PadLowBitrateFollowsJob`: true → `recordAgePadLowBitrate: true`; false → false.
  - `TestRecorder_NoSecretKeyInConfigOrChildEnv` (C22): a fake binary dumps its environment and config path; neither contains `AGE-SECRET-KEY-`, and the config file mode is 0600.
  - `TestRecorder_ResumesAfterKill` (C09, real binary, real time, 1 s parts): after the MediaMTX child is killed, the supervisor restarts it in ≤ 3 s, and a new part appears ≤ 4 s after the kill.
  - `TestRecorder_ChildDiesWithParent` (C07; Linux and Windows): kill a helper process that runs `New` → its MediaMTX child is gone within 1 s.
  - `TestHook_ForwardsSegmentClosed` (G05): the hook command with a valid token gives one `SegmentClosed{cam, profile, encrypted}` and one catalog row; a wrong token gets HTTP 401.
  - `TestStatus_LastPartAgoFromPartFileSize`.
  - `TestStop_WritesOpenGapClosedOnReapply`.
  - `TestReconcile_TornClearPart_TruncatesAndFinalizes`.
  - `TestReconcile_TornAgeTail_StaysDecryptable` (Review Focus 3): an `.age.part` with a truncated final chunk is resealed and renamed, decrypts with `age.Decrypt` (and with the age CLIs and rage when required), and its lost tail is one `restart` gap row.
  - `TestReconcile_PartialPart_KeepsWholeSamples` (R01): a last part with 13 of 20 samples durable keeps exactly those 13 samples, and the file has no torn part.
  - `TestReconcile_NewestFinalFileTorn_RepairedLikePart`: two final-named files, each the newest of its stream, are repaired, keep their names and verify clean. One is a `.mp4.age` whose final chunk is cut off, the other a clear `.mp4` with a torn last `mdat`. An older torn final file is left alone and counts in verification's `Unreadable`.
  - `TestReconcile_OverHalfLost_Aborts`: a `recDir` where 3 of 5 segments are unreadable gives `Aborted: true`, and no file or row changes.
  - `TestReconcile_ReportsUnreadableAndTornParts` (C12): verification of one corrupt `.age` and one torn clear file gives `Unreadable == 1` and `TornParts == 1` through `Observer.Verified`.
  - `TestReconcile_AgeTailOnlyInMemory` (G08): `tmp/` and `TMPDIR` are empty after reconcile.
  - `TestReconcile_CatalogMatchesFilesAfterwards` (G07): after a torn-tail reconcile, `Observer.Verified` reports `CatalogMismatches == 0`; deleting one file afterwards makes the next verification report 1.
  - `TestSupervisor_ForwardsChildStderr`: a fake child's stderr lines reach `Observer.ChildLog` in order.
  - `TestRun_EmitsReadyThenRecordingThenSegmentClosed` (with `segmentSecs: 10` in the test config), followed by a `segment_verified` event with the closed segment's path.
  - `TestRun_ExitCode3WhenReconcileAborts`.
  - `TestMTX2_SmallPartMatchesRecorder` (fork): the fork's small-part threshold equals `recorder.SmallPartBytes` and `crypto.ChunkSize`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/...` — Expected: FAIL.
- [ ] **Step 3: Implement** the API above.
- [ ] **Step 4: Run.**
  ```bash
  cd ipcam && MEDIAMTX_BIN=<built fork> MEDIAGEN_DIR=<media> go test -count=1 -race ./hub/... \
    && (cd hub/third_party/mediamtx && GOWORK=off go test -count=1 -run '^TestMTX2_LowBitrate|^TestMTX0_AcceptsHub' ./internal/...) \
    && tools/scripts/check-video-build-list.sh
  ```
  Expected: PASS, and `video build list unchanged`. CI's `recorder` and `windows` jobs are green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add ipcam-hub recorder supervisor, catalog and crash-recovery reconcile"`

### Task 6: T43 controller — RTSP server, SEI index, send log, beacon, cut and loss rules, link

Rulings: C05, C06, C08, C15, C16, G02 (media use), R07 (measurement), R08 (listeners).

**Files:**
- Create: `ipcam/tools/spikes/recording/controller/{rtspserver.go,sei.go,sendlog.go,beacon.go,cut.go,schedule.go,pacing.go,listen.go}` + tests
- Create: `ipcam/tools/spikes/recording/link/{link.go,messages.go}` + tests
- Create: `ipcam/tools/spikes/recording/lastindex/{lastindex.go,lastindex_test.go}`
- Create: `ipcam/tools/spikes/recording/cmd/reccontroller/main.go`
- Modify: `ipcam/tools/spikes/recording/{go.mod,go.sum}` (gortsplib v5.6.6, mediacommon v2.9.6, `filippo.io/age` v1.3.2)

**Interfaces:**
- Consumes: video `source.ReadAnnexB`, `source.NewSim`, `source.Clock`, `source.SentRecord`, `JitterSnapshot.P99Since`; the media `main.h264`, `sub.h264` and `sub-256k.h264` from `MEDIAGEN_DIR`.
- Produces:

```go
package controller // SPIKE-ONLY
const BeaconEvery = 50 * time.Millisecond
var SEIUUID = [16]byte{0x6b,0x1c,0x0e,0x52,0x9a,0x3f,0x4d,0x27,0x8e,0x15,0xc4,0x70,0x2b,0x9d,0x61,0xa8}
func StampIndex(nalus [][]byte, index uint64) [][]byte // prepends an SEI user_data_unregistered NAL (payload type 5: SEIUUID + 8-byte big-endian index, emulation prevention applied)
func ReadIndex(nalus [][]byte) (uint64, bool)
type Stream struct{ Path string /* "cam01_main" … "cam16_main", "cam01_sub" …, "cam99_main" (the 256 kbps camera) */; File string; FPS int; Camera uint8 }
type Credentials struct{ User, Pass string } // random per reccontroller start; never logged
type Server struct{ /* … */ }
func ServePrecoded(ctx context.Context, addr string, streams []Stream, creds Credentials, log *SendLog, clock source.Clock) (*Server, error)
type SendLog struct{ /* … */ }
func NewSendLog() *SendLog
func (l *SendLog) Record(stream string, index uint64, at time.Time)
func (l *SendLog) SendTime(stream string, index uint64) (time.Time, bool)
type Beacon struct{ Seq uint64; At time.Time } // At = controller clock at arrival
type ShutdownKind string // "kill9" | "sysrq" | "bugcheck" | "qemu-reset" | "power" | "forced"
type Cut struct{ RunID string; Kind ShutdownKind; BootAt, PromptAt, StopAt time.Time }
func StopMoment(kind ShutdownKind, beacons []Beacon) (at time.Time, errBound time.Duration) // last arrival; errBound ≤ 100 ms for a steady 50 ms stream
func Loss(stopAt time.Time, stream string, lastRecordedIndex uint64, log *SendLog) (time.Duration, bool) // StopAt − SendTime(index)
const AutoStopSlack = BeaconEvery + 100*time.Millisecond // 150 ms: the last beacon of an automated stop leaves up to one interval before the stop, plus StopMoment's bound
func Valid(c Cut) bool // ValidWithin(c, 3*time.Minute, 12*time.Minute)
// ValidWithin, by kind:
//   power, forced (the owner reacts to the prompt): minBoot ≤ StopAt−BootAt ≤ maxBoot AND PromptAt ≤ StopAt ≤ PromptAt+2 min
//   kill9, sysrq, bugcheck, qemu-reset (the stop follows `cut` within milliseconds): minBoot ≤ PromptAt−BootAt ≤ maxBoot AND PromptAt−AutoStopSlack ≤ StopAt ≤ PromptAt+2 min
func ValidWithin(c Cut, minBoot, maxBoot time.Duration) bool
func SchedulePrompt(bootAt time.Time, kind ShutdownKind, rnd *rand.Rand) time.Time // automated kinds: U[3,12] min; power/forced: U[3,10] min
func SchedulePromptWithin(bootAt time.Time, lo, hi time.Duration, rnd *rand.Rand) time.Time // used by --smoke: U[20,40] s
type PacingStats struct{ P99 time.Duration; SkippedAUs int } // R07: per run, from the sims' JitterSnapshot and failed RTSP writes

package lastindex // SPIKE-ONLY
// Before is the stream's segment files that existed when the host stopped. recspike lists them before it restarts ipcam-hub,
// which records new segments at once (ResumeMs, ColdBootMs); a .part name stands for the final name reconcile gives it.
type Before []string
// LastRecorded returns the SEI index of the last complete sample in the newest non-empty segment of files; .age files are decrypted with id (filippo.io/age).
func LastRecorded(ctx context.Context, files Before, id age.Identity) (uint64, error)

package link // SPIKE-ONLY: TCP JSON lines, controller listens on :7081
// Messages (field "type"):
//   hello{role,host,version}; session{rtspBase,user,pass,streams}; run_start{runId,set,kind,encrypted,padLowBitrate,streams};
//   ready{runId}; cut{runId,kind,promptAt}; report{runId,streams:[{stream,lastRecordedIndex}],reconcile,verify,hub04Clear,resumeMs,coldBootMs,ntpWaitMs,ntpSynced,writeCache};
//   results{name,data}; pull_results{}; results_tar{data}; bye{}
// Beacons: UDP :7082, 16 bytes = FNV-64a(runId) ‖ seq, big-endian.
```

- **Ownership of timestamps (C16).** The controller owns every timestamp. `BootAt` = the first beacon of a run. `PromptAt` = when the controller sends `cut`; for automated kinds it is the scheduled stop. `StopAt` = the last beacon.
- **Why automated stops get a slack.** recspike stops the host d + a few ms after `PromptAt`, and the last beacon left up to 50 ms before that stop. So on a LAN or loopback (d < 1 ms) the last beacon usually arrives *before* `PromptAt`. C16's literal `PromptAt ≤ StopAt` would make most kill9, sysrq, bugcheck and qemu-reset runs invalid and redone forever. Automated kinds therefore accept `StopAt ≥ PromptAt − AutoStopSlack`, and their boot window is checked on `PromptAt`, which the scheduler draws. Real cuts (`power`, `forced`) keep C16's strict rule, because the owner reacts seconds after the prompt.
- **Prompt.** For `power` and `forced`, the controller shows the Romanian prompt «Scoateți acum alimentarea» on its own screen. recspike mirrors it.
- **Listeners (R08).** `ListenAll(ctx, cfg)` opens every listener (`:8554`, `:7081`, `:7082`, `:7080`, `:7083`) with `tcp4` or `udp4` only.

- [ ] **Step 1: Write the failing tests.**
  - `TestCut_OutsideWindow_MarkedInvalid` (Review Focus 1, `power` and `forced`): StopAt−BootAt of 2 min or 13 min → invalid; StopAt 2 min 1 s after PromptAt → invalid; StopAt before PromptAt (no preceding prompt, even by 40 ms) → invalid; 7 min with StopAt 30 s after PromptAt → valid.
  - `TestCut_AutomatedStop_LastBeaconJustBeforePrompt`: for kill9, sysrq, bugcheck and qemu-reset, a last beacon 40 ms before PromptAt → valid; 2 s before → invalid; 2 min 1 s after → invalid; PromptAt−BootAt of 2 min 59 s → invalid. The same cases hold for `ValidWithin` with the smoke window (10 s, 60 s).
  - `TestSchedulePrompt_Ranges`: 10,000 draws per kind fall in [3, 12] min for kill9, sysrq, bugcheck and qemu-reset, and in [3, 10] min for power and forced.
  - `TestLoss_UsesControllerSendLog_NotHostClock` (Review Focus 2): two reports with the same `lastRecordedIndex` and host `atMs` values 5 s apart give the same `Loss`, which equals StopAt − SendTime(index).
  - `TestStopMoment_IsLastBeacon_RegardlessOfShutdownKind` (Review Focus 5): for each of the six kinds, the result is the last arrival with `errBound ≤ 100 ms`.
  - `TestSendLog_MapsIndexToSendTime`: index 719 of `cam01_main` maps to its recorded send time ± 1 ms.
  - `TestSEI_StampAndRead_RoundTrip`: indexes 0, 3, `0x0000000000000300` (needs emulation prevention) and `1<<63`.
  - `TestServePrecoded_StampsEveryAU` (media): a gortsplib client reads 100 AUs from `cam01_main`, the indexes increase by 1, and the send log has each.
  - `TestServePrecoded_NoCredentialInLogs`: with captured logs and errors, the server's own credentials never appear in them, including after a bad login.
  - `TestServePrecoded_NewSessionStartsAtCachedIDR`.
  - `TestLastIndex_ClearSegment`, `TestLastIndex_AgeSegment`, `TestLastIndex_StartsMidGOP` (the recording's first sample is the first IDR after a mid-GOP join, and the index is still exact), `TestLastIndex_SpansTwoSegments` (the newest segment is empty after reconcile → the previous segment's last sample).
  - `TestLastIndex_IgnoresSegmentsAfterRestart`: a segment that the restarted `ipcam-hub` created after the stop, absent from `Before`, never supplies the index; a `.part` listed in `Before` is read under its reconciled final name.
  - `TestLink_MessagesRoundTrip` and `TestLink_PullResultsTar` (G15: the far side's `results/` arrives byte-equal).
  - `TestPacing_P99AndSkipped`.
  - `TestListeners_BindIPv4Only`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/recording/controller/... ./tools/spikes/recording/lastindex/... ./tools/spikes/recording/link/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **RTSP.** gortsplib `Server` with a `ServerHandler`. Each path is fed by `source.NewSim` and keeps a GOP cache, as the video source does.
  - **Send log.** `index = Loop·frames + Frame` from `SentRecord`.
  - **`reccontroller` main.** It listens, shows prompts and keeps `results/` next to the binary. `--local` binds `127.0.0.1` for host-local use (G19).
  - **Text.** Every on-screen line is Romanian.
- [ ] **Step 4: Run.** The same command with `MEDIAGEN_DIR` set — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add T43 controller with RTSP server, SEI index, beacon and loss rules"`

### Task 7a: `ipcam-hub diag host`, Pi model and SD-card detection

Rulings: C23 (the `health` package; Pi model and SD detection here, card monitor in Task 8), C24, G16 (production gate), R07 (physical cores), G09 (write-cache state), T39 (production reads only `/proc/cpuinfo`). This task is new in v2. It runs before Task 7 because MI1 already records the recommended camera count and the storage kind.

**Files:**
- Create: `ipcam/hub/internal/health/{pimodel.go,sdcard.go,storage_linux.go,storage_windows.go,cores_linux.go,cores_windows.go}` + tests and `testdata/{cpuinfo-*,mountinfo-*}`
- Create: `ipcam/hub/cmd/ipcam-hub/{diag.go,diag_host.go,diag_pimodel.go}` + tests
- Modify: `ipcam/hub/cmd/ipcam-hub/run.go` (SD gate, HUB-17)
- Modify: `.github/workflows/ipcam-spike-recording.yml` (the `windows` job runs `go run ./hub/cmd/ipcam-hub diag host` and turns its output into a notice annotation, ≤ 3,500 bytes)

**Interfaces:**
- Produces:

```go
package health
type PiModel struct {
    Raw, Revision string
    Tier string // "H1" | "H1b" | "H1c" | "unsupported" | "unknown"
    Supported, Known bool
    RecommendedMin, RecommendedMax int
    Source string // "cpuinfo" (production, T39); the spike may set "device-tree"
}
func ParsePiModel(raw, revision string) PiModel       // the §4.8 table below, applied to a model string; reads no file
func ReadPiModel(cpuinfoPath string) (PiModel, error) // reads the Model and Revision lines, then ParsePiModel; never reads or returns Serial
func IsSdCard(mountinfoPath, recDir string) bool      // longest mount prefix of recDir; source /dev/mmcblk* or major 179
```

- **`ipcam-hub diag pimodel --raw "<model string>"`** prints `{"schema":"ipcam.diag.pimodel/1","piModel":{…}}` from `ParsePiModel` and reads no file. Production `ReadPiModel` stays cpuinfo-only (T39). The spike's device-tree fallback (G32, Task 7) passes the string from `/proc/device-tree/model` to this subcommand, so the table exists once and tools never import `health`.

- **`ipcam-hub diag host [--rec <dir>]`** prints one JSON object: `{"schema":"ipcam.diag.host/1","os","arch","physicalCores","logicalCpus","ramMb","piModel":{…}|null,"storage":"sd|ssd|hdd|unknown","writeCache":"on|off|unknown","recommendedCameras":<n>,"cores6Plus":bool}`.
  - **`recommendedCameras`.** H3 (Windows) → 8; a Pi → `RecommendedMax`; an unknown Pi → 0.
  - **`storage`.** Linux: `IsSdCard`, then `/sys/block/<dev>/queue/rotational`. Windows: `IOCTL_STORAGE_QUERY_PROPERTY` (seek penalty).
  - **`writeCache`.** Linux: `/sys/block/<dev>/queue/write_cache`. Windows: `IOCTL_DISK_GET_CACHE_INFORMATION`. `unknown` on any error.
- **SD gate.** `ipcam-hub run` refuses to write any segment while `rec/` is on an SD card and `sdCardConfirmed` is false. It emits `{"type":"sd_card_unconfirmed"}` and exits 4.
- **§4.8 table, in match order:**
  1. Pi 5, Pi 500 and CM5 → H1 8..8.
  2. Pi 4, Pi 400 and CM4 → H1b 4..4.
  3. "Zero 2 W" → H1c 1..2; it is matched before "Zero".
  4. Pi 3, 3 Model B Plus, 3 Model A Plus, Pi 2 Model B (Rev 1.1 and 1.2), CM3 and CM3 Plus → H1c 1..2. "Compute Module 3 Plus" is matched before "Compute Module 3".
  5. "Raspberry Pi Model …" (Pi 1), Zero, Zero W and "Compute Module Rev" (CM1) → unsupported.
  6. Anything else → unknown, `Known: false`, no number.

- [ ] **Step 1: Write the failing tests.**
  - `TestReadPiModel_EverySpecRow`, a table with these strings:
    - "Raspberry Pi 5 Model B Rev 1.0", "Raspberry Pi 500 Rev 1.0" and "Raspberry Pi Compute Module 5 Rev 1.0" → H1, 8..8;
    - "Raspberry Pi 4 Model B Rev 1.5", "Raspberry Pi 400 Rev 1.1" and "Raspberry Pi Compute Module 4 Rev 1.1" → H1b, 4..4;
    - "Raspberry Pi 3 Model B Rev 1.2", "Raspberry Pi 3 Model B Plus Rev 1.3", "Raspberry Pi 3 Model A Plus Rev 1.1", "Raspberry Pi Zero 2 W Rev 1.0", "Raspberry Pi 2 Model B Rev 1.1", "Raspberry Pi 2 Model B Rev 1.2", "Raspberry Pi Compute Module 3 Rev 1.0" and "Raspberry Pi Compute Module 3 Plus Rev 1.0" → H1c, 1..2;
    - "Raspberry Pi Model B Rev 2", "Raspberry Pi Zero Rev 1.3", "Raspberry Pi Zero W Rev 1.1" and "Raspberry Pi Compute Module Rev 1.0" → unsupported;
    - "Banana Pi M5" → unknown, `Known: false`.
  - `TestReadPiModel_NeverReadsSerial`: the fixture's `Serial` value appears nowhere in the marshalled result.
  - `TestReadPiModel_NoModelLine_Unknown`: `Raw == ""`, `Source == "cpuinfo"`.
  - `TestIsSdCard_DetectsMmcblk`: `mmcblk0p2` with major 179 → true; `nvme0n1p2` with major 259 → false; a `recDir` under a bind mount follows the longest prefix.
  - `TestDiagPimodel_RawUsesSameTableAndReadsNoFile`: every string of `TestReadPiModel_EverySpecRow` gives the same `Tier` and numbers through `diag pimodel --raw`, with the file-reading seams failing on any call.
  - `TestDiagHost_JSONShape`: with injected fixture roots, the output has the fields above, and the line is ≤ 3,500 bytes after annotate.sh's escaping.
  - `TestDiagHost_RecommendedForPC`: a 6-core Windows fixture → `recommendedCameras: 8`, `cores6Plus: true`.
  - `TestDiagHost_UnknownPiHasNoNumber`: `recommendedCameras: 0`.
  - `TestRun_RefusesSdCardUntilConfirmed` (HUB-17): with an SD fixture, `rec/` holds 0 files, the event is emitted, the exit code is 4, and with `sdCardConfirmed: true` recording starts.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/health/... ./hub/cmd/ipcam-hub/...` — Expected: FAIL.
- [ ] **Step 3: Implement** the functions and the subcommand.
- [ ] **Step 4: Run** — Expected: PASS. On the Windows CI job, `go run ./hub/cmd/ipcam-hub diag host` prints valid JSON with `"os":"windows"`.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add ipcam-hub diag host with Pi model and SD-card detection"`

### Task 7: MI1 power-cut spike — sets, judging, autostart, lab workflow (G10)

Rulings: C16, C17, C32, G09, G10, G11, G12, G13, G14, G15, G16 (owner confirmation), G30, G32 (device-tree fallback), R03, R07, R08, O17.

**Files:**
- Create: `ipcam/tools/spikes/recording/results/{envelope.go,verdict.go,write.go,annotations.go,inventory.go}` + tests
- Create: `ipcam/tools/spikes/recording/hostinfo/{hostinfo.go,devicetree_linux.go}` + tests (reads `ipcam-hub diag host`; G32 fallback through `ipcam-hub diag pimodel --raw`; C24 owner prompt for an unknown model)
- Create: `ipcam/tools/spikes/recording/hubproc/{hubproc.go,kill_linux.go,kill_windows.go}` + tests (writes `run.json`, starts, kills and restarts `ipcam-hub`, reads events, measures resume)
- Create: `ipcam/tools/spikes/recording/mi1/{judge.go,sets.go,host.go,session.go,lab.go,labimage_linux.go,qemu.go,stops_linux.go,stops_windows.go,hub04.go,runstate.go,lock_linux.go,lock_windows.go,window.go,preflight_linux.go,preflight_windows.go,autostart_linux.go,autostart_windows.go,ntpwait.go,uptime_linux.go,uptime_windows.go,keepawake_windows.go,keepawake_other.go,sdconfirm.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/recspike/{main.go,mi1.go,install.go,annotate.go,push.go}`; Modify: `ipcam/tools/spikes/recording/cmd/reccontroller/main.go` (MI1 sessions write results; `pull` subcommand)
- Modify: `ipcam/tools/spikes/recording/{go.mod,go.sum}` (`golang.org/x/sys` at hub's version, for the Windows calls)
- Create: `.github/workflows/ipcam-spike-recording-lab.yml`; Modify: `.github/workflows/ipcam-spike-recording.yml` (job `mi1-smoke`)

**Interfaces:**
- Consumes: Task 5 (`ipcam-hub run`, its events, exit codes), Task 7a (`diag host`), Task 6 (controller, link, `lastindex`, `Valid`, `Loss`, `StopMoment`, `PacingStats`).
- Produces:

```go
package results // SPIKE-ONLY
const Schema = "ipcam.phase0c1.result/1"
type Envelope struct {
    Schema, Code, RunID, Set string // Code: "MI1".."MI4", "MI9", "ENC-06"; Set: "lab-linux-kill9", "lab-linux-qemu-reset", "lab-windows-kill9", "pc-kill9", "pc-bugcheck", "pi-kill9", "pi-sysrq", "real-pc", "real-pi", …
    Host string // "pc" | "pi" | "lab-linux" | "lab-windows" | "lab-qemu"
    OS string; PiModel *PiModelInfo; Storage string /* "ssd"|"sd"|"hdd"|"unknown" */
    SdConfirmation *SdConfirmation // G16: the typed text and time, the audit stand-in
    Part, Parts int                // lab annotations: this run's 1-based position in its set and the set's run count (21); 0 elsewhere
    Runs []Run
    // Tasks 8–11 each add one optional field with its type: MI2 *Mi2Data, MI3 *Mi3Data, MI4 *Mi4Data, MI9 *Mi9Data.
}
type PiModelInfo struct{ Raw, Revision, Tier, Source string; Supported, Known, Estimate bool; RecommendedMin, RecommendedMax int } // mirrors diag host's piModel (tools cannot import health); Estimate = the owner typed the number (C24)
type SdConfirmation struct{ Text string; AtMs int64 }
type Run struct {
    RunID string; Valid bool; InvalidReason string; Slot int /* real cuts 1..5 */; Attempt int
    ShutdownKind string; Encrypted, PadLowBitrate bool
    Streams []StreamLoss; LossMs, ErrBoundMs int64
    Reconciled bool; Unreadable, TornParts int; Hub04Clear bool
    PacingP99Ms int64; SkippedAUs int; ResumeMs *int64; Cameras int
    ColdBootMs *int64 // sysrq and bugcheck runs: host uptime at the last job's `recording` event after the reboot (§4.8 cold-boot row)
    NtpWaitMs int64; NtpSynced bool; WriteCache string
}
type StreamLoss struct{ Stream string; BitrateKbps int; Encrypted, PadLowBitrate bool; LossMs, LimitMs int64 } // LimitMs: information only; Judge recomputes it
type Notice struct{ Title, Message string }
func ReadAnnotations(r io.Reader) ([]Envelope, []Notice, error) // parses `gh api …/check-runs/<id>/annotations` JSON: notices that are envelopes, and every other notice by title
func Inventory(dir string, piSupported bool) (present, missing []string) // G15: the codes and sets found under dir, and the expected ones absent
type Verdict string
const (Pass Verdict = "trece"; Fail Verdict = "pică"; Incomplete Verdict = "fără verdict: lipsesc măsurători")
func Deferred(reason string) Verdict // "amânat în Faza 0-bis: <reason>"
type Row struct{ Code, Value, Limit, Source string; Pass *bool }
type Judgement struct{ Gate string; Verdict Verdict; PlanB string; Reasons []string; Rows []Row }
func Write(dir string, e Envelope) error // results/<code>/<set>/<runId>.json

package mi1 // SPIKE-ONLY
const GeneralMedianMs, GeneralMaxMs = 1000, 2000
const PartMs = 1000
func UnpaddedLimitMs(bitrateBps int) int64 // §4.5 as amended by O17: PartMs·(1 + ⌈65536·8·1000 ÷ (bitrateBps·PartMs)⌉) = 4000 at 256 kbps
func StreamLimitMs(bitrateBps int, encrypted, padLowBitrate bool) int64 // UnpaddedLimitMs only for encrypted && !padLowBitrate && bitrateBps·PartMs ÷ 8000 < 65536 (parts under 64 KiB, ≈ 524 kbps); else 2000
func ConfidenceUpperBound(failures, n int) float64 // 0 failures: 1 − 0.05^(1/n)
func Judge(envs []results.Envelope) results.Judgement // G10; recomputes every stream's limit with StreamLimitMs(BitrateKbps·1000, Encrypted, PadLowBitrate) and never trusts the stored LimitMs
```

- **Sets (G10, G12, O17).** In a run, a stream is "encrypted" when its job has a scope. Every automated set, lab or owner, has 20 default runs plus **one O17-off run**: encrypted `cam01_main` plus `cam99_main` (256 kbps) with `PadLowBitrate: false` (spec MI1: «Fiecare set automat are și o rulare în plus»; HUB-07).
  - **Lab sets** (`lab-linux-kill9`, `lab-linux-qemu-reset`, `lab-windows-kill9`): of the 20 default runs, 10 are encrypted. Exactly one of those 10 also records `cam99_main` with `PadLowBitrate: true`. Every lab run records `cam01_main` (4 Mbps).
  - **Owner sets** (`pc-kill9`, `pc-bugcheck`, `pi-kill9`, `pi-sysrq`): of the 20 default runs, 10 are encrypted and one is a 256 kbps encrypted run with the setting on. Default kill9 runs record `recommendedCameras` main streams, so that MI2 reuses their 20 resumes (G13); the O17-off run's resume is information only. Other owner runs record `cam01_main`.
  - **Real sets** (`real-pc`, `real-pi`): 5 valid cuts in slots 1–5 with `cam01_main`. Slots 1, 3 and 5 are encrypted. A redo keeps its slot, and there are at most 8 attempts.
- **Run failure (G11).** A run fails when any of these holds:
  - any stream loss > its `StreamLimitMs`;
  - `!Reconciled`. `Reconciled` = the `reconcile` event has `aborted: false`, the `verify` event has `catalogMismatches: 0`, and nothing outside `ipcam-hub` touched the files (G07, C12);
  - `Unreadable > 0`;
  - `TornParts > 0`;
  - `!Hub04Clear`.

  A run's `LossMs` is the maximum over its streams (G10). Invalid runs (`controller.Valid` false, `PacingP99Ms > 100` or `SkippedAUs > 0`, R07) are excluded and redone, never counted.
- **Set and gate verdict.**
  - **Pass.** A set passes when its counted default runs have median ≤ 1000 ms, max ≤ 2000 ms and 0 failed runs.
  - **O17-off runs.** Each is judged alone against `StreamLimitMs` and reported in its own row per set. They are outside the median and the max and outside G10's verdict, and a failure triggers no plan B. Task 14 reports it and proposes any change to §4.5's bound or note as a Technical Decision for the owner.
  - **Statistics.** They count valid automated default runs per OS: Linux = lab kill9 + lab qemu + pi kill9 + pi sysrq; Windows = lab kill9 + pc kill9 + pc bugcheck.
  - **Gate.** G10 = `trece` only if every set passes. A missing Pi set gives `Deferred("Pi armv6")` when the model is unsupported, else `Incomplete`.
- **Timing (G09, C16).** `PromptAt` comes from `controller.SchedulePrompt`.
  - **Kill9.** recspike starts beaconing on `ipcam-hub`'s `ready`. At `cut` it sends SIGKILL (Linux) or calls `TerminateProcess` (Windows), and it stops beaconing when the child exits. It lists each stream's segment files (`lastindex.Before`), restarts the child at once and records `ResumeMs` = monotonic time from the kill to the last job's `recording` event (C07, G13).
  - **sysrq, bugcheck.** The OS stop ends the beacon. sysrq is `echo b > /proc/sysrq-trigger`; bugcheck is `notmyfault64.exe /accepteula /crash` next to recspike.
  - **After a reboot.** `recspike resume` lists each stream's segment files (`lastindex.Before`) and starts `ipcam-hub` at once on the previous run's `run.json`, so it reconciles and then records again. On sysrq and bugcheck runs it records `ColdBootMs` = host uptime (Linux `/proc/uptime`, Windows `GetTickCount64`) when the last job's `recording` event arrives. That value is boot to recording on the host's own boot clock, which NTP cannot shift; Task 9 judges it against §4.8's cold-boot row.
  - **Then, after either kind.** recspike waits for NTP sync for at most 10 min (`timedatectl show -p NTPSynchronized --value` = `yes`; `w32tm /query /status` with a non-local source), recording `NtpWaitMs` and `NtpSynced`. It waits for `ipcam-hub`'s `reconcile` and `verify` events. It reads `lastindex.LastRecorded(Before)` per stream with the run's ephemeral identity and runs the HUB-04 scan. It then stops `ipcam-hub` and sends `report`, and the controller computes the loss and writes the envelope (C16). The next run starts only after that.
- **HUB-04 scan (G08).**
  - **Scope.** For every encrypted stream's files, `rec/`, `tmp/` and the run directory.
  - **Needles.** `ftyp` plus brand, `moof`…`mfhd` and the first 16 bytes of each SPS, PPS and IDR NAL of the test media.
  - **Linux lab extra (kill9 shards).** Every **encrypted** run gets a fresh image. recspike creates a new zero-filled file (`truncate -s 1G`), runs `mkfs.ext4` on it, attaches it with `sudo losetup` and mounts it as that run's `dataDir`. It scans only that device, raw, after recording and again after reconcile, then unmounts, detaches and deletes it. Clear runs use a plain directory. A reused image would still hold the clear runs' `ftyp`/`moof` and NAL bytes, in live files and in freed blocks, and the harness would fail HUB-04 by itself.
- **Autostart (G14).**
  - **`recspike install` (PC).** It runs elevated (one UAC prompt) and registers a Task Scheduler task "ipcam-recspike" from generated XML: `BootTrigger`, `LogonType S4U`, `RunLevel HighestAvailable`, `recspike resume`, working directory = recspike's folder. It also adds a desktop shortcut «Proba înregistrare – tăieri reale» that runs `recspike mi1 --real --resume`.
  - **`sudo ./recspike install` (Pi).** It writes `/etc/systemd/system/ipcam-recspike.service` with `User=root`, `After=network-online.target time-sync.target`, `ExecStart=<abs>/recspike resume` and `WantedBy=multi-user.target`, and enables it.
  - **`uninstall`.** It removes both, plus the shortcut.
- **Resume.**
  - **Run state.** `results/state/run-state.json` holds `{set, mode, kind, nextRun, slot, attempts, window}`.
  - **`recspike resume`.** It always finishes the previous run's reconcile and report, then continues automated sets. For real sets it continues on Linux (the prompt shows on the PC controller), and on Windows it exits so the owner continues with the shortcut, which shows the prompt.
  - **Single instance.** Every recspike command that uses the run state holds an exclusive lock on `results/state/run-state.lock` (`flock` on Linux, `LockFileEx` on Windows). After a PC cut, the boot task may still be reconciling, or waiting up to 10 min for NTP, when the owner starts the shortcut. The shortcut then shows «Proba termină verificarea de după repornire. Așteptați…» and continues once the lock is free. So two `ipcam-hub` instances never share a `dataDir` and catalog.
- **Preflight (G14, R08).** recspike refuses to start a window with a Romanian reason when any check fails.
  - **PC.** `notmyfault64.exe` is present with a valid Authenticode signature by Microsoft Corporation (WinVerifyTrust). The session is elevated. `HKLM\SYSTEM\CurrentControlSet\Control\CrashControl\AutoReboot` = 1. Windows Update is paused (`HKLM\SOFTWARE\Microsoft\WindowsUpdate\UX\Settings\PauseUpdatesExpiryTime` is in the future).
  - **Pi.** Running as root, and free space ≥ (cameras × 4 Mbps × 12 min) + 2 GiB.
  - **Both.** The controller link works in both directions: TCP, the beacon acknowledged over TCP, and an RTSP pull. NTP is synced.
- **Window.** `--window HH:MM-HH:MM` (local time). Automated owner-device stops (kill9 included) never start outside it; real cuts ignore it.
- **SD confirmation (G16).** When `diag host` says `storage: sd`, recspike shows T40's text verbatim ("Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB.") and asks «Scrieți DA și apăsați Enter». It stores the confirmation in `SdConfirmation` and sets `sdCardConfirmed`.
- **Unknown Pi (C24).** The model reads "unknown" when `diag host` reports no `Model` line. recspike then reads `/proc/device-tree/model`, maps it with `ipcam-hub diag pimodel --raw "<string>"` (Task 7a, the same §4.8 table) and sets `Source: "device-tree"` (G32). When the model stays unknown, recspike asks the owner for a camera count, defaults to 1 and marks it as an estimate. Every envelope embeds `PiModel`.
- **Results transport (G15).**
  - **During sessions.** Every recspike driver writes its envelope into its own `results/` and also sends it to the controller as `results{name,data}` (MI1 envelopes are written by the controller itself). At the end of every session the PC side (recspike or reccontroller, whichever runs on the PC) sends `pull_results` and stores the Pi's folder under `results/pi/`. Covered by `TestLink_PullResultsTar` (Task 6) and `TestSession_PCPullsPiResultsAtEnd`.
  - **Final collection (guide step 13).** Nothing guarantees that a PC↔Pi session ends after the Pi's MI2 (host-local controller) and MI4 (standalone) runs, so there is an explicit last step. On the PC, `reccontroller pull` listens on `:7081` as always (R08). On the Pi, `./recspike push --controller <PC address>` connects out and answers `pull_results`. The PC stores the Pi's folder under `results/pi/` and prints `results.Inventory`: each code and set present on either device, and in Romanian each expected one that is missing («Lipsesc: …»). The expected list is the six MI1 sets, MI2, MI3 and MI9 `lan` per device, MI4 on the Pi, and the app's AGE result. With an unsupported Pi, nothing is expected with the Pi as host.
- **CLI:**
  - `recspike install|uninstall|resume`;
  - `recspike mi1 --controller <addr> --window <from-to> --kind kill9|sysrq|bugcheck`;
  - `recspike mi1 --controller <addr> --real [--resume]`;
  - `recspike mi1 --lab --kind kill9|qemu-reset --shard 1|2 [--smoke]`;
  - `recspike push --controller <addr>` and `reccontroller pull` (final collection, above);
  - `recspike annotate split <envelope.json>… --first <part> --out <dir>`: one notice line per run, each a one-run envelope with `Part` (its position in the set, starting at `--first`) and `Parts` (21). Each line is checked to be ≤ 3,500 bytes after annotate.sh's escaping, and `split` fails when a line is longer or a shard has more than 20 runs;
  - `recspike annotate emit <dir> --batch <k>`: prints lines 10(k−1)+1 … 10k, so a step never exceeds GitHub's 10 notices;
  - `recspike annotate read <dir|file>`: parses saved `gh api …/annotations` JSON with `results.ReadAnnotations` and prints `<set>: <n>/<parts>` per set. It exits 1 on a parse error, a duplicate part or a part outside 1..Parts.
- **Lab workflow `ipcam-spike-recording-lab.yml` (R03).**
  - **Triggers.** `workflow_dispatch`, plus `push` (branches `['**']`, the recorder paths of Task 1). Its jobs run only on dispatch or when the head commit message contains `[lab]`. The push trigger registers the workflow on its first push, so no THROWAWAY commit is needed.
  - **Settings.** Concurrency `ipcam-spike-recording-lab-${{ github.ref }}` with `cancel-in-progress: false`.
  - **`build` job.** Media plus `ipcam-hub`, `mediamtx`, `recspike` and `reccontroller` for linux/amd64 and windows/amd64.
  - **`shard` matrix.**
    - Shards: `{linux, kill9, 1|2}`, `{linux, qemu-reset, 1|2}`, `{windows, kill9, 1|2}`.
    - Each shard: `timeout-minutes: 180` and 10 runs. Shard 1 holds the 256 kbps setting-on run; shard 2 adds the O17-off run, so it has 11.
    - Output (C32): `recspike annotate split --first 1` (shard 1, parts 1–10) or `--first 11` (shard 2, parts 11–21), then two steps with `if: always()`, `recspike annotate emit --batch 1` and `--batch 2`. That is at most 10 notices per step and 11 per job.
  - **QEMU shards.**
    - **Image.** A dated Ubuntu 24.04 amd64 cloud image from `https://cloud-images.ubuntu.com/releases/noble/release-<date>/`, the newest release folder ≥ 14 days old. It is pinned by URL and by the SHA-256 that the folder's `SHA256SUMS` lists, and booted with `-enable-kvm -drive if=virtio,cache=none` and a QMP socket.
    - **Provisioning.** A NoCloud seed (`cloud-localds`) installs the binaries from a second read-only disk (`mkfs.ext4 -d`) and runs `recspike install`.
    - **Networking.** The guest reaches the runner's reccontroller at `10.0.2.2`.
    - **Cut.** On `cut`, the driver sends QMP `system_reset`.
  - **Set size.** A set of 20 default runs plus its O17-off run is the union of its two shards.
- **Smoke job `mi1-smoke` (main workflow).** 2 runs per kind (linux kill9, linux qemu-reset, windows kill9) with `--smoke`: prompt `SchedulePromptWithin(…, 20 s, 40 s)` and validity `ValidWithin(…, 10 s, 60 s)`, with the automated-stop slack of Task 6. The Linux leg installs ffmpeg and sets `RECSPIKE_REQUIRE_FFMPEG=1` for the band cross-check (C15, CI only).

- [ ] **Step 1: Write the failing tests.**
  - `TestEnvelope_RoundTripAndSchema`.
  - `TestEnvelope_HostIsPseudonym`: a host other than the five pseudonyms is rejected.
  - `TestMI1_Verdict_MedianAndMax`: median 0.8 s and max 1.9 s pass; max 2.1 s fails.
  - `TestMI1_LowBitrateEncrypted_DefaultPadding_Uses2s` (O17-A): a 256 kbps encrypted stream with padding on has limit 2000 ms; 1.9 s passes and 2.1 s fails.
  - `TestMI1_PaddingOff_UsesUnpaddedBound` (O17-B): `UnpaddedLimitMs(256000) == 4000` and `UnpaddedLimitMs(128000) == 6000`. At 256 kbps, 3.9 s passes and 4.1 s fails. The run is excluded from the set's median and max and from G10's verdict, and its failure triggers no plan B.
  - `TestMI1_Judge_RecomputesStoredLimit`: a 256 kbps pad-off stream whose envelope stores a stale `LimitMs` of 4048 fails at a loss of 4020 ms.
  - `TestMI1_SetComposition`: every automated set, lab and owner (`pc-kill9`, `pc-bugcheck`, `pi-kill9`, `pi-sysrq`), has 20 default runs (10 encrypted, exactly one 256 kbps pad-on) + 1 O17-off run; real sets have slots 1, 3 and 5 encrypted.
  - `TestMI1_RunFailureConditions`: each of the five G11 conditions alone fails the run.
  - `TestMI1_RunLossIsMaxOverStreams`.
  - `TestMI1_InvalidRunsExcludedAndRedone`: C16-invalid, pacing p99 110 ms and a skipped AU are each excluded and redone.
  - `TestMI1_InvalidRealCut_IsRedoneWithSameSlot` (Review Focus 1): slot 3 is invalid → the redo is slot 3 and encrypted; after 8 attempts the set stops as incomplete.
  - `TestMI1_ConfidenceFromCounts`: `ConfidenceUpperBound(0, 40)` = 0.072 ± 0.001, rendered "< ≈ 7 %"; `(0, 10)` = 0.259 ± 0.001, rendered "< ≈ 26 %".
  - `TestMI1_ArmV6PiSetsDeferred`.
  - `TestHub04_ScanFindsNeedles`: a clear file with an IDR prefix is found; an `.age` file of the same content is clean.
  - `TestHub04_LabImage_FreshPerEncryptedRun` (a plain file stands in for the loop device, so no root is needed): after a clear run writes the needles into the image, an encrypted run's raw scan of that reused image fails, while the fresh image the driver creates for it scans clean. The driver asks for exactly one new image per encrypted run and none for clear runs.
  - `TestRunState_ResumesAfterReboot`.
  - `TestResume_AfterReboot_StartsHubBeforeNTPWait`: `ipcam-hub` starts before the NTP wait, `ColdBootMs` is the uptime at the last job's `recording` event, and `lastindex` reads only the `Before` files.
  - `TestResume_RealSet_LinuxContinues`.
  - `TestResume_RealSet_WindowsWaitsForShortcut`.
  - `TestResume_SecondInstanceWaits`: a second recspike started while the first holds the run-state lock prints the Romanian waiting line, starts no `ipcam-hub`, and continues after the lock is released.
  - `TestInstall_WindowsTaskXML` (`BootTrigger`, `S4U`, `HighestAvailable`, command and working directory).
  - `TestInstall_SystemdUnit`.
  - `TestUninstall_RemovesTaskUnitAndShortcut`.
  - `TestPreflight_RefusesWithRomanianReason`: each failing check gives its message, for example «notmyfault64.exe lipsește sau nu e semnat de Microsoft».
  - `TestWindow_RefusesAutomatedStopOutsideWindow`.
  - `TestWindow_RealCutsIgnoreWindow`.
  - `TestNTPWait_RecordsWaitAndGivesUpAfter10Min` (Review Focus 2).
  - `TestSdConfirm_RequiresDA`.
  - `TestHostInfo_DeviceTreeFallback`: with no `Model` line, the device-tree string goes to `ipcam-hub diag pimodel --raw` (a fake binary records the argument), and the result carries `Source: "device-tree"`.
  - `TestHostInfo_UnknownModelAsksAndDefaultsTo1`.
  - `TestHubproc_KillStopsBeaconOnChildExit`.
  - `TestHubproc_ResumeMsFromRecordingEvents`.
  - `TestAnnotate_SplitsAt3500Bytes`: the 11 runs of shard 2 become 11 lines, each ≤ 3,500 bytes after annotate.sh's escaping (`%25`, `%0A`) and each a valid one-run envelope with `Part` 11…21 and `Parts` 21. `emit --batch 1` prints 10 lines and `--batch 2` prints 1. A run whose line would exceed 3,500 bytes makes `split` fail, and so does a shard of 21 runs.
  - `TestReadAnnotations_RoundTrip`: lines from `split`, unescaped as the runner does and wrapped in the `gh api …/annotations` JSON shape, come back from `results.ReadAnnotations` as the same envelopes. `annotate read` reports `lab-linux-kill9: 21/21` for both shards' files together, and exits 1 on a duplicate part.
  - `TestSession_PCPullsPiResultsAtEnd`.
  - `TestInventory_ReportsMissingSets`: a results folder without the Pi's MI2 and MI4 envelopes lists both under «Lipsesc: …»; with an unsupported Pi it lists no Pi set.
  - `TestPull_PushStoresPiFolderAndPrintsInventory` (in-process `reccontroller pull` and `recspike push` on loopback).
  - `TestMI1_LabKill9Smoke` (needs `MEDIAMTX_BIN` and `MEDIAGEN_DIR`; it fails instead of skipping when they are required): 2 smoke runs on loopback reconcile with loss < 2 s.
  - `TestMI1_SmokeBandCrossCheck` (ffmpeg, CI only; it skips without ffmpeg unless `RECSPIKE_REQUIRE_FFMPEG=1`): ffmpeg decodes the last recorded frame of a clear smoke run, and its band `Frame` equals the SEI index mod 1200.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/recording/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** Copy the keep-awake code from `tools/spikes/video/cmd/spikehub/keepawake*.go`. recspike keeps the PC awake during every set.
- [ ] **Step 4: Run.** The same command — Expected: PASS. Then:
  ```bash
  go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 \
    .github/workflows/ipcam-spike-recording.yml \
    .github/workflows/ipcam-spike-recording-lab.yml
  ```
  Expected: exits 0. `mi1-smoke` is green.

  Finally, run one full lab run, by dispatch or with `[lab]` in the commit message. Expected: all 6 shards are green, and each annotates its runs. Every lab set passes, or a failure is a measured result to report: the run itself completed and the annotations parse.

  Then check the round trip that Task 14 relies on:
  ```bash
  gh api repos/AndreiBesliu/DevWorld/actions/runs/<lab run id>/jobs --paginate --jq '.jobs[] | select(.name | startswith("shard")) | .id' \
    | while read -r id; do gh api "repos/AndreiBesliu/DevWorld/check-runs/${id}/annotations" --paginate > "<scratch>/ann/${id}.json"; done
  cd ipcam && go run ./tools/spikes/recording/cmd/recspike annotate read <scratch>/ann
  ```
  Expected: `lab-linux-kill9: 21/21`, `lab-linux-qemu-reset: 21/21` and `lab-windows-kill9: 21/21`, exit 0. Every parsed envelope passes the schema check and holds exactly one run.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI1 power-cut spike with autostart and lab workflow for G10"`

### Task 8: MI2 crash and 72 h stability — G11, plus the SD-card monitor

Rulings: C05, C07 (counting), C23 (`cardmonitor.go`), G13 (reuse), G17, G18, G19, R07 (host-local controller).

**Files:**
- Create: `ipcam/hub/internal/health/{cardmonitor.go,cardmonitor_test.go}`
- Create: `ipcam/hub/cmd/ipcam-hub/cardsampler.go` + test; Modify: `ipcam/hub/cmd/ipcam-hub/run.go` (sampler when `rec/` is on an SD card)
- Create: `ipcam/tools/spikes/recording/mi2/{judge.go,stability.go,window.go}` + tests; Create: `ipcam/tools/spikes/recording/cmd/recspike/mi2.go`
- Modify: `ipcam/tools/spikes/recording/results/envelope.go` (`Mi2Data`)
- Modify (if the imports change): `ipcam/hub/{go.mod,go.sum}`, `ipcam/tools/spikes/recording/{go.mod,go.sum}`

**Interfaces:**
- Consumes: Task 5 (`Recorder.Stop` with `catalog.GapReason "card"`, events), Task 7a (`IsSdCard`, `diag host`), Task 6 (`ServePrecoded`, `reccontroller --local`), Task 7 (owner `*-kill9` runs with `ResumeMs`, `hubproc`).
- Produces:

```go
package health
type CardHealth int // CardOK, CardDegraded, CardFailed
type IoEvent struct{ Kind string /* "eio" | "erofs" | "fsync" */; Latency time.Duration }
type IoStats struct{ IOErrors24h, IOErrors60s int; ReadOnly bool; FsyncP99_10m time.Duration }
type CardMonitor struct{ /* … */ }
func NewCardMonitor() *CardMonitor
func (m *CardMonitor) Observe(ev IoEvent, now time.Time) (CardHealth, bool /* alert raised now */)
func (m *CardMonitor) Stats(now time.Time) IoStats

package results
type Mi2Data struct{ Hours float64; Cameras int; Crashes72h, ChildRestarts int; OnSdCard bool; CardStates []CardState; WindowHours float64; Invalidated []string }
type CardState struct{ State string; AtMs int64 }
// Envelope gains: MI2 *Mi2Data

package mi2 // SPIKE-ONLY
func Judge(envs []results.Envelope) results.Judgement // G11
```

- **Thresholds (§13.1, verbatim).**
  - OK = 0 I/O errors in 24 h and fsync p99 ≤ 500 ms over 10 min.
  - DEGRADED = one I/O error in 24 h, or fsync p99 > 500 ms over 10 min.
  - FAILED = 3 I/O errors in 60 s, or a read-only remount.
- **Sampler (`ipcam-hub`).**
  - **Inputs.** Every 10 s it writes and fsyncs `rec/.health/probe` and times it (EIO or EROFS counts as an error). Every 5 s it polls the `ro` flag in `/proc/self/mountinfo`. A line in the child's stderr matching `input/output error` or `read-only file system` counts as an I/O error.
  - **Events.** `card {state}` and `alert {kind:"card"}`.
  - **Below FAILED.** Recording continues. Each child write error, received through `Observer.ChildLog`, becomes a `card` gap from the end of the last good part to the start of the next part.
  - **At FAILED.** It calls `Stop(…, "card")` for every job within ≤ 60 s of the triggering event.
- **MI2.**
  - **Resumes.** The 20 resumes are the `ResumeMs` of the owner `*-kill9` set's 20 default runs (G13).
  - **72 h run.**
    - **Setup.** `recspike mi2 --hours 72 --controller 127.0.0.1` runs against a host-local `reccontroller --local` (G18, G19). It records `recommendedCameras` main streams, half of them encrypted.
    - **Rolling window (G19).** recspike deletes segments older than N h, but only paths for which it has seen `ipcam-hub`'s `segment_verified` event (Task 5). tools cannot read the catalog, and the `verify` event carries only counts. N = min(6 h, ½ × free space ÷ write rate), with write rate = cameras × 4 Mbps, and recspike refuses to start if N < 1 h. The free space needed is therefore 2 × N × write rate: ≥ 29 GB for 8 cameras at N = 1 h (≈ 173 GB for the full 6 h), ≥ 15 GB for 4 cameras (≈ 87 GB) and ≥ 8 GB for 2 cameras (≈ 44 GB). recspike prints the figure for the host before it starts. The rows of deleted files read `lost` and show up in `verify`'s `catalogMismatches`; MI2 does not judge that count, because the deletes are the spike's own, outside `ipcam-hub`.
    - **Crash counting.** Only an unexpected `ipcam-hub` exit counts as a crash. MediaMTX child restarts are reported (HUB-13).
    - **Reboots.** An OS reboot detected by boot ID (Linux `/proc/sys/kernel/random/boot_id`; Windows last boot time) invalidates the run. It is redone and never counts as a crash.
    - **Card.** On an SD card, it records `CardStates`.
  - **Restart bound (C07).** recspike is the supervisor in the spike. The report states that its restart is a lower bound for the Faza 1 SCM and systemd recovery.
- **`Judge`.** Passes when all 20 resumes are ≤ 10,000 ms and `Crashes72h == 0`; otherwise `pică → ingest inversat (MediaMTX trage camera)`.

- [ ] **Step 1: Write the failing tests.**
  - `TestCardMonitor_Thresholds` (fake clock): one EIO → DEGRADED; three EIO within 60 s → FAILED; `erofs` → FAILED; fsync p99 600 ms over 10 min → DEGRADED; 24 h with no errors and p99 ≤ 500 ms → OK.
  - `TestSdCard_IoErrors_RaiseAlertThenStop` (Review Focus 4): injected EIO lines in the child's stderr move the state to DEGRADED and then FAILED. The `alert` event follows within 60 s, and `Stop(…, "card")` is called for every job. A read-only remount gives FAILED and stops with a gap.
  - `TestSdCard_DegradedKeepsRecording`: one injected child write error leaves recording running and writes one `card` gap row.
  - `TestMI2_Verdict`: 20 resumes all ≤ 10 s with 0 crashes pass; one resume of 12 s fails with the reverse-ingest plan B.
  - `TestMI2_ResumeFromKill9Runs`.
  - `TestMI2_CrashCountsUnexpectedExitsOnly`.
  - `TestStability_RollingWindow`: with 8 cameras (4 MB/s, 14.4 GB/h), 100 GB free gives N = 3.47 h ± 0.01 h, 500 GB gives 6 h, and 20 GB (N = 0.69 h) refuses to start. A segment older than N without a `segment_verified` event is kept, and one with it is deleted.
  - `TestStability_RebootInvalidatesRun`.
  - `TestStability_ReportsCardStates`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/health/... ./hub/cmd/ipcam-hub/... ./tools/spikes/recording/mi2/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI2 stability spike and SD-card monitor for G11"`

### Task 9: MI3 hardware-tier capacity rig — G12

Rulings: C24 (consumption), G20, R02, R07.

**Files:**
- Create: `ipcam/tools/spikes/recording/mi3/{capacity.go,loads.go,budgets.go,coldboot.go,cpu_linux.go,cpu_windows.go,judge.go,latency.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi3.go` (including the child-process loads `recspike mi3-load live|playback|export|upload`)
- Create: `ipcam/tools/spikes/recording/controller/{livepeer.go,sink.go}` + tests (receive-only Pion peers for live sessions, signalled over the link; HTTPS discard sink `PUT /sink/<name>` on `:7083` with a self-signed certificate); Modify: `ipcam/tools/spikes/recording/cmd/reccontroller/main.go` (wires them)
- Modify: `ipcam/tools/spikes/recording/results/envelope.go` (`Mi3Data`)
- Modify: `ipcam/tools/spikes/recording/{go.mod,go.sum}` (`github.com/pion/webrtc/v4` v4.2.22 directly, `golang.org/x/sys`)

**Interfaces:**
- Consumes: Task 7a (`diag host`: `piModel`, `recommendedCameras`, `cores6Plus`), Task 5 (`ipcam-hub run`), Task 6 (controller streams `camNN_main` and `camNN_sub`, `PacingStats`), Task 7 (`ColdBootMs` of the owner `pc-bugcheck` and `pi-sysrq` runs), video `session.NewAPI`/`session.Answer` and `source.NewRTSP`.
- Produces:

```go
package results
type Mi3Data struct{ Tier string; Cameras int; CpuPctTotal float64; RamMb float64; Budgets map[string]Budget; DiskRatio map[string]float64 /* "clear","encrypted" */; FsyncPerSecPerFile float64; LatencyP95Ms float64; PacingP99Ms int64; SkippedAUs int; Steps []Mi3Step; Sixteen *Mi3Step; Proxies []string }
type Budget struct{ Value, Limit float64; Pass bool }
type Mi3Step struct{ Cameras, Minutes, Attempt int; Pass, Valid bool; PacingP99Ms int64; SkippedAUs int }
// Envelope gains: MI3 *Mi3Data

package mi3 // SPIKE-ONLY
func Judge(envs []results.Envelope) results.Judgement // G12; reads the MI3 envelopes and, for the cold-boot row, the MI1 pc-bugcheck and pi-sysrq runs
```

- **Loads (G20).** Each load is a child process, so its CPU can be attributed. Cameras come from the controller, so the host's CPU excludes the simulator.
  - **Live.** The tier's session count: H3 3, H1 2, H1b 1, H1c 1. Each session is a grid of every camera's sub stream plus one main stream, answered by the video `session` package over `source.NewRTSP` from the controller. Its consumer is a receive-only Pion peer in reccontroller (`controller/livepeer.go`). This ingests main twice, so the CPU figure is conservative. recspike's answerer binds Pion UDP sockets on the host (the R08 exception in Global Constraints).
  - **Playback.** A 1× (4 Mbps) decrypt-read of a closed segment.
  - **Export.** A full-speed decrypt and re-encrypt of one 5-min segment.
  - **Upload.** age encryption plus an HTTPS PUT paced to 10 Mbps (on H1b and H1c: the cameras' total throughput) into the discard sink (`controller/sink.go`). This costs nothing and needs no account.
- **Budgets per tier (§4.8, G20).** The keys of `Budgets`:
  - **H3:** `cpuTotal` (total CPU ≤ 60 %), `tileSub` (CPU per tile sent ≤ 1 %), `tileMain` (≤ 3 %), `encUpload` (encryption + upload ≤ 5 % per 10 Mbps), `ram` (≤ 250 MB + 20 MB per camera + 15 MB per session), `disk` (≤ 1.03 × bitrate, clear cameras only, R02), `fsyncPerFile` (≤ 1 fsync/s per open file) and `latencyP95` (added latency ≤ 150 ms p95: controller send time to receive time of the SEI index on the live peer, both on the controller's clock).
  - **H1:** the H3 keys plus `ingestPerCamera` (ingest + record ≤ 3 % of one core per camera).
  - **H1b, H1c:** `cpuTotal` and `ram` only.
  - **Cold boot (§4.8 row, judged on H3).** It is not an MI3 load. Judge reads `ColdBootMs` from the owner `pc-bugcheck` runs (Task 7: host uptime at the last job's `recording` event after the reboot, so it includes the OS boot and the task start) and judges the row ≤ 20 s on the maximum. The row states the run's camera count (1, `cam01_main`) and that the time includes the Windows boot. A failure makes G12 `pică` for H3, but the camera number does not change boot time. So instead of dropping the number, the Task 14 PR proposes the fix as a Technical Decision, as R02 does for the encrypted disk ratio. On the Pi (`pi-sysrq`) the row is information only, because G20 judges only CPU and RAM there. Missing reboot runs make the row `Incomplete`.
  - **Not judged here: the timeline row.** It needs a catalog of 1 year × 16 cameras with `part_index` (HUB-20, Faza 1), and c1's minimal catalog holds only `segments` and `gaps` over a few days. The report states this.
- **Run shape.**
  - **Encryption (R02).** Half the cameras are encrypted. The report shows disk ÷ bitrate per mode, plus the O17-on note for low-bitrate cameras.
  - **Camera count.** MI3 runs at `recommendedCameras` (C24: `RecommendedMax`; an unknown model uses the owner's number, default 1).
  - **Pacing (R07).** reccontroller records `PacingStats` for every MI3 run and step, as for MI1. While the PC is measured, the Pi controller serves about 70 Mbps: 8 × (4 + 0.5) Mbps of RTSP, the live peers and the 10 Mbps sink. A run or step with p99 > 100 ms or a skipped AU is invalid and redone, so the controller's own delay never counts against the host's latency or CPU. There are at most 3 attempts per step; after that, the host's MI3 is `Incomplete` and the report shows the pacing figures.
  - **On failure (G20).** It steps down by halving with 15-min runs until a count passes, then confirms that count with a full 1 h run (halving again if the confirmation fails). The minimum is 1. The steps go into `Steps[]`.
  - **16 cameras (R07).** With `cores6Plus`, one extra hour at 16 cameras goes into `Sixteen`, as information.
- **`Judge`.** It gives `trece`, or `pică → numărul recomandat devine <n>` (for the cold-boot row: `pică → pornirea la rece: Decizie tehnică în PR`).

- [ ] **Step 1: Write the failing tests.**
  - `TestMI3_Verdict_BudgetsAndCpu`: 55 % total CPU with budgets met passes; 65 % fails.
  - `TestMI3_BudgetSetPerTier`: the H3 keys are exactly `cpuTotal`, `tileSub`, `tileMain`, `encUpload`, `ram`, `disk`, `fsyncPerFile` and `latencyP95`; H1 has those plus `ingestPerCamera`; H1b and H1c have only `cpuTotal` and `ram`.
  - `TestMI3_OnFail_StepsDownByHalving`: 8 fails for 1 h → 4 fails for 15 min → 2 passes → a 1 h confirmation at 2 passes → recommended 2, with the steps recorded. If 1 fails, the result is still 1.
  - `TestMI3_InvalidPacing_RunRedone`: a step with pacing p99 110 ms or one skipped AU is redone and not judged; three invalid attempts make the host's MI3 `Incomplete`.
  - `TestMI3_ColdBootRowFromRebootRuns`: `pc-bugcheck` runs with a maximum `ColdBootMs` of 18 s pass the row, and 22 s fail it with the Technical-Decision plan B and an unchanged recommended number. `pi-sysrq` values are information only, and missing reboot runs make the row `Incomplete`.
  - `TestMI3_DiskBudgetJudgedOnClearCameras` (R02): encrypted 1.05 does not fail; clear 1.04 fails.
  - `TestMI3_SixteenOnlyWithSixCores`.
  - `TestLoads_UploadPacedToTarget`: 10 Mbps on H3; 4 cameras × 4 Mbps on H1b.
  - `TestSink_DiscardsAndCountsBytes` (`controller`).
  - `TestLatency_FromSEIIndexOnControllerClock`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/recording/mi3/... ./tools/spikes/recording/controller/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** CPU times come from `/proc/<pid>/stat` and `/proc/stat` (Linux), and from `GetProcessTimes` and `GetSystemTimes` (Windows, `golang.org/x/sys/windows`).
- [ ] **Step 4: Run.** The same command — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI3 capacity rig for G12"`

### Task 10a: `@ipcam/core` `agerange` — TS random-access age decryption (ENC-07)

Rulings: C18, C21. This task is new in v2 and runs before Task 10.

**Files:**
- Create: `ipcam/apps/packages/core/{package.json,tsconfig.json,tsconfig.test.json}` (name `@ipcam/core`, `"exports": {"./agerange": "./agerange/index.ts"}`, `"sideEffects": false`; tsconfig like `@ipcam/player`: `types: []` for shipped sources)
- Create: `ipcam/apps/packages/core/agerange/{index.ts,agerange.ts,header.ts}` and tests `{agerange.test.ts,golden.test.ts,cctv.test.ts,property.test.ts}`, plus `ipcam/apps/packages/core/spikeonly.test.ts`
- Modify: `ipcam/pnpm-lock.yaml`; `.github/workflows/ipcam-apps.yml` (paths add `ipcam/hub/internal/crypto/testdata/**`, which the tests read)

**Interfaces:**
- Consumes: `hub/internal/crypto/testdata/golden/{*.age,vectors.json}` and `testdata/cctv/` (Task 2).
- Produces:

```ts
export interface RangeSource { readonly size: number; read(offset: number, length: number): Promise<Uint8Array>; }
export interface AgeRange { readonly plaintextSize: number; read(offset: number, length: number): Promise<Uint8Array>; }
export class AgeRangeError extends Error { readonly kind: 'header' | 'truncated' | 'auth' | 'range'; }
export function headerLength(prefix: Uint8Array): number;                          // bytes through the "--- <mac>\n" line
export function openAgeRange(src: RangeSource, fileKey: Uint8Array): Promise<AgeRange>; // the header is typage's job (decryptHeader); this derives the payload key and authenticates the final chunk
```

- **Dependencies.** `@noble/ciphers` 2.4.0 (`chacha20poly1305`) and `@noble/hashes` 2.4.0 (`hkdf`, `sha256`); dev `fast-check` (newest ≥ 14 days old).
- **Format.** Payload key = HKDF-SHA-256(fileKey, salt = 16-byte nonce, info `payload`). Chunk *i* is at `headerLength + 16 + i·65552`, with nonce = 11-byte big-endian *i* ‖ last flag.

- [ ] **Step 1: Write the failing tests** (Vitest):
  - `agerange_goldenFromGo_matchesPlaintext`: each golden file's `read(0, size)` has SHA-256 = `plaintextSha256`, and the 64 listed ranges match.
  - `agerange_randomRanges_matchFullDecrypt` (ENC-07): 10,000 seeded random ranges per golden file equal the slices of the verified full plaintext.
  - `agerange_cctvVectors` (C21): each copied vector with a `file key` behaves as its `expect` says: success with a matching payload hash, or an `AgeRangeError` for payload failures. Header-failure vectors are counted and skipped, as the header is typage's job.
  - `agerange_property_anyRangeEqualsSlice` (fast-check, golden files).
  - `agerange_rejectsTruncatedFile` (`truncated` or `auth`).
  - `agerange_rejectsWrongKey` (`auth`).
  - `agerange_headerLength_findsMacLine`.
  - `core_noSpikeOnlyMarker` (`spikeonly.test.ts`): no file under `apps/packages/core/` contains the marker; the needle is built as `'SPIKE' + '-ONLY'`.
- [ ] **Step 2: Run.** `pnpm -C ipcam --filter @ipcam/core test` — Expected: FAIL.
- [ ] **Step 3: Implement** in about 150 lines.
- [ ] **Step 4: Run.** `pnpm -C ipcam install && pnpm -C ipcam typecheck && pnpm -C ipcam lint && pnpm -C ipcam --filter @ipcam/core test` — Expected: PASS. `ipcam-apps` is green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add @ipcam/core agerange random-access age decryption"`

### Task 10: MI4 `age` random-access rig — G13 (ENC-08)

Rulings: C25, C26, G21, G22, G23, G24, C01 (`diag` benches).

**Files:**
- Create: `ipcam/hub/cmd/ipcam-hub/{diag_cryptobench.go,diag_readbench.go,diag_seekbench.go}` + tests (production)
- Create: `ipcam/tools/spikes/recording/mi4/{runner.go,judge.go,root_linux.go,root_other.go}` + tests; Create: `ipcam/tools/spikes/recording/cmd/recspike/mi4.go`
- Create: `ipcam/tools/spikes/recording/controller/{appapi.go,appapi_test.go}` (app endpoints on `:7080`: `GET /spike/v1/cameras` → `[]`, `POST /spike/v1/results` saved under `results/app/` with the same file and byte caps as spikehub); Modify: `ipcam/tools/spikes/recording/cmd/reccontroller/main.go` (wires them)
- Create: `ipcam/tools/spikes/video/app/src/scenarios/{age.ts,age.test.ts}` (SPIKE-ONLY); Modify: `ipcam/tools/spikes/video/app/src/{results.ts,scenarios/index.ts,i18n.ts}`, `ipcam/tools/spikes/video/app/package.json` (`@ipcam/core: workspace:*`, `age-encryption` newest ≥ 14 days old), `ipcam/pnpm-lock.yaml`
- Modify: `ipcam/tools/spikes/recording/markers_test.go` (adds `../video/app/src/scenarios/age.ts` to `TestMarkedNonGoFiles`)
- Modify: `ipcam/tools/spikes/recording/results/envelope.go` (`Mi4Data`)

**Interfaces:**
- Consumes: Task 2 (`crypto.NewWriter`, `NewRecording`), Task 5 (`ipcam-hub run` with `segmentSecs`), Task 7a (`diag host`: `piModel`, `recommendedCameras`), Task 10a (`openAgeRange`), the ENC-06 annotation (Task 2).
- Produces:
  - **`ipcam-hub diag cryptobench [--seconds 60]`** (G23). One goroutine under `GOMAXPROCS=1` encrypts through `crypto.Writer` into `io.Discard` for ≥ 60 s. It prints `{"schema":"ipcam.diag.cryptobench/1","mbPerSecSingleCore":…,"mbPerSecAllCores":…,"seconds":…}`; the all-cores figure is information only.
  - **`ipcam-hub diag readbench --file <seg.mp4.age> --identity <file> --reads 1000 --size part|65536|4194304`** (G22). Each `part` read is the length of one fMP4 part of that file (≈ 512 KiB at 4 Mbps), at a uniform random offset. It prints every read's ms plus max, p50 and p99. recspike drops caches on the Pi before the set (`sync; echo 3 > /proc/sys/vm/drop_caches`), so `recspike mi4` must run as root on Linux (`sudo ./recspike mi4 …`). Without root it refuses with «MI4 trebuie pornit cu sudo: ./recspike mi4 …» before recording anything. Windows reads are reported as warm, and 64 KiB and 4 MiB reads are information only.
  - **`ipcam-hub diag seekbench --file … --identity … --seeks 20 [--whole-ram]`** (G21). Each seek is timed from the request until the plaintext bytes of the first IDR sample at or after the target have been delivered by the read path (fMP4 `moof` index → sync sample). With `--whole-ram`, the whole segment is first decrypted into a buffer of ≤ 256 MiB. It prints each seek's ms.
  - **MI4 driver (`sudo ./recspike mi4 --controller <addr>`).** It records one 5-min 10 s encrypted `cam01_main` job (4 Mbps), served by the reccontroller at `<addr>`, through `ipcam-hub` and uses the first closed segment. When stage 1 fails, it also records a 2-min segment (`segmentSecs: 120`).
  - **`AGE` app scenario** (G24, SPIKE-ONLY).
    - **Bench.** It encrypts a fixed-seed 32 MiB buffer in the page with typage, gets the file key with typage's `decryptHeader`, and times `openAgeRange` + `read` of the whole plaintext.
    - **Output.** It posts `{scenario:"AGE", mbps, bytes, ms}` to `<hub>/spike/v1/results`.
    - **Label.** The Romanian label in `i18n.ts` is «Viteză agerange (MI4)».
    - **Test function.** `benchAgerange(cipher: Uint8Array, fileKey: Uint8Array, clock: () => number): Promise<{ mbps: number; pass: boolean }>` (pass = ≥ 12 Mbps).

```go
package results
type Mi4Data struct{ Model string; ReadMs []float64; ReadMaxMs, ReadP50Ms, ReadP99Ms float64; ReadSize int; Cold bool
    SeekMs []float64; SeekRamMs, Seek2MinMs []float64; CryptoMBs, CryptoMBsAllCores, ThresholdMBs float64; RecommendedAfter int
    AgerangeMbps *float64; Enc06 *bool }
// Envelope gains: MI4 *Mi4Data

package mi4 // SPIKE-ONLY
func Judge(envs []results.Envelope) results.Judgement // G13; reads Enc06 from the envelope recreport builds from the ENC-06 annotation
```

- **`mi4.Judge` (C25, C26, G21).**
  - **Reads.** Pass = `ReadMaxMs ≤ 50`.
  - **Seek.** Pass = max(SeekMs) ≤ 1500. On failure: RAM buffer when max(SeekRamMs) ≤ 1500; otherwise 2-min segments when max(Seek2MinMs) ≤ 1500; otherwise escalate.
  - **Throughput.** The threshold is 50 MB/s for H1, and 5 × `RecommendedMax` MB/s otherwise (Pi 4 → 20, H1c → 10). On the PC (H3) it is information only. On failure, `RecommendedAfter` = max(1, min(`RecommendedMax` − 1, ⌊measured ÷ 5⌋)). A failure always lowers the number: on H1, 45 MB/s gives 7, not ⌊45 ÷ 5⌋ = 9 above the model's 8.
  - **agerange.** ≥ 12 Mbps, from the device run only. A missing run makes that part `Incomplete`.
  - **ENC-06.** From the CI annotation.
  - **Plan B per sub-failure (G21).** A read > 50 ms or an age mismatch → RAM buffer; agerange → F6 whole-segment download.

- [ ] **Step 1: Write the failing tests.**
  - `TestCryptobench_SingleGoroutineGOMAXPROCS1` (`--seconds 1`; fields present; GOMAXPROCS restored).
  - `TestReadbench_EachReadIsOnePart`.
  - `TestSeekbench_FirstIDRAtOrAfterTarget`: in a synthetic fMP4 with an IDR every 20 samples, a mid-GOP target delivers the next IDR's bytes.
  - `TestMI4_ReadLatency_UsesMaxNotP99`: 999 reads at 10 ms and one at 51 ms fail; all ≤ 50 ms pass.
  - `TestMI4_SeekFirstFrame`:
    - max 1.2 s passes;
    - 1.7 s with RAM 1.3 s → plan B "RAM buffer";
    - RAM 1.6 s with 2-min 1.4 s → "2-min segments";
    - both > 1.5 s → escalate.
  - `TestMI4_ThroughputPerModel`:
    - Pi 4: 22 passes; 18 fails with `RecommendedAfter` 3.
    - Pi 5: threshold 50; 45 fails with `RecommendedAfter` 7. H1c: threshold 10; 8 fails with `RecommendedAfter` 1.
    - PC: information only.
  - `TestMI4_AgerangeMissing_Incomplete`.
  - `TestMI4_AgeMismatch_PlanBRamBuffer`.
  - `TestMI4_RefusesWithoutRootOnLinux`: with a fake non-root uid, `recspike mi4` prints the Romanian sudo line and starts no `ipcam-hub`.
  - `TestAppEndpoints_AcceptAgeResult` (`controller`).
  - `TestMarkedNonGoFiles` now covers `age.ts`.
  - `ageScenario_benchDecryptsGoldenAndComputesMbps` (Vitest, C18): the bench decrypts the Task 2 golden fixture, and an injected clock yields 11 Mbps → `pass: false` and 13 Mbps → `pass: true`.
  - `ageScenario_encryptsWithTypageThenTimesAgerange` (1 MiB in the test).
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/cmd/ipcam-hub/... ./tools/spikes/recording/mi4/... ./tools/spikes/recording/controller/... ./tools/spikes/recording/ && pnpm --filter spike-video-app test` — Expected: FAIL.
- [ ] **Step 3: Implement.** Run `pnpm -C ipcam install` after the `package.json` change and commit `pnpm-lock.yaml`.
- [ ] **Step 4: Run.** The same command — Expected: PASS. `ipcam-apps` and `ipcam-spike-recording` are green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI4 age random-access rig and agerange app bench for G13"`

### Task 11: MI9 playback over the data channel — G18 (CTR-12)

Rulings: C01 (hub-side reader), C19, C20, G25, G26, G36, R06, R08 (exception).

**Files:**
- Create: `ipcam/tools/spikes/recording/mi9/{harness.go,bulk.go,seek.go,reader.go,judge.go}` + tests
- Create: `ipcam/tools/spikes/recording/cmd/recspike/mi9.go` (`recspike mi9 --lan --controller <addr>`); Create: `ipcam/tools/spikes/recording/cmd/mi9page/{main.go,web/index.html}` (serves the page and the hub-side harness for the Chrome leg); Modify: `ipcam/tools/spikes/recording/cmd/reccontroller/main.go` (MI9 Pion peer, the playback side)
- Create: `ipcam/tools/spikes/video/e2e/mi9.spec.ts` (SPIKE-ONLY; skipped unless `MI9_PAGE` is set)
- Modify: `ipcam/tools/spikes/recording/markers_test.go` (adds `../video/e2e/mi9.spec.ts` and `cmd/mi9page/web/index.html` to `TestMarkedNonGoFiles`)
- Modify: `ipcam/tools/spikes/recording/{go.mod,go.sum}` (`github.com/pion/webrtc/v4` v4.2.22 and `github.com/pion/turn/v5` v5.1.2, imported directly)
- Modify: `.github/workflows/ipcam-spike-recording.yml` (job `mi9`); `ipcam/tools/spikes/recording/results/envelope.go` (`Mi9Data`)

**Interfaces:**
- Consumes: video `session.NewAPI` (not modified, C19); a 5-min 4 Mbps encrypted segment recorded by `ipcam-hub`. On the Pi this is MI4's segment when it exists. Otherwise, and always on the PC, `recspike mi9 --lan` records one itself first, as MI4's driver does: a 5-min 10 s encrypted `cam01_main` job served by the other device's reccontroller. In CI it is a recording made from `main.h264` in the job.
- Produces:

```go
package mi9 // SPIKE-ONLY
type Transport string // "lan" | "turns443" | "chrome"
type BulkResult struct{ Transport Transport; ThroughputMbps float64; SeekMs []float64 /* 20 seeks */; DisplayMs []float64 /* chrome leg */; RTTMs float64 }
func MeasureBulk(ctx context.Context, recPath string, id age.Identity, t Transport, ice []webrtc.ICEServer) (BulkResult, error)
func JudgeBulk(r BulkResult) (pass bool, reasons []string)
func Judge(envs []results.Envelope) results.Judgement // G18; see "Judge" below for which Source decides which threshold

package results
type Mi9Data struct{ Legs []Mi9Leg }
type Mi9Leg struct{ Transport string; ThroughputMbps float64; SeekMs, DisplayMs []float64; RTTMs float64; Source string /* "ci-loopback" | "ci-relay" | "ci-chrome" | "lan" */ }
// Envelope gains: MI9 *Mi9Data
```

- **Harness (C19).** An offerer (playback client) and an answerer (hub side).
  - **Hub-side reader (§8.5 step 4, C01 exception).** The hub side opens the `.mp4.age` segment with `age.DecryptReaderAt` and the run's identity. It sends **plaintext** fMP4 byte ranges, as hubd does: the Chrome leg appends them to MSE, which needs plaintext.
  - **Relay.** `turns443` uses `ICEServers` + `ICETransportPolicyRelay`.
  - **Channels (§7.6).** `bulk/<call_id>` channels are reliable and ordered, with at most 4 open at once. Messages are ≤ min(64 KiB, the negotiated `max-message-size`). Sending pauses above 1 MiB `bufferedAmount` and resumes below 256 KiB (`SetBufferedAmountLowThreshold` + `OnBufferedAmountLow`).
  - **Throughput.** One channel transfers the whole segment's plaintext; throughput = bits ÷ time.
  - **Seek.** 20 timeline seeks. Each is timed from the request until the first IDR sample's bytes at or after the target are received.
  - **RTT.** `RTTMs` is the selected ICE candidate pair's `CurrentRoundTripTime` from Pion's stats.
- **Legs (G26).**
  1. **CI Go.** Loopback (information only) and relay through coturn TLS/443 at 50 ms RTT (the relay verdict).
  2. **CI Chrome.** The page receives fMP4 over `bulk/<id>`, appends it to MSE and times the first `requestVideoFrameCallback` after each seek. This is the `DisplayMs` figure for CTR-12's "afișează".
  3. **Owner LAN, on each device as host.** `recspike mi9 --lan --controller <other device>` runs once on the Pi (with the PC's reccontroller as the playback peer) and once on the PC (with the Pi's). An armv6 Pi only serves as the peer, through `reccontroller-linux-armv6`. recspike's answerer binds Pion UDP sockets on the host, the R08 exception that the guide's firewall step covers.
- **`JudgeBulk` (G25).**
  - LAN ≥ 8 Mbps; turns443 ≥ 5 Mbps.
  - max(SeekMs) ≤ 1500 and max(DisplayMs) ≤ 1500. Every seek goes to the CSV.
  - On failure: `pică → redare ca track WebRTC 1×, salt prin RPC`.
- **`Judge` (G18), by Source.**
  - **LAN threshold.** Only legs with Source `lan`. Every such leg must pass, and at least one is needed.
  - **Relay threshold.** Only `ci-relay`.
  - **Seeks.** `SeekMs` from `lan` and `ci-relay`.
  - **Display.** `DisplayMs` only from `ci-chrome`.
  - **`ci-loopback`.** Never decides anything; it is information only.
  - **Missing data.** A part with no deciding leg is `Incomplete`; a measured failure still decides (G27).
- **`mi9` job.**
  - **netem (R06).** It installs `linux-modules-extra-$(uname -r)` if needed, probes `tc qdisc add … netem delay 25ms` and annotates the result. The 25 ms goes on the **host-side veth's egress only**, the traffic from the runner into the coturn container. Each direction of the relayed peer-to-peer path crosses it once, so the RTT is 50 ms; on both veth ends it would be 100 ms. If netem is unavailable, the fallback is an in-process TCP delay proxy in front of TURN-TLS. Either way the job asserts 45 ms ≤ `RTTMs` ≤ 60 ms on the relay leg, annotates the measured value and fails outside that range.
  - **coturn (G36).** It starts the `coturn/coturn` image (≥ 4.14, the newest ≥ 14 days old) on 443 with a self-signed certificate and `--lt-cred-mech`. It is pinned by digest, with version, digest and date read from `https://hub.docker.com/v2/repositories/coturn/coturn/tags` (`digest`, `last_updated`); the GitHub API is refused here. It never uses apt's 4.6.1.
  - **Runs.** It sets `MI9_REQUIRE_TURN=1` and runs the live Go tests. It builds `mi9page` and runs `MI9_PAGE=<url> pnpm --filter spike-video-e2e exec playwright test mi9.spec.ts --project chrome`. The e2e config picks tests per project by tag (`grep: /@chrome/`), so the test's title carries `@chrome`. The step fails when its output has no `mi9: {"transport":"chrome"` line.
  - **Results (C32).** The live Go tests print `mi9: <envelope json>` (sets `ci-loopback` and `ci-relay`), and the Playwright spec prints `mi9: {"transport":"chrome","displayMs":[…],"seekMs":[…]}`. Each test asserts that its line is ≤ 3,500 bytes after annotate.sh's escaping. The job turns every `mi9: ` line into a notice annotation, at most 10 per step. recreport wraps the Chrome line into `Envelope{Code:"MI9", Host:"lab-linux", Set:"ci-chrome"}`. `recspike mi9 --lan` writes its envelope (set `lan`, host `pc` or `pi`) into `results/` and sends it to the controller (G15).

- [ ] **Step 1: Write the failing tests.**
  - `TestJudgeBulk` (G25): LAN 9 passes and 7 fails; relay 6 passes and 4 fails; seek max 1.3 passes and 1.7 fails, with the WebRTC-track plan B.
  - `TestJudgeBulk_SeekIsMaxOver20`: 19 × 0.5 s + 1 × 1.6 s fails.
  - `TestMI9_Judge_LoopbackNeverDecidesLAN`: with only a `ci-loopback` leg at 50 Mbps, the LAN part is `Incomplete`. A `lan` leg from the PC at 9 Mbps and one from the Pi at 7 Mbps fail the LAN part. `DisplayMs` taken from a non-`ci-chrome` leg is ignored.
  - `TestBulk_FollowsSection76Rules`: message ≤ min(64 KiB, max-message-size); a 5th concurrent channel is refused; a fake channel pauses above 1 MiB and resumes below 256 KiB.
  - `TestMeasureBulk_LoopbackPlumbing`: in-process peers transfer the decrypted bytes intact (equal to `age.Decrypt` of the segment), and the seeks deliver IDR bytes.
  - `TestMeasureBulk_RelayInProcessTURN` (G36): a `pion/turn` server with a TLS listener and relay-only policy carries the plumbing.
  - `TestMeasureBulk_Coturn443` (C20): skipped unless `MI9_REQUIRE_TURN=1`; in the `mi9` job it measures the live relay and asserts 45 ms ≤ `RTTMs` ≤ 60 ms.
  - `TestMI9Lan_RecordsSegmentWhenNoneExists`: with no MI4 segment, `recspike mi9 --lan` first runs an `ipcam-hub` job with `segmentSecs: 300` (fake binary) and measures its first closed segment.
  - Playwright `mi9: shows the first frame after each of 20 seeks @chrome`: skipped unless `MI9_PAGE` is set.
  - `TestMarkedNonGoFiles` now covers `mi9.spec.ts` and `index.html`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/recording/mi9/... ./tools/spikes/recording/ ./tools/spikes/recording/cmd/recspike/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** The same command — Expected: PASS locally (the coturn test skips), and the `mi9` CI job is green with its annotations, including the measured RTT and the Chrome line.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI9 data-channel playback spike for G18"`

### Task 12: Report generator — G10–G13, G18

Rulings: C17, C32, G15, G20 (cold-boot row), G27, G28, G31, G33, G16 (SD sets), R07 (16 cameras), O17 (both settings shown).

**Files:**
- Create: `ipcam/tools/spikes/recording/cmd/recreport/{main.go,inputs.go,annotations.go,render.go,faza0bis.go,csv.go}` + tests and `testdata/` (two-device fixture, annotation JSON, `golden.md`)

**Interfaces:**
- Consumes: the `results` envelopes and `results.ReadAnnotations` (Task 7), `mi1.Judge`, `mi2.Judge`, `mi3.Judge`, `mi4.Judge`, `mi9.Judge` (Tasks 7–11); video `redact.Scan`; `gh`.
- Produces:
  - **`recreport -in <dir> [-in <dir>…] [-annotations <workflow run id>…] -out docs/research`.**
    - **Inputs.** It loads every envelope and de-duplicates by `RunID` (G15).
    - **Annotations (C32).** It lists each run's jobs with `gh api repos/AndreiBesliu/DevWorld/actions/runs/<id>/jobs --paginate`, then reads each job's `check-runs/<id>/annotations --paginate` and parses them with `results.ReadAnnotations`. Each lab notice is a one-run envelope with `Part`/`Parts`. A lab set with a missing part is named in the report, and its part of G10 becomes `Incomplete`.
    - **ENC-06 record.** It becomes `Envelope{Code:"ENC-06", Host:"lab-linux", Set:"ci-enc06", MI4:&Mi4Data{Enc06:&pass}}`.
    - **MI9 Chrome line.** It becomes `Envelope{Code:"MI9", Host:"lab-linux", Set:"ci-chrome", MI9:&Mi9Data{Legs:[…]}}`.
    - **Other notices.** `mtx2-loss` lines are kept as information for the R01 decision.
    - **Judging.** It calls each `Judge` once (C17) and only renders.
  - **The report `docs/research/<date>-phase0c-recorder-results.md`:**
    - a Romanian owner summary;
    - per host, a `Cod | Valoare | Prag | Trece` table with a source column (lab, pc, pi, ci-relay, ci-chrome, lan);
    - SD-card sets shown separately (G16);
    - G10's default-setting sets, plus one O17-off row per automated set, „Pierdere minimă la pană de curent: oprită (256 kbps, criptat)”, with its limit from §4.5 (4,000 ms) and the note that it does not count in G10;
    - MI3 disk ÷ bitrate per mode;
    - the cold-boot row „Pornire la rece până la înregistrare” (≤ 20 s, H3 judged, Pi information), with the camera count and the note that the time includes the OS boot; the timeline row as „amânat: HUB-20, Faza 1” (Task 9);
    - the row „16 camere (≥ 6 nuclee): măsurat” with the `Sixteen` figures when MI3 ran them, else „16 camere (≥ 6 nuclee): estimare, nemăsurată” (R07);
    - the verdicts G10–G13 and G18 with `trece`, `pică → <plan B>`, `fără verdict: lipsesc măsurători` or `amânat în Faza 0-bis: <motiv>` (G27);
    - triggered decisions in PH0-30 format;
    - the Pi model and the resulting recommended numbers (§4.8);
    - "Limitări": the hubd restream hop not measured (C06); no real-camera recording (C28); Android WebView SCTP not measured (G26); the MI3 loads are proxies (G20); recspike's restart is a lower bound for SCM and systemd (C07); the first-frame definition (G21); the cold-boot row measured on the MI1 reboot runs' camera (Task 9);
    - the static Faza 0-bis table (G33): H1/H2 → G10, G12, G13, OWN-15 (optional), "fără termen", with numbers "estimare, nemăsurată" (G31); iOS `agerange` → G13 part 3, OWN-05 and a borrowed iPhone, "înainte de Faza 4"; and, only when the Pi is unsupported, "Pi armv6 → G10–G13 pe un Pi suportat, OWN-15, înainte de Faza 4". For an unsupported Pi it also says that the Linux part of G10 rests on the lab, and that the Linux parts of G11–G13 read `amânat în Faza 0-bis: Pi armv6`, because they have no lab legs (G31, narrowed to what is measured).
  - **CSVs.** `docs/research/phase0-data/phase0c-recorder-{mi1,mi2,mi3,mi4,mi9}.csv`, one row per sample. The MI1 CSV lists every run, invalid ones included with their `InvalidReason`.
  - **Redaction (G28).** Before writing, the output passes through `redact.Scan`. Any match aborts and writes nothing. Host names are always pseudonyms.
  - **Scope (G34).** This is a per-spike report; the last Phase 0 plan writes `docs/research/<date>-phase0-results.md` as the index with the G1–G19 table.

- [ ] **Step 1: Write the failing tests.**
  - `TestRender_GoldenMarkdown` (matches `testdata/golden.md`).
  - `TestRender_NoRedactionMatches` (G28): seed the video spike's dirt list (an IPv4 and an IPv6 address, a MAC, an email, an `rtsp://` URL, a `.local` name, a Windows user path and a serial) into fields that the output renders: an invalid run's `InvalidReason` (MI1 CSV) and `PiModel.Raw` (the report). Each seed alone makes the render fail and write nothing.
  - `TestInputs_DedupByRunID_TwoDevices` (G15).
  - `TestInputs_ReadsAnnotations` (fake `gh` on `PATH` returning fixture JSON in two pages): 21 one-run parts of a set are read, and one missing part makes that set's G10 part `Incomplete`, named in the report.
  - `TestRender_VerdictIncompleteAndDeferred` (G27).
  - `TestRender_Faza0bisStaticTable` (G33, exact rows).
  - `TestRender_EstimatesMarked` (G31).
  - `TestRender_ArmV6StatesLabBasis` (G31): with an unsupported Pi, the report says that G10's Linux part rests on the lab, and the Linux parts of G11, G12 and G13 read `amânat în Faza 0-bis: Pi armv6`.
  - `TestRender_O17BothSettingsShown`: each automated set has its O17-off row labelled „Pierdere minimă la pană de curent: oprită …” with 4,000 ms as the limit.
  - `TestRender_SixteenMeasuredOrEstimate`: with `Sixteen` set the row reads „măsurat” with its figures; without it, „estimare, nemăsurată”.
  - `TestRender_ColdBootAndTimelineRows`.
  - `TestRender_SdSetsSeparate`.
  - `TestCSV_OneRowPerSample`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/recording/cmd/recreport/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** The same command — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add recorder spike report generator with G10-G13 and G18 verdicts"`

### Task 13: Release assets, probe and Romanian owner guide

Rulings: C02 (builds), C08, C27, C28, G24, G29, G32, G35, R08.

**Files:**
- Create: `ipcam/tools/spikes/recording/cmd/recspike-probe/{main.go,main_test.go}`
- Modify: `ipcam/tools/scripts/third-party-notices.sh` (`--release video|recording`, default `video` unchanged) and `ipcam/tools/scripts/test/third-party-notices.test.sh` (a recording case)
- Create: `.github/workflows/ipcam-spike-recording-release.yml`
- Create: `ipcam/docs/spike-recording.md` (Romanian owner guide); Modify: `ipcam/README.md` (link to the guide)

**Interfaces:**
- Consumes: every binary and the media (Tasks 1–11), the spike APK build (as in `ipcam-spike-release.yml`'s `apk` job), `check-guide-labels.sh`, `check-workflow-runs.sh`.
- Produces:
  - **`recspike-probe`** (GOARM=6; runs on every Pi). It prints the model (`/proc/cpuinfo` `Model`, else `/proc/device-tree/model`) and the archive to use, chosen by `uname -m`: `aarch64` → `spike-recording-linux-arm64.tar.gz`; `armv7l` → `spike-recording-linux-armv7.tar.gz`. On `armv6l` it prints the Romanian explanation instead: the Pi can only be the controller (`reccontroller-linux-armv6`), and the Pi measurements move to Faza 0-bis.
  - **Release workflow (C27).** It mirrors `ipcam-spike-release.yml`:
    - **Triggers and gate.** Tags `spike-recording-v*` plus `workflow_dispatch` with a boolean `publish`. The publish path is idempotent: `gh release upload --clobber` + `edit --prerelease` when the release exists. The `ci-status` gate runs `ipcam/tools/scripts/check-workflow-runs.sh "${GITHUB_REPOSITORY}" "${GITHUB_SHA}" ipcam-spike-recording ipcam-apps ipcam-mobile`, as `ipcam-spike-release.yml` does.
    - **Builds.** No caches. The media come from `mediagen` + `recmediagen`. `ipcam-hub`, `recspike` and `reccontroller` are built for windows/amd64, linux/arm64 and linux/arm GOARM=7. The fork `mediamtx` is built with `GOWORK=off` for the same three targets. `reccontroller` and `recspike-probe` are built for linux/arm GOARM=6 (G32).
    - **Other outputs.** The spike APK with the `AGE` scenario (G24); `third-party-notices.sh --release recording` (Go modules of every shipped binary, the fork's dependencies with `GOWORK=off`, explicit MediaMTX MIT and hls.js Apache-2.0 entries, and the app's npm packages including `@ipcam/core` and `@noble/*`; G35); a Syft SBOM.
    - **Assets.**
      - `spike-recording-windows.zip`: `recspike.exe`, `reccontroller.exe`, `ipcam-hub.exe`, `mediamtx.exe` (a sibling file, R08), `media/{main.h264,sub.h264,sub-256k.h264}` and `THIRD_PARTY_NOTICES.txt`.
      - `spike-recording-linux-arm64.tar.gz` and `spike-recording-linux-armv7.tar.gz`, with the same contents and executable bits kept.
      - `reccontroller-linux-armv6`, `recspike-probe-linux-armv6`, `ipcam-spike.apk`, `spike-recording-sbom.spdx.json`, `THIRD_PARTY_NOTICES.txt` and `SHA256SUMS`.
    - **Checks.** Every asset is checked: `file` per target, archive listings, `SHA256SUMS`, and the guide labels.
  - **Guide sections, in order (G29; the texts quoted between «» exist in the named program and are checked):**
    1. **What you need and how long it takes, per device.**
       - **Automated window.** ≈ 6.5 h per device: 42 runs, i.e. two sets of 20 runs plus each set's O17-off run. The PC shows a blue screen at each bugcheck.
       - **Real cuts.** ≈ 2.5 h in all.
       - **MI2.** 72 h, with both devices in parallel and each device on. Free disk space ≥ 2 × N × cameras × 4 Mbps, where N is the rolling window (Task 8). At the minimum N = 1 h that is ≥ 29 GB on the PC and on a Pi 5 (8 cameras), ≥ 15 GB on a Pi 4 (4 cameras) and ≥ 8 GB on an H1c Pi (2 cameras). The full 6 h window needs ≈ 173, 87 and 44 GB. recspike prints the figure for the device.
       - **MI3.** 1 h, plus step-downs, plus 1 h at 16 cameras if the PC has ≥ 6 cores.
       - **Short runs.** MI4 on the Pi ≈ 15 min, the MI9 LAN run ≈ 15 min per device (it records its own 5-min segment first when MI4's is absent), and the phone step ≈ 10 min.
    2. **Download, SmartScreen and firewall** (R08). Reuse the wording of `ipcam/docs/spike-video.md`: „Windows a protejat PC-ul” → „Mai multe informații” → „Rulați oricum”; on the firewall prompt, „Rețele private” → „Permiteți accesul”; for timeouts, switch the network to „Rețea privată”. The firewall prompt comes for reccontroller.exe when it first starts. It also comes for recspike.exe at the first MI3 or MI9 run, whose WebRTC peers open UDP ports on the host (the R08 exception); the answer is the same.
    3. **On the Pi: run `recspike-probe` first** (G32).
    4. **One-time preparation** (G14):
       - on the PC: download NotMyFault from Microsoft's Sysinternals page and put `notmyfault64.exe` next to `recspike.exe`; pause Windows Update; run `recspike install` (one UAC prompt);
       - on the Pi: back up the SD card or SSD, then run `sudo ./recspike install`;
       - the SD-card confirmation when asked (G16).
    5. **Which device is the host and which is the controller.** The Pi is the controller when the PC is tested, and vice versa; both are NTP-synced; reccontroller runs on the controller and recspike on the host.
    6. **The automated window, per device.**
    7. **The 5 real cuts per device.** The prompt «Scoateți acum alimentarea» appears on the controller and, on the PC, in recspike's window. On the desktop PC, pull the plug; on a laptop, hold power ≥ 10 s (Review Focus 5); on the Pi, pull the power cable. After a PC reboot, continue with the shortcut «Proba înregistrare – tăieri reale». An invalid cut is repeated (Review Focus 1).
    8. **MI2** (72 h).
    9. **MI3** (1 h).
    10. **MI4** (Pi): `sudo ./recspike mi4 --controller <PC address>` (root drops the read cache; without `sudo` recspike refuses with its Romanian line).
    11. **MI9 LAN**, on each device as host: `recspike mi9 --lan --controller <other device>`, first on the Pi, then on the PC. With an armv6 Pi, only on the PC.
    12. **The phone step** (G24). Send pending results; uninstall the old «Proba ipcam»; install the new `ipcam-spike.apk`; enter the PC's address `http://…:7080` while reccontroller runs on the PC; run «Viteză agerange (MI4)».
    13. **Results** (G15). On the PC, run `reccontroller pull`; on the Pi, run `./recspike push --controller <PC address>`. The PC lists what it holds from both devices and names anything missing («Lipsesc: …»). If something is missing, the step says which measurement to repeat. Then zip only the PC's `results` folder and attach it in the chat or put it in `ipcam-teste/` on Google Drive.
    14. **"Cum opriți testul și ștergeți pornirea automată"**: `recspike uninstall` on both devices, then resume Windows Update.
    15. **What to do when something fails** (Review Focus 1, 4, 5).

    There is no real-camera step (C28).

- [ ] **Step 1: Write the failing tests.**
  - `TestProbe_ChoosesArchiveByMachine`: `aarch64`, `armv7l` and `armv6l` each give the expected line; on `armv6l` the output includes «Faza 0-bis».
  - `TestProbe_DeviceTreeFallback`.
  - `third-party-notices.test.sh` recording case: the output has `MediaMTX` with `License: MIT`, `hls.js` with `License: Apache-2.0`, and `npm package: @noble/ciphers `; the video case is unchanged.
- [ ] **Step 2: Run.** `cd ipcam && go test ./tools/spikes/recording/cmd/recspike-probe/... && tools/scripts/test/third-party-notices.test.sh` — Expected: FAIL.
- [ ] **Step 3: Implement.** Implement the probe, the script option and the workflow. Write the guide.
- [ ] **Step 4: Verify.**
  - Run actionlint v1.7.12 on the new workflow. Expected: exits 0.
  - Register the workflow for manual runs with the THROWAWAY push-trigger pattern: a commit adding a branch push trigger, then its revert (as 314d077 and 5709941 did).
  - Dispatch it with `publish=false`. Expected: every job is green, the "release assets" notice lists every asset with its SHA-256, and `SHA256SUMS` verifies. The guide-label check in the release job passes against the sources and the built binaries. It follows `ipcam-spike-release.yml`'s pattern: non-test sources only, the Windows zip unpacked into `${RUNNER_TEMP}/win`, the APK's scripts unpacked into `${RUNNER_TEMP}/apk-app`:
    ```bash
    sources() { for f in "$@"; do [[ "${f}" == *_test.go ]] || printf '%s\n' "${f}"; done; }
    mapfile -t recspike_src < <(sources tools/spikes/recording/cmd/recspike/*.go tools/spikes/recording/{mi1,mi2,mi3,mi4,mi9,hostinfo,hubproc}/*.go)
    mapfile -t controller_src < <(sources tools/spikes/recording/cmd/reccontroller/*.go tools/spikes/recording/controller/*.go)
    mapfile -t apk_scripts < <(find "${RUNNER_TEMP}/apk-app/assets/public" -name '*.js')
    tools/scripts/check-guide-labels.sh docs/spike-recording.md \
      --program recspike "${recspike_src[@]}" --program recspike "${RUNNER_TEMP}/win/recspike.exe" \
      --program reccontroller "${controller_src[@]}" --program reccontroller "${RUNNER_TEMP}/win/reccontroller.exe" \
      --program probe tools/spikes/recording/cmd/recspike-probe/main.go \
      --program app tools/spikes/video/app/src/i18n.ts --program app "${apk_scripts[@]}" \
      --program android apps/mobile/android/app/src/main/res/values/strings.xml
    ```
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add recorder spike release, probe and owner guide"`

### Task 14: Owner run, report and decisions (PH0-30, PH0-31, PH0-32)

Rulings: C27 (R55 procedure), C32, G33, G34, R01 and R02 wording, O17.

**Files:**
- Create: `ipcam/docs/research/<date>-phase0c-recorder-results.md` and `ipcam/docs/research/phase0-data/phase0c-recorder-*.csv` (from `recreport`)
- Modify: `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`:
  - §2.2: triggered technical decisions, plus the R01 decision;
  - §4.8: the owner's Pi model and the measured recommended numbers, plus R02's clear/encrypted split, edited into the Disc row as O17 (5a102e4, merged before Task 1) left it.

  Each change references its gate.

**Interfaces:**
- Consumes:
  - the release workflow (Task 13);
  - the lab workflow and `recspike annotate` (Task 7);
  - `recreport` with `-in` and `-annotations` (Task 12);
  - `mi1.UnpaddedLimitMs` (Task 7);
  - the `mtx2-loss` and ENC-06 notices (Tasks 2 and 4).
- Produces:
  - the report and CSVs;
  - the §2.2 and §4.8 edits;
  - the PR for the owner.

- [ ] **Step 1: Release (C27, R55).**
  - Push so that `ipcam-spike-recording`, `ipcam-apps` and `ipcam-mobile` each have a green run on the head commit (one push whose range touches their paths).
  - Ask the owner, in Romanian, to create the pre-release `spike-recording-v0.1.0` in the GitHub web UI on branch `ipcam/phase0c1-recorder-spike`, at that commit.
  - Push nothing to the branch until it is verified: the tag is on the commit, the release run is green and every asset is attached.
- [ ] **Step 2: Lab and CI results (C32).**
  - Dispatch `ipcam-spike-recording-lab.yml` on the tag commit and wait until it is green.
  - Run `recreport -annotations <lab run id> -annotations <ipcam-spike-recording run id>` into `<scratch>/results/lab`. Expected: 3 lab sets of 21 runs (parts 1–21 each), the ENC-06 record, the MI9 relay and Chrome results, the measured relay RTT, and the `mtx2-loss` notice for the R01 decision.
- [ ] **Step 3: Message to the owner (Romanian).** The release link, the guide link, the per-device time estimate (§1 of the guide), and the reminder to pick the hard-stop window and to do the one-time preparation.
- [ ] **Step 4: Owner results.** When the zip arrives (chat attachment or Drive `ipcam-teste/`), unpack it into its own scratch directory. Run `recreport -in <owner results> -in <scratch>/results/lab -out ipcam/docs/research`. Inspect the output for any IP, MAC, serial, SSID, hostname or URL even though `redact.Scan` passed (PH0-22).
- [ ] **Step 5: O17 bound confirmation.** Confirm that this branch's spec has 5a102e4's §4.5 text («1 parte + 64 KiB ÷ bitrate, rotunjit în sus la părți întregi»). Confirm too that `mi1.UnpaddedLimitMs(256000) == 4000` and the `TestMTX2_WorstCaseLoss` padding-off assert use it, and that `origin/ipcam/design-spec` has no later change to that sentence (`git log origin/ipcam/design-spec -- ipcam/docs/superpowers/specs/`). `mi1.Judge` recomputes every limit, so a later change would mean editing `UnpaddedLimitMs` and its test, then rerunning recreport. The lab envelopes do not need a rerun.
- [ ] **Step 6: Decisions (PH0-31).** Write into §2.2 each triggered decision with its gate:
  - G10 fail → own gortsplib/mediacommon recorder;
  - G11 fail → reversed ingest;
  - G12 → recommended numbers per model (also §4.8);
  - G13 → RAM buffer, 2-min segments, a recommended-number drop, or F6 whole-segment;
  - G18 fail → WebRTC-track playback.

  Add the R01 Technical Decision: reconcile keeps the whole samples of the last partly durable part, departing from §4.5's literal "trunchiază la ultima parte completă", with the `mtx2-loss` figures.

  Add R02's §4.8 disk wording ("≤ 1,03 × în clar; ≤ 1,05 × criptat") to the Disc row. Keep O17's low-bitrate exception, which the row already carries.

  Record the owner's Pi model and the measured recommended numbers in §4.8.

  **Proposals, not decisions.** Some results trigger no plan B, so the PR description only proposes them as Technical Decisions for the owner, each with its figures:
  - an O17-off run above §4.5's bound (a change to the bound or to the app's note; the spec says the run is judged and reported separately);
  - a cold-boot row above 20 s (Task 9).
- [ ] **Step 7: PH0-32 and Faza 0-bis.**
  - List the services, accounts and devices used with their OWN rows: OWN-14 (PC and Galaxy A24), OWN-15 (the owner's Pi) and OWN-18 (CI); NotMyFault is a free Microsoft download.
  - Confirm that the report's static Faza 0-bis table matches §16.9 (G33). Do not hand-edit generated files.
  - Note that the Phase 0 index report comes from the last Phase 0 plan (G34).
- [ ] **Step 8: Commit and open a PR** for the owner's approval — `git commit -m "ipcam: add Phase 0 (c) recorder spike results and decisions"`
