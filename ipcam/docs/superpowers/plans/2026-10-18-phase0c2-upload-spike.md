# Phase 0 (c2) Upload Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Revision v2 (2026-10-25).** v1 was written before the Phase 0 (b) video spike and the c1 recorder spike existed. v2 lands, in the task texts, the controller rulings C2-R1…C2-R6, the rulings that `rulings-review.md` §1 keeps, amends, replaces or merges, and the pre-flight rulings of `preflight.md` §2 that it leaves untouched (all in `.superpowers/sdd/2026-10-18-phase0c2-upload-spike/`). What changed and why:
- **Process boundary and `ipcam-hub` (P01, P02, P14, P15, P18, P35).** Go's internal-package rule stops `tools/` from importing `hub/internal`. The harness therefore drives production code only through `ipcam-hub`: a `run.json` `uploads` section, upload and OAuth events, a 127.0.0.1 control endpoint and new `diag` subcommands. New Task 7b.
- **Catalog and outbox (P03, P11, P25, P26, P34).** Placements live in c1's catalog, through one appended migration and a segment-complete callback inside the row's transaction. The outbox gains §9.7's destination states, Retry-After per destination and its own hash check.
- **Drivers and fakes (P04, P05, P24, P27, P28, P35, P36, P57).** Drive is an own REST client, without `google.golang.org/api`. Drivers take a TokenSource and endpoint options. The fakes become permanent hub test infrastructure. Each driver has golden Caps, and the Drive driver remembers its folder IDs.
- **OAuth on the hub (P10, P19, P21, P27, P37, P39, P40, P46, P53).** A production `crypto.Sealer` and `oauth.Store` with ctx-first methods, sealed tokens in the catalog's `secrets` table, the B1 fallback, a 3 × 4 visibility matrix, and uploadspike's spike-only handoff pages. Task 9 now runs before Task 8, because MI5 uploads with MI6's tokens.
- **Shells (P20, P33, P52).** New Task 9a: the Android intent filter with Custom Tabs, the Tauri loopback through two plugins, and the OAuth scenario in the video app.
- **MI5 (P09, P15, P16, P17, P38, P49, P56).** Recording through c1's stack. A WAN block for the hub process only, by destination address, IPv4 and IPv6, verified from the blocked identity and from the hub's own upload events. The restart comes after the unblock. One hub per device: MI5 takes the 7-day MI6 hub over through a lock and a handover, and no command takes it from MI5 before its close. The hub never outlives the process that holds it, and the autostart's `resume`, which waits for the lock, takes it back however that process ends. On a Pi, min(4, the model's recommended count) cameras, and none on a model with no recommended count; quota pre-checks, autostart, and a 15-min lab leg on the real block in its own workflow.
- **MI8 (P06, P41, P42, P43, P44).** An own SOAP client over an allowlist of `kerberos-io/onvif` subpackages, camera credentials in RAM only, an LGPL FFmpeg process fetched at run time as the motion reference, ≥ 20 episodes per camera, and tests of the HUB-11 and FLW-16 signals. After its prompts MI8 runs on without the terminal on Linux, and `--continue` checks each camera's brand and model.
- **Results and report (P22, P23, P45).** One envelope and one Judge per gate, with evidence floors and four verdict strings, a static Faza 0-bis table, and redaction plus token patterns before anything is written.
- **Release and guide (P08, P48, P50, P52, A1–A16).** The release mirrors c1 Task 13 under R55. The guide has 15 fixed steps and text-only labels confirmed by the owner's screenshots. Task 12 holds the final Romanian owner texts.
- **Owner decisions and accounts (C2-R1…C2-R5, P54, P55).** D1–D3 are run-time settings with defaults: the test account, the device that `uploadspike host` recommends (C2-R2), every camera with ONVIF events. OneDrive personal's live runs are Faza 0-bis unless a 2-minute directory check passes. No label photos, and no personal identifier in any request.
- **Sequencing and CI (C2-R6, P07, P12, P13, P30, P31, P32, P47).** c1 is merged in, never rebased, before Task 1, which starts after c1 Task 10. Shared files have one writer. go1.27.3 comes from `go.work`. The workflow lists every path, grows per task and has a Windows job. Spike files carry a tested marker.

**Pe scurt pentru proprietar.** Construim o probă care verifică trei lucruri, pe dispozitivele dvs.:
- Înregistrările ajung în cloud fără goluri și fără duplicate, chiar dacă internetul cade. Tăiem internetul doar hub-ului, timp de 6 ore. Rețeaua locală și restul PC-ului rămân conectate. Hub-ul urcă în Google Drive, pe un cont Google nou, doar pentru teste. După ce revine internetul, repornim hub-ul în timpul urcării.
- Hub-ul se conectează singur la Google Drive. Vă autentificați pe telefon, pe PC și în browser, iar codul ajunge la hub. Hub-ul își reîmprospătează accesul zilnic, 6 zile la rând.
- Hub-ul ascultă 72 de ore evenimentele de mișcare ale camerelor dvs.

OneDrive personal: Microsoft cere acum un „director” pentru orice aplicație nouă. Un cont personal îl obține de obicei doar cu un card. De aceea OneDrive trece, cel mai probabil, în Faza 0-bis. O verificare de 2 minute ne spune sigur.

Ce aveți de făcut:
- **Acum** (circa 1–1,5 ore): ne spuneți mărcile și modelele camerelor, fără poza etichetei. Creați un cont Google de test. Creați proiectul Google Cloud și trei clienți OAuth, după ghid. Faceți verificarea Microsoft de 2 minute. Pregătiți camerele: ONVIF, un utilizator de nivel „User” și detecția de mișcare.
- **În săptămâna probei** (circa 2 ore de lucru): instalați programele și vă autentificați o dată pe fiecare platformă. Lăsați un dispozitiv pornit 7 zile. Aprobați o singură dată blocarea internetului pentru hub. La final ne trimiteți rezultatele.

Trei alegeri sunt ale dvs. Dacă nu ne spuneți nimic, rămân variantele implicite:
- D1: proba urcă doar pe contul Google de test. Drive-ul dvs. personal nu e atins.
- D2: rămâne pornit 7 zile dispozitivul pe care vi-l recomandăm după modelul Pi-ului.
- D3: toate camerele cu evenimente ONVIF intră în proba de 72 de ore.

Totul e gratuit. Nu vă cerem date de card și nici poze cu eticheta camerelor. Nu ne trimiteți niciodată parole sau secretele clienților (în chat, pe e-mail sau în altă pagină). Le scrieți doar în programul probei, pe dispozitivul hub: secretele în pagina de configurare, parolele camerelor la cererea programului. La final iese un raport cu verdictul pentru porțile G14, G15 (partea din Faza 0) și G17.

**Goal:** Measure MI5, MI6 (the free Phase 0 combinations) and MI8 on the owner's Windows 11 PC and Raspberry Pi. Uploads and logins go by default to a dedicated Google test account's Drive (owner decision D1), through a Testing-mode Google Cloud project on the owner's personal account. OneDrive personal's live runs are Faza 0-bis unless the owner's 2-minute Entra check (A5) passes. Produce the Phase 0 (c2) upload-and-accounts report with verdicts for gates G14, G15 (Phase 0 part) and G17.

**Architecture:**
- **Production-bound code in the `hub` module (§3.5, §16.1).** None of it carries a SPIKE-ONLY marker.
  - `hub/internal/storage`: the §9.1 `Destination` interface, `Caps`, typed errors and `Digest`. The drivers `local`, `gdrive` (own REST client over `net/http`, `golang.org/x/oauth2` for tokens) and `onedrive` (own Graph client).
  - `hub/internal/storage/fakes/{gdrive,graph,oauth}` and the command `hub/internal/storage/fakes/cmd/fakeproviders`: permanent test infrastructure (§14.5).
  - `hub/internal/storage/conformance` (STO-01) and `hub/internal/storage/oauth` (the hub-side PKCE store, T11, T34).
  - `hub/internal/outbox`, on placements kept in c1's catalog; `hub/internal/crypto.Sealer`; `hub/internal/playback.ReadOpen`; `hub/internal/cameras/events` (ONVIF PullPoint, HUB-11).
  - `hub/cmd/ipcam-hub` gains the `uploads` section of `run.json`, upload and OAuth events, a 127.0.0.1 control endpoint and the `diag` subcommands `readopen`, `dest-probe`, `reach`, `uploads`, `visibility`, `scan-secrets` and `onvif`.
- **`uploadspike` (SPIKE-ONLY, `tools/spikes/upload`).** It runs `ipcam-hub`, the c1 fork `mediamtx` and c1's `reccontroller --local`, and reaches production code only through them. It serves the OAuth handoff pages and forwards `code`+`state` to the hub. It drives the WAN block, the mid-upload restart, the 7-day refresh schedule and the 72 h ONVIF run with a frame-diff motion reference (an LGPL FFmpeg process). It writes one results envelope per run, judged by one `Judge` per gate.
- **Shells (Task 9a).** The Android Capacitor shell and the Windows Tauri shell open the system browser and receive the redirect. The OAuth scenario lives in the video spike app.
- **`uploadreport` (SPIKE-ONLY).** It renders the envelopes and lab annotations into the Romanian report and CSVs, through redaction. All of `tools/spikes/upload/` is deleted after the report; the hub packages and the fakes stay.

**Tech Stack:**
- **Go.** Go 1.27.3 from `ipcam/go.work` (C1-R12); module `go` lines stay 1.27.2. `golang.org/x/oauth2` v0.37.0 (BSD-3). `modernc.org/sqlite` v1.60.1 (already in hub). `github.com/kerberos-io/onvif` v1.2.3, only the subpackages of P06, which bring `github.com/clbanning/mxj/v2` (MIT). In `tools/spikes/upload`: `github.com/bluenviron/gortsplib/v5` v5.6.6, `github.com/bluenviron/mediacommon/v2` v2.9.6, `filippo.io/age` v1.3.2, `golang.org/x/sys` v0.48.0, `golang.org/x/term` v0.46.0, all at the workspace's versions. No `google.golang.org/api` (P05).
- **TypeScript and shells.** Node 24 LTS, pnpm 10 with `minimumReleaseAge: 10080`. `@capacitor/browser` and `@capacitor/app` (MIT, major 8 like `@capacitor/core` 8.5.2). The crates `tauri-plugin-opener` and `tauri-plugin-oauth` (MIT OR Apache-2.0) with their JS bindings `@tauri-apps/plugin-opener` and `@fabianlars/tauri-plugin-oauth`. Each is the newest version ≥ 14 days old at dispatch.
- **Harness-only, never shipped.** An LGPL FFmpeg build from BtbN (no libx264), fetched at run time by exact URL and SHA-256. Google Identity Services, loaded by the web reader page from `https://accounts.google.com/gsi/client`. In the lab: `nft` on the hosted runner, and the `toxiproxy` v2.12.0 binary pinned by SHA-256 only as P56's fallback.

**Spec:** `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`:
- §16.4 (MI5, MI6, MI8; test hosts), §16.5 gates G14, G15, G17 and PH0-30…32, §16.9 (Faza 0-bis)
- §9.1 (driver interface), §9.2 (per-provider notes), §9.7–§9.9 (segment lifecycle, retention, outage behaviour), §9.10 (folder layout), STO-01, STO-02, STO-07, STO-08, STO-13
- §3.3 (data map, the OAuth Phase 0 gate), §4.4 (interfaces with ctx, `Sealer`, `Enqueue` in the object's transaction), §4.6 (`placements`, `upload_sessions`, `secrets`), §5.6 (B1, B2), §8.7 (OAuth flow, T34 client table, OneDrive), FLW-22, FLW-31, §8.2 (ONVIF auth and clock), HUB-03, HUB-11, FLW-16, §13.1
- Decisions O11, O13, O14, T11, T34, T41, T42; ARH-08, PH0-22, TST-07, §14.5, §11.8, OWN-07…OWN-10, OWN-14, OWN-15, OWN-18, OWN-22, OWN-23

**Prerequisites (on c1's branch, merged into this one before Task 1):**
- **c1 Tasks 1–10 have landed** (C2-R6, P30). c1 Task 10 is the last c1 task that edits `hub/cmd/ipcam-hub` (c1 plan L687-689, L939, L1106).
  - **c1 Task 5/5b:** `ipcam-hub run` with `run.json` (`dataDir`, `mediamtx`, `identityFile`, `sdCardConfirmed`, `jobs`), JSON-lines events with `type`/`atMs`, `<dataDir>/{state/catalog.db,state/run,rec,tmp}`, exit codes 0/1/3/4; `catalog.Open`, `PutSegment`, `ReconcileFiles`; `recorder`; `crypto.NewTailSealer`, `TornAge` (`DurableSize`, `ReadAt`).
  - **c1 Task 6:** `reccontroller --local` (RTSP `127.0.0.1:8554`, link `127.0.0.1:7081`) serving `cam01_sub`…`cam16_sub` at 512 kbps; the link protocol (`hello` → `session{rtspBase,user,pass,streams}`).
  - **c1 Task 7a:** `ipcam-hub diag host` (`piModel`, `storage`, `recommendedCameras`), `diag pimodel`, the SD gate (exit 4).
  - **c1 Task 7:** the S4U Task Scheduler pattern and the systemd unit (c1 plan L830-831), the single-instance lock that keeps two `ipcam-hub` instances off one `dataDir` (L836), the SD confirmation text (L842), the unknown-Pi device-tree fallback (L843), `hubproc`'s kill semantics and the console-free graceful stop that c1 Task 7.2 ships (`hubproc.Stop`). c2 copies these, and its `hubproc` adds the bond that ends the hub with its holder (Task 9); it imports nothing from c1's module.
  - **c1 Task 10:** the `AGE` scenario in `tools/spikes/video/app`, which c2's `OAUTH` scenario sits next to.
- **c1 Task 13 has landed before c2 Task 12** (P30): `third-party-notices.sh --release recording`, `recspike-probe`, and guide step 14, which removes c1's autostart before c2's owner runs (P51).
- **Video spike (Phase 0 b):** `tools/spikes/video/{redact,cmd/mediagen,cmd/spikehub/keepawake*.go,app}`; `tools/scripts/{annotate.sh,check-licenses.sh,check-guide-labels.sh,check-workflow-runs.sh,check-video-build-list.sh,third-party-notices.sh,count-native-lines.sh}`; the Android and Tauri shells; `ipcam-spike-release.yml` (APK signed with the run's debug key, L183-184).
- **Kept for c2 (P14).** The c1 controller keeps `tools/spikes/recording/` until c2's report, as spec L2354 allows ("dacă nu sunt refolosite"): c2's release builds `reccontroller` and its media from it.

**Owner actions now.** As soon as this plan is approved, the controller sends Task 12's messages M1 (D1–D3) and M2 (A1–A7), and with M2 the account-setup text of guide steps 1–5, written from Task 12's specification (labels, the fixed port 7091, the Entra values). Nothing in them depends on c2 code. Task 12 later puts the same text, corrected by the owner's screenshots, into `docs/spike-upload.md`.

## Global Constraints

- **Repo rules.**
  - Touch only `ipcam/**` and `.github/workflows/ipcam-*.yml`. Every push or PR trigger has `paths:` covering every file its jobs build, plus its own workflow file, listed one by one, without brace globs (P13).
  - The one edit to another plan's workflow is `'!ipcam/tools/spikes/upload/**'` in both triggers of `ipcam-spikes.yml` (Task 1). Call it out in the PR.
  - Code, identifiers, comments and commits are in English. Owner-facing text is Romanian: the guide, the report summary, uploadspike's on-screen prompts, the app texts and the verdict strings. Owner-facing English uses they/them; the Romanian guide addresses the owner as «dvs.» (P29).
- **Branches and merges (C2-R6, P30).**
  - This plan is written on `ipcam/phase0c2-upload-spike`, branched at c1's 9309b2b. Before Task 1 the controller merges c1's branch into it with a merge commit, never a rebase, at or after c1 Task 10's commit. It merges c1 again before Task 12, which starts only after c1 Task 13 has landed.
  - c2's PR targets `ipcam/phase0c1-recorder-spike` until c1 merges, then `main`, so its diff holds only c2.
  - Never force-push. Each plan writes its §2.2 decisions under its own gate subheadings.
- **Single writer (P30).** One task at a time, of either plan, edits `ipcam/hub/go.mod`, `ipcam/hub/go.sum`, `ipcam/go.work`, `ipcam/go.work.sum`, `ipcam/hub/cmd/ipcam-hub/main.go`, `ipcam/tools/scripts/third-party-notices.sh` and its test, `ipcam/README.md` and `.github/workflows/ipcam-spikes.yml`. The c2 controller confirms with the c1 controller before it dispatches a c2 task that touches one, and announces every edit of c1's `hub/internal/catalog/catalog.go` (P03).
- **Process boundary (c1 C01, P01).** Nothing under `tools/` imports `…/ipcam/hub/internal/…`, and `tools/spikes/upload` imports nothing from `tools/spikes/recording`. uploadspike uses production code only by running `ipcam-hub` (its `run` and `diag` subcommands, its stdout events and its 127.0.0.1 control endpoint) and the `fakeproviders` command. Three things talk to a provider or a camera directly, by definition harness code: the web reader page (P21), the lab's fake-login rig (Task 8) and the motion reference's RTSP or snapshot reader (P41).
- **Inbound listeners (c1 R08, P40).**
  - `ipcam-hub` keeps no LAN listener. Its hook and control endpoints bind `127.0.0.1` only.
  - On each device, only the uploadspike process that holds the device's `hub.lock` (Task 9) listens on `127.0.0.1:7091` (`HandoffPort`; also `[::1]:7091` when the host has IPv6 loopback), for as long as it holds the lock. It serves there the handoff pages, `setup`, the reader, `status.html` and the handover endpoint. Every other uploadspike command (`status`, `mi8`, `results`, `host`, `install`, `uninstall`, `annotate`, `unblock`) never listens: it reads the run state, or calls the holder on `127.0.0.1:7091`. The holder also listens on the LAN only while a login from another device is open (the A24, or the PC when the hub runs on the Pi), and closes that listener on completion or when the login's state expires after 10 min. The LAN listener binds each of the host's RFC 1918 IPv4 addresses (`<lan-ip>:7091`), never the wildcard: Linux refuses `0.0.0.0:7091` with EADDRINUSE while `127.0.0.1:7091` listens.
  - In the lab, `fakeproviders` binds `198.51.100.1` (TEST-NET-2, P56).
- **Toolchains and versions (C1-R12, P07, P31).**
  - Go 1.27.3 from `ipcam/go.work`; CI uses `go-version-file: ipcam/go.work`, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`. Every module `go` line stays 1.27.2.
  - **Local runs.** The steps' local commands run plain `go` from `ipcam/`, which takes go1.27.3 from `go.work`; `GOTOOLCHAIN=local` stays in CI only, because this container's system Go is 1.24.7 and refuses the workspace with it. This container has no IPv6 and a transparent proxy that accepts TCP to any address on ports 80 and 443, and about 6 GB of free disk. So tests that need IPv6 loopback skip when the host has none (CI runners have it), unreachable-address tests use port 9 (`203.0.113.1:9`), never 80 or 443, and the fork, media and process runs that need `MEDIAMTX_BIN` or `MEDIAGEN_DIR` run in CI when the disk is short.
  - Every dependency: the newest version ≥ 14 days old at dispatch. govulncheck runs in CI only; a finding there allows the fixed version before 14 days, with the reason in the commit message (c1 R09).
  - Each task names the `go.mod`/`go.sum` files it changes and runs `GOWORK=off go mod tidy` in them. When `hub/go.mod` changes, it also tidies `hub/third_party/mediamtx` with `GOWORK=off` and commits its `go.mod`/`go.sum` if they changed, and `tools/scripts/check-video-build-list.sh` stays green. A task that changes a `package.json` runs `pnpm -C ipcam install` and commits `ipcam/pnpm-lock.yaml`.
- **Licenses.**
  - Allowlist (`tools/licenses/allowlist.txt`): MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC, MPL-2.0.
  - `kerberos-io/onvif`: only `event`, `event/topic`, `xsd`, `xsd/iso8601`, `xsd/onvif` and `device` may appear in `go list -deps ./hub/...`, and the modules `github.com/juju/errors` (LGPL) and `github.com/elgs/gostrgen` (no license) never (P06, Task 10's test).
  - FFmpeg is LGPL, runs as a separate process started by uploadspike, is fetched at run time, and is never in `hub/**`, in a release asset or in the repo (P41). `toxiproxy` is a lab fallback binary only.
- **Zero cost (O13, PH0-32).**
  - No paid service and no payment details. Devices: the owner's Windows 11 PC (WIN-1, web), Raspberry Pi and Galaxy A24 (AND-1, MI6 only); DIGI home internet (P29). GitHub-hosted runners only (OWN-18). No CI job uses an account secret (P52).
  - Google: a project in **Testing** on the owner's personal Google account (OWN-07), `drive.file` only; 7-day refresh tokens are accepted.
  - **D1 (C2-R1), run-time setting `--drive`, default `test`:** MI5 uploads and MI6 logs in to the new test account only (A2), and the personal Drive is not touched. `test+personal` (D1-B) adds one camera for ≈ 2 h (≈ 0.5 GB) on the personal Drive. `personal` (D1-C) uses the personal Drive for everything (≈ 7–8 GB) and refuses to start below 11 GB free.
  - **D2 (C2-R2), run-time choice.** With no answer, the device is the one that `uploadspike host` recommends: B (the Pi holds the 7 days, MI5 on the PC) on a Pi 4, 5, 400 or 500 with a 64-bit system, A (the PC for everything) otherwise. P17's "PC by default" is that A. The owner may pick A, B or C at run time.
  - **D3 (C2-R3), run-time choice.** With no answer, every camera with an events service and PullPoint support (D3-A); `--one-per-brand` is D3-B; with D3-C MI8 does not run and `uploadreport -d3 c` renders G17's note.
  - **OneDrive personal (C2-R4, P54):** its live runs are Faza 0-bis by default. They come back only if A5's check finds a directory that belongs to the owner and Entra's «New registration» opens. The driver and the fake are written regardless.
- **Secrets and privacy (PH0-22, C2-R5).**
  - Camera credentials, RTSP and snapshot URIs, OAuth codes, verifiers, access and refresh tokens, client secrets, upload session URIs and `uploadUrl`s never enter the repo, logs, events, results or the report. Refresh tokens and client secrets exist only as sealed rows of the catalog's `secrets` table (P19, P46); access tokens stay in RAM.
  - No camera label photos: brand and model are typed (A1). No request made by the agents or by c2's code carries the owner's email or any personal identifier. HTTP requests that c2's tools make on their own use the User-Agent `ipcam-spike/0.1`, which names no person and no account: the repository URL carries the owner's GitHub handle, which is their name (C2-R5).
  - Pseudonyms: hosts `pc`, `pi`, `lab-linux`, `lab-windows`; cameras `real-1`, `real-2`, …; destinations `dst-1`, `dst-2`, `dst-od` and the MI6 pairs `g-android`, `g-windows`, `g-web`, `m-android`, `m-windows`, `m-web`; accounts `acct-1`, `acct-2`. The drivers never fetch account identity (Drive `fields=storageQuota`, Graph `$select=quota`); P21's `permissionId` comparison stays in `ipcam-hub`'s RAM (P45).
  - Test sentinels are assembled at run time from parts (`"SENT"+"INEL-…"`) and never take a provider shape; the only provider-shaped test strings are Task 11's pattern tests, also assembled at run time (P46).
- **OAuth on the hub (T11, T34, FLW-22, FLW-31).** The PKCE verifier and `state` are generated and kept by `ipcam-hub` (RAM, 10 min, single use). Apps deliver only `code`+`state`. The hub redeems with the exact `redirect_uri` that `Begin` returned, and refreshes. Web × Google has no plan A and uses the hub's device flow. Client secrets of the Desktop and TV clients are non-confidential and live sealed in the hub. The spike channel from app to hub is plain HTTP through uploadspike; T11's encrypted channel (lanapi with HIK, T42) is Faza 1, and the report says so (P40).
- **Provider rules, exactly as in §9.2.** Google Drive (`drive.file` only): resumable session (URI valid one week), 16 MiB chunks (multiple of 256 KiB), resume with `PUT bytes */size`, verify `md5Checksum` + size; 750 GiB/day per user → 24 h pause; ≈ 2 creates/s; quota units (list 100, download 200, get 5), the 400M-units/day threshold is per project → **zero `files.list` in normal operation**; 100 refresh tokens per account and client. OneDrive personal: upload session, 10 MiB fragments (32 × 320 KiB, < 60 MiB), in order, **no** `Authorization` on `uploadUrl`, resume with `GET uploadUrl`, verify `quickXorHash` + size; the recycle bin **occupies quota** (`quota.deleted`); `Retry-After` on 429.
- **SPIKE-ONLY (P47).** Every non-test `.go` file under `tools/spikes/upload/` starts with `// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md`. So do, in their own comment syntax, `tools/spikes/upload/ci/s4u-run.ps1`, uploadspike's embedded HTML pages and the two video-app files of Task 9a. `markers_test.go` checks them (needle built as `"SPIKE" + "-ONLY"`), and `hub/spikeonly_test.go` keeps the marker out of `hub/**`. Spike-only by definition: the WAN block, the restart orchestration, the handoff pages and forwarder, the setup page, the web reader page, the lab login rig, the copied link client and SD confirmation, the probe and the motion reference.
- **Results path (c1 C32, P09).** Job logs and artifacts are refused here (403); check-run annotations are readable. Lab and CI measurements travel as notice annotations through `tools/scripts/annotate.sh`, each line ≤ 3,500 bytes after its escaping (`%25`, `%0A`), at most 10 per step. `uploadreport -annotations <run id>` reads them with `gh api`. Owner-device results travel as zip files.
- **Release (R55, P08).** Tag pushes are refused here and are not routed around: the owner creates the pre-release in the GitHub web UI, and the controller verifies the tag, the run and the assets.
- **Thresholds, as in §16.4 / §16.5:**

  | Code | Threshold |
  |---|---|
  | MI5 (G14, STO-02, SUC-04) | 4 cameras on the PC; on a Pi min(4, the model's `RecommendedMax`), so a weaker Pi records fewer and a Pi 5 records the spec's 4; continuous + events; the hub process's WAN blocked 6 h by the host firewall (LAN stays); after the unblock, one hub restart in the middle of an upload: 0 missing segments, 0 duplicates, every hash verified. Recorded: recovery time vs uplink, Drive quota units per segment (Cloud console), OneDrive personal quota after delete (`quota.deleted`, only if A5 passed), `playback` reading a still-open segment. Plan B: fix the outbox before Faza 3; if the open-segment read fails, 2-min segments |
  | MI6 (G15 Phase 0 part, T11, T34) | PKCE on the device, code handed to the hub (web × Google: device flow on the hub); daily refresh from the hub on days 1–6 (Testing mode). Phase 0: Google Drive × Android (AND-1), Windows (WIN-1) and web (PC); OneDrive personal on the same platforms only if A5 passed. Cross-client visibility: a file created by each hub client type (Android, Desktop, TV) is listed and read by the web client, the device-flow client and each native client of the same project. Pass per §3.3; the result goes to §2.2 |
  | MI8 (G17, HUB-11) | the owner's cameras on the spike hub for 72 h; a harness frame-diff motion reference (test tool only): ≥ 95 % of motion episodes have an ONVIF event within 5 s; 0 silent subscription losses (> 5 min with no successful `Renew`/`PullMessages`) |

- **Plan B per gate (written into §2.2 on failure, PH0-31).**
  - **G14** → fix the outbox before Faza 3; if open-segment reads fail, 2-min segments.
  - **G15** → §3.3's order per platform × provider pair (P39): plan A fails → B1, the hub's device flow, run the same day; B1 also fails → «pică → B2»: §2.2 records the cloud stateless proxy of §5.6 for that pair, built with the cloud in Faza 2, with the data-map and privacy-policy rows updated first. A failed visibility cell → «F6 pe <platformă> folosește tipul de client al hub-ului».
  - **G17** → per affected brand, a native event adaptor (Hikvision `alertStream`, Dahua `eventManager`, Reolink push) as a Technical Decision; otherwise the camera is marked «evenimente nesigure» and FLW-16 applies.
- **Verdicts (P22).** Exactly `trece`, `pică → <plan B>`, `fără verdict: lipsesc măsurători` and `amânat în Faza 0-bis: <motiv>`. No Judge returns `trece` on an empty judged set; a measured failure decides even when other parts are missing.
- **Faza 0-bis leftovers to list, not execute (§16.9, P23, P28, P55).** OneDrive business (6 h outage, with MI7's tenant); OneDrive personal, unless A5 passed; Entra ID P1, MI7 and G16; the Drive refresh on day 7 and after, with the project in Production; all iOS combinations; `ipcam-lab` (TST-07's 6 h on real accounts and the 72 h weekly run, OWN-22); the OneDrive redirect on `<domeniu>`; the nightly STO-01 runs on real accounts. Task 11 renders the exact rows.

## Review Focus

1. **The Google project is in Testing, so every refresh token dies 7 days after issuance, and a run that counts days from the first refresh or crosses day 7 fails for a reason that is not the hub's.** Expected: day N counts from the issuance time that `Complete` records; days 1–6 are judged; an `invalid_grant` on day 7 is `expected-expiry`, never `failed`; the day-7+ Production check is a Faza 0-bis row. *Owner:* Task 9, `TestRefresh_Day7Expiry_NotCountedAsFailure` and `TestRefresh_DayCountedFromIssuance`.
2. **The WAN block cuts the LAN or loopback, or misses IPv6 (DIGI is dual-stack), so the test breaks the setup instead of the hub, or never blocks it.** Expected: the rule matches destination addresses for the hub process only, IPv4 and IPv6; before the clock starts and every 10 min, `diag reach`, run as the blocked identity, shows every provider host blocked over both families (`timeout`, `unreachable` or `denied`; a `refused` connection means the path is open) while a LAN address and loopback answer; otherwise the run unblocks and aborts with a Romanian reason. The hub's own events confirm it: no upload verified at a provider during the block. *Owner:* Task 8, `TestWanBlock_KeepsLanAndLoopback`, `TestVerifyBlocked_AbortsWhenProviderReachableOverIPv6` and `TestG14_UploadsDuringBlock_FaraVerdict`.
3. **`files.list` runs during normal uploads, or a restart re-creates the `ipcam` folder because nothing remembered its ID, burning the per-project Drive quota and splitting the uploads.** Expected: zero `files.list` in steady state, folder IDs persisted before `files.create`, a restart reuses them with 0 lists. *Owner:* Task 5, `TestDrive_ZeroListInNormalOperation` and `TestDrive_RestartReusesFolders_ZeroList`.
4. **OneDrive's recycle bin occupies quota, so a quota-driven delete deletes copy after copy without freeing space.** Expected: at ≥ 98 % with bytes in the bin the destination goes `QUOTA_FULL`, the placements stay local (`PAUSE_KEEP_LOCAL`), the alert names the 30-day bin, and the outbox makes 0 `Delete` calls. *Owner:* Task 4, `TestOutbox_RecycleQuota_PausesKeepLocal`.
5. **A verifier, code, token, session URI, client secret, camera password or camera IP leaks into an event, a log, the app payload, the results or the report.** Expected: the verifier never leaves `ipcam-hub`; events carry offsets, never URIs; a planted value of each kind appears 0 times anywhere; the report aborts on any redaction or token-pattern match. *Owners:* Task 9 `TestHandoff_VerifierNeverLeavesHub` and `TestMI6_TokenNotInResults`; Task 7b `TestRun_UploadEventsCarryNoSessionURL`; Task 11 `TestRender_NoSecretInOutput`.

## File structure

- `ipcam/hub/` (production module):
  - `internal/storage/` (types, errors, `Digest`), `internal/storage/{local,gdrive,onedrive,conformance,oauth}`, `internal/storage/fakes/{gdrive,graph,oauth}` and `internal/storage/fakes/cmd/fakeproviders`;
  - `internal/outbox`; `internal/playback`; `internal/cameras/events` and `internal/cameras/events/onviftest`;
  - changes to c1's `internal/catalog/catalog.go` and `internal/crypto` (new `sealer*.go`);
  - changes to c1's `cmd/ipcam-hub` (`uploads.go`, `control.go`, `hubid.go`, `diag_{readopen,destprobe,reach,uploads,visibility,scansecrets,onvif}.go`).
- `ipcam/tools/spikes/upload/` (spike module): `results`, `hubproc`, `hostinfo`, `runstate`, `handoff`, `linkclient`, `sdconfirm`, `install`, `mi5`, `mi6`, `mi8`, `ci/s4u-run.ps1`, `cmd/{uploadspike,uploadspike-probe,uploadreport}`.
- `ipcam/tools/spikes/video/app/src/{oauthbridge.ts,scenarios/oauth.ts}` (SPIKE-ONLY, c2's marker); the Android and Tauri shell changes.
- `.github/workflows/`: `ipcam-spike-upload.yml` (CI), `ipcam-spike-upload-lab.yml` (MI5 lab), `ipcam-spike-upload-release.yml`.

---

### Task 1: Upload spike module, markers test, upload CI and lab workflow

Rulings: C2-R6, P07, P09 (lab workflow and its paths), P12, P13, P30, P31, P32, P47, P49 (the S4U round-trip tool).

**Files:**
- Modify: `ipcam/go.work` (add `./tools/spikes/upload`; single writer)
- Create: `ipcam/tools/spikes/upload/{go.mod,.golangci.yml,doc.go,markers_test.go}`
- Create: `ipcam/tools/spikes/upload/ci/s4u-run.ps1`
- Create: `.github/workflows/ipcam-spike-upload.yml`, `.github/workflows/ipcam-spike-upload-lab.yml`
- Modify: `.github/workflows/ipcam-spikes.yml` (add `'!ipcam/tools/spikes/upload/**'` after the includes of both triggers; single writer; PR call-out)

**Interfaces:**
- Produces:
  - **Module** `github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload`, `go 1.27.2`, no requirements yet. Its lint config mirrors c1's spike module (`version: "2"`; govet, staticcheck, errcheck, gosec, revive, unused, ineffassign). Hub code uses `hub/.golangci.yml`.
  - **Markers.** `markerText = "SPIKE" + "-ONLY" + ": ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md"`. `markerLine(path)`: `.go`, `.ts`, `.js` → `// <text>`; `.html` → `<!-- <text> -->`; `.ps1` → `# <text>`; any other extension is an error. `markedNonGoFiles` starts as `[]string{"ci/s4u-run.ps1"}`; Task 9 adds `cmd/uploadspike/web/*.html`, Task 9a adds the two video-app files.
  - **`ci/s4u-run.ps1 -Exe <path> [-Arguments <string>] -Out <file> [-TimeoutSec 300]`.** Registers a temporary scheduled task for the current user with `New-ScheduledTaskPrincipal -LogonType S4U -RunLevel Highest`, starts it once, waits, writes the program's stdout and stderr to `-Out`, unregisters the task and exits with the program's exit code (124 on timeout). Task 9 uses it to unseal inside an S4U logon.
  - **`ipcam-spike-upload.yml`.**
    - **Triggers.** `push` on `branches: ['**']` and `pull_request`, each with these `paths:`, one per line: `ipcam/hub/**`, `ipcam/tools/spikes/upload/**`, `ipcam/tools/spikes/video/**` (uploadreport imports `redact`), `ipcam/tools/go.mod`, `ipcam/tools/go.sum`, `ipcam/tools/scripts/**`, `ipcam/tools/licenses/**`, `ipcam/go.work`, `ipcam/go.work.sum`, `ipcam/.gitattributes`, `.github/workflows/ipcam-spike-upload.yml`. A workflow comment says that `ipcam-spike-recording.yml` also tests c2's hub packages through its `./hub/...`, accepted double coverage.
    - **Settings.** `permissions: contents: read`; concurrency `ipcam-spike-upload-${{ github.ref }}` with cancel-in-progress; env `GOTOOLCHAIN: local`, `GOFLAGS: -mod=readonly`; actions pinned to the SHAs `ipcam-spike-recording.yml` uses; `setup-go` with `go-version-file: ipcam/go.work`.
    - **Job `upload`** (ubuntu-24.04). Job env `TREES: ./tools/spikes/upload/...`; Tasks 2, 4, 7b, 9 and 10 append their trees in the same commit (P12). Steps: `go build ${TREES}`; `go tool golangci-lint run ${TREES}`; `go test -count=1 -race ${TREES}`; `tools/scripts/check-video-build-list.sh`; govulncheck in `ipcam/tools/spikes/upload` and, from Task 2, in `ipcam/hub`; `tools/scripts/check-licenses.sh ${TREES}`.
    - **Job `windows`** (windows-latest, `defaults.run.shell: bash`, same `TREES`): `go test -count=1 ${TREES}`; `go build ${TREES}`; the S4U smoke step `pwsh -File tools/spikes/upload/ci/s4u-run.ps1 -Exe "$WINDIR/System32/whoami.exe" -Out "$RUNNER_TEMP/s4u.txt"`, which must exit 0 with the runner's user name (`$USERNAME`, case-insensitive) in the file.
    - **Step pattern (c1 G04).** `set -o pipefail` with `tee` into `$RUNNER_TEMP/<step id>.log`, and a last `if: failure()` step that pipes each failed step's failure lines into `tools/scripts/annotate.sh`.
  - **`ipcam-spike-upload-lab.yml` (P09).** `workflow_dispatch`, plus `push` on `branches: ['**']`. Its `paths:` are the list above as Task 1 writes it, with its own workflow file in place of the upload workflow's, plus `ipcam/tools/spikes/recording/**`; a comment says why it differs from the CI list: `build` compiles `reccontroller` and `mi5-lab` runs `recmediagen` from that tree (P13: `paths:` cover every file a job builds). The `docs/research` paths that Task 11 adds to the CI workflow are not added here. Every job has `if: github.event_name == 'workflow_dispatch' || contains(github.event.head_commit.message, '[lab]')`. Concurrency `ipcam-spike-upload-lab-${{ github.ref }}` with `cancel-in-progress: false`. The push trigger registers it on its first push. Job `build` (`timeout-minutes: 30`) builds for linux/amd64 `ipcam-hub` (`./hub/cmd/ipcam-hub`), the fork (`cd hub/third_party/mediamtx && GOWORK=off go build -o "$RUNNER_TEMP/bin/mediamtx" .`) and `reccontroller` (`./tools/spikes/recording/cmd/reccontroller`), and uploads them as the artifact `upload-lab-bin` (retention 1 day). Task 7 adds `fakeproviders` and Task 8 adds `uploadspike` (`./tools/spikes/upload/cmd/uploadspike`) to it; Task 8's `mi5-lab` job reads it in the same run.

- [ ] **Step 0: Preconditions.** `git merge-base --is-ancestor <c1 Task 10 commit> HEAD` exits 0 on this branch, and the merge that brought it is a merge commit. The c1 controller has confirmed that no c1 task holds `go.work` or `ipcam-spikes.yml`, and has agreed to keep `tools/spikes/recording/` until c2's report.
- [ ] **Step 1: Write the failing tests** (`markers_test.go`):
  - `TestEverySourceFileIsMarkedSpikeOnly`: every non-test `.go` file of the module starts with the marker line; at least one file is checked.
  - `TestMarkedNonGoFiles`: every path in `markedNonGoFiles` exists and starts with the marker in its comment syntax.
  - `TestMarkerLine_KnownTypes`: `.go`, `.ts`, `.js`, `.html` and `.ps1` give the lines above; `.txt` gives an error.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/upload/...` — Expected: FAIL (`go.work` does not use the module yet).
- [ ] **Step 3: Implement** the module, `doc.go` (marker first), the script and both workflows; add the module to `go.work`; edit `ipcam-spikes.yml`.
- [ ] **Step 4: Verify.**
  ```bash
  cd ipcam && go build ./tools/spikes/upload/... \
    && go tool golangci-lint run ./tools/spikes/upload/... \
    && go test -count=1 -race ./tools/spikes/upload/... \
    && tools/scripts/check-video-build-list.sh \
    && go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 \
         ../.github/workflows/ipcam-spike-upload.yml ../.github/workflows/ipcam-spike-upload-lab.yml ../.github/workflows/ipcam-spikes.yml
  ```
  Expected: every command exits 0; the build-list check prints `video build list unchanged (<n> modules)` with the same `<n>` it prints on the merged c1 commit. Push: `ipcam-spike-upload` is green (both jobs, the S4U smoke step included) and `ipcam-spikes` does not start for a push that touches only `tools/spikes/upload/`.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add upload spike module, markers test and upload CI"` (PR note: the minimal `ipcam-spikes.yml` path edit).

### Task 2: `storage.Destination`, `Caps`, typed errors and `Digest`

Rulings: P11, P24, P34.

**Files:**
- Create: `ipcam/hub/internal/storage/{doc.go,destination.go,caps.go,errors.go,types.go,digest.go}` and tests `{caps_test.go,errors_test.go,digest_test.go}`
- Create: `ipcam/hub/internal/storage/testdata/quickxor/{vectors.json,SOURCE.md}`
- Modify: `.github/workflows/ipcam-spike-upload.yml` (`TREES` += `./hub ./hub/internal/storage/...` in both jobs, so `hub/spikeonly_test.go` runs too; govulncheck in `ipcam/hub`)

**Interfaces:**
- Produces (§9.1, every method ctx-first):

```go
package storage
type Kind string // KindLocal "LOCAL", "NAS_SMB", KindGoogleDrive "GOOGLE_DRIVE", KindOneDrivePersonal "ONEDRIVE_PERSONAL", "ONEDRIVE_BUSINESS", "S3", "WEBDAV", "SFTP"
type ResumeKind int // ResumeNone (zero value), ResumeSessionURI, ResumeUploadURL, ResumeMultipart, ResumeOffsetWrite, ResumeDavChunked
type LinkKind int   // LinkProxyOnly (zero value), LinkPresigned, LinkPreauthShort, LinkLocalFile
type DeleteKind int // DeleteRecycleBin (zero value), DeletePermanent
type QuotaKind int  // QuotaNone (zero value), QuotaAccount, QuotaFilesystem
type HashKind string // HashMD5 "MD5", HashQuickXor "QUICKXOR", HashSHA256 "SHA256", HashETagMultipart "ETAG_MULTIPART"
type Caps struct {
    Resumable ResumeKind; ChunkAlign, PartSize, MaxRequest int64; EqualParts bool; MaxObject int64
    Playback LinkKind; RangeRead bool; Delete DeleteKind; Quota QuotaKind; Hashes []HashKind
    ServerLifecycle bool; DailyUploadCap int64; CreatesPerSec float64
}
type ObjectKey string
type Meta struct{ ContentType, Class string }
type CallCounts map[string]int // API calls by method name (P18)
type UploadState struct {
    Key ObjectKey; Size int64
    Session string // the provider's session: Drive session URI, Graph uploadUrl. Catalog only; never in an event, log or result
    Offset int64   // last confirmed
    Calls CallCounts
}
type ObjectRef struct{ ID string; Key ObjectKey; Size int64; Hash string; HashKind HashKind } // Commit's provider id, size and provider hash
type ObjectInfo struct{ Ref ObjectRef; ModTimeMs int64 }
type ProbeReport struct{ Writable, RangeRead, Deletable bool; Quota Quota; Warnings []string }
type DeleteOutcome int // DeletedPermanent, DeletedRecycled
type Quota struct{ Total, Used, Free, InBin int64; Unknown bool }
type Destination interface {
    Kind() Kind
    Capabilities() Caps
    Probe(ctx context.Context) (ProbeReport, error)
    BeginUpload(ctx context.Context, key ObjectKey, size int64, meta Meta) (UploadState, error)
    PutChunk(ctx context.Context, st *UploadState, off int64, r io.Reader, n int64) error
    Resume(ctx context.Context, st *UploadState) (nextOffset int64, err error) // detects already-complete sessions
    Commit(ctx context.Context, st *UploadState) (ObjectRef, error)
    Abort(ctx context.Context, st *UploadState) error
    Stat(ctx context.Context, ref ObjectRef) (ObjectInfo, error)
    OpenRange(ctx context.Context, ref ObjectRef, off, n int64) (io.ReadCloser, error)
    PlaybackURL(ctx context.Context, ref ObjectRef, ttl time.Duration) (string, error) // or ErrProxyRequired
    Delete(ctx context.Context, ref ObjectRef) (DeleteOutcome, error)
    Quota(ctx context.Context) (Quota, error)
    PutSmall(ctx context.Context, key ObjectKey, data []byte) (ObjectRef, error)
    List(ctx context.Context, prefix ObjectKey) iter.Seq2[ObjectInfo, error] // reconciliation and import only
}
var ErrAuthExpired, ErrQuotaFull, ErrSessionExpired, ErrNotFound, ErrTransient, ErrPermanent, ErrProxyRequired error
type ErrRateLimited struct{ RetryAfter time.Duration } // drivers return it without sleeping (P26)
type ErrDailyCap struct{ ResetAfter time.Duration }    // Drive: 24 h
const ReasonUnspecified, ReasonAuthExpired, ReasonQuotaFull, ReasonRateLimited = 0, 2100, 2101, 2102 // §7.7
type ReasonInfo struct{ Code int; RetryAfter time.Duration; Retryable bool }
func Reason(err error) ReasonInfo
func Digest(kind HashKind, r io.Reader) (string, error) // MD5 and SHA256: lowercase hex; QUICKXOR: standard base64 of the 20-byte hash; ETAG_MULTIPART: error
```

- [ ] **Step 1: Write the failing tests.**
  - `TestCaps_ZeroValueIsConservative`: `Caps{}` has `Resumable == ResumeNone`, `Delete == DeleteRecycleBin`, `Playback == LinkProxyOnly`, `Quota == QuotaNone` and empty `Hashes`.
  - `TestErrors_MapToReasons` (table, each error also wrapped once with `%w`):
    - `ErrAuthExpired` → `{2100, 0, false}`; `ErrQuotaFull` → `{2101, 0, false}`;
    - `ErrRateLimited{RetryAfter: 30 * time.Second}` → `{2102, 30 s, true}`; `ErrDailyCap{ResetAfter: 24 * time.Hour}` → `{2102, 24 h, true}`;
    - `ErrTransient` → `{0, 0, true}` (a Faza 1 Connect mapping gives it `unavailable`, CTR-14);
    - `ErrNotFound`, `ErrSessionExpired`, `ErrPermanent`, `ErrProxyRequired` → `{0, 0, false}`.
  - `TestErrors_ProxyRequiredIs`: `errors.Is(fmt.Errorf("x: %w", ErrProxyRequired), ErrProxyRequired)`.
  - `TestDigest_FixedVectors`: MD5 of `""` = `d41d8cd98f00b204e9800998ecf8427e`; MD5 of `abc` = `900150983cd24fb0d6963f7d28e17f72`; SHA256 of `abc` = `ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad`; every entry of `testdata/quickxor/vectors.json` gives its expected QUICKXOR value. The vectors come from rclone's `backend/onedrive/quickxorhash` tests (MIT, an independent implementation); `SOURCE.md` names the repository, the file path, the commit and the license.
  - `TestDigest_QuickXorStreamingEqualsWhole`: 10 MiB + 7 seeded random bytes read in 1-byte, 320 KiB and 7 MiB pieces give the same QUICKXOR digest.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/storage/...` — Expected: FAIL (`undefined: Caps`, …).
- [ ] **Step 3: Implement** the types, errors, `Reason` and `Digest`; no driver yet.
- [ ] **Step 4: Run** the same command with `-race` — Expected: PASS; `ipcam-spike-upload` green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add storage Destination interface, Caps, typed errors and Digest"`

### Task 3: Local-disk driver

Rulings: P24 (golden Caps), P32 (quota per OS).

**Files:**
- Create: `ipcam/hub/internal/storage/local/{local.go,quota_unix.go,quota_windows.go}` and tests `{local_test.go,quota_unix_test.go,quota_windows_test.go}`

**Interfaces:**
- Consumes: Task 2.
- Produces: `func New(root string) (*Local, error)`, a `Destination` with `Kind() == KindLocal`.
  - **Writes.** `<root>/<key>.part` written with `WriteAt` + fsync per chunk; `Commit` fsyncs, renames to `<root>/<key>` (Windows: `MoveFileEx(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)`), syncs the directory (non-Windows) and returns the SHA-256 of the final file as `ObjectRef.Hash`. `Resume` returns `Size` when the final file exists with `Size` bytes (complete), else the `.part` size. `PutSmall` writes `.tmp` then renames.
  - **Golden Caps.** `Resumable: ResumeOffsetWrite`, `PartSize: 8 MiB`, `Hashes: [HashSHA256]`, `Delete: DeletePermanent`, `Quota: QuotaFilesystem`, `Playback: LinkLocalFile`, `RangeRead: true`; every other field zero.
  - **Quota.** `quota_unix.go`: `statfs` `Bavail × Bsize` as `Free` (never `Bfree`), `Blocks × Bsize` as `Total`. `quota_windows.go`: `GetDiskFreeSpaceExW` with `lpFreeBytesAvailableToCaller` as `Free` and `lpTotalNumberOfBytes` as `Total`.
  - **Role.** It is a destination like any other and the conformance baseline; the spool is `ipcam-hub`'s `<dataDir>/rec/` (P14).

- [ ] **Step 1: Write the failing tests.**
  - `TestLocal_CapsGolden`: `Capabilities()` equals the golden value above.
  - `TestLocal_WriteVerifyRange`: an upload of 20 MiB + 3 bytes commits; `Stat` gives its size and SHA-256; `OpenRange(ref, 10 MiB − 5, 4096)` returns exactly those bytes.
  - `TestLocal_ResumeAfterPartialWrite`: a `.part` left at 3 MiB + 5 bytes resumes at 3 MiB + 5, not 0.
  - `TestLocal_ResumeDetectsComplete`: after `Commit`, `Resume` on the old state returns `Size`.
  - `TestLocal_Quota`: `Free` and `Total` are each within 1 % of an independent reading (Linux: `df -B1 --output=size,avail <root>`; Windows: PowerShell `[System.IO.DriveInfo]::new('<root>')` `TotalSize` and `AvailableFreeSpace`). After writing 64 MiB of random bytes with fsync, `Free` drops by 32–256 MiB.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/storage/local/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS here and in CI's `upload` and `windows` jobs.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add local disk storage driver"`

### Task 4: Catalog upload tables and `outbox.Queue`

Rulings: P03 (the migration, `Placement` in catalog, `WithTx`), P11, P25, P26, P34.

**Files:**
- Modify: `ipcam/hub/internal/catalog/{catalog.go,catalog_test.go}` (announce the edit to the c1 controller)
- Create: `ipcam/hub/internal/outbox/{queue.go,worker.go,backoff.go,state.go,verify.go,key.go}` and tests `{queue_test.go,backoff_test.go,state_test.go,verify_test.go,key_test.go,kill_test.go,fakedest_test.go}`
- Modify: `.github/workflows/ipcam-spike-upload.yml` (`TREES` += `./hub/internal/outbox/... ./hub/internal/catalog/...`)

**Interfaces:**
- Consumes: Tasks 2–3; c1's `catalog.DB`, `Segment`.
- Produces:
  - **One migration** appended as the next element of `migrations`, §4.6 L552 and L557. Its number is `len(migrations)+1` on the merged c1 tip (3 at c1's HEAD today; c1 Tasks 7–10 may add one), and comments and tests name it by position, never by a fixed number:

    ```sql
    CREATE TABLE placements (
      id TEXT PRIMARY KEY,
      object_kind TEXT NOT NULL CHECK (object_kind IN ('segment')),
      object_id TEXT NOT NULL, destination_id TEXT NOT NULL,
      class TEXT NOT NULL CHECK (class IN ('EVENT_CLIP','THUMBNAIL','CONTINUOUS','EXPORT')),
      rule_ids TEXT NOT NULL DEFAULT '', encrypt INTEGER NOT NULL, retain_until_ms INTEGER NOT NULL DEFAULT 0,
      state TEXT NOT NULL CHECK (state IN ('pending','uploading','verified','failed','deleted')),
      remote_ref TEXT NOT NULL DEFAULT '', remote_hash TEXT NOT NULL DEFAULT '',
      bytes INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
      UNIQUE (object_id, destination_id));
    CREATE INDEX placements_queue ON placements (state, next_attempt_at_ms);
    CREATE TABLE upload_sessions (placement_id TEXT PRIMARY KEY REFERENCES placements(id), state_json BLOB NOT NULL, offset INTEGER NOT NULL);
    CREATE TABLE secrets (id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('camera','oauth','s3','webdav','sftp','smb')), sealed_blob BLOB NOT NULL, sealer TEXT NOT NULL);
    CREATE TABLE remote_folders (destination_id TEXT NOT NULL, path TEXT NOT NULL, remote_id TEXT NOT NULL, PRIMARY KEY (destination_id, path));
    ```

    All four tables are in this one migration, because P03 allows one `user_version` series and Task 4 is their first user. Task 7b adds the folder accessors and Task 9 the secrets accessors.
  - **Catalog API** (catalog never imports `outbox` or `storage`):

    ```go
    type Placement struct {
        ID, ObjectKind, ObjectID, DestinationID, Class, RuleIDs string
        Encrypt bool; RetainUntilMs int64
        State, RemoteRef, RemoteHash string
        Bytes int64; Attempts int; NextAttemptAtMs int64
    }
    func (db *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error
    func PutPlacementTx(ctx context.Context, tx *sql.Tx, p Placement) (superseded bool, err error) // upsert on (object_id, destination_id)
    func (db *DB) DuePlacements(ctx context.Context, nowMs int64, destIDs []string, limit int) ([]Placement, error) // only these destinations' rows
    func (db *DB) UpdatePlacement(ctx context.Context, p Placement) error
    func (db *DB) Placements(ctx context.Context) ([]Placement, error)
    func (db *DB) SegmentByID(ctx context.Context, id string) (Segment, error)
    func (db *DB) PutUploadSession(ctx context.Context, placementID string, stateJSON []byte, offset int64) error
    func (db *DB) UploadSession(ctx context.Context, placementID string) (stateJSON []byte, offset int64, ok bool, err error)
    func (db *DB) DeleteUploadSession(ctx context.Context, placementID string) error
    ```

    - `PutPlacementTx`: a new pair inserts `pending`. An existing pair that is not `verified` and has other `Bytes` is reset to `pending` with `attempts = 0`, its `upload_sessions` row deleted, and `superseded = true`. An existing `verified` pair stays unchanged: a verified copy cannot be replaced yet (P03), and the report's «Limitări» says so.
    - `DuePlacements`: `destination_id IN (destIDs)`, and `state = 'pending' AND next_attempt_at_ms ≤ now` or `state = 'failed' AND next_attempt_at_ms BETWEEN 1 AND now`; ordered by class (`EVENT_CLIP` and `THUMBNAIL`, then `CONTINUOUS`, then `EXPORT`), then the segment's `start_ms`, then `id` (§9.7).
  - **Outbox:**

    ```go
    package outbox
    type Clock interface{ Now() time.Time; After(d time.Duration) <-chan time.Time }
    type Options struct {
        Clock Clock; Rand func() float64 /* [0,1) */; Workers int /* default 2 */
        RecDir string            // the spool, ipcam-hub's <dataDir>/rec
        HubID, HubName string    // object keys (§9.10)
        Observer Observer
        QuotaEvery time.Duration // default 15 min (§9.8 step 4)
    }
    type DestState string // §9.3 L1510: StateOK "OK", StateBlockedAuth "BLOCKED_AUTH", StateQuotaFull "QUOTA_FULL", StatePausedDailyCap "PAUSED_DAILY_CAP", StateUnreachable "UNREACHABLE"
    type Observer interface {
        Queued(p catalog.Placement) // when a worker first reads p from DuePlacements, so only committed placements are reported
        Resumed(p catalog.Placement, offset int64)
        Progress(p catalog.Placement, confirmedOffset, size int64)
        Verified(p catalog.Placement, ref storage.ObjectRef, calls storage.CallCounts)
        Failed(p catalog.Placement, reason string, attempts int)
        DestState(destID string, s DestState, alert string)
    }
    type Queue struct{ /* … */ }
    func New(cat *catalog.DB, dests map[string]storage.Destination, opts Options) *Queue
    func (q *Queue) Enqueue(ctx context.Context, tx *sql.Tx, p catalog.Placement) error // writes the row in the caller's transaction (S3)
    func (q *Queue) Run(ctx context.Context) error // workers; polls DuePlacements every 1 s and on Enqueue
    func (q *Queue) Unblock(destID string)          // a new Complete for destID's token: BLOCKED_AUTH → OK
    func (q *Queue) State(destID string) DestState
    func Backoff(n int, r float64) time.Duration    // 1 s + r·(min(5 min, 1 s·2^n) − 1 s)
    func KeyFor(hubID, hubName string, s catalog.Segment, class string) storage.ObjectKey
    func PlacementFor(s catalog.Segment, destID, class string) catalog.Placement
    ```

  - **Worker.** For a due placement: emit `Queued` the first time it is read; mark it `uploading`; read the segment row; resume from its `upload_sessions` row with `Resume` (emit `Resumed` when > 0) or `BeginUpload`; read the spool file `RecDir/<segment path>` with `ReadAt` in chunks of `Caps.PartSize`; after each confirmed chunk persist the session and emit `Progress`; `Commit`; then `verified`, or `failed`/`pending` with its next attempt time.
  - **States (§4.6, P25).** `pending` → `uploading` (a worker took it) → `verified` | `failed` (with `next_attempt_at_ms`, due again) | `pending` (a destination state paused it). `failed` with `next_attempt_at_ms = 0` is never due again. `deleted` is set only by `diag dest-probe --delete-segment`. `Run` resets every `uploading` row of a configured destination to `pending`, keeping `attempts` and its `upload_sessions` row, before its first poll: a row left `uploading` by a kill is due again, and the worker resumes it from the session.
  - **Unconfigured destinations.** `Run` passes the keys of `dests` to `DuePlacements`, and resets, reads and updates only their rows. A placement whose destination is not in `dests` stays in the catalog untouched, whatever its state, until a queue that has its destination runs, and it is never an error of `Run`. An MI5 run cut short leaves such rows when its destinations leave the hub's composition (Task 9).
  - **Verification (P34).** Before marking `verified`, the outbox computes `storage.Digest(Caps.Hashes[0])` over the byte stream it uploaded (the spool file in c2) and compares digest and size with `Commit`'s `ObjectRef`. Drivers do not verify. On a mismatch: delete the remote object, mark the placement `failed` with `attempts + 1`, and re-upload from 0 after `Backoff`, at most 3 times; after the third mismatch it stays `failed` (`next_attempt_at_ms = 0`). `ErrPermanent` → `failed`, never due again.
  - **Destination states (P26, §9.7–§9.9).**
    - `ErrAuthExpired` → `BLOCKED_AUTH` until `Unblock(destID)`; alert «Reconectează Google Drive» or «Reconectează OneDrive».
    - `ErrQuotaFull`, or a quota read with `Used ≥ 0.98 × Total` (every `QuotaEvery`, and before any upload over 1 GiB) → `QUOTA_FULL`; every class uses `PAUSE_KEEP_LOCAL` (no rules engine in c2; `DELETE_OLDEST` and `ALERT_ONLY` are Faza 1). The alert is «Spațiul se eliberează după golirea coșului (max. 30 de zile).» only when `Caps.Delete == DeleteRecycleBin` and `Quota.InBin > 0`, else «Destinația e plină; copiile rămân pe hub.». A later read below 98 % → `OK`.
    - `ErrDailyCap{ResetAfter}` → `PAUSED_DAILY_CAP` for `ResetAfter` (alert «Plafonul zilnic Google Drive a fost atins; urcarea se reia peste 24 h.»), then one probe upload: success → `OK`, another `ErrDailyCap` → paused again.
    - `ErrTransient` (network, 5xx, and a token endpoint that does not answer, Task 9) → backoff; while the backoff sits at its 5-min cap (consecutive failures `n ≥ 9`) the destination shows `UNREACHABLE` (alert «Destinația nu răspunde; se reîncearcă.»), keeps retrying and returns to `OK` on the first success, with no action.
    - `ErrRateLimited{RetryAfter}` → that destination waits `max(Backoff(n, r), RetryAfter)`; the others continue.
    - `ErrSessionExpired` → `Abort` on the old session (best effort), then a new session from 0; it never leaves the outbox.
  - **Keys (§9.10).** Encrypted: `e/<hubID>/<cam>/<YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>_<kind>_<profile>.mp4.age`. Clear: `<hubName>/<cam>/<YYYY-MM-DD>/<…>_<kind>_<profile>.mp4`. `<kind>` is `eveniment` for `EVENT_CLIP`, `cont` for `CONTINUOUS`; times are UTC from `StartMs`; characters forbidden on Windows/SMB are removed.

- [ ] **Step 1: Write the failing tests.** `fakedest_test.go` is a fake `Destination` with injectable faults per call, call counts and an object map; the kill test serves it over HTTP from the parent test.
  - `TestCatalog_MigrationAddsUploadTables`: a catalog created by c1's code (at c1's last migration, `len(migrations)−1`) with segment and gap rows migrates to `len(migrations)`, keeps its rows, and has the four tables.
  - `TestCatalog_PutPlacementTx_SupersedesUnverified` and `TestCatalog_PutPlacementTx_KeepsVerified`.
  - `TestCatalog_DuePlacementsOrder`: an `EVENT_CLIP` segment starting 10:05 comes before a `CONTINUOUS` one starting 10:00, which comes before an `EXPORT` from 09:00; within a class the oldest comes first.
  - `TestCatalog_DuePlacementsOnlyNamedDestinations`: due rows of `dst-a` and `dst-gone`, asked for `[dst-a]`, give only `dst-a`'s.
  - `TestOutbox_UnconfiguredDestinationUntouched`: a catalog with `pending`, due `failed` and `uploading` rows of `dst-gone`, and a queue whose `dests` hold only `dst-a`. Over 10 min of fake clock `Run` keeps running and returns only when its context ends; `dst-gone`'s rows are unchanged field for field and get no Observer call; `dst-a`'s placements are verified meanwhile.
  - `TestOutbox_EnqueueInSameTx`: rolling back the segment's transaction also rolls back the placement.
  - `TestOutbox_ResumesFromLastOffset` (P25): a helper subprocess (the test binary with `OUTBOX_KILL_HELPER=1`) runs the queue against the HTTP-served fake. The fake blocks chunk K+1 once the helper reports offset K persisted. The parent kills the helper (`Process.Kill`: SIGKILL, or TerminateProcess on Windows) and reopens the catalog in a new helper. Asserted: the killed helper left the placement `uploading`, and the new helper's `Run` resets it to `pending` before its first poll; the first `PutChunk` after the restart is at offset K, exactly `size − K` bytes follow, there is one `Commit`, and one object exists.
  - `TestOutbox_Backoff`: with `r = 0`, `Backoff(n, 0) == 1 s` for n = 0…20. With `r = 0.999999`, n = 1…8 give 2^n s ± 1 ms and n ≥ 9 gives 300 s ± 1 ms. 50 delays from `math/rand/v2` PCG(1, 2) all lie in [1 s, 300 s] and are not all equal.
  - `TestOutbox_VerifyHashAndSize` (P34): a provider hash mismatch keeps the placement `failed`, never `verified`; a mismatch followed by a good retry leaves exactly one object and one `Delete` call; three mismatches leave it `failed` with no further attempt.
  - `TestOutbox_RetryAfterHonoured`: `ErrRateLimited{RetryAfter: 90 s}` at n = 1 → the next attempt is 90 s later, not 2 s.
  - `TestOutbox_RateLimitIsPerDestination`: `dst-a` rate-limited for 10 min; `dst-b`'s placements are verified meanwhile.
  - `TestOutbox_DailyCapPauses24h`: `PAUSED_DAILY_CAP`, no `PutChunk` for 24 h (fake clock), then one probe; success → `OK`.
  - `TestOutbox_SessionExpiredRestartsFrom0`: one `Abort` on the old session, a new `BeginUpload`, first `PutChunk` at 0.
  - `TestOutbox_AuthExpiredBlocks`: `BLOCKED_AUTH` with its alert; no chunk until `Unblock`; then verified.
  - `TestOutbox_QuotaFullKeepsLocal`: a 15-min read at 98.5 % → `QUOTA_FULL`, placements stay `pending`, the spool file is untouched; a read at 97 % → `OK`. A 1.5 GiB upload triggers a read before it starts.
  - `TestOutbox_RecycleQuota_PausesKeepLocal` (Review Focus 4): a `DeleteRecycleBin` destination with `InBin > 0` at 98.5 % → `QUOTA_FULL`, placements pending locally, alert = the bin text, **0 `Delete` calls**; with `InBin == 0` the alert is the generic text.
  - `TestOutbox_OutageRecoversWithoutAction`: fake clock, 6 h of `ErrTransient` then success → every placement verified, the destination `OK`, `UNREACHABLE` entered once at n = 9 and left on the first success, `Unblock` never called.
  - `TestOutbox_ChangedBytesSupersedePending`: `Enqueue` with new bytes while a placement uploads → its session is aborted, it restarts from 0, and one object exists.
  - `TestKeyFor_Deterministic`: an encrypted `cam01/main` CONTINUOUS segment starting 2026-10-17T08:15:00Z with hub ID `H` → `e/H/cam01/2026-10-17/20261017T081500Z_cont_main.mp4.age`; the clear EVENT_CLIP one with hub name `hub` → `hub/cam01/2026-10-17/20261017T081500Z_eveniment_main.mp4`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/catalog/... ./hub/internal/outbox/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** Tell the c1 controller about the `catalog.go` edit.
- [ ] **Step 4: Run** with `-race`, plus `go test -count=1 ./hub/...` (c1's recorder tests still pass) — Expected: PASS here and in both CI jobs.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add catalog upload tables and outbox queue with destination states"`

### Task 5: Google Drive driver (own REST client) and its fake

Rulings: P04, P05, P18, P24, P27 (token input), P35, P45 (minimisation).

**Files:**
- Create: `ipcam/hub/internal/storage/gdrive/{gdrive.go,rest.go,folders.go,errors.go,calllog.go}` and `gdrive_test.go`
- Create: `ipcam/hub/internal/storage/fakes/gdrive/{fake.go,fake_test.go}` (`package fakegdrive`)
- Modify: `ipcam/hub/{go.mod,go.sum}` (`golang.org/x/oauth2 v0.37.0`; single writer); `ipcam/hub/third_party/mediamtx/{go.mod,go.sum}` if `GOWORK=off go mod tidy` changes them

**Interfaces:**
- Consumes: Task 2; `oauth2.TokenSource`.
- Produces:

```go
package gdrive
type FolderStore interface { // catalog.DB implements it (Task 7b)
    Folder(ctx context.Context, destID, path string) (remoteID string, ok bool, err error)
    PutFolder(ctx context.Context, destID, path, remoteID string) error
}
type Options struct {
    DestID string
    HTTPClient *http.Client // plain; the driver sets "Authorization: Bearer" on each request from Tokens
    Tokens oauth2.TokenSource
    BaseURL, UploadBaseURL string // defaults "https://www.googleapis.com"
    PartSize int64 // 0: 16 MiB; another value only with BaseURL set (the fakes), a multiple of 256 KiB; Capabilities() reports it
    Clock func() time.Time
    Folders FolderStore
    HubID, RootName string // RootName default "ipcam"
}
func New(opts Options) (*Drive, error) // a Destination with Kind() == KindGoogleDrive
func (d *Drive) CallLog() storage.CallCounts // totals since New
```

- **REST calls (P05).** Resumable `POST {UploadBaseURL}/upload/drive/v3/files?uploadType=resumable` (`files.create.resumable`); chunk `PUT` with `Content-Range` (`upload.put`); status `PUT` with `Content-Range: bytes */<size>` (`upload.status`: 308 with `Range: bytes=0-<K−1>`, or 200/201 when complete); `GET /drive/v3/files/<id>?fields=id,md5Checksum,size` (`files.get`); `GET …?alt=media` with `Range` (`files.get.media`); `DELETE` (`files.delete`); `GET /drive/v3/about?fields=storageQuota` (`about.get`, never `user`); `GET /drive/v3/files/generateIds?count=1&space=drive&type=files` (`files.generateIds`); folder and small-file `files.create`; `files.list` only inside `List`. `UploadState.Calls` counts one upload's calls; `CallLog` counts all.
- **Timeouts (P26).** `ipcam-hub` builds `HTTPClient` with a 10 s dial and TLS-handshake timeout and a 60 s response-header timeout, and the driver gives each metadata call a 30 s deadline and each chunk `PUT` 10 min. A dropped route then ends a call as `ErrTransient` instead of hanging a worker.
- **Golden Caps.** `Resumable: ResumeSessionURI`, `ChunkAlign: 256 KiB`, `PartSize: 16 MiB`, `Playback: LinkProxyOnly`, `RangeRead: true`, `Delete: DeletePermanent`, `Quota: QuotaAccount`, `Hashes: [HashMD5]`, `DailyUploadCap: 750 GiB`, `CreatesPerSec: 2`; every other field zero. `PlaybackURL` returns `ErrProxyRequired`.
- **Creates ≤ 2/s (STO-07).** Resumable POSTs, `PutSmall` and folder creates take a slot from a 2-per-second bucket on `Clock`; without a slot the driver returns `ErrRateLimited{RetryAfter: <time to the next slot>}` without calling the API or sleeping.
- **Error mapping.** 401 → `ErrAuthExpired`; 403 `storageQuotaExceeded` → `ErrQuotaFull`; 403 `userRateLimitExceeded` or `rateLimitExceeded`, and 429 → `ErrRateLimited{RetryAfter}` from `Retry-After` (0 when absent); 403 `dailyLimitExceeded` or `uploadLimitExceeded` → `ErrDailyCap{ResetAfter: 24 h}`; 404 or 410 on a session URI → `ErrSessionExpired`; 404 on a file → `ErrNotFound`; 5xx and network errors → `ErrTransient`; any other 4xx → `ErrPermanent`. A token error from `Tokens` that wraps a `storage` error passes through (Task 9: `ErrAuthExpired` for `invalid_grant`, `ErrTransient` for a token endpoint that does not answer); any other token error is `ErrTransient`, never `ErrPermanent`. The reason names follow Drive's error reference; the live run reports any other reason as `ErrPermanent` in its `upload_failed` event.
- **Folders (P35, §9.10).** The root `ipcam` in My Drive, `.ipcam/<hubID>`, `.ipcam/<hubID>/catalog/days`, and each camera and day level. For each: `Folders.Folder` hit → use it; miss → `files.generateIds`, then `PutFolder` (persisted **before** create), then `files.create` with that ID; a 409 "already exists" is success. A cached ID whose parent returns 404 on upload is created again with the same ID. Probe confirms the expected 409 against the live API.
- **Probe (§8.7 step 6).** Ensure the root; `PutSmall` `.ipcam/<hubID>/README.txt` with the fixed text «Acest folder conține înregistrările unui hub ipcam. Fișierele .age sunt criptate cu age (https://age-encryption.org) și se deschid cu: age -d -i <cheia de recuperare> <fișier>.mp4.age > <fișier>.mp4»; a 1 MiB write, a range read, a delete, a quota read.
- **Fake (`fakegdrive`).** `New(opts) *Fake` with `Handler() http.Handler`, `Counts() map[string]int`, `Objects() []Object`, `SetQuota(total, used int64)`, `FailNext(method string, status int, reason string, retryAfter time.Duration)`, `DailyCapAfter(bytes int64)`, `AcceptToken(func(string) bool)`; resumable semantics with 308 and `Range`, `md5Checksum` of the stored bytes, `generateIds`, 409 on a reused ID, per-method counters, and a per-second create meter. Task 9 adds per-client `drive.file` scoping.

- [ ] **Step 1: Write the failing tests** (against `fakegdrive` over `httptest`):
  - `TestDrive_CapsGolden`.
  - `TestDrive_ResumableUploadAndVerify`: a 40 MiB + 1 byte upload interrupted after the first chunk resumes through `upload.status` at 16 MiB and commits once; `Commit`'s hash equals `storage.Digest(HashMD5, …)` and the fake's `md5Checksum`.
  - `TestDrive_ResumeDetectsCompleteSession`: a status query answered 200 → `Resume` returns `Size`, and no second object exists.
  - `TestDrive_ZeroListInNormalOperation` (Review Focus 3, STO-07): `Probe` once, then 10 uploads, verification and an `OpenRange` → `files.list` count 0; 10 `BeginUpload` calls at one fake-clock instant → 2 reach the fake and 8 return `ErrRateLimited` with `RetryAfter ≤ 1 s`; the fake never sees more than 2 creates in any 1-s window.
  - `TestDrive_RestartReusesFolders_ZeroList` (Review Focus 3): a second `Drive` on the same `FolderStore` uploads into the same day folder with 0 `files.list` and 0 folder creates.
  - `TestDrive_FolderCreateIsIdempotent`: a stored ID with no folder at the fake → one create with that ID; a stored ID whose folder exists → the 409 counts as success; both leave exactly one folder.
  - `TestDrive_DailyCapPauses`: a 403 `dailyLimitExceeded` → `ErrDailyCap{ResetAfter: 24 h}`.
  - `TestDrive_QuotaUnitsRecorded` (P18, now a call-count test): one 40 MiB upload gives `UploadState.Calls == {"files.create.resumable":1, "upload.put":3, "files.get":1}`, and `CallLog()` equals the fake's counts.
  - `TestDrive_ErrorMapping`: one row per mapping above, including a reset connection → `ErrTransient`, `Retry-After: 7` → `RetryAfter == 7 s`, a token error wrapping `ErrTransient` passed through, and a plain token error → `ErrTransient`.
  - `TestDrive_PartSizeOverrideOnlyWithBaseURL`: `PartSize` 512 KiB with `BaseURL` set → `Capabilities().PartSize` is 512 KiB and a 1.9 MB upload sends 4 chunks; `PartSize` without `BaseURL`, or not a multiple of 256 KiB, makes `New` fail.
  - `TestDrive_BearerFromTokenSourceOnPlainClient`: every request carries `Bearer <tok>` from `Tokens`; a token changed between two calls is used on the next call; `HTTPClient` is never wrapped by `oauth2.NewClient`.
  - `TestDrive_AboutAsksStorageQuotaOnly`: the `about.get` query is exactly `fields=storageQuota`.
  - `TestDrive_Probe`: root created and remembered, README written with the fixed text, 1 MiB written, read, deleted, quota read.
  - `TestDrive_PlaybackURLIsProxyRequired`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/storage/gdrive/... ./hub/internal/storage/fakes/gdrive/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** `GOWORK=off go mod tidy` in `ipcam/hub` and `ipcam/hub/third_party/mediamtx`.
- [ ] **Step 4: Run** with `-race`, then `tools/scripts/check-video-build-list.sh` — Expected: PASS, `video build list unchanged`; both CI jobs green, and `ipcam-spike-recording` green on the same push (it builds the fork).
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add Google Drive storage driver and fake"`

### Task 6: OneDrive personal driver and `fake-graph`

Rulings: P04, P24, P26 (driver facts only), P35, P36, P45, C2-R4, P54.

**Files:**
- Create: `ipcam/hub/internal/storage/onedrive/{onedrive.go,graph.go,errors.go}` and `onedrive_test.go`
- Create: `ipcam/hub/internal/storage/fakes/graph/{fake.go,quickxor.go,fake_test.go}` (`package fakegraph`; its QuickXor is its own copy, not `storage.Digest`)

**Interfaces:**
- Consumes: Task 2.
- Produces: `func New(opts Options) (*OneDrive, error)` with `Options{DestID, HubID string; HTTPClient *http.Client; Tokens oauth2.TokenSource; BaseURL string /* default "https://graph.microsoft.com" */; PartSize int64 /* 0: 10 MiB; another value only with BaseURL set, a multiple of 320 KiB */; Clock func() time.Time}`, a `Destination` with `Kind() == KindOneDrivePersonal`. Token errors and timeouts follow Task 5.
  - **Account (P36).** Authority `consumers`; scope `Files.ReadWrite.AppFolder offline_access`; root `/special/approot`, which Probe uses and creates nothing in.
  - **Graph calls.** `POST {BaseURL}/v1.0/me/drive/special/approot:/<key>:/createUploadSession` with the body `{"item":{"@microsoft.graph.conflictBehavior":"replace"}}`, so a new session replaces a partial or earlier copy; fragment `PUT`s on `uploadUrl` with `Content-Range` through the **plain** `HTTPClient` and **no** `Authorization` header; resume with `GET uploadUrl` (`nextExpectedRanges`). Graph deletes a session once it completes, so a 404 on `GET uploadUrl` is followed by `GET /v1.0/me/drive/special/approot:/<key>?$select=id,size,file`: an item of `Size` bytes means the upload completed, `Resume` returns `Size` and `Commit` returns that item's id, size and `quickXorHash` (the outbox's hash check decides, P34); no such item → `ErrSessionExpired`; the last fragment's 200/201 item gives `id`, `size` and `file.hashes.quickXorHash`; `GET /v1.0/me/drive/items/<id>?$select=id,size,file`; content `GET …/content` with `Range`; `DELETE /v1.0/me/drive/items/<id>` → `DeletedRecycled`; `GET /v1.0/me/drive?$select=quota` (`remaining` → `Free`, `deleted` → `InBin`, `total`, `used`).
  - **Golden Caps.** `Resumable: ResumeUploadURL`, `ChunkAlign: 320 KiB`, `PartSize: 10 MiB`, `MaxRequest: 60 MiB − 1 B`, `Playback: LinkPreauthShort`, `RangeRead: true`, `Delete: DeleteRecycleBin`, `Quota: QuotaAccount`, `Hashes: [HashQuickXor]`; every other field zero.
  - **Errors.** 401 → `ErrAuthExpired`; 507 or `quotaLimitReached` → `ErrQuotaFull`; 429 → `ErrRateLimited{RetryAfter}` from `Retry-After`, without sleeping; 404 on `uploadUrl` with no complete item at the key → `ErrSessionExpired`; 404 on an item → `ErrNotFound`; 5xx and network → `ErrTransient`; other 4xx → `ErrPermanent`.
  - **Fake (`fakegraph`).** Strict: 320 KiB multiples, < 60 MiB, in order, 401 when `Authorization` is present on `uploadUrl`, 404 on a completed session's `uploadUrl` (as Graph), `conflictBehavior` honoured; `Retry-After` injection; a recycle bin that occupies quota, with `EmptyBin()` for tests; `$select` enforcement.
  - **Live runs (C2-R4).** Faza 0-bis by default; they run only if A5's check passes. The driver, the fake and their tests run regardless. Task 4's `TestOutbox_RecycleQuota_PausesKeepLocal` covers the bin policy; STO-13's retention loop is tested in Faza 1, as the report's «Limitări» says.

- [ ] **Step 1: Write the failing tests** (against `fakegraph`):
  - `TestOneDrive_CapsGolden`.
  - `TestOneDrive_FragmentsAndNoAuthHeader` (STO-08): a 25 MiB upload sends fragments that are multiples of 320 KiB and < 60 MiB, in order, with no `Authorization` on `uploadUrl`; a 429 with `Retry-After: 12` returns `ErrRateLimited{RetryAfter: 12 s}` at once.
  - `TestOneDrive_PlainClientOnUploadURL` (P27): the driver built as `ipcam-hub` builds it (plain client plus TokenSource) uploads; a variant whose client adds the bearer to every request gets the strict fake's 401 on `uploadUrl`.
  - `TestOneDrive_ResumeFromNextExpectedRanges`: a cut after two fragments resumes at 2 × 10 MiB.
  - `TestOneDrive_ResumeDetectsCompletedSessionAfter404`: after the last fragment, `GET uploadUrl` is 404 and `Resume` returns `Size` from the item at the key, with no new session; a 404 with no item there → `ErrSessionExpired`; the session request carries `conflictBehavior: replace`.
  - `TestOneDrive_QuickXorVerify`: `Commit`'s `quickXorHash` and size equal `storage.Digest(HashQuickXor, …)` of the bytes.
  - `TestOneDrive_DeleteIsRecycled_QuotaInBin`: `Delete` → `DeletedRecycled`; `Quota().InBin` grows by the object's size and `Free` does not; after `EmptyBin()`, `InBin == 0` and `Free` grows.
  - `TestOneDrive_QuotaFull`: 507 → `ErrQuotaFull`.
  - `TestOneDrive_QuotaSelectOnly`: the quota request is exactly `$select=quota`.
  - `TestOneDrive_Probe`: README at `.ipcam/<HubID>/README.txt` under approot, 1 MiB write, range read, delete, quota read; no folder create call.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/storage/onedrive/... ./hub/internal/storage/fakes/graph/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** with `-race` — Expected: PASS; CI green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add OneDrive personal storage driver and fake"`

### Task 7: Storage conformance suite (STO-01) and `fakeproviders`

Rulings: P04, P24 (resume checks by name), P28, P57.

**Files:**
- Create: `ipcam/hub/internal/storage/conformance/{suite.go,suite_test.go}`
- Create: `ipcam/hub/internal/storage/fakes/cmd/fakeproviders/{main.go,main_test.go}`
- Modify: `.github/workflows/ipcam-spike-upload-lab.yml` (the `build` job also builds `fakeproviders`)

**Interfaces:**
- Consumes: Tasks 3, 5, 6.
- Produces:

```go
package conformance
type Options struct{ KillOffsets int /* 20 in CI */; Seed int64 }
type Report struct{ Ran, Skipped []string } // subtest names, e.g. "Resume/KillAtRandomOffset"
func Run(t *testing.T, newDest func(t *testing.T) storage.Destination, opts Options) Report
```

- **Subtests, by name.**
  - `Resume/KillAtRandomOffset`: `KillOffsets` offsets drawn from `Seed`. Each upload stops after the chunk that confirms offset K; the test keeps only the `UploadState` JSON, drops the `Destination`, builds a new one with `newDest`, resumes (`Resume ≥ K`), finishes and commits once. One object exists under the key, and `Digest(Caps.Hashes[0])` and the size equal `Commit`'s.
  - `Resume/CompleteSessionDetected`: `Resume` after `Commit` returns `Size` and creates nothing.
  - `RangeRead`: three ranges, the last ending at the object's last byte.
  - `Delete`: `DeletePermanent` → `Stat` gives `ErrNotFound`; `DeleteRecycleBin` → `DeletedRecycled` and `Quota().InBin` grows.
  - `Quota`: `QuotaAccount` → `Total ≥ Used` and `Free ≥ 0`; `QuotaFilesystem` → `Free > 0`.
  - `Caps/ResumableNotNone`: fails if `Resumable == ResumeNone`.
- **Nightly (P28)**, as `suite.go`'s doc comment and Task 11's Faza 0-bis row say: "Nightly runs on real test accounts are not built or run here (§14.5): Drive after the project moves to Production (OWN-07, before Faza 3); OneDrive personal is free when the registration is free (OWN-08) and is scheduled once the weekly secret rotation exists (OWN-23, Faza 1); OneDrive business (the CA test tenant, OWN-09), B2, R2, Wasabi and AWS SSE-KMS are Faza 0-bis (cost, §16.9)."
- **`fakeproviders -listen <ip:port> [-throttle <bytes/s>]`.** Serves `fakegdrive` under `/gdrive/` and `/gdrive-upload/`, and `fakegraph` under `/graph/`, over plain HTTP; Task 9 adds `/oauth/`. `-throttle` reads each upload request's body at that rate (the lab uses 1 MiB/s), so a chunk's `upload_progress` and the next commit are seconds apart, not milliseconds (Task 8's lab restart). At start it prints one line `{"schema":"ipcam.fakeproviders/1","gdrive":"http://…/gdrive","gdriveUpload":"http://…/gdrive-upload","graph":"http://…/graph"}`; SIGTERM exits 0. It lives outside `hub/cmd/`, which §3.5 keeps for shipped binaries.

- [ ] **Step 1: Write the failing tests.**
  - `TestConformance_Local`, `TestConformance_FakeDrive`, `TestConformance_FakeGraph`: each runs `Run` with `Options{KillOffsets: 20, Seed: 1}` and asserts that its `Report.Ran` holds `Resume/KillAtRandomOffset` and `Resume/CompleteSessionDetected` and that `Report.Skipped` holds neither.
  - `TestConformance_FailsOnResumeNone`: a failing subtest always fails its parent and `*testing.T` cannot be faked, so the test re-runs its own binary with `CONFORMANCE_STUB_HELPER=1`, whose helper test runs `Run` on a stub driver reporting `ResumeNone`. The parent asserts a non-zero exit and `Caps/ResumableNotNone` in the helper's output.
  - `TestFakeProviders_ServesBothAndPrintsURLs`: the command starts on `127.0.0.1:0`, prints the schema line, and a resumable upload succeeds against each URL.
  - `TestFakeProviders_Throttle`: with `-throttle 1048576`, a 2 MiB chunk takes at least 1.9 s.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/storage/conformance/... ./hub/internal/storage/fakes/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** with `-race` — Expected: PASS; CI green; actionlint v1.7.12 on the lab workflow exits 0.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add storage conformance suite and fake providers command"`

### Task 7b: `ipcam-hub` uploads

Rulings: P01 (items 1, 2 and 4: `diag readopen`, `diag dest-probe`, `diag reach`), P02, P03 (`OpenWith` and the callback), P14 (spool, `playback`, `diag readopen`), P15 (`diag reach`), P16 (the `upload_resumed` event), P18 (calls in `upload_verified`), P35 (`diag dest-probe`); OK14 (`diag uploads`, the reconciliation-path audit). New in v2.

**Files:**
- Modify: `ipcam/hub/internal/catalog/{catalog.go,catalog_test.go}` (`OpenWith`, `Options`, the callback in `PutSegment` and `ReconcileFiles`, `Folder`/`PutFolder`)
- Modify: `ipcam/hub/cmd/ipcam-hub/{main.go,run.go,events.go,diag.go}` (single writer for `main.go`)
- Create: `ipcam/hub/cmd/ipcam-hub/{uploads.go,hubid.go,diag_readopen.go,diag_destprobe.go,diag_reach.go,diag_uploads.go}` and tests `{uploads_test.go,diag_readopen_test.go,diag_destprobe_test.go,diag_reach_test.go,diag_uploads_test.go}`
- Create: `ipcam/hub/internal/playback/{readopen.go,readopen_test.go}`
- Modify: `.github/workflows/ipcam-spike-upload.yml`: `TREES` += `./hub/cmd/ipcam-hub/... ./hub/internal/playback/...`; the `upload` job installs ffmpeg, runs `go run ./tools/spikes/video/cmd/mediagen -out "$RUNNER_TEMP/media"`, builds the fork with `GOWORK=off` into `$RUNNER_TEMP/bin/mediamtx`, and sets `MEDIAMTX_BIN`, `MEDIAGEN_DIR`, `RECSPIKE_REQUIRE_MEDIAMTX=1` and `SPIKE_REQUIRE_MEDIA=1` for its tests; the `windows` job builds `go build -o "$RUNNER_TEMP/bin/ipcam-hub.exe" ./hub/cmd/ipcam-hub`, the path that Task 8's WanBlock smoke step reads.
- Not modified: `ipcam/hub/internal/recorder/**`.

**Interfaces:**
- Consumes: Tasks 3–7; c1's `run.json`, events, `recorder`, `crypto.NewTailSealer`.
- Produces:
  - **Catalog:**

    ```go
    type Options struct{ OnSegmentComplete func(ctx context.Context, tx *sql.Tx, s Segment) error }
    func OpenWith(ctx context.Context, path string, opts Options) (*DB, error) // Open(ctx, path) keeps its signature and is OpenWith(ctx, path, Options{}); its three recorder-test call sites stay untouched, and run.go's (L204 at c1's HEAD) moves to OpenWith
    func (db *DB) Folder(ctx context.Context, destID, path string) (string, bool, error)
    func (db *DB) PutFolder(ctx context.Context, destID, path, remoteID string) error
    ```

    `PutSegment` now runs in its own transaction and calls the callback inside it when the row's new state is `complete` and it was not complete before, or its bytes changed. `ReconcileFiles` calls it, inside its transaction, for every `Added` row and every `Updated` row whose bytes changed. A callback error rolls the row back. The callback always gets the stored row, never the caller's argument: every recorder call site passes no ID (supervisor.go L622, L922; reconcile.go L795), and `PutSegment` would otherwise hand it the fresh UUIDv7 it generates while `ON CONFLICT (local_path)` keeps the old id. So `PutSegment`'s upsert ends with `RETURNING id` and the callback gets that id; `ReconcileFiles` passes the new id for an `Added` row and `rows[path].ID` for an `Updated` row. So the MTX-3 hook, the C1-R9 rewrite, the mid-run repair and the startup reconcile all enqueue in the row's transaction, with no edit to `recorder`.
  - **`run.json` `uploads` section** (unknown fields refused):

    ```json
    "uploads": {
      "hubName": "hub",
      "destinations": [{"id": "dst-1", "kind": "GOOGLE_DRIVE", "folder": "ipcam", "tokenRef": "dst-1", "baseUrl": "", "uploadBaseUrl": "", "partSize": 0}],
      "routes": [{"cam": "cam01", "profile": "sub", "class": "EVENT_CLIP", "destinations": ["dst-1"], "fromMs": 0, "untilMs": 0}]
    }
    ```

    Kinds: `LOCAL` (`folder` = an absolute directory), `GOOGLE_DRIVE`, `ONEDRIVE_PERSONAL`. `baseUrl`/`uploadBaseUrl` override the provider endpoints, for the fakes only. `partSize` (0 = the driver's own) is accepted only together with `baseUrl` and must be a multiple of the driver's alignment (Drive 256 KiB, OneDrive 320 KiB); the lab uses 512 KiB (Task 8). A route's optional `fromMs`/`untilMs` (0 = open) bound the segments it places by their `StartMs`, checked in the callback; D1-B uses it (Task 8). A segment of a stream without a matching route is not uploaded. `tokenRef` names the destination's sealed token row; until Task 9 wires `oauth.Store`, a non-`LOCAL` destination fails at start with `no token store for tokenRef <ref>` (tests inject a token source).
  - **Wiring in `run`.** Read or create `<dataDir>/state/hub_id` (UUIDv7, 0600). Open the catalog with `OpenWith`, whose callback calls `q.Enqueue(ctx, tx, outbox.PlacementFor(s, dest, route.class))` for each destination of each route that matches the segment's cam and profile and whose window holds its `StartMs`; `q` is created right after the catalog and before `recorder.New`. Build each driver with a plain `http.Client`; the Drive driver's `FolderStore` is the catalog. Run `q.Run` in a goroutine with `RecDir = <dataDir>/rec`.
  - **New events** (added to `events.go`'s schema comment). None carries a session URI, `uploadUrl`, token or provider error text:
    - `upload_queued {placementId, cam, profile, startMs, dest, class, bytes}`, when a worker first reads the placement (Task 4's `Queued`): only committed placements are reported, so it may come before or after the segment's `segment_closed`
    - `upload_resumed {placementId, dest, offset}`
    - `upload_progress {placementId, dest, offset, size}` (offset = confirmed)
    - `upload_verified {placementId, dest, bytes, hashKind, hash, attempts, calls}` (`calls` = `{method: n}`)
    - `upload_failed {placementId, dest, reason, attempts}` (`reason` ∈ `auth_expired`, `quota_full`, `rate_limited`, `daily_cap`, `session_expired`, `not_found`, `transient`, `permanent`, `hash_mismatch`)
    - `dest_state {dest, state, alert}`
  - **`playback`:**

    ```go
    package playback
    type OpenRead struct{ Encrypted bool; Parts int; DurableBytes int64; ReadMs float64 }
    var ErrNoOpenSegment error
    func ReadOpen(ctx context.Context, recDir, cam, profile string, tail crypto.TailSealer) (OpenRead, error)
    ```

    It takes the newest `*.mp4.part` or `*.mp4.age.part` under `<recDir>/<cam>/<profile>/<UTC date>/`. A clear file is read directly: `ftyp`, `moov` and every complete `moof`+`mdat` pair. An `.age` file is opened read-only through `tail.OpenTorn` and walked over `ReadAt` up to `DurableSize()`, because `Recording.OpenReaderAt` needs the final chunk. It has its own minimal box walker; `recorder`'s is unexported.
  - **`ipcam-hub diag readopen --config <run.json> --cam <cam> [--profile main]`** (MI5 always passes `--profile sub`, the only profile it records) → `{"schema":"ipcam.diag.readopen/1","ok":true,"encrypted":false,"parts":17,"durableBytes":…,"readMs":…}`, or `{"schema":…,"ok":false,"error":"no-open-segment"|"unreadable"}`; exit 0 either way, 1 on a usage error. It never prints a path.
  - **`ipcam-hub diag dest-probe --config <run.json> --dest <id> [--delete-segment] [--poll 30s] [--for 10m]`** → one line `{"schema":"ipcam.diag.destprobe/1","dest","kind","probe":{"writable","rangeRead","deletable","quota":{"total","used","free","inBin"},"warnings":[]},"quotaAfterDelete":[{"tMs","free","inBin"}]}`. `--delete-segment` deletes the remote object of the oldest verified placement on that destination, marks it `deleted`, then reads `Quota` every `--poll` for `--for` (P35's quota-after-delete figure: `free` = remaining, `inBin` = deleted).
  - **`ipcam-hub diag reach --hosts <host[:port],…> --lan <host:port>`** (P15) → one JSON line per probe: `{"schema":"ipcam.diag.reach/1","target":"provider","host":"www.googleapis.com","port":443,"family":"v4","ok":false,"blocked":true,"ms":5000,"err":"timeout"}`. A host without a port uses 443; the lab passes the fakes' `198.51.100.1:18080`. For each host it resolves A and AAAA and TCP-connects to the first address of each family (5 s timeout). `err` ∈ `timeout`, `refused`, `unreachable`, `denied` (EACCES or EPERM: a Windows Firewall block surfaces as WSAEACCES, a netfilter drop as EPERM), `no-address`. `blocked` is true for `timeout`, `unreachable`, `denied`, and `no-address` (a family the host lacks). `refused` is a reset from the far end, so the path is open: `ok: false`, `blocked: false`. `"target":"lan"` probes `--lan` without printing the address. `"target":"loopback"` connects to listeners the command opens itself on `127.0.0.1:0` and, when the host has IPv6 loopback, `[::1]:0`.
  - **`ipcam-hub diag uploads --config <run.json> [--list-remote]`** → one line per destination: `{"schema":"ipcam.diag.uploads/1","dest","segments","verified","pending","failed","deleted","missing","duplicates","hashMismatches","listCalls"}`. `segments` counts complete local segments that a route of `--config` sends to the destination (its cam, profile and window, matched as the callback matches them), except those whose placement is `deleted`; a segment outside every route's window is in no count; `deleted` counts those (`dest-probe --delete-segment` made them), and they are left out of `segments`, `verified` and `missing`. With `--list-remote` it lists each day folder through `List` (the reconciliation path, counted in `listCalls` and never in the running hub's counters): `duplicates` = keys with more than one object, `missing` = complete segments with no object, `hashMismatches` = objects whose provider hash differs from `Digest` of the local file.

- [ ] **Step 1: Write the failing tests.** Process tests read `MEDIAMTX_BIN` and `MEDIAGEN_DIR`; they skip when unset and fail when `RECSPIKE_REQUIRE_MEDIAMTX=1` or `SPIKE_REQUIRE_MEDIA=1`.
  - `TestCatalog_OpenEqualsOpenWithZero`: `Open` gives a catalog that calls no callback; c1's tests still pass.
  - `TestCatalog_PutSegmentCallbackInSameTx`: a callback error leaves no row.
  - `TestCatalog_CallbackOnCompleteOrBytesChange`: writing → complete calls it once; complete with the same bytes 0 times; complete with new bytes once; lost 0 times.
  - `TestCatalog_ReconcileFilesCallsCallback`: Added and bytes-changed Updated rows call it; Lost rows do not.
  - `TestCatalog_CallbackGetsPersistedID`: a `PutSegment` with no ID on a known path (as every recorder call site writes) and a `ReconcileFiles` Updated row both pass the row's original id to the callback; an Added row passes its new id.
  - `TestCatalog_ImportsNoOutbox`: `go list -deps` of `hub/internal/catalog` contains neither `outbox` nor `storage`.
  - `TestRun_UploadsSection_Validates`: an unknown field, an unknown kind, a route to an unknown destination, a duplicate destination id, a `partSize` without `baseUrl`, a `partSize` off the driver's alignment and a route with `untilMs ≤ fromMs` each exit 1 with a message naming it.
  - `TestRun_RouteWindow`: with `untilMs` = T, a segment starting before T gets a placement and one starting at or after T gets none.
  - `TestRun_SegmentClosedIsUploadedToLocalDest` (Task 3's driver as the destination, `segmentSecs: 10`): each `segment_closed` has exactly one `upload_queued`, at least one `upload_progress` and one `upload_verified` (`hashKind: "SHA256"`) with the same cam, profile and `startMs`, in any order relative to it, and the destination file's SHA-256 equals the segment's.
  - `TestRun_TornSegmentFinalisedAtStartupIsUploaded`: a torn `.part` left by a killed run is finalised by the startup reconcile, enqueued in its row's transaction and verified.
  - `TestRun_UploadEventsCarryNoSessionURL` (Review Focus 5): with `fakegraph` and a static token assembled from parts, stdout and stderr contain neither the fake's `uploadUrl` nor the token.
  - `TestRun_HubIDStableAcrossRestarts`.
  - `TestPlayback_ReadOpen_ClearPart` (synthetic fMP4 with 3 complete parts and a torn 4th → `Parts == 3`), `TestPlayback_ReadOpen_AgePart` (an `.age.part` with 2 flushed chunks → `DurableBytes == 2·65536`, parts counted on the plaintext), `TestPlayback_ReadOpen_NoOpenSegment`.
  - `TestDiagReadopen_JSONShape`.
  - `TestDiagDestProbe_LocalAndFakeGraphQuotaAfterDelete` (`--poll 10ms --for 50ms`): the probe fields are present; after the delete, `inBin` rises and `free` does not.
  - `TestDiagReach_FamiliesAndTargets`: loopback v4 succeeds, and v6 when the host has IPv6 loopback (skipped otherwise); `203.0.113.1:9` (TEST-NET-3, on a port no transparent proxy intercepts) reads `blocked: true` within 5 s with `timeout` or `unreachable`; `127.0.0.1:<port with no listener>` reads `err: refused`, `ok: false`, `blocked: false`; `127.0.0.1:<a listener's port>` reads `ok: true`; the `lan` line has no address.
  - `TestDiagUploads_CountsMissingAndDuplicates`: a fake Drive holding one key twice and lacking another reports `duplicates: 1`, `missing: 1`, and `listCalls > 0`.
  - `TestDiagUploads_DeletedPlacementNotMissing`: after `dest-probe --delete-segment`, `diag uploads --list-remote` reports `deleted: 1`, `missing: 0` and `verified == segments`.
  - `TestDiagUploads_CountsOnlyTheRouteWindow`: a complete segment that starts before its route's `fromMs` and has no placement is in neither `segments` nor `missing`.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/catalog/... ./hub/internal/playback/... ./hub/cmd/ipcam-hub/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** `cd ipcam && MEDIAMTX_BIN=<built fork> MEDIAGEN_DIR=<media> go test -count=1 -race ./hub/... && tools/scripts/check-video-build-list.sh` — Expected: PASS, `video build list unchanged`; `ipcam-spike-upload` (both jobs) and `ipcam-spike-recording` green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add uploads, upload events and upload diag commands to ipcam-hub"`

### Task 9: OAuth on the hub, the sealer, and the MI6 driver — G15 (Phase 0 part)

Rulings: P01 (item 3; item 6 narrowed by P21), P10, P11, P19, P21, P22, P27, P36, P37, P39, P40, P45 (known-secrets scan, ARH-08), P46, P49 (the S4U round trip in CI), P53, C2-R1 (`--drive` for MI6), C2-R2 (`uploadspike host`), P02 and P17 (uploadspike's copy of c1's SD confirmation: the first hub start on a Pi is MI6's, and c1's SD gate exits 4 whatever the jobs, run.go L164-170). This task runs before Task 8 (P19): MI5 uploads with this task's token store, and the guide runs the MI6 logins before MI5. It has two parts with their own commits. `hub/internal/crypto` is CODEOWNERS-sensitive (spec L2217): Part A gets a second review.

**Files — Part A (hub, production):**
- Create: `ipcam/hub/internal/crypto/{sealer.go,sealer_windows.go,sealer_other.go}` and tests `{sealer_test.go,sealer_windows_test.go}`
- Create: `ipcam/hub/internal/storage/oauth/{oauth.go,pkce.go,table.go,store.go,tokensource.go,daily.go}` and tests
- Create: `ipcam/hub/internal/storage/fakes/oauth/{fake.go,fake_test.go}` (`package fakeoauth`); Modify: `fakes/gdrive/fake.go` (per-client scoping, `HideFrom`, `permissionId`), `fakes/cmd/fakeproviders/main.go` (serves `/oauth/` and adds `"oauth":"http://…/oauth"` to its schema line, which `uploadspike mi5 --fakes` reads)
- Modify: `ipcam/hub/internal/catalog/catalog.go` (`PutSecret`, `Secret`, `SwapSecret`, `DeleteSecret`, `SecretIDs`; `OpenReadOnly(ctx, path)`: its own DSN with `mode=ro` and `busy_timeout`, no `journal_mode` pragma and no migration, refusing a schema version other than `len(migrations)`)
- Modify: `ipcam/hub/cmd/ipcam-hub/{main.go,run.go,events.go,diag.go,uploads.go}`; Create: `ipcam/hub/cmd/ipcam-hub/{control.go,diag_visibility.go,diag_scansecrets.go}` and tests
- Modify: `.github/workflows/ipcam-spike-upload.yml` (`TREES` += `./hub/internal/crypto/...`; the `windows` job's S4U seal/unseal step)

**Files — Part B (uploadspike):**
- Create: `ipcam/tools/spikes/upload/{results,hubproc,hostinfo,runstate,handoff,sdconfirm,mi6}/*.go` and tests (`sdconfirm` lives here, not in Task 8, because the first hub start on a Pi is MI6's)
- Modify: `ipcam/tools/spikes/upload/{go.mod,go.sum}`: `golang.org/x/sys v0.48.0` (`hubproc`'s Windows kill and Job Object, `runstate.BootID` and `LockFileEx`) and `golang.org/x/term v0.46.0` (prompts without echo), both at the workspace's versions; `GOWORK=off go mod tidy` (P31)
- Create: `ipcam/tools/spikes/upload/cmd/uploadspike/{main.go,setup.go,host.go,resume.go,status.go,results.go,mi6.go}` and `cmd/uploadspike/web/{setup.html,device.html,reader.html,status.html}` (embedded; each starts with c2's marker comment)
- Modify: `ipcam/tools/spikes/upload/markers_test.go` (`markedNonGoFiles` += the four pages)
- Modify: `.github/workflows/ipcam-spike-upload.yml` (the `windows` job runs `TestHubproc_StopIsCleanWithoutConsole` through `ci/s4u-run.ps1`)

**Interfaces — Part A:**

```go
package crypto // §4.4 L525, ctx first
type Sealer interface {
    Seal(ctx context.Context, label string, plain []byte) ([]byte, error)
    Unseal(ctx context.Context, label string, sealed []byte) ([]byte, error)
    Backend() string // "dpapi-machine" | "software"
}
func NewSealer(dataDir string) (Sealer, error)
var ErrSealedLabel error // Unseal under another label
```

- **Windows (P19):** `CryptProtectData` with `CRYPTPROTECT_LOCAL_MACHINE | CRYPTPROTECT_UI_FORBIDDEN`, the label as optional entropy; `Backend() == "dpapi-machine"`. Machine scope works under the S4U autostart task and lets any `ipcam-hub` start unseal after a reboot; the data directory keeps the user profile's ACL.
- **Linux:** `<dataDir>/state/keys/sealer.key`, 32 random bytes created 0600 (refused if group or other bits are set), owned by `ipcam-spike` once Task 8's install runs; XChaCha20-Poly1305 with the label as AAD; blob = version byte ‖ nonce ‖ ciphertext; `Backend() == "software"`. TPM and service-account DPAPI (§11.8) are Faza 1, stated in «Limitări».

```go
package oauth
type Platform string   // "android" | "windows" | "web"
type ClientType string // Google: "android" | "desktop" | "tv" | "web"; Microsoft: "entra"
type Client struct{ ID, Secret string } // Secret only for desktop and tv (non-confidential, §8.7)
type Clients struct{ Google map[ClientType]Client; Microsoft *Client } // Microsoft nil unless A5 passed
type Endpoints struct{ GoogleAuth, GoogleToken, GoogleDevice, GoogleRevoke, MicrosoftAuth, MicrosoftToken, MicrosoftDevice string }
func DefaultEndpoints() Endpoints // accounts.google.com/o/oauth2/v2/auth, oauth2.googleapis.com/{token,device/code,revoke}; login.microsoftonline.com/consumers/oauth2/v2.0/{authorize,token,devicecode}
type SecretStore interface { // catalog.DB implements it; no method holds a transaction across its caller's I/O
    PutSecret(ctx context.Context, id, kind string, sealed []byte, sealer string) error
    Secret(ctx context.Context, id string) (sealed []byte, sealer string, ok bool, err error)
    // SwapSecret writes next only while the row still holds old (compare-and-swap in one short
    // transaction); swapped is false when another writer changed the row first.
    SwapSecret(ctx context.Context, id string, old, next []byte, sealer string) (swapped bool, err error)
}
type Config struct {
    Clients Clients; Endpoints Endpoints; Clock func() time.Time; Sealer crypto.Sealer; HTTP *http.Client
    Secrets SecretStore; Rand io.Reader
    TestingExpiry time.Duration // Google refresh-token life in Testing mode: 7×24 h in Phase 0
}
type Route struct{ ClientType ClientType; RedirectURI, Plan string }
func RouteFor(p storage.Kind, pl Platform, plan string, loopbackPort int) (Route, error)
type BeginRequest struct{ DestID string; Provider storage.Kind; Platform Platform; LoopbackPort int; Plan string /* "A" (default) | "B1" */ }
type BeginResult struct{ State, Plan, AuthURL, RedirectURI, VerificationURL, UserCode string; ExpiresAt time.Time }
type Completion struct{ DestID string; Provider storage.Kind; Platform Platform; Plan string; ClientType ClientType; IssuedAt time.Time }
type RefreshResult struct{ At time.Time; Rotated bool }
type DailyResult struct{ DestID string; Day, Attempt int; Outcome string /* "ok" | "failed" | "expected-expiry" */; Rotated bool }
type Store struct{ /* … */ }
func New(cfg Config) (*Store, error)
func (s *Store) SetClients(ctx context.Context, c Clients) error // seals them as rows client:google:<type> and client:microsoft
func (s *Store) Begin(ctx context.Context, r BeginRequest) (BeginResult, error)
func (s *Store) Complete(ctx context.Context, state, code string) (Completion, error)
func (s *Store) DeviceStatus(ctx context.Context, state string) (Completion, error) // ErrAuthorizationPending while the hub polls the device token endpoint
func (s *Store) Refresh(ctx context.Context, ref string) (RefreshResult, error)           // ref: a tokenRef, the destId of its login
func (s *Store) TokenSource(ctx context.Context, ref string) oauth2.TokenSource // one per ref
func (s *Store) RunDaily(ctx context.Context, emit func(DailyResult)) error
var ErrStateUnknown, ErrStateExpired, ErrStateUsed, ErrAuthorizationPending error
```

- **Platform × provider table (`RouteFor`, T34, FLW-31, P36, P39).** Plan B1 is the hub's device flow on any pair: Google with the TV client, Microsoft with the same Entra public client.

  | Provider | Platform | Plan A client | Exact redirect |
  |---|---|---|---|
  | GOOGLE_DRIVE | android | Android (custom URI scheme, no secret) | `ro.ipcam.spike:/oauth2redirect` |
  | GOOGLE_DRIVE | windows | Desktop (secret) | `http://127.0.0.1:<loopbackPort>/` |
  | GOOGLE_DRIVE | web | none: always B1 (TV, secret) | — |
  | ONEDRIVE_PERSONAL | android | Entra public | `ro.ipcam.spike://oauth/callback` |
  | ONEDRIVE_PERSONAL | windows | Entra public | `http://localhost:<loopbackPort>/oauth/callback` |
  | ONEDRIVE_PERSONAL | web | Entra public | `http://localhost:7091/oauth/callback` |

- **Authorization request.** Google: `scope=https://www.googleapis.com/auth/drive.file`, `access_type=offline`, `prompt=consent`, `code_challenge` S256, `state`, the exact `redirect_uri`. Microsoft: authority `consumers`, `scope=Files.ReadWrite.AppFolder offline_access`, `response_mode=query`, PKCE. Verifier and state live in RAM for 10 min and are single-use; an expired or used state wipes its verifier.
- **Tokens (P19, P27).** The refresh token is sealed under label `oauth:<ref>` as JSON `{refreshToken, provider, platform, clientType, plan, issuedAtMs}` and stored as the `secrets` row `oauth:<ref>` (kind `oauth`); a login's `ref` is its `destId`, and `run.json`'s `tokenRef` names it. Access tokens stay in RAM, one TokenSource per `ref`, shared by every destination that names it.
  - **No lock across I/O.** `Refresh` and the TokenSource read the sealed row, call the token endpoint outside any transaction with a 5 s deadline, seal the result and write it with `SwapSecret(old, new)` **before** returning the access token. When the swap misses, or the endpoint answers `invalid_grant`, they re-read the row once: if another process rotated it meanwhile, they refresh again with the new token. So two processes never keep a revoked token, and the catalog's other writers (the MTX-3 hook's `PutSegment` and its enqueue, the outbox's updates; `busy_timeout` is 10 s) never wait on a token endpoint that a WAN block has silenced.
  - **Errors.** `invalid_grant` on an unchanged row, and `invalid_client`, wrap `storage.ErrAuthExpired`, so the outbox goes `BLOCKED_AUTH`. A network error, a timeout, a 429 or a 5xx from the token endpoint wraps `storage.ErrTransient`: during MI5's 6 h block the hourly access-token expiry then ends in backoff and `UNREACHABLE`, and recovers with no `Unblock`. `Config.HTTP` has the drivers' timeouts (Task 5).
- **Daily refresh (P53).** Day N (1…7) has its first attempt at `IssuedAt + N·24 h` and retries every 15 min until `IssuedAt + (N+1)·24 h`. A day is `ok` at its first success and `failed` only when every attempt fails. A Google `invalid_grant` at or after `IssuedAt + TestingExpiry` is `expected-expiry`, never `failed`.
- **Control endpoint (P01 item 3, P46).** `ipcam-hub run` reads `IPCAM_CONTROL_TOKEN` (≥ 32 hex characters) from its environment. When it is set, `run` listens on `127.0.0.1:0` and emits `{"type":"control","addr":"127.0.0.1:<port>"}`; the token is never printed. Every request needs `Authorization: Bearer <token>` (401 otherwise). Endpoints:
  - `POST /control/v1/clients` (`Clients` JSON) → 204; sealed through `SetClients`.
  - `POST /control/v1/oauth/begin` (`BeginRequest`) → `BeginResult`.
  - `POST /control/v1/oauth/complete` `{state, code}` → `Completion`; on success `q.Unblock` for every destination whose `tokenRef` is that login's.
  - `GET /control/v1/oauth/status?state=<s>` → `{"status":"pending|completed|expired"}` plus the `Completion`.
  - `POST /control/v1/oauth/refresh` `{destId}` → `RefreshResult`.
  - `GET /control/v1/visibility/targets` → `[{"creator":"desktop","folderId":"…"}]`, from RAM, for the web reader only.
  - `POST /control/v1/uploads/reset-connections` → 204: closes the drivers' idle connections and cancels their in-flight requests; each counts as a transient failure and is retried on a new connection. A firewall rule added under an established flow may leave it open (Windows Firewall authorizes at connect), so MI5 calls this right after `Block` (Task 8).
- **`run.json` gains** `"oauth": {"endpoints": {…} | absent, "testingExpiryDays": 7}`; absent endpoints mean `DefaultEndpoints()`. `tokenRef` now resolves through `Store.TokenSource`; several destinations may name one `tokenRef` (MI5 reuses an MI6 login, Task 8).
- **Events:** `oauth_begin {destId, provider, platform, plan}`, `oauth_completed {destId, provider, platform, plan, clientType, issuedAtMs}`, `oauth_failed {destId, stage: "complete"|"device"|"refresh", reason}` (`reason` ∈ `invalid_grant`, `access_denied`, `redirect_uri_mismatch`, `invalid_client`, `expired_token`, `state`), `oauth_refresh {destId, day, attempt, outcome, rotated}`.
- **`ipcam-hub diag visibility --config <run.json> --creator <destId> --reader <destId>` (P21)** → `{"schema":"ipcam.diag.visibility/1","provider":"GOOGLE_DRIVE","creator":"desktop","reader":"tv","self":false,"listed":true,"read":true}`.
  - It first compares `about.user.permissionId` of both tokens in RAM. On a mismatch it prints `{"schema":…,"error":"accounts-differ"}` and exits 5; the ID is never printed.
  - The creator ensures `vis-<creatorType>.txt` (16 fixed bytes) in its own root folder through `PutSmall`, its ID cached under path `vis/<creatorType>`.
  - The reader makes one `files.list` with `q="'<creator root id>' in parents and trashed=false"` (reconciliation, counted). `listed` = the probe file's ID is in the result. `read` = a ranged `files.get?alt=media` with `Range: bytes=0-15` by the listed ID returns the 16 bytes; not listed → `read: false`.
  - Microsoft, only if A5 passed: approot children and a content read across the Entra app's platforms.
- **`ipcam-hub diag scan-secrets --config <run.json> --dir <results dir>` (P45, §16.2.3, ARH-08).** The known-secrets scan runs inside the hub, which holds the secrets sealed; uploadspike keeps none on disk (P46). It unseals in RAM every `client:*` row (client IDs and the Desktop and TV secrets) and every `oauth:*` refresh token, adds the values uploadspike passes on stdin as JSON lines `{"value":"…"}` (the control tokens of its hub starts; for MI8 every typed camera IP, user and password), and searches every file under `--dir` for each value raw, URL-encoded (`QueryEscape` and `PathEscape`), base64 (Std, URL, Raw) and hex (lower, upper). It removes each file with a match and prints `{"schema":"ipcam.diag.scansecrets/1","values":<n>,"removed":["MI6/pc/<runId>.json"]}`, never a value. `--config` is optional (without it, only stdin's values). It runs as uploadspike's own identity: root on Linux reads `results/` and the `ipcam-spike` key, and machine-scope DPAPI unseals for any local process. It opens the catalog with `catalog.OpenReadOnly`, so a scan never migrates and never takes the write lock (c1's DSN has `_txlock=immediate`, so `Open`'s migration transaction would wait behind the hub). Root never leaves the hub a file it cannot open: SQLite's unix VFS, run as root, chowns every `-wal`, `-journal` or `-shm` file it creates to the database file's owner (os_unix.c `robustFchown`, called from `unixOpen` and `unixOpenSharedMemory`; present in modernc.org/sqlite v1.60.1's Linux builds), and `hubproc.Start` re-owns any `catalog.db*` file of another owner before each start (Part B).
- **Fake OAuth provider (`fakeoauth`, strict).** Google and Microsoft endpoints; S256 PKCE; client-type rules (a "web" client without a secret is refused, a mismatched `redirect_uri` gives `invalid_grant`); device flow; it rotates the refresh token on every refresh and revokes the old one; a Testing mode where a refresh token dies 7 days after issuance; auto-approve for the lab rig. `fakegdrive` gains `drive.file` scoping by client ID: it records the client that created each file, lets every client of the same project see those files by default, and `HideFrom(creatorClientID, readerClientID)` hides one creator's files from one reader. It also gains `about.user.permissionId` per account.
- **CI (P49).** The `windows` job compiles `crypto.test.exe`, runs `TestSealer_SealToFile` in a normal step (writing `$RUNNER_TEMP/sealed.bin`), then runs `pwsh -File tools/spikes/upload/ci/s4u-run.ps1 -Exe <crypto.test.exe> -Arguments "-test.run ^TestSealer_UnsealFromFile$ -test.v -sealer-blob <path>" -Out "$RUNNER_TEMP/s4u-unseal.txt"`. The output must contain `--- PASS: TestSealer_UnsealFromFile`. It also runs `TestOAuth_RefreshAfterKill9Restart`.

**Interfaces — Part B:**

```go
package results // SPIKE-ONLY
const Schema = "ipcam.phase0c2.result/1"
type Envelope struct {
    Schema, Code, RunID, Set string // Code "MI5" | "MI6" | "MI8"; Set e.g. "pc", "pi", "lab-linux"
    Host string                     // "pc" | "pi" | "lab-linux" | "lab-windows"
    OS string; PiModel *PiModelInfo; Storage string /* "ssd"|"sd"|"hdd"|"unknown" */; SdConfirmation *SdConfirmation
    Sealer string                   // "dpapi-machine" | "software"
    Account, AccountKind string     // "acct-1"; "test" | "personal"
    Cameras int; Part, Parts int
    Runs []Run
    MI5 *Mi5Data; MI6 *Mi6Data; MI8 *Mi8Data // Mi5Data in Task 8, Mi8Data in Task 10
}
type PiModelInfo struct{ Raw, Revision, Tier, Source string; Supported, Known bool; RecommendedMin, RecommendedMax int } // mirrors diag host
type SdConfirmation struct{ Text string; AtMs int64 }
type Run struct{ RunID string; StartMs, EndMs int64; Valid bool; InvalidReason string; Gaps []Gap }
type Gap struct{ StartMs, EndMs int64; Reason string /* "reboot" | "sleep" | "hub-crash" */ }
type Mi6Data struct{ OneDriveEnabled bool; Pairs []Mi6Pair; Visibility []VisibilityCell; Handoffs []Handoff }
type Mi6Pair struct{ Provider, Platform, Plan string /* "A" | "B1" | "none" */; HandoffOK bool; PlanAFailure string; IssuedAtMs int64; Refresh []RefreshDay }
type RefreshDay struct{ Day, Attempts int; Outcome string /* "ok" | "failed" | "expected-expiry" | "blocked-by-test" */ }
type VisibilityCell struct{ Provider, Creator, Reader string; Listed, Read, Self bool }
type Handoff struct{ Account, Client string; Count int }
type Verdict string // Pass "trece", Incomplete "fără verdict: lipsesc măsurători"
func FailWith(planB string) Verdict  // "pică → <plan B>"
func Deferred(reason string) Verdict // "amânat în Faza 0-bis: <motiv>"
type Row struct{ Code, Value, Limit, Source string; Pass *bool }
type Judgement struct{ Gate string; Verdict Verdict; PlanB string; Reasons []string; Rows []Row; Decisions []string /* §2.2 lines */ }
func Write(dir string, e Envelope) error // results/<code>/<set>/<runId>.json, 0600 (the known-secrets scan is ipcam-hub's diag scan-secrets)

package hubproc // SPIKE-ONLY: runs ipcam-hub
type Config struct{ Bin, RunJSON string; RunAsUser string /* Linux, when root: "ipcam-spike" */; ControlToken string; Env []string; PIDFile string /* <base>/state/hub.pid */ }
type Event struct{ Type string; AtMs int64; Raw json.RawMessage }
// Start: RunJSON and the identity file it names live in <base>/hub/spike/, inside the hub's data directory.
// On Linux as root that directory is owned by RunAsUser with mode 0700, both files are chowned to RunAsUser and
// must be 0600, and any <dataDir>/state/catalog.db* file of another owner is chowned back to RunAsUser;
// a missing RunAsUser gives ErrNoSpikeUser. Every such write and chown follows no link (Files for the hub, below).
// Exit 4 (c1's SD gate, run.go L164-170) gives ErrSdCardUnconfirmed.
// The hub never outlives this process (One hub per device, below). Start records the child's PID and start time in PIDFile.
func Start(ctx context.Context, cfg Config) (*Hub, error)
var ErrSdCardUnconfirmed, ErrNoSpikeUser error // uploadspike shows their Romanian texts (below)
func (h *Hub) Events() <-chan Event
func (h *Hub) Control(ctx context.Context) (*ControlClient, error) // after the "control" event
func (h *Hub) Stop(ctx context.Context) error // clean stop: the console-free graceful stop that c1 Task 7.2 ships (hubproc.Stop), which works from the S4U task and the systemd unit, neither of which has a console; Kill after 30 s
func (h *Hub) Kill() error // Process.Kill; MediaMTX dies with it (c1 C07)
// WaitGone is a new holder's first step after it takes hub.lock: while the process that PIDFile names is still that
// ipcam-hub (same PID and start time), it waits, and after grace (60 s) it kills it.
func WaitGone(ctx context.Context, pidFile string, grace time.Duration) error
func Diag(ctx context.Context, cfg Config, args []string, stdin io.Reader) ([]json.RawMessage, error) // same binary; as RunAsUser when set (diag reach runs as the blocked identity), else as uploadspike (diag scan-secrets)

package hostinfo // SPIKE-ONLY
type Host struct{ OS, Arch string; PiModel *results.PiModelInfo; Storage string; RecommendedCameras int }
// Read runs `ipcam-hub diag host --rec <recDir>`. When the model reads "unknown", it reads /proc/device-tree/model and
// maps it with `ipcam-hub diag pimodel --raw` (Source "device-tree"), as c1's recspike does (c1 plan L843, c1 Task 7a).
func Read(ctx context.Context, hubBin, recDir string) (Host, error)
func RecommendD2(h Host) (letter, reason string) // "B" for a Pi of tier H1 or H1b on arm64 (Pi 4, 5, 400, 500, CM4, CM5); "A" otherwise

package runstate // SPIKE-ONLY; <base>/state/run-state.json, 0600, one per device
type State struct{
    Mode string; BootID string; LastWallMs, LastMonoMs int64; Gaps []results.Gap
    Handoffs map[string]int          // per account and client, every command of this device
    Pending map[string]json.RawMessage
    Sd *results.SdConfirmation       // the SD-card answer, asked once per device
    HandoverToken string             // 32 random bytes, hex: the bearer of /spike/v1/run/handover
    Claim *Claim                     // a command that is taking the hub; resume yields to it (One hub per device)
    Open map[string]json.RawMessage  // "MI6", "MI5", "MI8": each open run's resumable state (MI5: also the PID and start time of the mi5 process that opened it; MI8: per real-N brand, model, hours, episodes; never an address, user or password)
}
type Claim struct{ PID int; ProcStartMs, UntilMs int64 } // valid while that process lives and UntilMs (now + 90 s) is ahead
func Load(path string) (*State, error)
func Update(path string, fn func(*State) error) error // load, apply fn, atomic rename, all under <base>/state/run-state.lock, held only for the update, so concurrent commands (mi8 and the hub holder) keep each other's fields
func Lock(path string) (release func(), err error) // exclusive, non-blocking: flock, LockFileEx on Windows; hub.lock is held for as long as a hub runs
func LockFile(path string) (*os.File, error)       // Task 10 adds it for MI8. Linux: the same lock as an open file, so a child inherits it (ExtraFiles) and it lasts until every copy is closed
var ErrLocked error
func (s *State) Heartbeat(now time.Time, mono time.Duration, bootID string) *results.Gap // a new boot ID → "reboot" gap since the last heartbeat; wall time advancing more than 2 min beyond monotonic → "sleep" gap
func BootID() (string, error) // Linux /proc/sys/kernel/random/boot_id; Windows the last boot time

package handoff // SPIKE-ONLY
const HandoffPort = 7091
type Server struct{ /* … */ }
type Options struct{ Clock func() time.Time; RemoteHub string /* `mi6 reader --hub`: the Pi's address; "" otherwise */; Port int /* 0 = HandoffPort; tests pass a free port */ }
func New(hub *hubproc.ControlClient, opts Options) *Server
func (s *Server) ServeLoopback(ctx context.Context) error            // 127.0.0.1:7091 (and [::1]:7091) while this process holds hub.lock; retries the bind for 10 s after a handover
func (s *Server) OpenLAN(ctx context.Context, state string) error    // <lan-ip>:7091 on each RFC 1918 IPv4 address of the host, until Complete for state or 10 min

package sdconfirm // SPIKE-ONLY, c2's copy of c1 G16's text (c1 plan L842)
const Warning = "Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB."
const Prompt = "Scrieți DA și apăsați Enter"
func Ask(in io.Reader, out io.Writer, now func() time.Time) (results.SdConfirmation, bool)
```

- **uploadspike pages and API (P40).** Served only by the process that holds the device's `hub.lock` (Global Constraints); plain HTTP; every response sets `Referrer-Policy: no-referrer`; query strings are never logged.
  - `GET /spike/v1/oauth/next?platform=<p>` → the login uploadspike waits for on that platform: `{dest, provider, plan}`.
  - `GET /spike/v1/oauth/begin?dest=&provider=&platform=&port=` → the hub's `BeginResult`.
  - `POST /spike/v1/oauth/complete` `{state, code}` → forwarded to the control endpoint; only those two fields are accepted.
  - `GET /oauth/device?state=` (`device.html`: verification URL and user code).
  - `GET /oauth/callback` (OneDrive web leg, only if A5 passed).
  - `GET /reader` (`reader.html`), `GET /spike/v1/visibility/targets`, `POST /spike/v1/visibility/web` `{creator, listed, read}`.
  - `GET|POST /setup` (`setup.html`, loopback only; the LAN listener answers 404 for `/setup` and `/reader`).
  - `POST /spike/v1/run/handover` (loopback only; `Authorization: Bearer <HandoverToken>` from the run state, else 401) → 204; the holder then stops the hub and exits (one hub per device, below). While an MI5 run is open it answers 409 instead and keeps the hub (MI5 is never handed over, below).
  - **CORS, as spikehub's `withCORS` (tools/spikes/video/cmd/spikehub/server.go).** The shells call these endpoints from another origin: the Android WebView from `http://localhost` (androidScheme `http`), the Tauri WebView2 from `http://tauri.localhost` or `https://tauri.localhost`. `/spike/v1/oauth/*` answers exactly those three origins with `Access-Control-Allow-Origin` (and `Vary: Origin`), answers `OPTIONS` (with `Access-Control-Allow-Private-Network: true` when the preflight asks for it), and refuses with 403 a POST whose `Origin` is neither one of them nor uploadspike's own. `/spike/v1/visibility/*` allows only `http://localhost:7091`, the reader page's origin. The app sends `complete` as `text/plain` JSON, as `results.ts` does, so it needs no preflight.
- **Setup (P46).** It runs on each device that runs a hub: under D2-A the PC, under D2-C the Pi, under D2-B both (the Pi for the 7 days, the PC for MI5). On the PC, `uploadspike setup` opens `http://localhost:7091/setup`: each Google client ID with the Desktop and TV secrets, or the client JSON files downloaded at creation (A4), plus the Microsoft client ID if A5 passed. On the Pi, `sudo ./uploadspike setup` asks the same in the terminal, secrets without echo, and the SD-card confirmation when the run state has none yet (below). Both hand them to `ipcam-hub` through `POST /control/v1/clients` and keep nothing on disk. Client IDs alone may be sent in the chat; nothing of this is committed or built into a release.
- **Web reader (P21).** `reader.html` is served at exactly `http://localhost:7091/reader`, the Web application client's registered origin. It loads Google Identity Services, gets a `drive.file` token with `google.accounts.oauth2.initTokenClient`, keeps it in page memory, lists and reads each creator's probe file, and posts only booleans. With the hub on the Pi, `uploadspike mi6 reader --hub <Pi address>` on the PC serves the same page; it starts no hub but takes the PC's `hub.lock` for its session, so it is the PC's only listener on 7091; the page fetches the targets from, and posts to, the Pi's uploadspike, whose LAN listener is open for that reader session and answers with `Access-Control-Allow-Origin: http://localhost:7091` on those two endpoints only. If A5 passed, the page also reads approot as the F6 reader through the Entra SPA redirect `http://localhost:7091/oauth/f6` (authorization code with PKCE in the page, the token in page memory).
- **CLI (P37).** `uploadspike setup`; `uploadspike host` (prints the D2 recommendation in Romanian, C2-R2); `uploadspike mi6 [--drive test|test+personal|personal] [--onedrive] [--platforms android,windows,web]`; `uploadspike mi6 --b1 android|windows|web` (B1, below); `uploadspike mi6 reader --hub <address>`; `uploadspike resume` (the autostart's command: it runs for the whole test and takes the hub whenever no other process holds it, below); `uploadspike status` (a client: each open run's state from the run state: MI6's pairs and days, with «Reconectați <platformă> cu B1: uploadspike mi6 --b1 <platform>» for a pair whose refresh day failed (while an MI5 run is open: «Reconectați <platformă> cu B1, după proba MI5: uploadspike mi6 --b1 <platform>»), MI5's phase, MI8's hours and episode count per camera, or that MI8 stopped; the holder's `status.html` on the loopback listener shows the same); `uploadspike results` (zips `results/` into `results-<host>.zip` in the current folder and prints its path; under `sudo` it chowns the zip to `SUDO_USER`). Task 8 adds `mi5`, `install`, `uninstall`, `unblock`, `annotate`; Task 10 adds `mi8`.
- **One hub per device (P19, P53; c1 plan L836).** Each device has one data directory, `<base>/hub` (`<base>` = `%LOCALAPPDATA%\ipcam-spike` on Windows, so the S4U task, which runs as the same user, sees it; `/var/lib/ipcam-spike` on Linux, Task 8), and at most one `ipcam-hub run` on it, so the clients, every token and P53's refresh schedule live in one catalog.
  - **Files for the hub.** uploadspike writes the hub's composed `run.json`, and any identity file, into `<base>/hub/spike/`, inside the data directory: on Linux owned by `ipcam-spike` with mode 0700, the files 0600 and owned by it, so the hub reaches them through directories it owns. `<base>/state/` (root, 0700 on Linux) holds only uploadspike's own files: the run state, `hub.lock` and `hub.pid`.
  - **No link is followed (Linux, root).** `<base>/hub` belongs to `ipcam-spike`, so a process of that uid could plant a link there, and `fs.protected_symlinks` covers only sticky world-writable directories. So every write and chown that root makes under `<base>/hub` goes through an `os.Root` opened on it, which no path leaves through a symlink or `..`. A file (`run.json`, MI5's identity file) is created as `<name>.tmp` with `O_CREATE|O_EXCL` after `Root.Remove` of any old `.tmp` (`O_EXCL` fails on a symlink), written, chowned on its descriptor (`File.Chown`) and renamed over `<name>` with `Root.Rename`, which replaces a link instead of following it. An existing `catalog.db*` file is re-owned through one descriptor, so the check and the chown cover the same inode: `Root.Lstat` must show a regular file, `Root.OpenFile` opens it read-only with `O_NONBLOCK` (a FIFO swapped in cannot block the start), `File.Stat` on that descriptor must show a regular file with `Nlink == 1`, and `File.Chown` re-owns that descriptor. A name swapped for a hard link after the open changes nothing, because the chown never resolves the name again. Anything else stops the start with an error naming the path.
  - **Lock.** The uploadspike process that runs the hub holds `<base>/state/hub.lock` (`runstate.Lock`) for as long as the hub runs, and only that process listens on 7091 (Global Constraints). Two hubs never share a data directory: c1's recspike rule (L836), which `ipcam-hub` itself does not enforce. A process that has just taken the lock runs `hubproc.WaitGone` on `<base>/state/hub.pid` before it starts a hub, so a previous holder's hub that is still stopping is gone first.
  - **Hub lifetime.** The hub never outlives its holder. c1's hubproc gives no such bond: nothing in it ends the hub when recspike dies. c2's copy adds the bond below. Its clean stop is the console-free graceful stop that c1 Task 7.2 ships (`hubproc.Stop`), so a holder with no console (`resume` under the S4U task or the systemd unit) also stops its hub cleanly.
    - *Linux:* `ipcam-hub` starts in a session of its own (`Setsid`), so a terminal hangup or Ctrl-C reaches only uploadspike, and with `Pdeathsig: SIGTERM`. The start runs on a goroutine locked to its OS thread (`runtime.LockOSThread`) for the child's whole life, because Linux sends that signal when the creating thread ends (go.dev/issue/27505). Go sets it after the switch to `ipcam-spike`, which would otherwise clear it (syscall/exec_linux.go).
    - *Windows:* `ipcam-hub` starts in a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, whose only handle uploadspike holds, not inheritable. When uploadspike ends, however it ends, the hub ends with it, and so does its MediaMTX, which sits in the hub's own nested job (c1 C07).
  - **Interactive holders hand back.** `setup`, `mi6`, `mi6 --b1`, `mi6 reader --hub` and `mi5` catch SIGINT, SIGTERM and SIGHUP on Linux, and CTRL_C, CTRL_BREAK and CTRL_CLOSE_EVENT on Windows (Go delivers a console close as SIGTERM, and Windows ends the process about 5 s later). On any of them the holder stops its hub, if it runs one, marks an open MI5 run interrupted, saves the run state and releases the lock. A holder that dies with no handler (SIGKILL, a crash, the 5 s running out) takes its hub with it (above), and its death releases the lock. Either way the autostart's waiting `resume` takes the hub.
  - **Self-healing autostart.** `uploadspike resume` (Task 8's task or unit) runs for the whole test. It waits until the run state has an open run. Then it takes `hub.lock`, retrying every 5 s while another process holds it, and runs the hub. After a handover it stops the hub, releases the lock and waits again; when no open run is left, it stops the hub and waits. It ends only at `uninstall`, and the autostart restarts it if it ever ends (Task 8: systemd `Restart=always`; on Windows a 5-min repetition with `IgnoreNew`). So within 5 min of any end of an interactive holder, exactly one hub runs again under `resume`, and MI6's refresh days go on. The placements of destinations that leave the composition (an MI5 run cut short leaves `dst-1`'s, `dst-2`'s or `dst-od`'s pending) stay in the catalog untouched: the outbox reads only its configured destinations' rows (Task 4), so the `resume` hub neither uploads them nor fails on them. The cut run's torn segments, which this hub's startup reconcile finalises, get no placement at all, since no MI5 route is in its composition. The next MI5 run counts neither these segments nor the leftover placements, because its routes start at its own start (`fromMs`, Task 8). When `resume` takes the lock over an open MI5 run whose `mi5` has ended or marked it interrupted, it first removes the stale block (`RemoveStale`), then marks the run invalid (Task 8), as a command that takes the lock first does (MI5 is never handed over, below).
  - **Composition.** The hub's `run.json` is built from the device's open runs: MI6's destinations from day 0 to its close and, while an MI5 run is open, MI5's destinations, jobs and routes (Task 8). `sdCardConfirmed` comes from the run state.
  - **Handover.** A command that needs the hub first checks that no MI5 run with a live `mi5` is open (below), then records its `Claim` in the run state (its PID and start time, valid 90 s). A `resume` that gets the lock while another live process's claim is valid releases it and waits again, so it never overtakes the caller. While another process holds the lock, the caller sends `POST /spike/v1/run/handover` to `127.0.0.1:7091`. Unless an MI5 run is open, the holder runs `diag scan-secrets` with its control tokens, stops the hub, saves the run state and releases the lock; an interactive holder then exits 0, and `resume` waits again. A holder that gives up the hub while one of its logins is open (from that login's `Begin` until its `oauth_completed` or `oauth_failed`), to a handover or on a signal above, records no handoff, plan or refresh outcome for that login, so the pair stays as it was; after a handover it prints «Autentificarea s-a întrerupt: alt program al probei a preluat hub-ul. Rulați din nou: <comanda>.», where `<comanda>` is the command that started the login (`uploadspike mi6` or `uploadspike mi6 --b1 <platform>`). The caller takes the lock within 60 s, or stops with «Alt program al probei ține hub-ul. Închideți-l și încercați din nou.».
  - **MI5 is never handed over.** An MI5 run is open from `mi5`'s start, once it holds the lock, until its close or until it is marked invalid; the run state records with it the PID and start time of the `mi5` that opened it. While one is open on the device, its holder answers a handover with 409 and keeps running. A command that needs the hub (`setup`, `mi5`, `mi6`, `mi6 --b1`, `mi6 reader --hub`) and finds the open run in the run state while that `mi5` lives (same PID and start time) and has not marked it interrupted (above) records no claim; one whose check passed before the run opened gets the 409 and removes its claim. Either way it records no outcome, prints «Proba MI5 rulează; rulați comanda după ce se termină.» and exits 1. An open MI5 run whose `mi5` has ended or marked it interrupted (a reboot, a crash, a closed window) is ended by whichever process takes `hub.lock` next: the waiting `resume`, or, when no `resume` takes it first (no autostart, or a broken one), the next command that needs the hub. Under the lock, before any hub starts, that process removes the stale block (`RemoveStale`; on Windows a command that is not elevated relaunches itself elevated for it, as `mi5` does), marks the run invalid (Task 8), then goes on. So a dead `mi5` never leaves the device refusing every command. A block rule exists only inside an open MI5 run: Task 8 adds it after the run opens, and `mi5`, `unblock` or `resume`'s `RemoveStale` removes it before the run closes or turns invalid. So a block never cuts another command's hub, and no B1 attempt is made, or recorded as a failure, while one exists.
  - **Back to the autostart.** An interactive command with nothing left to do in the foreground (`mi6` after the visibility matrix, `mi6 --b1` after its login, `mi5` after its close) stops the hub and releases the lock. The autostart's waiting `resume` takes it at once and runs the open runs until day 7, so closing the window after that never stops the 7-day hub. Without the autostart, the command keeps running and says «Lăsați fereastra deschisă până în ziua 7.».
- **SD-card confirmation (P02, P17; c1 G16).** On Linux, `sudo ./uploadspike setup` (and Task 8's `install`) runs `hostinfo.Read` on `<base>/hub/rec`. For `storage: sd` with no answer in the run state, it shows `sdconfirm.Warning`, asks `sdconfirm.Prompt`, and stores the `SdConfirmation` in the run state. Every hub start on that device (`mi6`, `mi5`, `resume`) sets `sdCardConfirmed` from it and copies it into the envelope; without it no hub starts there. `ErrSdCardUnconfirmed` reads «Hub-ul nu pornește: cardul SD nu este confirmat. Rulați sudo ./uploadspike setup și scrieți DA.»; `ErrNoSpikeUser` reads «Rulați întâi sudo ./uploadspike install.».
- **MI6 driver (P21, P39, P53, C2-R1).**
  - **MI6's part of the composed `run.json`:** `jobs: []`; `uploads.destinations` = `g-android`, `g-windows`, `g-web` (+ `m-android`, `m-windows`, `m-web` with `--onedrive`, only when A5 passed); no routes. `--drive personal` (D1-C) means the owner logs in with the personal account; every other value means the test account. All logins use one Google account (and one Microsoft account), recorded as `acct-1` with `AccountKind`.
  - **Logins on day 0.** For each pair, uploadspike shows the Romanian prompt for that platform and waits for `oauth_completed`. The LAN listener opens for a login whose platform runs on another device: Android always; Windows and web when uploadspike runs on Linux. Handoffs are counted per account and client and capped at 20: the 21st is refused with «Ați atins limita de 20 de autentificări pentru acest client. Spuneți-ne.».
  - **Visibility, after the logins.** Creators `android`, `desktop`, `tv` × readers `web`, `tv`, `android`, `desktop` = 12 cells; the 3 same-client cells are `Self` and not judged, leaving 9. Hub readers use `diag visibility`; the `web` reader is the GIS page.
  - **Schedule.** uploadspike prints the dated schedule «Programul probei»: logins on day 0, MI8 from day 0–1 for 72 h, MI5 on days 1–5, `uploadspike status` once a day, the hub on for 7 days, results on day 7. It records each `oauth_refresh` into the pair's `Refresh`.
  - **B1 (P39).** When plan A fails for a pair, the owner runs the scenario again on that platform with B1 (the app shows the code) the same day, and uploadspike sets `Plan = "B1"`. A redemption refused on day 0 is asked in `mi6`'s own window. A refresh day that fails under `resume`, which has no window, shows in `uploadspike status` and `status.html` as «Reconectați <platformă> cu B1: uploadspike mi6 --b1 <platform>», which the daily `status` check of the schedule reads. While an MI5 run is open on the device, the line reads «Reconectați <platformă> cu B1, după proba MI5: uploadspike mi6 --b1 <platform>», and `mi6 --b1` is refused until the run ends, recording no B1 outcome (MI5 is never handed over, above). `uploadspike mi6 --b1 <platform>` takes the hub through the handover, runs that pair's B1 login (opening the LAN listener when the platform runs on another device), and gives the hub back. The B1 token is refreshed on the remaining days; the pair's refresh evidence is the series of the pair that shares its client (Drive: `g-web`; OneDrive: the other `m-*` pairs), and the 7-day schedule is not extended.
  - **Gaps (P49, P53).** A heartbeat every minute feeds `runstate`. A day whose attempts all fall inside a `reboot` or `sleep` gap is `blocked-by-test`; a hub crash is never relabelled.
  - **Close.** The envelope is written at day 7 or on `uploadspike results`. Then `ipcam-hub diag scan-secrets --dir results/` (Part A) runs with this process's control tokens on its stdin. The hub adds every client ID, client secret and refresh token it holds sealed, so a close after any number of reboots and handovers still scans with every secret typed into the setup. A non-empty `removed` fails the run.
- **`mi6.Judge(envs []results.Envelope) results.Judgement` (G15, P22).**
  - The Phase 0 pairs are Drive × android, windows and web; OneDrive pairs are judged only when `OneDriveEnabled`, else they render as `Deferred("înregistrarea Microsoft cere un director plătit (A5)")`.
  - A pair passes with `HandoffOK` and days 1–6 `ok`, under plan A, or under B1 after a plan-A failure (P39). A failed day or a failed redemption under plan A is never final: it starts B1 that day. A failed B1 handoff, or a failed day under B1, → `pică → B2`. A login cut by a handover or a signal is no attempt and is not in the envelope (Handover, above). A `blocked-by-test` day with no failed day → `Incomplete`. `expected-expiry` on day 7 is ignored.
  - B1 → Decision «G15: <platformă> × <furnizor> folosește B1 (device flow pe hub).». B1 failed → `FailWith("B2 (proxy fără stare în cloud, §5.6)")` and Decision «G15: <platformă> × <furnizor> trece pe B2: proxy fără stare în cloud (§5.6), construit cu cloud-ul în Faza 2; rândurile din harta datelor și din politica de confidențialitate se actualizează întâi.».
  - Each reader platform with a failed judged cell (reader `web` and `tv` → web, `android` → Android, `desktop` → Windows) adds Decision «F6 pe <platformă> folosește tipul de client al hub-ului (creatori: <listă>).» and the gate is `FailWith("F6 folosește tipul de client al hub-ului")`.
  - No judged pair → `Incomplete`. Platform names in Romanian: Android, Windows, web.

- [ ] **Step 1 (Part A): Write the failing tests.**
  - `TestSealer_RoundTrip`, `TestSealer_LabelBound` (another label → `ErrSealedLabel`), `TestSealer_BackendNames`, `TestSealer_SoftwareKeyIs0600` (Linux; a 0644 key is refused), `TestSealer_DPAPIUsesMachineScope` (Windows; the flag seam sees `CRYPTPROTECT_LOCAL_MACHINE`), `TestSealer_SealToFile` and `TestSealer_UnsealFromFile` (skipped unless `-sealer-blob` is given; the CI round trip).
  - `TestOAuth_RouteTable`: one subtest per table row, with the exact redirect; Windows with port 53123 → `http://127.0.0.1:53123/`.
  - `TestMI6_ClientTypePerPlatform` (FLW-31, strict fake): a "web" client with no secret is refused; a mismatched `redirect_uri` → `invalid_grant`; web × Google → device flow; the hub repeats `Begin`'s `RedirectURI` byte for byte.
  - `TestOAuth_StateSingleUseAndExpiry`: `Complete` at 10 min + 1 s → `ErrStateExpired` and the verifier wiped; a second `Complete` with the same state → `ErrStateUsed`; an unknown state → `ErrStateUnknown`.
  - `TestHandoff_VerifierNeverLeavesHub` (Review Focus 5, FLW-22): an attacker redeeming the code at the fake token endpoint without the verifier gets `invalid_grant`; the verifier (from an injected `Rand`) appears in no control response, event or stderr line.
  - `TestRefresh_Day7Expiry_NotCountedAsFailure` (Review Focus 1): fake clock and Testing mode; days 1–6 `ok`, day 7 `expected-expiry`.
  - `TestRefresh_DayCountedFromIssuance`: issuance at 23:50; day 1's first attempt is 24 h later, not at the next midnight.
  - `TestRefresh_RetriesEvery15MinWithinDay`: three failed attempts then a success → day `ok` with 4 attempts; all 96 attempts failing → `failed`.
  - `TestOAuth_RefreshAfterKill9Restart` (P19, P27): a helper process refreshes (the fake rotates and revokes) and is killed; a new `Store` on the same catalog unseals the rotated token and refreshes. Runs on the `windows` job too.
  - `TestStore_TwoStoresSameCatalog_NoStaleRefresh`: two `Store`s on one catalog file alternate 10 refreshes against the rotating fake; all succeed.
  - `TestTokenSource_InvalidGrantIsAuthExpired`: `errors.Is(err, storage.ErrAuthExpired)`.
  - `TestOAuth_NoPlaintextRefreshTokenInDataDir`: after a login, the refresh token's bytes appear in no file under the data directory.
  - `TestControl_RequiresBearer`, `TestControl_BindsLoopbackOnly`, `TestControl_ClientsSealed` (the client secret's bytes are absent from the catalog file), `TestControl_CompleteUnblocksEveryDestinationOfItsTokenRef`, `TestControl_ResetConnections` (an in-flight chunk to the fake is cancelled, counted transient and retried on a new connection).
  - `TestRefresh_SlowTokenEndpointDoesNotBlockCatalogWrites`: a token endpoint that holds the request 30 s makes `Refresh` return an `ErrTransient` within 6 s, while a concurrent `PutSegment` with its enqueue on the same catalog commits within 1 s.
  - `TestTokenSource_NetworkErrorIsTransient`: a refused connection, a timeout and a 503 from the token endpoint each give `errors.Is(err, storage.ErrTransient)`; `invalid_grant` on an unchanged row gives `ErrAuthExpired`.
  - `TestOutbox_TokenEndpointOutageRecoversWithoutAction`: fake clock; the Drive destination's access token expires and the fake token endpoint is unreachable for 6 h, then answers: every placement is verified, the destination is `OK`, and `Unblock` is never called.
  - `TestDiagScanSecrets_AllEncodings`: a client secret and a refresh token from the catalog and a value from stdin, each planted in each of the eight encodings, are found and their files removed; the output holds no value.
  - `TestDiagScanSecrets_OpensCatalogReadOnly`: the catalog's `user_version` and bytes are unchanged after a scan, and a scan run while another connection holds the write lock finishes without waiting for it.
  - `TestDiagVisibility_MatrixCellAndHideMode`: with `HideFrom(desktop, tv)` the desktop→tv cell is `listed: false, read: false` and the other judged cells are true.
  - `TestDiagVisibility_AccountsDiffer`: exit 5, and the output has no permission ID.
  - `TestARH08_NoTokenBytesInUploadedObjects` (P45): after uploads through `fakegdrive`, no stored object contains the access or refresh token.
  - `TestFakeGdrive_ScopesDriveFilePerClient`.
- [ ] **Step 2 (Part A): Run.** `cd ipcam && go test -count=1 ./hub/internal/crypto/... ./hub/internal/storage/... ./hub/internal/catalog/... ./hub/cmd/ipcam-hub/...` — Expected: FAIL.
- [ ] **Step 3 (Part A): Implement**; tell the c1 controller about the `catalog.go` edit.
- [ ] **Step 4 (Part A): Run** the same with `-race` — Expected: PASS; both CI jobs green; the S4U step's output contains `--- PASS: TestSealer_UnsealFromFile`.
- [ ] **Step 5 (Part A): Commit** — `git commit -m "ipcam: add hub OAuth store, sealer and control endpoint"`
- [ ] **Step 6 (Part B): Write the failing tests.**
  - `TestEnvelope_RoundTripAndSchema`; `TestEnvelope_HostIsPseudonym` (a host other than the four pseudonyms is rejected).
  - `TestClose_ScanAfterRestartUsesSealedClients`: a new uploadspike process, with no secret in RAM, closes MI6, and a client secret planted in `results/` is found through `diag scan-secrets` and fails the run.
  - `TestRunState_RebootGapMarkedBlockedByTest` and `TestRunState_SleepGapFromWallVsMono`.
  - `TestHandoffServer_LanListenerOnlyDuringRemoteLogin` (on Linux CI with the real port 7091, while `127.0.0.1:7091` listens; the only test that binds the real port; every other one sets `Options.Port` to a free port): no LAN socket before; during an Android login a socket on each of the runner's RFC 1918 addresses and none on `0.0.0.0`; closed after `complete`, and after 10 min without one.
  - `TestHandoffServer_CORSAllowsShellOriginsOnly`: `Origin` `http://localhost`, `http://tauri.localhost` and `https://tauri.localhost` get `Access-Control-Allow-Origin` on `/spike/v1/oauth/*`; their `OPTIONS` with `Access-Control-Request-Private-Network: true` gets `Access-Control-Allow-Private-Network: true`; another origin gets no CORS header and its POST gets 403; `/spike/v1/visibility/*` answers only `http://localhost:7091`.
  - `TestHubLock_SecondHubOnSameDataDirRefused` and `TestHandover_SecondCommandTakesTheHubAndKeepsOpenRuns`: a second uploadspike process gets the lock only after the holder's handover, and its hub's `run.json` keeps MI6's destinations; a handover request without the run-state token gets 401.
  - `TestHandover_RefusedWhileMI5Open`: with an open MI5 run in the run state whose recorded `mi5` is the live holder, the holder answers a direct handover request with 409 and keeps its stand-in hub (same PID); `mi6 --b1 android` finds the open run, sends no handover request, exits 1 with «Proba MI5 rulează; rulați comanda după ce se termină.» and records no claim and no B1 outcome; a caller whose check passed before the run opened (a test seam between its check and its request) gets the 409, removes its claim and exits 1 the same way; a failed `g-android` refresh day makes `status` and `status.html` show «Reconectați Android cu B1, după proba MI5: uploadspike mi6 --b1 android». Task 8's `TestHandover_RefusedWhileMI5Runs` repeats it with a real `mi5` during its block, and Task 8's `TestMI5_DeadMI5WithoutResume_NextCommandEndsTheRun` covers a dead `mi5`.
  - `TestHubLife_HolderEndsAnyWay_OneHubUnderResume` (both CI jobs; the stand-in hub is the test binary in a helper mode that records its start, the signals it gets and its exit; a `resume` runs as the autostart runs it): an `mi6` holder is ended in turn by SIGHUP, SIGINT, SIGTERM and SIGKILL (Windows: CTRL_BREAK_EVENT, SIGTERM injected into the handler, which is what Go makes of CTRL_CLOSE_EVENT, and TerminateProcess). After each, within 5 min (seconds in practice) exactly one stand-in hub runs, `resume` is its parent and `hub.pid` names it; the holder's hub has exited, after SIGKILL and TerminateProcess too (Pdeathsig, the Job Object).
  - `TestHubLife_HangupReachesOnlyUploadspike` (Linux): SIGHUP to the holder's process group, as a terminal hangup sends it, never reaches the stand-in hub, which stops through the holder's hand-back.
  - `TestHubproc_StopIsCleanWithoutConsole` (both CI jobs; the `windows` job runs it inside an S4U task through `ci/s4u-run.ps1`, which gives it no console): `Stop` ends the stand-in hub through its clean-stop path (the stand-in records the stop request and exits 0), never through `Kill`.
  - `TestResume_YieldsToLiveClaim` (a `resume` that gets the lock under another live process's valid claim releases it, and the claimant takes it) and `TestResume_WaitsForPreviousHubToExit` (a recorded hub still running is waited for, and killed after the grace).
  - `TestHubproc_SdGateExitIsRomanianPrompt` and `TestHubproc_LinuxFilesOwnedBySpikeUser` (a non-root uid seam): `run.json` and the identity file are in `<base>/hub/spike/`, are 0600 and are chowned to the seam's uid. The test also walks every directory from `<base>/hub` down to them and checks that the seam's uid owns each one with search permission, so it can open the files, not only own them. A `catalog.db-wal` and a `catalog.db-shm` of another owner are chowned back to the seam's uid before the start. A missing user gives `ErrNoSpikeUser`.
  - `TestHubproc_PlantedLinksNeverFollowed` (Linux, the non-root uid seam), one case at a time: a symlink planted at `hub/spike/run.json`, at `hub/spike/run.json.tmp`, at `hub/state/catalog.db-wal` or in place of `hub/spike`, pointing at a file or directory outside `<base>/hub`, or a hard link at `hub/state/catalog.db-shm`, or a regular `catalog.db-shm` that a seam swaps for a hard link to an outside file between `Root.Lstat` and the open, leaves every outside file's bytes, mode and owner unchanged. In the first two cases the start goes on with a regular 0600 `run.json`; in the other four `Start` fails with an error naming the path. The `mi5-lab` job's real run (Task 8) starts the hub as `ipcam-spike` from those paths.
  - `TestMI6_SdCardNeedsConfirmation`: with `diag host` reporting `storage: sd` and no `DA`, no hub starts and no `run.json` holds `sdCardConfirmed: true`; with `DA`, the `mi6` and `resume` hub starts carry `sdCardConfirmed: true` and the envelope carries the confirmation. `TestSdConfirm_RequiresDA`.
  - `TestHandoffServer_ForwardsCodeAndStateOnly`: a body with an extra `verifier` field is refused with 400; the forwarded JSON has exactly `code` and `state`.
  - `TestHandoffServer_SetupAndReaderOnlyOnLoopback`.
  - `TestHost_D2Recommendation`: Pi 4, 5, 400 and 500 on arm64 → `B`; Pi 4 on armv7 → `A`; Pi 3 → `A`; unknown → `A`; an armv6 Pi → `A` with «Pi-ul nu poate rula hub-ul».
  - `TestHost_UnknownModelUsesDeviceTree`: a `diag host` with no model and a device-tree model of a Pi 4 give tier `H1b`, `RecommendedMax` 4 and `Source: "device-tree"`.
  - `TestSetup_PiTerminalTakesJSONOrValues`.
  - `TestMI6_TokenNotInResults` (Review Focus 5, end to end; needs `MEDIAMTX_BIN`, which `run` starts even with no jobs): `fakeproviders` with `fakeoauth` issues a known code, access token and refresh token, each assembled from parts, and the hub's verifier comes from an injected seed. After a full MI6 run on the fakes, none of the four appears in `results/` or in uploadspike's or `ipcam-hub`'s stdout, stderr or events. The payloads the handoff server received carry the code by design: their keys are exactly `code` and `state`, and the verifier and both tokens appear in them 0 times.
  - `TestMI6_PlanAFails_RunsB1SameDay`: a refused Android redemption → B1 prompted that day, `Plan == "B1"`, and the pair's refresh evidence is `g-web`'s series.
  - `TestMI6_FailedRefreshDay_StatusAsksB1`: a refresh day of `g-android` that fails under `resume` makes `status` and `status.html` show «Reconectați Android cu B1: uploadspike mi6 --b1 android»; a first `mi6 --b1 android`, whose hub `uploadspike setup` in another process takes through a handover between its `Begin` and the device flow's completion, exits 0 with «Autentificarea s-a întrerupt: alt program al probei a preluat hub-ul. Rulați din nou: uploadspike mi6 --b1 android.», and leaves the pair's `Plan`, its `HandoffOK`, its `Refresh` series and the client's `Handoffs` count unchanged, with `status` still asking for B1; a second `mi6 --b1 android` then takes the hub through the handover, completes the device flow on the fakes, sets `Plan == "B1"`, and the hub returns to the waiting `resume`.
  - `TestStatus_IsAClient`: `status` opens no listener, and with a holder on 7091 both report the same open runs.
  - `TestMI6_HandoffCapIs20`.
  - `TestMI6_VisibilityMatrixShape`: 12 cells, 3 `Self`, 9 judged.
  - `TestG15_AllPairsOk_Trece`, `TestG15_BlockedDay_FaraVerdict`, `TestG15_EmptySet_FaraVerdict`, `TestG15_B1Fails_PicaB2` (exact Decision line), `TestG15_FailedDayUnderB1_PicaB2`, `TestG15_VisibilityFails_HubClientType` (exact «F6 pe web …» line naming `desktop`), `TestG15_Day7Expiry_NotFailure`, `TestG15_OneDriveDeferredWithoutA5`.
- [ ] **Step 7 (Part B): Run.** `cd ipcam && go test -count=1 ./tools/spikes/upload/...` — Expected: FAIL.
- [ ] **Step 8 (Part B): Implement.** uploadspike keeps every on-screen label in Go constants (Task 12 checks the guide against them).
- [ ] **Step 9 (Part B): Run** with `-race` and `MEDIAMTX_BIN` set — Expected: PASS; CI green.
- [ ] **Step 10 (Part B): Commit** — `git commit -m "ipcam: add uploadspike MI6 OAuth-on-hub driver for G15"`

### Task 9a: OAuth handoff in the Android and Windows spike shells

Rulings: P20, P33, P36 (the OneDrive redirects), P47, P52 (the custom scheme; B1 ready for Android × Drive). New in v2; it starts after c1 Task 10 (P30).

**Files:**
- Modify: `ipcam/apps/mobile/android/app/src/spike/AndroidManifest.xml` (on `.MainActivity`: an intent filter with `VIEW`, `DEFAULT`, `BROWSABLE` and `android:scheme="ro.ipcam.spike"`)
- Modify: `ipcam/apps/mobile/package.json` (`@capacitor/browser`, `@capacitor/app`), and `ipcam/apps/mobile/android/app/capacitor.build.gradle` and `ipcam/apps/mobile/android/capacitor.settings.gradle` as `cap sync android` regenerates them
- Create: `ipcam/apps/mobile/test/oauth-intent.test.ts`
- Modify: `ipcam/apps/desktop/src-tauri/{Cargo.toml,Cargo.lock,src/main.rs,tauri.conf.json}`; Create: `ipcam/apps/desktop/src-tauri/capabilities/spike-oauth.json`
- Create: `ipcam/tools/spikes/video/app/src/{oauthbridge.ts,oauthbridge.test.ts}`, `ipcam/tools/spikes/video/app/src/scenarios/{oauth.ts,oauth.test.ts}`
- Modify: `ipcam/tools/spikes/video/app/src/{results.ts,scenarios/index.ts,i18n.ts}`, `ipcam/tools/spikes/video/app/package.json` (`@capacitor/browser`, `@capacitor/app`, `@tauri-apps/plugin-opener`, `@fabianlars/tauri-plugin-oauth`), `ipcam/pnpm-lock.yaml`
- Modify: `ipcam/tools/spikes/upload/markers_test.go` (`markedNonGoFiles` += `../video/app/src/oauthbridge.ts`, `../video/app/src/scenarios/oauth.ts`)

**Interfaces:**
- Consumes: Task 9's uploadspike API on port 7091.
- Produces:

```ts
// oauthbridge.ts (SPIKE-ONLY, c2's marker on line 1)
export type OAuthPlatform = 'android' | 'windows' | 'web';
export interface OAuthBridge {
  readonly platform: OAuthPlatform;
  /** Windows: starts tauri-plugin-oauth's loopback listener and returns its port; android and web: null. */
  startLoopback(): Promise<number | null>;
  /** Opens url in the system browser: Custom Tabs (@capacitor/browser) on Android, the default browser (plugin-opener) on Windows. */
  openSystemBrowser(url: string): Promise<void>;
  /** The first redirect URL starting with prefix: appUrlOpen (@capacitor/app) on Android, the loopback request on Windows. */
  waitForRedirect(prefix: string, timeoutMs: number): Promise<string>;
}
export function detectBridge(): OAuthBridge; // Capacitor native android → android; window.__TAURI_INTERNALS__ → windows; else web

// scenarios/oauth.ts (SPIKE-ONLY)
export interface OAuthRunInput { hub: string; bridge: OAuthBridge; fetch: typeof fetch; now: () => number }
export type OAuthOutcome =
  | { kind: 'completed' }
  | { kind: 'device-code'; verificationUrl: string; userCode: string }
  | { kind: 'failed'; reason: 'state-mismatch' | 'timeout' | 'denied' | 'hub' };
export function runOAuth(input: OAuthRunInput): Promise<OAuthOutcome>;
```

- **Flow.** `GET <hub>/spike/v1/oauth/next?platform=<p>` gives `{dest, provider, plan}`; `GET /spike/v1/oauth/begin?…&port=<loopback port on Windows>`. Plan A: open `AuthURL` in the system browser, wait for the redirect (5 min), check its `state`, then `POST /spike/v1/oauth/complete` with exactly `{"state","code"}` as a `text/plain` body, as `results.ts` does, so neither WebView sends a preflight. `next` and `begin` are plain GETs with no custom header; uploadspike answers the shells' origins with CORS (Task 9). B1: show `VerificationURL` and `UserCode` and post nothing; the hub polls. `<hub>` is the address the owner enters, `http://<hub device>:7091`.
- **Scenario.** `ScenarioId` gains `'OAUTH'`, registered in `scenarios/index.ts`. Romanian texts in `i18n.ts`: `oauthScenario: 'Conectare la stocare (MI6)'`, `oauthOpening: 'Se deschide pagina de autentificare…'`, `oauthDeviceCode: 'Deschideți {url} și scrieți codul {code}.'`, `oauthDone: 'Conectat. Puteți închide pagina browserului.'`, `oauthFailed: 'Conectarea nu a reușit: {reason}.'`.
- **Tauri.** `main.rs` adds exactly two plugin registrations, `.plugin(tauri_plugin_opener::init())` and `.plugin(tauri_plugin_oauth::init())`: the shell's Rust is at 290 of the 299 lines that `ipcam-windows.yml` enforces, so a hand-written listener does not fit. `capabilities/spike-oauth.json` grants window `main` `core:default`, `oauth:allow-start`, `oauth:allow-cancel` and `opener:allow-open-url` scoped to the two authorization hosts: `{"identifier":"opener:allow-open-url","allow":[{"url":"https://accounts.google.com/*"},{"url":"https://login.microsoftonline.com/*"}]}`. Without a scope entry, tauri-plugin-opener's `is_url_allowed` refuses every URL (plugins-workspace v2, `plugins/opener/src/scope.rs`), so a bare `opener:allow-open-url` would refuse `openUrl(AuthURL)`. `tauri.conf.json`'s CSP `connect-src` gains `http://localhost:7091 http://127.0.0.1:7091 http://*:7091`. Google redirects to `http://127.0.0.1:<port>/`; OneDrive, only if A5 passed, to `http://localhost:<port>/oauth/callback`.
- **Android.** Google Android client redirect `ro.ipcam.spike:/oauth2redirect`; OneDrive `ro.ipcam.spike://oauth/callback`. Google discourages custom schemes and keeps them off by default, so Android × Drive B1 is ready in advance (P52); if Google refuses the scheme, Android runs B1 and the run records it.
- **CI.** `ipcam-apps`, `ipcam-mobile` and `ipcam-windows` cover these paths already and build the changes; Task 12 attaches the APK and the installer.

- [ ] **Step 1: Write the failing tests** (Vitest):
  - `oauthScenario_android_opensCustomTabAndPostsCodeAndState`
  - `oauthScenario_windows_beginCarriesLoopbackPort`: begin's URL has `port=53123`; the redirect `http://127.0.0.1:53123/?code=C&state=S` posts `{"state":"S","code":"C"}`.
  - `oauthScenario_b1_showsUserCodeAndPostsNothing`
  - `oauthScenario_payloadIsExactlyCodeAndState`: the posted JSON's keys are exactly `["code","state"]`.
  - `oauthScenario_stateMismatch_postsNothing`
  - `oauthBridge_detectsPlatform`
  - `tauriCsp_allowsHandoffPort` (reads `apps/desktop/src-tauri/tauri.conf.json`).
  - `tauriCapability_scopesOpenUrlToAuthHosts` (reads `apps/desktop/src-tauri/capabilities/spike-oauth.json`): the `opener:allow-open-url` entry is scoped to exactly `https://accounts.google.com/*` and `https://login.microsoftonline.com/*`, and no unscoped opener permission is granted.
  - `oauthScenario_requestsNeedNoPreflight`: `complete` is a POST whose only header is `Content-Type: text/plain`, with the JSON body; `next` and `begin` are GETs with no custom header.
  - `spikeManifest_hasOAuthIntentFilter` (`apps/mobile/test/oauth-intent.test.ts`): the spike manifest's `.MainActivity` filter has `VIEW`, `DEFAULT`, `BROWSABLE` and scheme `ro.ipcam.spike`.
  - Go: `TestMarkedNonGoFiles` now covers the two TS files.
- [ ] **Step 2: Run.** `pnpm -C ipcam --filter spike-video-app test && pnpm -C ipcam --filter @ipcam/mobile test && cd ipcam && go test ./tools/spikes/upload/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Add the packages at the newest versions ≥ 14 days old (major 8 for Capacitor), run `pnpm -C ipcam install`, `pnpm -C ipcam --filter spike-video-app build` (the shell's `webDir`, which `cap sync` copies and fails without) and `pnpm -C ipcam --filter @ipcam/mobile sync`, and commit the lockfile and the regenerated Gradle files. The Gradle build itself runs in `ipcam-mobile`, not here.
- [ ] **Step 4: Run** the same, plus `pnpm -C ipcam typecheck && pnpm -C ipcam lint` and `tools/scripts/count-native-lines.sh 299 apps/desktop/src-tauri/src` — Expected: PASS; the line count is ≤ 299 (292 expected). Push: `ipcam-apps`, `ipcam-mobile` (APK build) and `ipcam-windows` (cargo-deny licenses, line count, installer) are green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add OAuth handoff to the Android and Windows spike shells"`

### Task 8: MI5 upload-during-outage spike — G14

Rulings: P02 (Task 9's SD confirmation, reused), P09, P10, P14, P15, P16, P17, P18, P19 (one hub per device), P38, P49, P53 (the block avoids a due refresh), P56, C2-R1, C2-R2, C2-R4.

**Files:**
- Create: `ipcam/tools/spikes/upload/mi5/{config.go,runner.go,ranges.go,wanblock.go,wanblock_windows.go,wanblock_linux.go,verify.go,restart.go,audit.go,judge.go}` and tests
- Create: `ipcam/tools/spikes/upload/linkclient/{client.go,messages.go}` and test (a copied minimal client of c1's link protocol: `hello` → `session`)
- Create: `ipcam/tools/spikes/upload/install/{install.go,task_windows.go,unit_linux.go,user_linux.go,layout_linux.go,keepawake_windows.go,keepawake_other.go}` and tests (keep-awake copied from `tools/spikes/video/cmd/spikehub/keepawake*.go`)
- Create: `ipcam/tools/spikes/upload/cmd/uploadspike/{mi5.go,install.go,unblock.go,annotate.go}`; Modify: `cmd/uploadspike/main.go`
- Create: `ipcam/tools/spikes/upload/cmd/uploadspike-probe/{main.go,main_test.go}`
- Modify: `ipcam/tools/spikes/upload/results/envelope.go` (`Mi5Data`); `ipcam/tools/spikes/upload/{go.mod,go.sum}` (`filippo.io/age v1.3.2` only; x/sys and x/term came with Task 9)
- Modify: `.github/workflows/ipcam-spike-upload-lab.yml` (job `mi5-lab`; the `build` job also builds `uploadspike`), `.github/workflows/ipcam-spike-upload.yml` (`windows` job: the WanBlock smoke; the `go test` step of both jobs gains `-timeout 30m`, Step 2)

**Interfaces:**
- Consumes: Task 7b (`run.json` `uploads`, upload events, `diag reach`, `diag readopen`, `diag dest-probe`, `diag uploads`), Task 9 (`hubproc`, `hostinfo`, `runstate`, `results`, `sdconfirm`, the token store, the hub lock and the handover), Task 7 (`fakeproviders`), c1's `reccontroller --local`, `diag host`, the SD gate.
- Produces:

```go
package mi5 // SPIKE-ONLY
type DestConfig struct{ ID, Kind, Account, TokenRef string /* Kind as in run.json: "GOOGLE_DRIVE" | "ONEDRIVE_PERSONAL" */; Cameras []string; Window time.Duration /* every route's fromMs = the run's start; 0 = untilMs 0 (to the run's end); else untilMs = run start + Window (Task 7b) */ }
type Mi5Config struct {
    HubBin, MediaMTXBin, ReccontrollerBin, MediaDir, DataDir string
    Cameras int                 // 4 on the PC; min(4, diag host's RecommendedMax) on a Pi; a Pi with RecommendedMax 0 refuses (Host, below)
    SegmentSecs int             // 300 owner, 30 lab
    PartSize int64              // 0 owner (the driver's Caps); 512 KiB in the lab, accepted only with the fakes
    EventCamera string          // "cam01": class EVENT_CLIP, the stand-in for event clips
    Drive string                // D1: "test" (default) | "test+personal" | "personal"
    OneDrive bool               // only when A5 passed (C2-R4)
    BlockFor time.Duration      // 6 h owner, 15 min lab (P09)
    ProviderHosts []string      // host[:port], 443 by default: www.googleapis.com, oauth2.googleapis.com, accounts.google.com; with OneDrive: graph.microsoft.com, login.microsoftonline.com; lab: 198.51.100.1:18080
    LanProbe string             // owner: the default gateway's IPv4 address (the home router) on TCP 80, else 53, read from the routing table, kept in RAM, never written; lab: a listener on the runner's RFC 1918 address
    ReadOpenCalls int           // ≥ 20 on a clear camera
    Lab bool
}
func Destinations(cfg Mi5Config) []DestConfig
type WanBlock interface {
    Block(ctx context.Context) error       // one rule group for ipcam-hub and mediamtx only
    Unblock(ctx context.Context) error
    RemoveStale(ctx context.Context) error // at startup
    ScheduleCleanup(ctx context.Context, at time.Time) error
}
func HostFirewall(binDir string) WanBlock // Windows Firewall or nft, chosen by GOOS
type ReachResult struct{ ProviderBlockedV4, ProviderBlockedV6, Lan, Loopback bool }
func VerifyBlocked(ctx context.Context, hub hubproc.Config, hosts []string, lan string) (ReachResult, error) // runs `ipcam-hub diag reach` as the blocked identity
func Run(ctx context.Context, cfg Mi5Config) (results.Envelope, error)
func Judge(envs []results.Envelope) results.Judgement // G14

package results
type Mi5Data struct {
    BlockSecs, RecoverySecs, UplinkMbps float64; DNSResolvedDuringBlock, Lab bool
    Reach []ReachCheck; Dests []Mi5Dest; Restart *Mi5Restart; ReadOpen []ReadOpenRun; InjectedGaps int
}
type ReachCheck struct{ AtMs int64; ProviderBlocked, Lan, Loopback bool }
type Mi5Dest struct {
    Dest, Kind, Account string; Cameras, Segments, SegmentsDuringBlock, Verified, Missing, Duplicates, HashMismatches int
    VerifiedDuringBlock, FailedDuringBlock int; UnreachableDuringBlock bool // from the hub's events between Block + 2 min and Unblock
    CallsPerSegment map[string]float64; QuotaAfterDelete []QuotaSample; Deferred string
}
type QuotaSample struct{ TMs, Free, InBin int64 }
type Mi5Restart struct{ KilledAtMs, Size, LastConfirmedOffset, ResumedOffset int64; TornSegmentUploaded bool }
type ReadOpenRun struct{ Encrypted bool; Calls, OK int; P50Ms, MaxMs float64 }

package linkclient // SPIKE-ONLY, copied from c1's link protocol
type Session struct{ RTSPBase, User, Pass string; Streams []string } // RAM only; reaches ipcam-hub only in run.json, as in c1
func Dial(ctx context.Context, addr, hostPseudonym string) (Session, error)
```

- **Source (P14).** `ipcam-hub` with the fork `mediamtx` records `cam01_sub`…`camNN_sub` (512 kbps) from `reccontroller --local`, in 5-min segments (30 s in the lab). A lab segment is ≈ 1.9 MB, less than one 16 MiB part, so the lab's destination has `partSize` 512 KiB (4 parts per segment; Task 7b accepts it only with the fakes) and `fakeproviders -throttle 1048576` keeps each part ≈ 0.5 s on the wire: the restart then lands between two parts of one segment instead of racing a single PUT on loopback. uploadspike learns the RTSP base, the per-start credentials and the stream names only through `linkclient`. The spool is `ipcam-hub`'s `<dataDir>/rec/`. Routes: `cam01` → `EVENT_CLIP`, the others → `CONTINUOUS` (named in «Limitări»). Cameras record clear, so no C1-R9 rewrite replaces a verified copy (P03).
- **Host (P17, P38).** On a Pi, MI5 records min(4, `RecommendedMax`) cameras: 4 on a Pi 5, 500, CM5, Pi 4, 400 or CM4, and 2 on a Pi 3 or Zero 2 W. That is the spec's 4 cameras, fewer only where the model's recommendation is lower: a Pi 5's 8 would put ≈ 11 GB in the backlog, and P38's 2× check would then need ≈ 22 GB free on a 15 GB account. The envelope records the count, and «Limitări» says so. c1's `health/pimodel.go` gives `RecommendedMax` 0 for an `unknown` or `unsupported` tier, so on a Pi whose model is still `unknown` after `hostinfo.Read`'s device-tree fallback, or is `unsupported`, `mi5` starts no hub and refuses with «Nu știm câte camere poate înregistra acest Pi. Rulați proba MI5 pe PC: uploadspike setup, apoi uploadspike mi5.»; MI6 and MI8 on that Pi are unaffected. On the PC, MI5 then runs as under D2-B (its own hub and one device-flow login). The SD confirmation comes from the run state (Task 9), so on an SD card MI5 runs only after `sudo ./uploadspike install` or `setup` asked it; SD results are reported separately. `uploadspike-probe` (GOARM=6) refuses armv6. The envelope records host, Pi model, camera count and storage kind.
- **The device's hub (P19, P53).** MI5 runs in the device's one hub (Task 9). On a device that holds the 7-day MI6 run (the PC under D2-A, the Pi under D2-C), `uploadspike mi5` takes that hub over through the handover, adds MI5's destinations, jobs and routes to its composed `run.json`, and at its close stops its hub and releases the lock, so the autostart's waiting `resume` restarts the hub with MI6's destinations only (Task 9). MI6's destinations stay in the hub throughout and its refresh days go on; the block cuts that hub too, which the pre-checks below account for. On the PC under D2-B, which holds no MI6 run, `mi5` starts the PC's own hub.
  - **Holding the hub for ≈ 8 h.** At its start, `mi5` prints «Lăsați fereastra deschisă până la final.». No other command takes the hub from it: `mi5` answers a handover with 409, and the caller exits with «Proba MI5 rulează; rulați comanda după ce se termină.» (Task 9, MI5 is never handed over). If `mi5` ends any other way (its window or SSH session closes, Ctrl-C, a crash), its hub ends with it, and within 5 min the waiting `resume` takes the hub, removes the block and marks the MI5 run invalid (Task 9's hub lifetime and hand-back; when no `resume` takes the lock first, the next command that needs the hub does it). The 7-day hub goes on with MI6's destinations only. The cut run's unverified placements stay in the catalog, untouched by that hub (Task 4: the queue reads only its configured destinations). Its torn segments, which that hub's startup reconcile finalises, get no placement, because no MI5 route is in that hub's composition. MI5 is run again on another day of days 1–5.
  - **Each run counts only its own segments.** Every MI5 route has `fromMs` = the run's start, the time it opens, before its first hub start (Task 7b's route window). So the drain, the audit (`diag uploads`) and every count of the envelope cover only that run's segments. A cut run's leftover placements upload in the next MI5 run that names their destination, outside its counts, and its torn segments with no placement are in no count.
- **Destinations (C2-R1, P38).** `test` → `dst-1`, the test account, all cameras. `test+personal` → also `dst-2`, the personal Drive (`acct-2`), `cam02` with a route window of the first 2 h (`untilMs`, ≈ 0.46 GB), so no second restart is needed. `personal` → `dst-1` on the personal Drive. With `OneDrive` → `dst-od` gets `cam01` only (≈ 1.4 GB, ≤ 2 GB), because its bin keeps counting for 30 days; without it, the destination's field reads `amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5)`.
  - **Tokens.** A destination whose account already has an MI6 login on this hub reuses that row through `tokenRef`: `dst-1` takes `g-windows` on the PC and `g-web` on the Pi (any MI6 row of the D1 account would do), and `dst-od` takes `m-windows` or `m-web`. Every other destination needs one login on this hub, which `uploadspike mi5` asks for before the block through Task 9's device flow (B1): uploadspike shows the address and the code in its window, and the owner signs in from any browser. That is the PC's extra 5-min login under D2-B, and `dst-2`'s personal login under D1-B. Each counts in the 20-handoff cap.
- **Pre-checks (P38, P53).** Expected backlog per destination = its cameras × 512 kbps × `BlockFor`. Refuse to start, with a Romanian reason, when a destination's `Quota().Free` (`diag dest-probe`) is below 2 × its backlog, when the spool's free space is below 1.5 × the total backlog, or when `--drive personal` has less than 11 GB free. Refuse to start the block when a daily refresh of any token of the device's one hub (MI6's pairs and MI5's own logins) has its first attempt inside [block start, block start + `BlockFor` + 30 min], and print the next allowed start; any overlapping day is recorded `blocked-by-test`. Refuse to start when the LAN probe answers neither TCP 80 nor 53: «Routerul nu răspunde în rețeaua locală; proba nu poate verifica blocarea.».
- **Order of the run.**
  1. **Encrypted pre-run (10 min).** One encrypted job (`cam01_sub`) with an ephemeral age pair (identity in a 0600 file in `<base>/hub/spike/`, Task 9), `diag readopen --profile sub` every 30 s (20 calls).
  2. **Recording and warm-up (10 min).**
  3. **Block.** `RemoveStale`, `Block`, `POST /control/v1/uploads/reset-connections` (Task 9), `ScheduleCleanup(start + BlockFor + 30 min)`. `VerifyBlocked` runs before the clock starts and every 10 min; a failure unblocks and aborts with «Blocarea a tăiat și rețeaua locală; proba s-a oprit.» (LAN or loopback down) or «Furnizorul e încă accesibil prin IPv<4|6>; blocarea nu e completă.». `diag readopen --profile sub` runs ≥ 20 times on `cam02` (every `BlockFor / 24`, at least every 15 min). uploadspike records whether DNS still resolves.
  4. **Unblock** at `BlockFor`.
  5. **Restart (P16).** After the unblock, wait for an `upload_progress` with 0 < offset < size; record that placement's last confirmed offset; `Kill` `ipcam-hub` (MediaMTX dies with it) and start it once more.
  6. **Drain.** Until every placement of the run's own segments is verified (the route window, above). `RecoverySecs` = drain end − unblock; `UplinkMbps` = bytes uploaded during the drain ÷ its seconds.
  7. **OneDrive only.** `diag dest-probe --dest dst-od --delete-segment` → `QuotaAfterDelete`; that placement turns `deleted`, which step 8's audit leaves out of `segments` and `missing` (Task 7b).
  8. **Audit.** `diag uploads --list-remote`; `CallsPerSegment` from the `upload_verified` events.
  9. **Close.** Envelope, then `diag scan-secrets` (Task 9); MI5's jobs, routes and destinations leave the composition: `mi5` stops its hub and releases the lock, and the autostart's waiting `resume` takes the hub (Task 9).
- **Restart assertions (P16).** The resumed session's first offset (`upload_resumed`) is > 0 and ≥ the last confirmed offset seen before the kill, because the provider's status query may report bytes persisted after the last event. Each object exists once, with size and hash verified. The torn segment that the startup reconcile finalises is enqueued and uploaded. The kill's recording gap counts in `InjectedGaps`, never as a missing segment.
- **Block rule (P15, P56).** The allow list matches destination addresses, never an interface: `127.0.0.0/8`, `::1`, `::` (the unspecified address, never a destination; listed so that the IPv6 ranges below start at `::2` and the two sets cover IPv6 with no gap), `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `fe80::/10`, `fc00::/7`, `224.0.0.0/4`, `ff00::/8`, `255.255.255.255`.
  - **Windows.** Rule group `ipcam-spike-wanblock`: one outbound Block rule per program (`-Program` = the absolute paths of `ipcam-hub.exe` and `mediamtx.exe`), whose `-RemoteAddress` lists the complement explicitly. IPv4: `0.0.0.0-9.255.255.255`, `11.0.0.0-126.255.255.255`, `128.0.0.0-169.253.255.255`, `169.255.0.0-172.15.255.255`, `172.32.0.0-192.167.255.255`, `192.169.0.0-223.255.255.255`, `240.0.0.0-255.255.255.254`. IPv6: `::2-fbff:ffff:ffff:ffff:ffff:ffff:ffff:ffff`, `fe00::-fe7f:ffff:ffff:ffff:ffff:ffff:ffff:ffff`, `fec0::-feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff`. There is no Allow rule: a Windows Block rule wins over any Allow rule. `uploadspike mi5` relaunches itself elevated (one UAC prompt) when it is not. The cleanup is a scheduled task `ipcam-spike-unblock` running `uploadspike unblock`.
  - **Linux.** `nft` table `inet ipcam_spike`, chain `output` (hook output, priority 0): `meta skuid "ipcam-spike" ip daddr != { <v4 allow list> } drop` and `meta skuid "ipcam-spike" ip6 daddr != { <v6 allow list> } drop`; fallback `iptables`/`ip6tables -m owner --uid-owner ipcam-spike` with the same sets. The cleanup is `systemd-run --on-active=<BlockFor+30m> --unit ipcam-spike-unblock /opt/ipcam-spike/uploadspike unblock`.
  - **Verification.** `diag reach` runs as the blocked identity: on Windows the same `ipcam-hub.exe`; on Linux under the `ipcam-spike` uid. A provider counts as blocked only on `timeout`, `unreachable` or `denied` (and `no-address` for a family it lacks); `refused` is a reset from the far end, so the path is open and the run aborts. Because a rule added under an established flow may not cut it, the hub's own events are the second check (`mi5.Judge`'s block floor). The rest of the PC stays online.
  - **Deblocare, one line.** Windows: `uploadspike unblock` (or `Remove-NetFirewallRule -Group ipcam-spike-wanblock`); Linux: `sudo ./uploadspike unblock` (or `sudo nft delete table inet ipcam_spike`).
- **Install (P49).**
  - **`uploadspike install` (PC, elevated once; on every PC that runs a hub, guide step 6).** Registers the Task Scheduler task `ipcam-uploadspike` from generated XML: `BootTrigger`, plus a `TimeTrigger` that starts at install time and repeats every 5 min with no end (`Repetition Interval PT5M`); `MultipleInstancesPolicy IgnoreNew`, so a repetition only starts `resume` when none runs (Task 9's self-healing autostart); `ExecutionTimeLimit PT0S`, because the schema's default `PT72H` would end the 7-day `resume` on day 3; `DisallowStartIfOnBatteries` and `StopIfGoingOnBatteries` false; `LogonType S4U`, `RunLevel HighestAvailable`, command `uploadspike resume`, working directory = uploadspike's folder. It then starts the task once (`schtasks /Run /TN ipcam-uploadspike`), so `resume` waits from the install on, before any reboot. uploadspike keeps the PC awake during runs with `SetThreadExecutionState(ES_CONTINUOUS|ES_SYSTEM_REQUIRED)`.
  - **`sudo ./uploadspike install` (Pi).** Creates the system user `ipcam-spike` (`useradd --system --no-create-home --shell /usr/sbin/nologin`). Copies the bundle's binaries and media to `/opt/ipcam-spike` (root, 0755): Raspberry Pi OS's home folders are 0700, so `ipcam-spike` could not reach them in the unpacked bundle. Creates `/var/lib/ipcam-spike` (root, 0755) with `hub/` (the hub's data directory, owned by `ipcam-spike`, 0700), in it `hub/spike/` (the same owner and mode: the `run.json` and identity files that uploadspike writes for the hub, Task 9), and `state/` and `results/` (root, 0700). It creates and re-owns these paths without following a link, as `hubproc.Start` does (Task 9): an existing `hub/` or `hub/spike` that `Lstat` does not show as a directory, a symlink included, is refused. Asks the SD confirmation when `diag host --rec /var/lib/ipcam-spike/hub/rec` reports `storage: sd` (Task 9's `sdconfirm`, stored in the run state). Writes `/etc/systemd/system/ipcam-uploadspike.service` (`User=root`, `After=network-online.target time-sync.target`, `ExecStart=/opt/ipcam-spike/uploadspike resume`, `Restart=always`, `RestartSec=10`, `WantedBy=multi-user.target`) and runs `systemctl enable --now`, so `resume` waits from the install on (Task 9's self-healing autostart). Every later command, run from the bundle or from `/opt/ipcam-spike`, uses `/opt/ipcam-spike`'s binaries and `/var/lib/ipcam-spike`. uploadspike runs as root and starts `ipcam-hub` as `ipcam-spike`, so the block cuts only the hub; `hubproc` hands the hub its files 0600 and owned by `ipcam-spike` (Task 9).
  - **`uninstall`.** Stops and removes the task or unit (`schtasks /End` then `/Delete`; `systemctl disable --now`), which ends the waiting `resume` and its hub, then removes the rule group or table, the cleanup timer, the `ipcam-spike` user and `/opt/ipcam-spike`; the data directory (`/var/lib/ipcam-spike` on Linux) stays.
  - **`--lab`.** Creates the user, `/opt/ipcam-spike` and `/var/lib/ipcam-spike` as above, and no autostart, so the lab runs from the Pi's paths whatever the runner's home folder allows.
- **`uploadspike-probe` (GOARM=6, P17).** Prints the model (`/proc/cpuinfo` `Model`, else `/proc/device-tree/model`) and the archive by `uname -m`: `aarch64` → `spike-upload-linux-arm64.tar.gz`, `armv7l` → `spike-upload-linux-armv7.tar.gz`; on `armv6l`: «Acest Raspberry Pi (armv6) nu poate rula hub-ul. Proba de upload pe Pi trece în Faza 0-bis. Folosiți PC-ul.».
- **Lab (P09, P56).** `uploadspike mi5 --lab --block 15m --segment-secs 30 --part-size 512KiB --cameras 4 --fakes <fakeproviders schema line>`; `--part-size` is accepted only with `--fakes`. It logs in through the fake OAuth provider itself (it fetches `AuthURL`, takes the auto-approved redirect's code and completes it; SPIKE-ONLY lab rig). It opens its own LAN probe listener on the runner's first RFC 1918 address, and its provider hosts are `198.51.100.1:18080`.
  - **Job `mi5-lab`** (`needs: build`, ubuntu-24.04, `timeout-minutes: 90`, setup-go as in the CI workflow): `sudo ip addr add 198.51.100.1/32 dev lo`; start `fakeproviders -listen 198.51.100.1:18080 -throttle 1048576`; mediagen + recmediagen media (ffmpeg); `sudo ./uploadspike install --lab`; as the runner user `go test -c -o "$RUNNER_TEMP/mi5.test" ./tools/spikes/upload/mi5/`, then `sudo env UPLOADSPIKE_REQUIRE_NFT=1 UPLOADSPIKE_HUB_BIN=/opt/ipcam-spike/ipcam-hub "$RUNNER_TEMP/mi5.test" -test.run '^TestWanBlock_KeepsLanAndLoopback$' -test.v` (sudo's `secure_path` holds no setup-go toolchain, and root would get neither the job's `GOTOOLCHAIN` and `GOFLAGS` nor its module cache); `sudo ./uploadspike mi5 --lab …`; then, with `if: always()`, `sudo ./uploadspike annotate emit --code MI5 --set lab-linux | while IFS= read -r l; do printf '%s\n' "$l" | tools/scripts/annotate.sh mi5-lab 1 notice; done` (annotate.sh joins its stdin into one annotation and keeps its first `<lines>`, so each envelope goes in its own call). Fallback when `nft` is unusable on the runner (the job annotates why): the `toxiproxy` v2.12.0 binary pinned by SHA-256, never a Go dependency.
  - **Windows smoke (`windows` job of `ipcam-spike-upload.yml`).** `Get-NetFirewallProfile` is checked and the profiles are enabled for the job if off; `UPLOADSPIKE_REQUIRE_WINFW=1 UPLOADSPIKE_HUB_BIN="$RUNNER_TEMP/bin/ipcam-hub.exe" go test -run '^TestWanBlock_WindowsSmoke$' ./tools/spikes/upload/mi5/`.
- **`uploadspike annotate emit --code <code> --set <set>`.** Prints each envelope of `results/<code>/<set>/` as one raw JSON line with no prefix, each ≤ 3,500 bytes after annotate.sh's escaping (it fails otherwise), at most 10. uploadreport reads each `mi5-lab` notice's message as one envelope (Task 11).
- **`mi5.Judge` (G14, P22).**
  - **Judged set.** Owner envelopes only (`Lab == false`) with `BlockSecs ≥ 6 h`, as MI5 requires. A lab envelope is TST-07's 15-min variant (P09): uploadreport renders it as its own TST-07 row, and it never decides G14. No judged envelope → `Incomplete`.
  - **Floors**, per judged envelope: every non-deferred destination has `SegmentsDuringBlock > 0`; `Restart.ResumedOffset > 0`; and the hub's own evidence that the block held. For every non-deferred destination other than `LOCAL`, between Block + 2 min and Unblock: `VerifiedDuringBlock == 0`, and `FailedDuringBlock > 0` (`upload_failed{reason: transient}`) or `UnreachableDuringBlock` (`dest_state UNREACHABLE`). A missed floor → `Incomplete`; the last one with the reason «blocarea nu a oprit urcarea hub-ului».
  - `trece` when every floor holds and every non-deferred destination has `Missing == 0`, `Duplicates == 0`, `HashMismatches == 0`, `Verified == Segments`, and every `ReadOpen` call succeeded.
  - Missing, duplicate or mismatch → `FailWith("outbox-ul se corectează înainte de Faza 3")`. A failed open-segment read → `FailWith("segmente de 2 min")`. A measured failure decides even when a floor is missed.

- [ ] **Step 1: Write the failing tests.**
  - `TestBlockRanges_ComplementOfAllowList`: the ranges and the allow list (`::` included) together cover all of IPv4 and IPv6 with no gap and no overlap (range endpoints swept); `8.8.8.8`, `142.250.0.1`, `2001:4860::8888` and `2620:1ec::1` are blocked; `192.168.1.20`, `10.0.0.1`, `127.0.0.1`, `::1`, `fe80::1` and `fd00::1` are allowed.
  - `TestWanBlock_WindowsRuleShape`: the generated `New-NetFirewallRule` lines use group `ipcam-spike-wanblock`, `-Direction Outbound -Action Block`, one rule per program with its absolute path, those exact ranges, and no Allow rule.
  - `TestWanBlock_LinuxRuleShape`: the nft script matches `meta skuid "ipcam-spike"`, drops by `daddr !=` the allow sets for `ip` and `ip6`, and never mentions `oif`.
  - `TestWanBlock_CleanupScheduledAtBlockPlus30m`: a 6 h block → cleanup at +6 h 30 min; 15 min → +45 min.
  - `TestWanBlock_StartupRemovesStaleGroup`.
  - `TestWanBlock_KeepsLanAndLoopback` (Review Focus 2; root and nft; skipped unless `UPLOADSPIKE_REQUIRE_NFT=1`, which the `mi5-lab` job sets): before `Block`, `diag reach --hosts 198.51.100.1:18080` as `ipcam-spike` reads `ok: true` (the fakes answer); after a real `Block` it reads `blocked: true` (`timeout` or `denied`) while `127.0.0.1`, `::1` (when present) and the runner's RFC 1918 listener answer; after `Unblock`, `198.51.100.1:18080` reads `ok: true` again.
  - `TestVerifyBlocked_AbortsWhenProviderReachableOverIPv6`, `TestVerifyBlocked_RefusedCountsAsReachable` and `TestVerifyBlocked_AbortsWhenLanOrLoopbackFails` (fake `diag reach` output): each gives its Romanian message, and the runner calls `Unblock`.
  - `TestWanBlock_WindowsSmoke` (skipped unless `UPLOADSPIKE_REQUIRE_WINFW=1`): `www.googleapis.com` reads `blocked: true` over IPv4 (and IPv6, or `no-address`) from `ipcam-hub.exe`, loopback and the runner's private address answer, and after `Unblock` the provider answers again.
  - `TestMI5_RestartOnlyAfterUnblockMidUpload` (P16): the kill happens only after an `upload_progress` with 0 < offset < size seen after the unblock, never during the block.
  - `TestMI5_ResumeOffsetAtLeastLastConfirmed`: a resumed offset of 0 fails; below the last confirmed offset fails; at or above passes.
  - `TestMI5_InjectedGapNotMissing`; `TestMI5_TornSegmentEnqueuedAndUploaded` (from the audit).
  - `TestMI5_RefusesWhenQuotaOrSpoolTooSmall` (P38): `Free < 2 ×` backlog and spool `< 1.5 ×` backlog each refuse with their Romanian reason; `--drive personal` with 10.9 GB free refuses.
  - `TestMI5_DriveSetting` (C2-R1): `test`, `test+personal` and `personal` give exactly the destinations above; `test+personal` gives `dst-2`'s route `untilMs` = run start + 2 h, with no restart; every route of every setting has `fromMs` = run start.
  - `TestMI5_OneDriveGetsOneCamera` and `TestMI5_OneDriveDeferredWithoutA5`.
  - `TestMI5_BlockAvoidsDueRefresh` (P53): a first attempt 3 h into the window, of an MI6 pair's token or of an MI5 login on the same hub, refuses and prints the next allowed start.
  - `TestMI5_ReadOpenAtLeast20Times`: ≥ 20 calls during the block on a clear camera, plus 20 in the encrypted pre-run, with successes and timings recorded.
  - `TestMI5_PiCameraCount`: Pi 5 → 4, Pi 4 → 4, Pi 3 → 2, Zero 2 W → 2, the PC → 4, and the envelope records the count; a Pi whose model stays `unknown` after the device-tree fallback, and an `unsupported` one, refuse with «Nu știm câte camere poate înregistra acest Pi. …» and start no hub. `TestMI5_UsesStoredSdConfirmation`: the run state's confirmation goes into `run.json` and the envelope; an SD device with none refuses with Task 9's text.
  - `TestLabConfig_AtLeastTwoChunksPerSegment`: 30 s of `camNN_sub` with `partSize` 512 KiB needs at least 2 chunks; `--part-size` without `--fakes` is refused.
  - `TestMI5_TakesOverTheMI6Hub` (D2-A, D2-C): with an MI6 run open, `mi5` sends the handover; its hub's `run.json` holds MI6's destinations plus MI5's, `dst-1`'s `tokenRef` is an MI6 row of the D1 account, and no login is asked; at the close `mi5` releases the lock and the waiting `resume` restarts the hub with MI6's destinations only.
  - `TestMI5_HolderEndsAnyWay_OneHubUnderResume` (Linux; the `windows` job with TerminateProcess): during the block, SIGHUP to `mi5`, and in a second run SIGKILL, each leave within 5 min exactly one hub running under `resume`, with MI6's destinations only, no block rule left and no orphaned hub; the MI5 run is invalid. After the SIGKILL run only, that hub stays up for 3 min with the same PID while the catalog holds the cut run's pending `dst-1` placements: it emits no `upload_*` event for `dst-1`, and an MI6 refresh through `POST /control/v1/oauth/refresh` succeeds on the fakes (Task 4's `TestOutbox_UnconfiguredDestinationUntouched` covers 10 min on a fake clock). `mi5`'s first lines hold «Lăsați fereastra deschisă până la final.».
  - `TestHandover_RefusedWhileMI5Runs` (D2-A): `mi6 --b1 android` during `mi5`'s block finds the open run in the run state, sends no handover request, exits 1 with «Proba MI5 rulează; rulați comanda după ce se termină.» and records no claim and no B1 outcome; a direct `POST /spike/v1/run/handover` with the run state's token gets 409; `mi5` and its hub keep running with the same PIDs and the run stays valid; `status` meanwhile shows «Reconectați Android cu B1, după proba MI5: uploadspike mi6 --b1 android». `TestMI5_OwnLoginWithoutMI6Run` (D2-B on the PC): one device-flow login for `dst-1`, counted in the cap.
  - `TestMI5_BlockResetsConnections`: `Block` is followed by one `reset-connections` call before the clock starts.
  - `TestMI5_LanProbeIsDefaultGateway`: the probe target comes from the routing table and appears in no output; a gateway that answers neither port refuses the run with its Romanian reason.
  - `TestResume_OpenMI5AfterRebootIsInvalid`: `resume` after a reboot, or after an `mi5` that ended before its close, removes stale rules, marks the open MI5 run invalid («repornire în timpul probei MI5», or «proba MI5 s-a oprit înainte de final») and restarts the hub with MI6's destinations only; the run's pending placements stay pending, untouched.
  - `TestMI5_DeadMI5WithoutResume_NextCommandEndsTheRun` (no `resume` running): an open MI5 run whose recorded `mi5` was killed (SIGKILL), with its block rule left; `mi6 --b1 android` takes the free `hub.lock`, removes the rule, marks the run invalid («proba MI5 s-a oprit înainte de final») and runs its login; a recorded PID that now names another process (another start time) counts as dead; while the recorded `mi5` lives, the same command refuses as in `TestHandover_RefusedWhileMI5Runs`.
  - `TestMI5_RunAfterCutRun_CountsOnlyItsOwnSegments` (Linux; the `windows` job with TerminateProcess; the seams of `TestMI5_HolderEndsAnyWay_OneHubUnderResume`): a first `mi5` is cut during its block as a reboot cuts it: SIGKILL to its hub (as `hub.pid` names it) and at once to `mi5`; on Windows TerminateProcess of `mi5`, whose Job Object kills the hub. At least one torn `.part` is left under `rec/`, else the test fails, since it would not cover the path. The `resume` hub's startup reconcile finalises them, and they get no placement. A second `mi5` run on the same data directory then completes: its routes' `fromMs` is its start; its audit shows `missing: 0`, `duplicates: 0` and `verified == segments` for `dst-1`, counting none of the first run's segments; the first run's leftover pending placements are verified during it.
  - `TestLinkClient_HelloSession` and `TestLinkClient_NeverLogsCredentials`.
  - `TestProbe_ChoosesArchiveByMachine` (the armv6 line contains «Faza 0-bis»); `TestProbe_DeviceTreeFallback`.
  - `TestInstall_WindowsTaskXML` (`BootTrigger`, the `TimeTrigger` with `Repetition Interval PT5M`, `IgnoreNew`, `ExecutionTimeLimit PT0S`, both battery settings false, `S4U`, `HighestAvailable`, command, working directory; the task is run once after registration), `TestInstall_SystemdUnit` (`ExecStart=/opt/ipcam-spike/uploadspike resume`, `Restart=always`, `RestartSec=10`, `enable --now`), `TestInstall_LinuxLayout` (command and non-root uid seams: `/opt/ipcam-spike` root-owned 0755 with the binaries; `/var/lib/ipcam-spike/hub` and `hub/spike` 0700 owned by the seam's uid; `state/` and `results/` root-owned 0700; a symlink planted in place of `hub/spike` before a second install is refused, and its target keeps its owner and mode), `TestInstall_PiAsksSdConfirmation`, `TestUninstall_RemovesTaskUnitRulesTimerAndUser`.
  - `TestAnnotate_EmitRawJSONLines`: one raw JSON line per envelope, with no prefix, each ≤ 3,500 bytes after annotate.sh's escaping, at most 10.
  - `TestG14_NoGaps_Trece`, `TestG14_NoSegmentsDuringBlock_FaraVerdict`, `TestG14_NoResumeAboveZero_FaraVerdict`, `TestG14_OpenSegmentReadFails_PlanB2MinSegments`, `TestG14_Duplicate_PicaOutbox`.
  - `TestG14_EmptySet_FaraVerdict`; `TestG14_LabOnly_FaraVerdict` (a lab envelope that meets every condition still gives `Incomplete`); `TestG14_UploadsDuringBlock_FaraVerdict` (an `upload_verified` at a provider during the block → `Incomplete` with «blocarea nu a oprit urcarea hub-ului»).
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 -timeout 30m ./tools/spikes/upload/...` — Expected: FAIL. `-timeout 30m`, here, in Step 4 and in both CI jobs, because the `mi5` process tests of one package, each with real `mi5` starts, can together exceed `go test`'s 10-min default per package, above all under `-race`.
- [ ] **Step 3: Implement.** Tidy the spike module.
- [ ] **Step 4: Run** with `-race` and `-timeout 30m` — Expected: PASS. actionlint v1.7.12 on both workflows exits 0. Push with `[lab]` in the commit message: `mi5-lab` is green and its `mi5-lab` notice parses as one envelope that shows 0 missing, 0 duplicates and a resumed offset > 0, or a measured failure whose run completed and whose annotation parses. The `windows` job's smoke test passes.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI5 upload-during-outage spike for G14 [lab]"`

### Task 10: MI8 ONVIF events spike — G17

Rulings: P01 (item 4: `diag onvif`), P06, P11, P41, P42, P43, P44, C2-R3, C2-R5.

**Files:**
- Create: `ipcam/hub/internal/cameras/events/{soap.go,wsse.go,auth.go,device.go,media.go,pullpoint.go,subscription.go,cameraref.go}` and tests `{subscription_test.go,auth_test.go,cameraref_test.go,deps_test.go}`
- Create: `ipcam/hub/internal/cameras/events/onviftest/{sim.go,sim_test.go}`
- Create: `ipcam/hub/cmd/ipcam-hub/{diag_onvif.go,diag_onvif_test.go}`; Modify: `ipcam/hub/cmd/ipcam-hub/diag.go`
- Modify: `ipcam/hub/{go.mod,go.sum}` (`github.com/kerberos-io/onvif v1.2.3` and the `github.com/clbanning/mxj/v2` version it requires; single writer); `ipcam/hub/third_party/mediamtx/{go.mod,go.sum}` if tidy changes them
- Create: `ipcam/tools/spikes/upload/mi8/{runner.go,cameras.go,rtsp.go,ffmpeg.go,snapshot.go,motionref.go,episodes.go,judge.go}` and tests; Create: `ipcam/tools/spikes/upload/cmd/uploadspike/{mi8.go,detach_linux.go,detach_other.go}`
- Modify: `ipcam/tools/spikes/upload/results/envelope.go` (`Mi8Data`); `ipcam/tools/spikes/upload/runstate` (`LockFile`, Task 9's interface); `ipcam/tools/spikes/upload/{go.mod,go.sum}` (gortsplib v5.6.6, mediacommon v2.9.6)
- Modify: `.github/workflows/ipcam-spike-upload.yml` (`TREES` += `./hub/internal/cameras/...`; a `workflow_dispatch` trigger with no inputs, so the other jobs run on it too; job `ffmpeg-pin` with `if: github.event_name == 'workflow_dispatch' || contains(github.event.head_commit.message, '[ffmpeg-pin]')` and `env: GH_TOKEN: ${{ github.token }}` for `gh api`)

**Interfaces:**
- Produces:

```go
package events // HUB-11; imports only kerberos-io/onvif/{event,event/topic,xsd,xsd/iso8601,xsd/onvif,device} (P06)
type CameraRef struct{ ID, XAddr, User, Pass string } // String, GoString, MarshalJSON and LogValue redact XAddr, User and Pass
type Event struct{ Cam, Topic string; AtMs int64 /* the hub's receipt time, never the camera's UtcTime */; Motion bool }
type Signal struct{ Cam, Kind string /* "events_unhealthy" | "events_lost" | "events_healthy" */; AtMs int64 }
type Options struct {
    HTTP *http.Client; Clock func() time.Time
    Term, RenewAt, UnhealthyAfter, LostAfter time.Duration // defaults 10 min, 8 min, 60 s, 5 min
    AuthPause time.Duration // 60 s after 3 failures (§8.2)
}
type Subscription interface {
    Events() <-chan Event
    Signals() <-chan Signal
    Healthy() bool
    Close(ctx context.Context) error // Unsubscribe
}
func Subscribe(ctx context.Context, cam CameraRef, opts Options) (Subscription, error)
type ProbeResult struct{ EventsService, PullPoint bool; ClockOffsetMs int64; Auth string /* "wsse-digest" | "http-digest" | "basic" */; Brand, Model string }
func Probe(ctx context.Context, cam CameraRef, opts Options) (ProbeResult, error) // GetDeviceInformation's Manufacturer and Model only, never the serial
func StreamURI(ctx context.Context, cam CameraRef, profile string, opts Options) (string, error)   // own SOAP structs for GetProfiles and GetStreamUri
func SnapshotURI(ctx context.Context, cam CameraRef, profile string, opts Options) (string, error) // GetSnapshotUri
```

- **SOAP (P06).** Own requests over `net/http` + `encoding/xml`: CreatePullPointSubscription, PullMessages, Renew, Unsubscribe, GetSystemDateAndTime, GetDeviceInformation, GetCapabilities, GetProfiles, GetStreamUri, GetSnapshotUri. The kerberos packages give types only; `media` is not in the allowlist, so the media calls use own structs.
- **Auth and clock (§8.2, P42).** WS-UsernameToken PasswordDigest with `Created` shifted by the GetSystemDateAndTime offset; then HTTP digest (MD5, SHA-256); then basic. PasswordText is never sent. After 3 failures, a 60 s pause, so the owner's camera account is not locked.
- **Signals (HUB-11, P44).** Subscription state decides, never motion. After a loss (Renew or PullMessages fails, or the PullPoint disappears), `Healthy()` is false within 60 s and `events_unhealthy` is emitted. After more than 5 min without a successful Renew or PullMessages, `events_lost` is emitted: §13.1's EȘUAT and MI8's silent-loss count. Successful empty PullMessages for 10 min keep `Healthy()` true and emit nothing. FLW-16 is cited only as G17's plan B; MOTION mode, the ring buffer and `motion_fallback` stay Faza 1.
- **Simulator (`onviftest`).** `httptest`, `MaxPullPoints=5`, with renew, silent, expired, clock-skew and digest-only modes; it records the auth method of every request.
- **`ipcam-hub diag onvif` (P01 item 4, P41, P42).** Cameras arrive only as JSON lines on stdin, `{"id":"real-1","xaddr":"…","user":"…","pass":"…"}`, never through a file of any mode. Output is JSON lines on stdout:
  - `--probe`: `{"type":"probe","cam":"real-1","eventsService":true,"pullPoint":true,"clockOffsetMs":…,"auth":"wsse-digest","brand":"…","model":"…"}`, or `{"type":"probe","cam":"real-1","error":"onvif-port"}` when nothing answers at the XAddr's port; never a URL, address, credential or serial.
  - `--uris --profile sub`: `{"type":"stream_uri","cam":"real-1","uri":"…"}` and `{"type":"snapshot_uri",…}`, read by uploadspike over the pipe into RAM and never logged.
  - Default (subscribe until stdin closes or SIGTERM): `onvif_event {cam, atMs, topic, motion}`, `events_unhealthy|events_lost|events_healthy {cam, atMs}`, `subscribe_failed {cam, reason: "auth"|"no-pullpoint"|"unreachable"}`.

```go
package mi8 // SPIKE-ONLY
type CameraRef struct{ ID, XAddr, User, Pass string } // uploadspike's copy, with the same redaction
type Frame struct{ AtMs int64; Gray []byte /* 160×90 */ }
type FrameSource interface{ Frames(ctx context.Context) (<-chan Frame, error) }
func MotionEpisodes(ctx context.Context, src FrameSource, p EpisodeParams) (<-chan Episode, error)
type EpisodeParams struct{ PixelDelta int /* 25 */; AreaFraction float64 /* 0.01 */; QuietGap time.Duration /* 10 s */ }
type Episode struct{ StartMs, EndMs int64 }
func Match(episodes []Episode, events []int64, window time.Duration /* 5 s */) (matched int)
func Run(ctx context.Context, cfg Mi8Config) (results.Envelope, error) // 72 h
func Judge(envs []results.Envelope) results.Judgement // G17

package results
type Mi8Data struct{ Hours float64; QuietGapSecs int; Cameras []Mi8Camera }
type Mi8Camera struct {
    Camera, Brand, Model string; Episodes, Matched int; Coverage float64; SilentLosses int; RefLagMs int64 /* L below */
    SubscribeFailed bool; ReferenceMethod string /* "substream" | "snapshot" */; EpisodeTimes []EpisodeTime
}
type EpisodeTime struct{ StartMs, EndMs int64; Matched bool }
```

- **Cameras (P42, C2-R3, C2-R5).** `uploadspike mi8` asks on the hub device for the number of cameras, then each camera's address as `IP[:port ONVIF]` (port 80 when absent; XAddr = `http://<ip>:<port>/onvif/device_service`), ONVIF user and password, the password without echo. The per-brand A7 steps say where the camera shows its ONVIF port (Reolink: 8000 by default), and `--probe-only` prints «portul ONVIF nu răspunde» for a camera whose port does not answer, apart from «abonare eșuată (configurare)». They stay in RAM and reach `ipcam-hub diag onvif` only on its stdin. It runs `--probe` and shows brand and model per `real-N`. By default every camera with an events service and PullPoint support takes part; `--one-per-brand` keeps the first camera of each brand (D3-B). A camera whose subscription cannot be created is reported as «abonare eșuată (configurare)», apart from coverage. The early probe runs during the MI6 week with `uploadspike mi8 --probe-only`. At close, `ipcam-hub diag scan-secrets` (Task 9) runs over `results/` with every typed IP, user and password on its stdin (P45).
- **72 h without the terminal (P42, P49).** After the prompts and the probe, MI8 needs no terminal, and the 72 h must not depend on one. Under D2-B and D2-C it runs on the Pi, reached over SSH from a PC that is not kept on.
  - *Linux:* `uploadspike mi8` starts a copy of itself, `uploadspike mi8 --detached`, in a new session (`Setsid`), with the cameras as JSON lines on the copy's stdin, a pipe that the copy reads into RAM and closes. That is RAM to RAM and never a file (P42); the copy's argv and environment hold no camera value. The parent waits for the copy's first heartbeat in the run state, prints «MI8 rulează în fundal 72 de ore. Puteți închide fereastra; progresul îl vedeți cu uploadspike status.» and exits. The copy ignores SIGHUP, has the null device as stdout and stderr, and writes its progress (hours, episodes per camera) only into the run state, which `status` and the holder's `status.html` read. So the SSH window, and the PC it runs on, may close.
  - *Windows:* the console window is MI8's process, so `mi8` prints «Lăsați fereastra deschisă 72 de ore.» and keeps the window.
  - *One MI8 per device.* As c1's recspike holds its run-state lock for every command (c1 plan L836), MI8 holds `<base>/state/mi8.lock` for its whole run. `mi8` and `mi8 --continue` take it before any prompt (`runstate.LockFile` on Linux, `runstate.Lock` on Windows). On Linux the parent hands it to the detached copy as an inherited descriptor (`ExtraFiles`), so it never lapses between the two processes, and the copy holds it until it ends; on Windows the window's process holds it. A second `mi8` or `mi8 --continue` that finds it held prints «MI8 rulează deja; progresul îl vedeți cu uploadspike status.» and exits 1 before any prompt, so no camera gets two subscribers and no `real-N` series two writers. It is a lock of its own: `run-state.lock` is held only for each `runstate.Update`, so `status` and the hub's holder keep updating the run state.
- **Reboots and `--continue` (P49, P42).** The camera credentials live only in MI8's RAM. So MI8 stops when the device reboots or its process ends (on Windows, when its window closes), and `uploadspike resume` does not restart it; `uploadspike status` then says «MI8 s-a oprit; rulați uploadspike mi8 --continue.».
  - *Matching cameras.* The run state keeps each `real-N`'s brand and model (not secrets; an address, user or password never enters it). `uploadspike mi8 --continue` lists each `real-N` with its brand and model and asks that camera's address, ONVIF user and password. Its probe must return the same brand and model before the camera's new hours and episodes join `real-N`'s series; any other answer is refused with «Aceasta nu este camera real-N (<marcă> <model>). Verificați adresa.», and the camera is asked again. Two cameras of one brand and model cannot be told apart this way; a swap between them stays within that brand, by which G17 groups, and «Limitări» says so.
  - *Hours.* Observed hours add up across sessions in the same envelope, and a reboot in between is a `reboot` gap. After the prompts, `--continue` runs on without the terminal as above.
- **Motion reference (P41).** uploadspike reads each camera's substream itself with gortsplib, using the URI from `diag onvif --uris` plus the typed credentials. It pipes the H.264 or H.265 elementary stream to FFmpeg's stdin: `ffmpeg -hide_banner -loglevel error -f h264|hevc -i pipe:0 -vf fps=2,scale=160:90,format=gray -f rawvideo pipe:1`. No URL, address or credential is in FFmpeg's argv or environment, and its stderr is discarded. The diff is pure Go: a frame is motion when ≥ 1 % of its pixels change by more than 25 gray levels; an episode ends after a 10 s quiet gap, which the report prints. Times are the hub's receipt times, for events and frames alike, never the camera's `UtcTime`, whose clock may be skewed. An episode starts at the receipt time of its first motion frame, which trails the real motion by the reference's own delay L: per camera, the p90 of (decoded-frame time − receipt time of its access unit) plus one frame interval (0.5 s), capped at 3 s; for the snapshot fallback, the p90 snapshot fetch time plus 0.5 s. An episode is matched when a `motion: true` event's receipt time lies in [start − L, start + 5 s]: within 5 s of the motion, as MI8 requires, with L allowing for the reference's lag. The report prints L per camera in «Limitări»; the camera's own encode and network delay is not in L. Frames and snapshots stay in RAM and never reach disk, logs or results; results carry only episode times and counts.
- **Fallback.** On armv7, after a failed download, or for a substream that does not decode: an ONVIF snapshot diff (`GetSnapshotUri`, ≤ 2 fps as the camera allows, JPEG decoded in Go to 160 × 90 gray, the same RAM-only rule, the URI never logged). `ReferenceMethod` records each camera's method.
- **FFmpeg fetch.** On the first MI8 run, before any WAN block, uploadspike downloads the pinned BtbN LGPL build (`win64-lgpl` or `linuxarm64-lgpl`, FFmpeg 9.0, ≥ 14 days old) by exact URL with the User-Agent `ipcam-spike/0.1`, checks its SHA-256 and unpacks `ffmpeg[.exe]` next to itself: `archive/zip` for win64, the system `tar -xJf` for the linuxarm64 `.tar.xz` (Raspberry Pi OS ships `xz-utils`; Go's standard library has no xz reader, and no xz module is added); a missing `tar` or `xz` falls back to the snapshot diff. The URLs and hashes are constants in `mi8/ffmpeg.go`, taken from the `ffmpeg-pin` job: it runs on `workflow_dispatch` or when the head commit message contains `[ffmpeg-pin]`, lists BtbN's releases with `gh api repos/BtbN/FFmpeg-Builds/releases --paginate`, and picks the newest month-end build at least 14 days old with both LGPL archives. BtbN's retention policy keeps the last 14 daily builds and each month's last build for two years, so a daily build at the 14-day edge would be pruned before the owner's run and its URL would 404. The job takes the dated releases of months before the current one, picks the newest, asserts that no later release of the same month exists (it is that month's last build), downloads both archives, and emits the notice `ffmpeg-pin: {"tag":…,"win64":{"url":…,"sha256":…},"linuxarm64":{…}}`. This container cannot list BtbN's releases.
- **Episodes floor (P43).** A camera's coverage needs ≥ 20 reference episodes; below that it reads «fără verdict: prea puține episoade (N)», and its silent losses are still judged over every observed hour, whatever its episode count. MI8 writes each camera's episode count into the run state, where `uploadspike status` (any terminal of the device, SSH included) and `status.html` show it.
- **`mi8.Judge` (G17, P22, P43).** Cameras are grouped by brand.
  - **Measured failures decide.** A camera with `SilentLosses > 0` fails, whatever its episode count. A camera with ≥ 20 episodes and Coverage < 0.95 fails. A failing camera gets its brand's plan B: Hikvision `alertStream`, Dahua `eventManager` or Reolink push as a Technical Decision; any other brand «evenimente nesigure» + FLW-16. Any failing camera → `FailWith`, even when other parts are missing.
  - **Pass.** Otherwise G17 passes only with `Hours ≥ 72` observed (sessions added up, the reboot rule above) and at least one camera with ≥ 20 episodes, every such camera at Coverage ≥ 0.95; else `Incomplete`.
  - **D3-C.** MI8 does not run, so no envelope exists; uploadreport's `-d3 c` renders the G17 row as `Incomplete` with the note «MI8 rulează după detectorul din Faza 0 (a) (D3-C)» (Task 11).

- [ ] **Step 1: Write the failing tests.**
  - `TestEvents_RenewsBefore10Min`: renew at 8 min; after 10 rounds the simulator holds 0 leftover subscriptions with `MaxPullPoints=5`.
  - `TestEvents_HealthyFalseAfterLoss` (≤ 60 s, `events_unhealthy`), `TestEvents_LostAfter5MinWithoutSuccess` (`events_lost`), `TestEvents_QuietSceneStaysHealthy` (10 min of empty PullMessages: healthy, no signal), `TestEvents_ExpiredSubscriptionResubscribes`.
  - `TestEvents_ClockSkewUsesOffset` (a 2 h skew still authenticates), `TestEvents_DigestOnlyCamera` (falls back to HTTP digest), `TestEvents_NeverSendsPasswordText`, `TestEvents_PausesAfter3AuthFailures` (60 s).
  - `TestCameraRef_RedactsEverywhere`: `%v`, `%+v`, `%#v`, `json.Marshal` and `slog` show neither address, user nor password.
  - `TestDeps_OnvifAllowlist` (P06): `go list -deps ./hub/...` holds no `github.com/kerberos-io/onvif` package outside the six allowed, and neither module `github.com/juju/errors` nor `github.com/elgs/gostrgen`.
  - `TestDiagOnvif_ProbeLineHasNoAddressOrSerial` and `TestDiagOnvif_ReadsCamerasFromStdinOnly`.
  - `TestMotionRef_FrameDiffEpisodes`: synthetic 160 × 90 frames with motion at 10–14 s and at 30 s → two episodes with a 10 s gap.
  - `TestMotionRef_NoCredentialInFFmpegArgvOrEnv`: a fake `ffmpeg` dumps argv and environment; neither holds the URI, IP, user or password.
  - `TestMotionRef_FramesNeverOnDisk`: no file over 10 KB appears under `TMPDIR` or `results/` during a 60 s run.
  - `TestFFmpegFetch_PinnedHashAndNeutralUA` (`httptest`): a wrong hash is refused and nothing is unpacked; the request's User-Agent is exactly `ipcam-spike/0.1`; a `.tar.xz` archive is unpacked through the `tar -xJf` seam.
  - `TestMI8_SnapshotFallbackOnArmv7`, `TestMI8_SubscribeFailedReportedSeparately`, `TestMI8_OnePerBrandSelection`, `TestMI8_DefaultSelectsEveryCameraWithEvents`.
  - `TestMI8_Coverage`: 24 of 25 matched (96 %) passes; 47 of 50 (94 %) fails with the brand's plan B.
  - `TestMI8_MinEpisodes20`: 19 of 20 passes; 18 of 19 → «fără verdict: prea puține episoade (19)».
  - `TestMI8_SilentLoss`: one `events_lost` → one silent loss, and the camera fails.
  - `TestMI8_FewEpisodesWithSilentLoss_Fails` (= `TestG17_SilentLossBelowEpisodeFloor_Pica`): a camera with 10 episodes and one `events_lost` fails G17 with its brand's plan B.
  - `TestG17_Under72Hours_FaraVerdict`: 60 observed hours with every camera passing → `Incomplete`; the same with a silent loss → `pică`.
  - `TestMI8_ContinueAfterRebootAddsHours`: sessions of 40 h and 33 h around a reboot give `Hours` 73 and one `reboot` gap; `--continue` lists `real-1` and `real-2` with their brand and model from the run state and asks each one's address, user and password; the run state holds no address, user or password.
  - `TestMI8_ContinueRefusesAnotherCamera`: under `--continue`, an address whose probe gives another brand or model than `real-1`'s is refused with its Romanian text, asked again, and adds no hours to `real-1`.
  - `TestMI8_SurvivesHangupAfterPrompts` (Linux, on a pty): after the prompts, closing the pty and SIGHUP to its process group leave MI8 running. The onviftest simulator keeps receiving PullMessages for 2 min, the run state's heartbeat advances, and `status` shows the run. The detached copy's argv and environment hold no camera value, and its stdin is closed once read.
  - `TestMI8_WindowsKeepsWindowText`: on Windows the prompts end with «Lăsați fereastra deschisă 72 de ore.», and no detached copy starts.
  - `TestMI8_SecondInstanceRefused` (Linux with a detached copy, Windows with the window's process): while MI8 runs, a second `mi8` and an `mi8 --continue` each print «MI8 rulează deja; progresul îl vedeți cu uploadspike status.» and exit 1 before any prompt, and the simulator still holds one subscription per camera; on Linux a second `mi8` started between the parent's exit and the copy's first heartbeat is refused too.
  - `TestMI8_CameraAddressWithOnvifPort`: `192.0.2.10:8000` gives an XAddr on port 8000 and no port gives 80; `--probe-only` on a closed port prints «portul ONVIF nu răspunde».
  - `TestMotionRef_MatchWindowUsesReferenceLag`: with L = 1.2 s an event 1 s before the start matches and one 2 s before does not; 5 s after matches, 5.1 s does not.
  - `TestG17_NoJudgedCamera_FaraVerdict`, `TestG17_GroupedByBrand` (a failing Hikvision → «Hikvision `alertStream`»; a failing unknown brand → «evenimente nesigure» + FLW-16).
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./hub/internal/cameras/... ./hub/cmd/ipcam-hub/... ./tools/spikes/upload/mi8/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** Tidy `hub` and the fork with `GOWORK=off`. Push with `[ffmpeg-pin]`, read the `ffmpeg-pin` notice with `gh api repos/AndreiBesliu/DevWorld/check-runs/<job id>/annotations`, and commit the URLs and hashes.
- [ ] **Step 4: Run** with `-race`, then `tools/scripts/check-video-build-list.sh` and `tools/scripts/check-licenses.sh ./hub/...` — Expected: PASS; `video build list unchanged`; licenses within the allowlist; CI green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add ONVIF events package and MI8 spike for G17"`

### Task 11: Report generator and verdicts (G14, G15, G17)

Rulings: P09 (`-annotations`), P18 (unit conversion), P22, P23, P28, P45, P55, C2-R4.

**Files:**
- Create: `ipcam/tools/spikes/upload/cmd/uploadreport/{main.go,inputs.go,annotations.go,render.go,faza0bis.go,csv.go}` and tests with `testdata/` (fixtures for PC and Pi, annotation JSON in two pages, `golden.md`)
- Create: `ipcam/tools/spikes/upload/results/{tokens.go,tokens_test.go}`
- Modify: `ipcam/tools/spikes/upload/{go.mod,go.sum}` (`require github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video v0.0.0` and `replace … => ../video`)
- Modify: `.github/workflows/ipcam-spike-upload.yml`: both triggers' `paths:` gain `ipcam/docs/research/*phase0c-upload*` and `ipcam/docs/research/phase0-data/phase0c-upload-*`, one per line, so Task 13's report commit runs the workflow; a step runs `go run ./tools/spikes/upload/cmd/uploadreport -scan docs/research/*phase0c-upload*.md docs/research/phase0-data/phase0c-upload-*.csv` when such files exist

**Interfaces:**
- Consumes: `mi5.Judge`, `mi6.Judge`, `mi8.Judge`; `results`; `tools/spikes/video/redact.Scan` (unchanged: the video spike's branch owns it); `gh`.
- Produces:
  - **`uploadreport -in <dir> [-in <dir>…] [-annotations <workflow run id>…] -onedrive free|paid|unknown [-d3 a|b|c] [-console-units <n>] -out docs/research`**, run from `ipcam/` (where `go.work` lives). It loads the envelopes and de-duplicates them by `RunID`. For each annotation run it lists the jobs (`gh api repos/AndreiBesliu/DevWorld/actions/runs/<id>/jobs --paginate`) and reads each job's `check-runs/<id>/annotations --paginate`. It calls each Judge once and only renders. `-onedrive free` = A5 passed; `paid` = A5 stopped at a GUID, a card or a subscription prompt; `unknown` = no answer, which also defers OneDrive (C2-R4). `-d3` is the owner's D3 answer (default `a`); `c` renders G17 as `Incomplete` with the D3-C note (Task 10), since MI8 then writes no envelope. Each `mi5-lab` notice's message is one raw envelope (Task 8's `annotate emit`), read as a `lab-linux` envelope; it renders as its own TST-07 row (the 15-min variant, P09) and never enters G14's judged set (Task 8's `mi5.Judge`).
  - **`uploadreport -scan <file>…`** applies `redact.Scan` and the token patterns to committed files and exits 1 on a match.
  - **Token patterns (P45).** In `results/tokens.go`: Google `ya29.`, `1//`, `GOCSPX-`; Microsoft `M.C` followed by its token body and `EwB`; JWT `eyJ` with two dots; device user codes (`XXXX-XXXX` uppercase letters). Applied after `redact.Scan`; any match aborts and writes nothing. Their tests build provider-shaped strings at run time from parts.
  - **Report `docs/research/<date>-phase0c-upload-results.md`:**
    - a Romanian owner summary;
    - per gate a `Cod | Valoare | Prag | Trece | Sursă` table; SD results apart from SSD results (P17);
    - **MI5:** per destination; Drive calls per segment by method; units only for the methods §9.2 prices (get 5, list 100, download 200); the resumable POST, chunk PUTs, status query, delete and `about.get` as call counts marked «unități: din consolă»; the console's units per segment, which the controller types from the owner's screenshot with `-console-units <n>` (P18);
    - **MI6:** per pair (plan, days 1–6, day 7) and the 3 × 4 visibility matrix with `Self` cells marked;
    - **MI8:** by brand, then per camera, with the reference method and the 10 s quiet gap;
    - the verdicts G14, G15 and G17 with the four strings, and the triggered decisions in PH0-30 format, taken from each `Judgement.Decisions`;
    - «Limitări»: the unencrypted, unauthenticated spike channel, with P40's consequence: a device on the home LAN could read the authorization URL and race in a code from another account under the same state; T11's encrypted channel (lanapi with HIK, T42) prevents this, so that property stays untested until Faza 1 (P40); on a Pi, MI5 records min(4, the model's recommended count) cameras (P17, P38); a lab run is TST-07's 15-min variant on the fakes, never G14 (P09); a firewall rule may leave an established flow open, so the block is also checked against the hub's own upload events (P15); `cam01`'s EVENT_CLIP stand-in (P14); a verified copy is never replaced, so MI5 records clear (P03); DNS resolved during the block (P15); the Windows sealer is `dpapi-machine`, weaker than the spec's until Faza 1 (P19); STO-13's retention loop is tested in Faza 1 (P26); the motion reference's method and lag; across an MI8 `--continue`, two cameras of one brand and model cannot be told apart (Task 10); quota units come from the console (P18);
    - the static Faza 0-bis table below.
  - **Faza 0-bis table** (gate, cost, deadline), exact rows:
    1. «G14 – OneDrive business (pana de 6 h), cu tenantul MI7: Entra ID P1, OWN-09, înainte de Faza 3» (always).
    2. «G14 și G15 – OneDrive personal: înregistrarea Microsoft cu date de plată, OWN-08, înainte de Faza 3» (unless `-onedrive free`).
    3. «G15 – combinațiile iOS: Apple Developer (OWN-05, 99 USD/an) și un iPhone împrumutat, înainte de Faza 3».
    4. «G15 – refresh-ul Drive din ziua 7 și de după, cu proiectul în Production: domeniul pentru verificarea brandului (OWN-04, OWN-07), înainte de Faza 3».
    5. «G15 – OneDrive business: tenantul de test (OWN-09), înainte de Faza 3».
    6. «G16 (MI7) – Conditional Access și `AppFolder` pe conturi de firmă: Entra ID P1 (OWN-09, ≈ 6–7 USD/lună), înainte de Faza 3».
    7. «ipcam-lab: TST-07 de 6 h pe conturi reale (PH3-02) și stabilitatea de 72 h, OWN-22 ≈ 15 €/lună, înainte de Faza 3».
    8. «redirect-ul OneDrive de pe `<domeniu>`, după domeniu (OWN-04), înainte de Faza 3».
    9. «STO-01 nightly pe conturi reale (§14.5): Drive după trecerea proiectului în Production (OWN-07, înainte de Faza 3); OneDrive personal e gratuit dacă înregistrarea e gratuită (OWN-08) și se programează după rotația săptămânală a secretelor (OWN-23, Faza 1); OneDrive business (OWN-09), B2, R2, Wasabi și AWS SSE-KMS: Faza 0-bis, cost, înainte de Faza 3».
  - **CSVs.** `docs/research/phase0-data/phase0c-upload-{mi5,mi6,mi8}.csv`, one row per sample: per placement for MI5, per pair-day and per visibility cell for MI6, per episode for MI8.
  - **Scope (G34).** This is a per-spike report; the PH0-30 index is written by the last Phase 0 plan to close (Task 13).

- [ ] **Step 1: Write the failing tests.**
  - `TestRender_GoldenMarkdown` (matches `testdata/golden.md`).
  - `TestRender_NoSecretInOutput` (Review Focus 5): an email, a display name, a Drive file ID, an access token, a refresh token, a camera password and a camera IP, each assembled from parts and planted alone into a rendered field, make the render fail and write nothing.
  - `TestTokenPatterns_Match` (one case per pattern, built at run time) and `TestTokenPatterns_NoFalsePositiveOnGolden`.
  - `TestRender_Faza0bisStaticTable`: `-onedrive free` gives rows 1 and 3–9; `paid` and `unknown` give rows 1–9; the texts are exact.
  - `TestRender_VerdictStrings`: each of the four strings renders from a Judge result.
  - `TestRender_DriveUnitsOnlyForPricedMethods`: `files.get` 2 per segment → 10 units; `upload.put` 3 → «3 apeluri, unități: din consolă».
  - `TestRender_G15DecisionsLines` and `TestRender_G17GroupedByBrand`.
  - `TestRender_SdResultsSeparate` and `TestRender_LimitariListed` (every item above present, P40's LAN-race sentence included).
  - `TestRender_G17_D3C_Note`: `-d3 c` with no MI8 envelope renders the G17 row as «fără verdict: lipsesc măsurători» with the note «MI8 rulează după detectorul din Faza 0 (a) (D3-C)».
  - `TestRender_LabEnvelopeIsTST07Row`: a `lab-linux` envelope renders as a TST-07 row and leaves G14 at `Incomplete` when no owner envelope exists.
  - `TestInputs_DedupByRunID` and `TestInputs_ReadsLabAnnotations` (a fake `gh` on `PATH` returning two pages; a `mi5-lab` notice whose message is an envelope line from `annotate emit`, escaped and unescaped as annotate.sh and the API do, round-trips to the same `lab-linux` envelope).
  - `TestCSV_OneRowPerSample`.
  - `TestScan_CommittedFiles`: `-scan` exits 1 on a planted token and 0 on clean files.
- [ ] **Step 2: Run.** `cd ipcam && go test -count=1 ./tools/spikes/upload/cmd/uploadreport/... ./tools/spikes/upload/results/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** Tidy the spike module; `check-video-build-list.sh` stays green.
- [ ] **Step 4: Run** with `-race` — Expected: PASS; CI green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add upload spike report generator with G14, G15 and G17 verdicts"`

### Task 12: Release assets and Romanian owner guide

Rulings: P08, P30 (after c1 Task 13), P46, P48, P50, P51, P52; the owner actions A1–A16 and decisions D1–D3, whose final texts are below.

**Precondition:** c1 Task 13 has landed, and the controller has merged c1's branch into this one again (merge commit).

**Files:**
- Create: `.github/workflows/ipcam-spike-upload-release.yml`
- Modify: `ipcam/tools/scripts/third-party-notices.sh` (`--release upload`; `video` and `recording` unchanged; single writer) and `ipcam/tools/scripts/test/third-party-notices.test.sh` (an upload case)
- Create: `ipcam/docs/spike-upload.md` (Romanian owner guide); Modify: `ipcam/README.md` (link to the guide; single writer)

**Interfaces:**
- Consumes: every binary (Tasks 1–10), c1's `reccontroller`, the fork, `mediagen` + `recmediagen`, the spike APK and Tauri builds (Task 9a), `check-guide-labels.sh`, `check-workflow-runs.sh`.
- Produces:
  - **Release workflow (P08, mirroring c1 Task 13 and `ipcam-spike-release.yml`).**
    - **Triggers.** Tag `spike-upload-v*` (paths `ipcam/**` and its own file) plus `workflow_dispatch` with a boolean `publish`. Concurrency cancel-in-progress `${{ github.ref_type != 'tag' }}`.
    - **Gate.** `ci-status` runs `ipcam/tools/scripts/check-workflow-runs.sh "${GITHUB_REPOSITORY}" "${GITHUB_SHA}" ipcam-spike-upload ipcam-spike-recording ipcam-apps ipcam-mobile ipcam-windows`. `ipcam-spike-recording` is included because it tests the recorder, the fork and `reccontroller`, which this release ships.
    - **Builds, without caches.** `uploadspike`, `ipcam-hub` and `reccontroller` for windows/amd64, linux/arm64 and linux/arm GOARM=7; the fork `mediamtx` with `GOWORK=off` for the same targets; `uploadspike-probe` for linux/arm GOARM=6; media `main.h264`, `sub.h264`, `sub-256k.h264` from `mediagen` + `recmediagen`; the spike APK signed with the run's debug key; the Tauri NSIS installer (`bundle:spike`); `third-party-notices.sh --release upload`; a Syft SBOM.
    - **APK SHA-1 (P52).** `apksigner verify --print-certs` gives the SHA-1, emitted as the notice `apk-sha1` and written into the release notes. No keystore and no repository secret exist.
    - **Assets.** `spike-upload-windows.zip` (`uploadspike.exe`, `ipcam-hub.exe`, `mediamtx.exe`, `reccontroller.exe`, `media/{main.h264,sub.h264,sub-256k.h264}`, `THIRD_PARTY_NOTICES.txt`); `spike-upload-linux-arm64.tar.gz` and `spike-upload-linux-armv7.tar.gz` (the same, executable bits kept); `uploadspike-probe-linux-armv6`; `ipcam-spike.apk`; `ipcam-desktop-setup.exe`; `spike-upload-sbom.spdx.json`; `THIRD_PARTY_NOTICES.txt`; `SHA256SUMS`. No FFmpeg and no `fakeproviders` (P41).
    - **Publish.** Idempotent: `gh release upload --clobber` + `gh release edit --prerelease` when the release exists. Notes: the APK SHA-1; that the Android libraries and the Rust crates are not in the notices; that FFmpeg is downloaded at run time.
    - **Registration.** The THROWAWAY pattern: a commit adding a branch push trigger, then its revert (as 314d077 and 5709941).
  - **`third-party-notices.sh --release upload`.** The Go modules of every shipped binary for the three targets (workspace), the fork's dependencies with `GOWORK=off` as c1's recording case does, explicit MediaMTX (MIT) and hls.js (Apache-2.0) entries, and the app's npm production dependencies, the new Capacitor and Tauri plugin packages included.
  - **Guide `docs/spike-upload.md`** (P48, P50). Text only, with exact UI labels. uploadspike's texts are quoted as `«…»<!--uploadspike-->` (the probe's as `<!--probe-->`, the app's as `<!--app-->`, the Android launcher name «Proba ipcam» as `<!--android-->`; headings, step names and descriptions carry no «», because `check-guide-labels.sh` reads every «…» as a program's text), and Windows, Google and Microsoft labels as „…”, which are confirmed by the owner's screenshots, not by the label check. Screenshots show the owner's email addresses, project and tenant IDs and client IDs: they stay in the chat and in scratch, and are never committed or copied into the guide, the report or the results. Sources: Microsoft labels are checked against MicrosoftDocs/entra-docs and microsoftgraph/microsoft-graph-docs-contrib on GitHub, ONVIF behaviour against onvif/specs; Google's docs and both consoles are unreadable from here, so Google labels come from search snippets and are confirmed on the screenshots. The numbering is fixed:
    0. **What you need and how long it takes per device**, and the rule on passwords and client secrets, in these words: Nu ne trimiteți niciodată parole sau secretele clienților (în chat, pe e-mail sau în altă pagină). Le scrieți doar în programul probei, pe dispozitivul hub: secretele în pagina de configurare, parolele camerelor la cererea programului.
    1. **The test accounts:** A2; A6 only if A5 passes.
    2. **Console language, then the Google Cloud project.** First ask which language the consoles show, and quote the labels in it, or ask the owner to switch Google Cloud and Entra to English for the setup (P50). Then: 2-Step Verification if Google asks; „APIs & Services” → „Library” → „Google Drive API” → „Enable”; „Google Auth Platform” → „Get started” (app name, user support email, Audience „External”, contact email); „Audience”: „Testing”, „Test users” → „Add users” (the owner's address and the A2 account); „Data access”: `https://www.googleapis.com/auth/drive.file` only → „Save”. Decline every trial or billing banner. Screenshot checkpoint: „Audience” and „Data access”.
    3. **The Desktop, TV and Web clients (A4).** „Clients” → „Create client”: „Desktop app”; „TVs and Limited Input devices”; „Web application” with „Authorized JavaScript origins” = `http://localhost:7091`. Download each JSON at creation; if a secret is lost, add a new secret on the client's page instead of recreating the client. Send the three client IDs, not the secrets, and a screenshot of the Clients list.
    4. **A5's 2-minute Entra check (P54), in full.** Sign in at https://entra.microsoft.com with the personal Microsoft account. Continue only if a directory that belongs to the owner shows under the account name and „Entra ID” → „App registrations” → „New registration” opens. Stop and tell the agent at a bare GUID under the account name (a long string of letters and digits), at „You can't sign in here with a personal account”, or at any Azure sign-up, free-account, subscription or card prompt. Never use an employer's or school's tenant; do not join the Microsoft 365 Developer Program or start an Azure free account. Only if the check passes, the Entra values (P36):
       - „Personal accounts only” (older portal builds: „Personal Microsoft accounts only”);
       - „Authentication” → „Redirect URI configuration” → „Add Redirect URI” → „Mobile and desktop applications” → „Custom redirect URIs”: `http://localhost/oauth/callback` (one entry, no port) and `ro.ipcam.spike://oauth/callback`; no `http://127.0.0.1` entry;
       - „Single-page application”: `http://localhost:7091/oauth/f6`;
       - „Allow public client flows” / „Enable the following mobile and desktop flows” = „Yes”;
       - „API permissions” → „Microsoft Graph” → „Delegated”: `Files.ReadWrite.AppFolder`, `offline_access`;
       - screenshots of „Overview”, „Authentication” and „API permissions”.
    5. **The unverified-app screen (P53):** „Google hasn't verified this app” → „Continue” (in Romanian: „Google nu a verificat această aplicație” → „Continuați”), as the language checkpoint says.
    6. **The installs**, only after step 14 of the recording-test guide (P51). On the PC: the bundle, then `uploadspike install` (one UAC prompt), so the hub restarts by itself after a reboot, or within 5 min after a window of the probe closes. On the Pi: `uploadspike-probe` first (armv6 → Faza 0-bis), then the bundle and `sudo ./uploadspike install`, which installs into `/opt/ipcam-spike` and, when the data sits on the SD card, asks the confirmation («Scrieți DA și apăsați Enter»<!--uploadspike-->). On the A24: uninstall the old «Proba ipcam»<!--android--> first (the signatures differ), install `ipcam-spike.apk`, then create the fourth Google client, „Android”: package `ro.ipcam.spike`, the SHA-1 from the release notes, „Advanced settings” → „Enable custom URI scheme” (A8). After a rebuilt APK, edit that SHA-1 when the agent asks.
    7. **SmartScreen and the firewall,** in the video guide's wording: „Windows a protejat PC-ul” → „Mai multe informații” → „Rulați oricum”; for uploadspike's prompt, „Rețele private” → „Permiteți accesul”.
    8. **The setup, on each device that runs a hub** (P46): the setup page on the PC (`uploadspike setup`), `sudo ./uploadspike setup` in the terminal on the Pi, over SSH or with a monitor. Under D2-A only the PC, under C only the Pi; under B both, the Pi for the 7 days and the PC for MI5.
    9. **The dated 7-day schedule** that uploadspike prints («Programul probei»<!--uploadspike-->), with sleep off and Windows Update paused on the D2 device; `uploadspike host` prints the recommended device first. This step is the schedule only (P48); the runs start in step 10.
    10. **Camera preparation (A7), then the start of the runs.** For each camera chosen in D3, with the agent's per-brand steps, including where the camera shows its ONVIF port (Reolink: 8000): ONVIF and RTSP on (Hikvision: „Configuration” → „Network” → „Advanced Settings” → „Integration Protocol”, or „Maintenance” → „System Service” → „ONVIF” on newer firmware; ONVIF is off by default from firmware 5.5.0); an ONVIF user at the lowest level, „User” (Hikvision: „Media User”), with its own password, raised to „Operator” only if `uploadspike mi8 --probe-only` reports that events are refused, never „Administrator”; the camera's own motion detection over the whole frame, with the linkage that publishes it as an ONVIF event; an H.264 substream; NTP or the clock. Then day 0 (A12): `uploadspike mi6` on the D2 device (on the Pi every command is `sudo ./uploadspike …`), then the logins it asks for: on the A24 the app's «Conectare la stocare (MI6)»<!--app--> with the address `http://<hub device>:7091`, on the PC the Windows app's same button, on the web the device-code page, then the web reader page `http://localhost:7091/reader`; when it has nothing left to show, it hands the hub to the autostart and its window may be closed. Day 0–1 (A13): `uploadspike mi8` on the D2 device, typing each camera's address (with `:<port>` when its ONVIF port is not 80), ONVIF user and password at its prompts. On the Pi it then shows «MI8 rulează în fundal 72 de ore. Puteți închide fereastra; progresul îl vedeți cu uploadspike status.»<!--uploadspike-->, and the SSH window may be closed; on the PC it shows «Lăsați fereastra deschisă 72 de ore.»<!--uploadspike-->, and that window stays open. When `uploadspike status` shows «MI8 s-a oprit; rulați uploadspike mi8 --continue.»<!--uploadspike--> (after a reboot, or a closed window), run `uploadspike mi8 --continue`: it names each camera with its brand and model and asks its address, user and password again. At 24 h, the episode check: `uploadspike status` shows each camera's episode count; walk past every camera still below 20, waiting out the 10 s gap between passes (P43). From day 1, once a day, `uploadspike status` on the D2 device; when it asks for a reconnection with B1, run the command it shows (when the line says «după proba MI5»<!--uploadspike-->, only once the MI5 probe has ended), then the login on that platform as on day 0, with the code the app shows (P39).
    11. **The MI5 window:** about 8 h, on the PC under D2 A or B and on the Pi under C. `uploadspike mi5` takes over the 7-day hub where one runs (A, C) and gives it back at the end; until then `uploadspike setup`, `uploadspike mi6`, `uploadspike mi6 --b1` and `uploadspike mi6 reader` on that device answer «Proba MI5 rulează; rulați comanda după ce se termină.»<!--uploadspike-->, while `uploadspike status` and `uploadspike mi8 --continue` work as usual (step 10). It shows «Lăsați fereastra deschisă până la final.»<!--uploadspike-->: keep its window open, and on the Pi its SSH window, with the PC it runs on awake, for the whole 8 h, or run it at the Pi's own keyboard and monitor. If the window closes early, the 7-day hub goes on by itself and MI5 is run again on another of days 1–5. On a Pi whose model the program does not know, it asks for MI5 on the PC instead. Under B, and with D1-B, it first asks one short login: an address and a code shown in its window, typed into any browser. One UAC or sudo prompt; only `ipcam-hub` and `mediamtx` lose internet (on the 7-day device the 7-day hub too, at a time when no daily refresh falls in the window); the one-line deblocare command (`uploadspike unblock`, or `sudo ./uploadspike unblock` on the Pi).
    12. **Two screenshots of the Cloud console's Google Drive API page for that window** (P18, A14): the quota page (units used) and the Metrics tab (requests by method).
    13. **Sending the results:** `uploadspike results` on each device that ran a hub; on the Pi `sudo ./uploadspike results`, which leaves `results-pi.zip` in the current folder, owned by the SSH user. Copy it to the PC with `scp <utilizator>@<adresa Pi-ului>:<cale>/results-pi.zip .` (the user name the owner logs in with; Raspberry Pi OS has no fixed `pi` user) or a USB stick; attach the zips in the chat or put them in `ipcam-teste/` on Drive.
    14. **Cum opriți testul** (A15): `uploadspike uninstall` on each device. Optionally: revoke the spike's access (Google Account → „Security” → „Third-party apps & services”; Microsoft account → the apps that can access your data); delete the `ipcam` folders on the test accounts and, if A5 created one, the Entra app; for each camera, keep the ONVIF user for Phase 1, or delete it and turn ONVIF off.
  - **Owner messages (final Romanian texts; formal «dvs.», ș and ț with comma below).** The controller sends M1 and M2 as soon as the plan is approved, M3 in Task 13 Step 1, M4 in Task 13 Step 3, M5 with the PR.
    - **M1 (D1–D3, before M2):**
      > Avem trei întrebări despre proba de upload. Toate variantele sunt gratuite. Dacă nu ne răspundeți, aplicăm varianta implicită: la D1 contul de test, la D2 varianta pe care v-o recomandăm pentru Pi-ul dvs., la D3 varianta A.
      >
      > **D1.** Proba urcă doar pe contul Google de test nou. Drive-ul dvs. personal nu este atins. Proiectul Google Cloud rămâne pe contul dvs. personal. Dacă vreți totuși să folosim și Drive-ul personal, alegeți: (B) o verificare mică, de circa 0,5 GB; (C) toată proba, circa 7–8 GB. Pentru C trebuie să aveți cel puțin 11 GB liberi. Spațiul acesta este comun cu Gmail și Google Foto. Dacă nu ne spuneți nimic, rămâne contul de test.
      >
      > **D2.** Ce dispozitiv poate rămâne pornit 7 zile? Hub-ul reîmprospătează accesul zilnic. Evenimentele camerelor se ascultă 72 de ore. (A) PC-ul. Rămâne pornit 7 zile, fără somn și cu actualizările Windows amânate. Proba de circa 8 ore fără internet rulează tot pe el; doar hub-ul pierde internetul. (B, recomandat dacă Pi-ul este un Pi 4, 5, 400 sau 500 cu sistem pe 64 de biți) Pi-ul ține cele 7 zile. PC-ul îl folosim doar pentru login-ul de pe Windows și pentru proba fără internet, într-o singură zi. (C) Doar Pi-ul, pentru tot. Pe un Pi 3 sau Zero 2 W proba urcă de la 2 camere în loc de 4, deci dovada este mai slabă. Modelul Pi-ului îl citim noi. Dacă sistemul Pi-ului este pe 32 de biți, vă recomandăm varianta A.
      >
      > **D3.** Ce camere intră în proba de evenimente de 72 de ore? După ce ne spuneți mărcile și modelele, vă spunem care camere suportă evenimente ONVIF. (A, recomandat) Toate aceste camere. (B) Câte o cameră din fiecare marcă. Configurați mai puțin, dar verdictul unei mărci se sprijină pe un singur model. (C) Amânăm proba până există detectorul de camere (Faza 0 a). Alegeți, dacă puteți, camere care văd mișcare des. O cameră cu mai puțin de 20 de mișcări în 72 de ore rămâne fără verdict. Fiecare cameră aleasă primește un utilizator ONVIF de nivel „User”. Parola rămâne la dvs. și o scrieți doar în programul de pe hub.
    - **M2 (A1–A7, now):**
      > Pregătiri pentru proba de upload. Toate sunt gratuite. Le puteți face acum, cât rulează proba de înregistrare.
      > 1. Spuneți-ne câte camere aveți. Pentru fiecare, scrieți marca și modelul, așa cum apar pe etichetă sau în pagina web a camerei. Nu ne trimiteți poza etichetei: are seria și, la unele mărci, un cod de verificare. Dacă nu știți marca, o află programul nostru.
      > 2. Creați un cont Google nou, doar pentru teste. Google poate cere un număr de telefon, dar nu cere card. Nu ne trimiteți parola.
      > 3. Pe contul dvs. Google personal, creați proiectul Google Cloud după ghidul nostru (circa 20–25 min). Dacă Google vă cere să activați verificarea în doi pași, activați-o: e gratuită și obligatorie pentru consola Google Cloud. Dacă apare o ofertă „free trial” sau o cerere de card, închideți-o: proiectul nu are nevoie de facturare. La final, trimiteți-ne capturi ale paginilor „Audience” și „Data access”. Capturile rămân în conversație și nu se publică.
      > 4. În același proiect, creați cei trei clienți OAuth din ghid. La fiecare, descărcați fișierul JSON oferit la creare și păstrați-l pe PC: Google arată secretul o singură dată. Trimiteți-ne doar ID-urile clienților, nu și secretele.
      > 5. Intrați pe entra.microsoft.com cu contul Microsoft personal (2 min). Dacă sub numele dvs. apare doar un șir lung de litere și cifre, sau vi se cere un abonament Azure ori un card, opriți-vă și spuneți-ne: OneDrive trece în Faza 0-bis, cum ne așteptam. Nu folosiți contul de la serviciu. Dacă vedeți un director al dvs. și „New registration” se deschide, spuneți-ne și continuați cu pasul 4 din ghid; trimiteți capturile „Overview”, „Authentication” și „API permissions”.
      > 6. După ce ne spuneți mărcile, vă trimitem pașii pentru fiecare cameră: pornirea ONVIF, un utilizator ONVIF de nivel „User” și detecția de mișcare. Parolele rămân la dvs. Le scrieți doar în programul de pe hub, la rulare.
      >
      > Proba propriu-zisă începe după ce se termină testele probei de înregistrare. Durează circa 7 zile cu un dispozitiv pornit, din care circa 2 ore de lucru pentru dvs.
    - **M3 (A9, Task 13 Step 1):**
      > Programele probei de upload sunt gata. Vă rugăm să creați pre-release-ul în GitHub: deschideți pagina „Releases” a depozitului DevWorld, apăsați „Draft a new release”, scrieți eticheta `spike-upload-v0.1.0`, alegeți ramura `ipcam/phase0c2-upload-spike`, bifați „Set as a pre-release” și apăsați „Publish release”. Durează 2 minute. Noi verificăm apoi că fișierele s-au atașat.
    - **M4 (the run week, Task 13 Step 3; `<release>` is the release's URL and `<guide>` the guide's URL on the branch):**
      > Proba de upload poate începe. Linkul către fișiere: <release>. Ghidul: <guide>. Începeți cu pasul 6 din ghid, după ce ați terminat pasul 14 al probei de înregistrare. Pe telefon, după instalare, creați clientul Google „Android” cu amprenta SHA-1 din notele versiunii (pasul 6). Ziua 0: autentificările pe telefon, pe PC și în browser. În fiecare zi după aceea, rulați uploadspike status; dacă vă cere o reconectare, rulați comanda pe care o arată, iar dacă scrie «după proba MI5», rulați-o după ce se termină proba fără internet. Zilele 1–5: proba de circa 8 ore fără internet, când alegeți dvs.; la pornire, programul vă poate cere o autentificare scurtă, cu un cod pe care îl scrieți în browser. Lăsați fereastra probei deschisă până la final. Ziua 7: trimiteți rezultatele și dezinstalați. Totul durează circa 2 ore de lucru, plus un dispozitiv pornit 7 zile.
    - **M5 (A16):**
      > Raportul probei de upload e gata, cu verdictele și deciziile. Vă rugăm să-l citiți și să aprobați PR-ul (circa 15 minute). Dacă ceva nu e clar, întrebați-ne.

- [ ] **Step 1: Write the failing test.** `third-party-notices.test.sh` upload case: the output has `MediaMTX` with `License: MIT`, `hls.js` with `License: Apache-2.0`, `golang.org/x/oauth2`, `github.com/clbanning/mxj/v2` and `npm package: @capacitor/browser `; the video and recording cases are unchanged.
- [ ] **Step 2: Run.** `cd ipcam && tools/scripts/test/third-party-notices.test.sh` — Expected: FAIL.
- [ ] **Step 3: Implement** the script option and the workflow; write the guide and the README link.
- [ ] **Step 4: Verify.**
  - actionlint v1.7.12 on the release workflow exits 0. Register it with the THROWAWAY pattern, then dispatch it with `publish=false`. Expected: every job green; the "release assets" notice lists every asset with its SHA-256; `SHA256SUMS` verifies; the `apk-sha1` notice is present.
  - The guide-label check runs in the release job against the sources and the built binaries, as `ipcam-spike-release.yml` does:

    ```bash
    sources() { for f in "$@"; do [[ "${f}" == *_test.go ]] || printf '%s\n' "${f}"; done; }
    mapfile -t spike_src < <(sources tools/spikes/upload/cmd/uploadspike/*.go tools/spikes/upload/{mi5,mi6,mi8,handoff,sdconfirm,install,hostinfo,hubproc,runstate,results}/*.go)
    mapfile -t apk_scripts < <(find "${RUNNER_TEMP}/apk-app/assets/public" -name '*.js')
    tools/scripts/check-guide-labels.sh docs/spike-upload.md \
      --program uploadspike "${spike_src[@]}" tools/spikes/upload/cmd/uploadspike/web/*.html \
      --program uploadspike "${RUNNER_TEMP}/win/uploadspike.exe" \
      --program probe tools/spikes/upload/cmd/uploadspike-probe/main.go \
      --program app tools/spikes/video/app/src/i18n.ts --program app "${apk_scripts[@]}" \
      --program android apps/mobile/android/app/src/main/res/values/strings.xml
    ```

    Expected: exits 0. `spike_src` holds every package with Romanian strings, `hostinfo` («Pi-ul nu poate rula hub-ul») and `hubproc` included.
  - No step of the guide asks for payment details or a GitHub secret (zero-cost check, read by the reviewer).
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add upload spike release and Romanian owner guide"`

### Task 13: Owner run, report and decisions (PH0-30, PH0-31, PH0-32)

Rulings: P05 (Technical Decision), P08 (the owner's pre-release), P22 (G34), P23, P51, P55, C2-R4, C2-R6.

**Files:**
- Create: `ipcam/docs/research/<date>-phase0c-upload-results.md` and `ipcam/docs/research/phase0-data/phase0c-upload-{mi5,mi6,mi8}.csv` (from `uploadreport`)
- Modify: `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md` §2.2, under c2's own gate subheadings G14, G15 and G17
- If c2 is the last Phase 0 plan to close: Create `ipcam/docs/research/<date>-phase0-results.md`, the PH0-30 index

- [ ] **Step 1: Release (R55, P08).** Push so that `ipcam-spike-upload`, `ipcam-spike-recording`, `ipcam-apps`, `ipcam-mobile` and `ipcam-windows` each have a green run on the head commit. Send M3. When the owner has created `spike-upload-v0.1.0` on `ipcam/phase0c2-upload-spike`, push nothing until the tag sits on that commit, the release run is green and every asset is attached.
- [ ] **Step 2: Lab results.** Dispatch `ipcam-spike-upload-lab.yml` on the tag commit and wait until it is green. Run `uploadreport -annotations <lab run id> -onedrive unknown -out <scratch>/lab` and check that the `mi5-lab` envelope parses and that its TST-07 row is present (a lab envelope never decides G14, Task 8).
- [ ] **Step 3: Message to the owner.** Send M4 with the release and guide links. c2's runs start after c1's guide step 14 (P51); one combined time estimate for both plans goes to the owner once.
- [ ] **Step 4: Owner results.** When the zips arrive (chat attachment or Drive `ipcam-teste/`), unpack each into its own scratch directory. Run, from `ipcam/`:
  ```bash
  go run ./tools/spikes/upload/cmd/uploadreport -in <pc results> -in <pi results> \
    -annotations <lab run id> -onedrive <free|paid|unknown from A5> -d3 <a|b|c from D3> -console-units <n from the screenshot> -out docs/research
  ```
  Expected: exit 0. Inspect the report and CSVs for any IP, MAC, serial, SSID, hostname, URL, email or token, even though `redact.Scan` and the token patterns passed (PH0-22). The owner's screenshots stay in scratch.
- [ ] **Step 5: Decisions (PH0-31)** into §2.2, each with its gate:
  - **G14:** the outbox fix before Faza 3, or 2-min segments if the open-segment read failed.
  - **G15:** per pair, the plan that passed (A or B1); for a pair whose B1 failed, the B2 decision text from `mi6.Judge`; each «F6 pe <platformă> folosește tipul de client al hub-ului (creatori: …)» line.
  - **G17:** per affected brand, the native adaptor or «evenimente nesigure» + FLW-16.
  - **The P05 Technical Decision:** «Decizie tehnică (G14, §3.4): driverul Google Drive este un client REST propriu peste `net/http`, cu `golang.org/x/oauth2` pentru tokenuri, în locul `google.golang.org/api`. Biblioteca strică build-ul comun al workspace-ului și ascunde URI-ul sesiunii resumable, de care reluarea după kill -9 are nevoie (§9.1, §9.9).»
  - The PR description may propose, for the owner's approval, the wording fix of §16.9's G16 row ("părțile OneDrive din G14" means the parts Phase 0 cannot run, P55).
- [ ] **Step 6: PH0-32 and Faza 0-bis.**
  - List the services, accounts and devices used, each with its OWN row: OWN-07 (the Testing project; 2-Step Verification is free), OWN-08 (only if A5 passed: the free registration), OWN-10 (the test accounts), OWN-14 (PC and A24), OWN-15 (the owner's Pi), OWN-18 (CI); the cameras (O10) and the DIGI line (O14); the BtbN FFmpeg build, a free download used by the harness only.
  - Confirm that the report's static Faza 0-bis table matches §16.9 and that no generated file was edited by hand.
  - **PH0-30 index (G34).** If (a), (b) and (c1) have all closed, write `docs/research/<date>-phase0-results.md` with the G1–G19 table; otherwise note in the PR which plan writes it.
- [ ] **Step 7: Commit and open the PR** for the owner's approval — `git commit -m "ipcam: add Phase 0 (c2) upload spike results and decisions"`. Push, and check that `ipcam-spike-upload` ran on that commit (its `paths:` hold the report and the CSVs, Task 11) and that its `uploadreport -scan` step scanned them and passed. The PR targets `ipcam/phase0c1-recorder-spike` while c1 is unmerged, else `main`. Send M5. After the owner approves, tell the c1 controller that `tools/spikes/recording/` may be deleted (P14).
