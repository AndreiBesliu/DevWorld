# Phase 0 (c2) Upload Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Pe scurt pentru proprietar.** Construim o probă care verifică, pe dispozitivele dvs., că înregistrările ajung sigur în cloud, fără goluri și fără duplicate, chiar dacă internetul cade:
- blocăm internetul (nu și rețeaua locală) timp de 6 ore în timp ce hub-ul urcă în Google Drive și, dacă se poate gratuit, în OneDrive personal, și repornim hub-ul în mijloc;
- verificăm că hub-ul se conectează singur la Drive și OneDrive (login pe telefon, cod predat hub-ului) și reîmprospătează accesul zilnic;
- lăsăm hub-ul să asculte evenimentele de mișcare ale camerelor dvs. 72 de ore.

Pentru asta aveți de făcut un lucru pregătitor: creați un proiect Google Cloud pe contul dvs. personal și, dacă nu cere date de plată, o înregistrare de aplicație Microsoft. Vă dăm un ghid pas cu pas. Totul e gratuit. La final iese un raport cu verdictul pentru porțile G14, G15 (partea din Faza 0) și G17.

**Goal:** Measure MI5, MI6 (Phase 0 free combinations) and MI8 on the owner's Windows 11 PC or Raspberry Pi, against the owner's Google Drive (personal account, Testing mode) and — only if the Microsoft app registration needs no payment details — OneDrive personal, and produce the Phase 0 (c) upload-and-accounts report with verdicts for gates G14, G15 (Phase 0 part) and G17.

**Architecture:**
- **Production-bound code (written for real, §3.5, §16.1).** The `storage.Destination` driver interface and its `Caps`/typed errors (`hub/internal/storage`), the local-disk spool driver, the Google Drive (`drive.file`) driver and the OneDrive personal driver, and the `outbox.Queue` subset (`hub/internal/outbox`, with a minimal SQLite upload-state store) are the real hub modules, scoped to what G14/G15 exercise. The OneDrive driver is written for real regardless; only the live run against OneDrive is gated on the free-registration decision.
- **`uploadspike` harness.** Drives the 6 h WAN block (host firewall on the owner's device; `toxiproxy`/`iptables` in the lab), the mid-upload hub restart, the OAuth on-hub handoff (PKCE code+state delivered to the hub; device flow for web × Google), the daily refresh loop, and the 72 h ONVIF-events run with a harness motion reference. Harness-only; lives under `tools/spikes/upload/` and is deleted after the report.
- **Fakes for CI.** `fake-gdrive` and `fake-graph` run the conformance and outage logic in CI without any account.
- **The report.** A Go generator turns the results into the Romanian report and the G14, G15 and G17 verdicts.

**Tech Stack:**
- Go (newest `go1.27.x` whose toolchain is ≥ 14 days old), `google.golang.org/api` (Drive v3), an own Microsoft Graph REST client (OneDrive), `modernc.org/sqlite` (Go-pure), `github.com/kerberos-io/onvif` v1.2.3
- Node 24 LTS / pnpm 10 only to drive the OAuth handoff on the reused Android, Windows and web shells
- `toxiproxy` and host firewall rules as lab/CI tools for the WAN block; ffmpeg with libx264 (CI/lab only) for the motion reference input

**Spec:** `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`:
- §16.4 (MI5, MI6, MI8; test hosts; controller), §16.5 gates G14, G15, G17 and PH0-30…32
- §9.1 (driver interface), §9.2 (per-provider notes), §9.7–§9.9 (segment lifecycle, retention, outage behaviour), §9.10 (folder layout), §9.11 (per-destination catalog)
- §3.3 (data map, OAuth Phase 0 gate), §5.6 (OAuth plans, B1 device flow, B2 proxy), §8.7 (F7 per-destination, T11, T34, client types, OneDrive redirects), §8.2/§8.4 (ONVIF events)
- §16.9 (Faza 0-bis deferrals), decisions O11, O13, O14, T11, T34, T41; requirements STO-02, STO-07, STO-08, SUC-04, HUB-11, FLW-16, FLW-31, G17/HUB-11

**Prerequisites:** The video spike plan (`2026-10-18-phase0b-video-spike.md`) Task 1 has created `ipcam/go.work`, `ipcam/tools/go.mod` and the Go CI patterns; the phase0c1 recorder plan has added `./hub` to the workspace (if that plan has not run, this plan's Task 1 adds `./hub` instead). MI6 OAuth on Android, Windows and web reuses the video spike shells and app: the Android Capacitor shell (video spike Task 10), the Windows Tauri shell (video spike Task 11) and the spike web app (video spike Task 9) carry the on-device login surface and hand `code`+`state` to the hub. MI8 reuses the `cameras` ONVIF events code from Phase 0 (a) (`hub/internal/cameras`); if Phase 0 (a) has not landed, this plan's Task 10 pulls the ONVIF subscription subset into `hub/internal/cameras/events` directly.

## Global Constraints

- **Repo rules.** Touch only `ipcam/**` and `.github/workflows/ipcam-*.yml`; every push/PR trigger has `paths:` limited to the files it builds plus its own workflow file. Code, identifiers, comments and commits are in English. Owner-facing text (guide, report summary) is Romanian.
- **Zero cost (O13, PH0-32).** No paid service and no payment details. Google Drive uses a Google Cloud project the owner creates on his **personal** Google account, in **Testing** mode (`drive.file` only; 7-day refresh tokens accepted for the spike; the day-7-and-after refresh belongs to Faza 0-bis, with the project in Production). OneDrive **personal** runs only if the Microsoft app registration needs no payment details; otherwise it moves to Faza 0-bis (§16.9) — the driver is still written, the live run is not planned. OneDrive **business**, the Entra ID P1 tenant (MI7/G16) and all iOS paths are Faza 0-bis — list them, do not plan their execution. Only GitHub-hosted runners (free, O12). Devices: the owner's Windows 11 PC and Raspberry Pi; DIGI home internet.
- **Secrets and privacy (PH0-22).** The owner's camera credentials, RTSP URLs and all storage tokens never enter the repo, logs, results or report. Refresh tokens are sealed on the hub (RAM/sealed only, §3.3, T11), never in cloud, never in results. CI uses dedicated test accounts whose refresh tokens are GitHub secrets (§14.5), never the owner's. Results published to the public repo are redacted per §16.2.3. Real cameras appear as `real-1`, `real-2`; destinations as `dst-1`.
- **OAuth is on the hub (T11, T34).** The PKCE `code_verifier` is generated and kept by the hub (RAM, 10 min); the app delivers only `code`+`state` on the encrypted channel; the hub redeems and refreshes. Web × Google has no plan A and uses device flow started by the hub (T34). Per-platform Google client types (same project): Android ("Android", custom URI scheme), Windows ("Desktop", loopback `http://127.0.0.1:<port>/`), web (device flow, "TVs and Limited Input"). `BeginOAuth` returns the exact `redirect_uri` the hub repeats at redemption (FLW-31). Cloud never holds a confidential secret; the Desktop/TV client secrets are non-confidential and live in the hub.
- **Toolchains.** Go: newest `go1.27.x` whose toolchain is ≥ 14 days old. Node 24 LTS, pnpm 10 with `minimumReleaseAge: 10080`. Every other dependency: newest ≥ 14 days old. Lockfiles committed; `-mod=readonly` and `--frozen-lockfile` in CI.
- **Licenses.** Allowlist: MIT, BSD-2/3, Apache-2.0, ISC, MPL-2.0. `google.golang.org/api` (BSD-3), the own Graph client, `modernc.org/sqlite` (BSD-3), `kerberos-io/onvif` (MIT). ffmpeg with libx264 and `toxiproxy` are lab/CI only, never shipped.
- **Provider rules, exactly as in §9.2.** Google Drive (`drive.file` only): resumable session (URI valid one week), 16 MiB chunks (multiple of 256 KiB), resume with `PUT bytes */size`, verify `md5Checksum` + size; 750 GiB/day per user → 24 h pause; ≈ 2 creates/s; quota in units (list 100, download 200, get 5), the 400M-units/day billing threshold is **per project** → **zero `files.list` in normal operation**, cache on playback; 100 refresh tokens per account+client. OneDrive personal: upload session, 10 MiB fragments (32 × 320 KiB, < 60 MiB), in order, **no** `Authorization` header on `uploadUrl`, resume with `GET uploadUrl`, verify `quickXorHash` + size; recycle bin **occupies quota** (`quota.deleted`); `Retry-After` on 429.
- **Spike-only shortcuts.** None of these may appear in `hub/**`, and each is marked `// SPIKE-ONLY` with a reference to this plan: the WAN-block driver, the mid-upload restart orchestration, the OAuth handoff test rig and the harness motion reference. Production driver, outbox and ONVIF code carry no such marker.
- **Thresholds, exactly as in §16.4 / §16.5:**

  | Code | Threshold |
  |---|---|
  | MI5 (G14, STO-02, SUC-04) | 4 cameras continuous + events; 6 h WAN block via host firewall (LAN stays); one hub restart mid-upload: 0 missing segments, 0 duplicates, every hash verified. Record: recovery time vs uplink, Drive quota units per segment, OneDrive personal quota freed after delete (`quota.deleted`), `playback` reading a still-open segment. Plan B: fix outbox before Faza 3; if reading an open segment fails, segments drop to 2 min |
  | MI6 (G15 Phase 0 part, T11, T34) | PKCE on device + code handed to the hub (web × Google: device flow on the hub); daily refresh from the hub, days 1–6 (Testing mode). Phase 0: Google Drive × Android (AND-1), Windows (WIN-1) and web (PC); OneDrive personal on the same platforms only if the Microsoft registration is free. Cross-client visibility: a file created by the hub's OAuth client is listed and read by the web, device-flow and native clients of the same project / Entra app. Pass per §3.3; result written to §2.2 |
  | MI8 (G17, HUB-11, FLW-16) | the owner's cameras on the spike hub, 72 h; a harness frame-diff motion reference (lab-only, not a shipped feature): ≥ 95 % of motion episodes have an ONVIF event within 5 s; 0 silent subscription losses (> 5 min with no successful `Renew`/`PullMessages`) |

- **Plan B per gate (written into §2.2 on failure, PH0-31).** G14 → fix outbox before Faza 3; if open-segment reads fail, 2-min segments. G15 → the §3.3 plan B order: (1) device flow on the hub for Drive and OneDrive personal; (2) cloud stateless proxy (B2, §5.6) for the rest, with the data-map and privacy-policy rows updated first; if cross-client visibility fails, F6 uses the hub's client type. G17 → per affected brand, a native event adaptor (Hikvision `alertStream`, Dahua `eventManager`, Reolink push) as a technical decision; else the camera is marked "unreliable events" and FLW-16 applies.
- **Faza 0-bis leftovers to list, not execute (§16.9).** OneDrive personal if the Microsoft registration needed payment details; OneDrive business; the Entra ID P1 tenant and MI7/G16; the Drive day-7+ refresh with the project in Production; all iOS OAuth combinations; the nightly runs on real paid storage accounts (B2, R2, Wasabi, AWS SSE-KMS).

## Review Focus

1. **The Google project is in Testing, so the refresh token expires after 7 days and a run that crosses day 7 fails for a reason that is not the hub's fault.** Expected: the harness asserts refresh only on days 1–6, marks a token that expires at day 7 as `expected-expiry` rather than a failure, and the report lists the day-7+ Production check as a Faza 0-bis leftover. *Owner: Task 9, test `refresh_day7Expiry_notCountedAsFailure`.*
2. **The WAN block also severs the LAN or the hub's own loopback, so the test breaks the setup instead of the hub.** Expected: the firewall rule blocks only WAN egress to the provider hosts; the harness verifies LAN and loopback reachability before starting the 6 h clock and aborts the run with a clear message if the block is too broad. *Owner: Task 8, test `wanBlock_keepsLanAndLoopback`.*
3. **`files.list` is called during normal upload, burning the per-project Drive quota shared across all of the product's users.** Expected: zero `files.list` in steady state; listing happens only on explicit reconciliation; the Drive driver asserts the call count stays 0. *Owner: Task 5, test `TestDrive_ZeroListInNormalOperation` (STO-07).*
4. **OneDrive's recycle bin occupies quota (`quota.deleted`), so a "delete oldest until the quota frees" loop deletes copy after copy without freeing any space.** Expected: deletion counts only what the catalog accounting requires (age and GB caps), never "until the quota drops"; at ≥ 98 % with bytes in the bin the destination goes `QUOTA_FULL` with `PAUSE_KEEP_LOCAL`, and the alert says space frees after the bin empties (max 30 days). *Owner: Task 6, test `TestOneDrive_RecycleQuota_NoDeleteLoop`.*
5. **A camera credential, RTSP URL or storage token leaks into the results, logs or report.** Expected: tokens are sealed in hub memory only and never written to results; the handoff keeps the PKCE verifier on the hub; a sentinel test proves a planted secret appears zero times in every result and report artifact (PH0-22, ARH-08). *Owner: Task 11, test `TestRender_NoSecretInOutput`, and Task 9, test `handoff_verifierNeverLeavesHub`.*

---

### Task 1: Upload spike module, conformance scaffolding and upload CI

**Files:**
- Modify: `ipcam/go.work` (add `./hub` if not already present, and `./tools/spikes/upload`)
- Create: `ipcam/hub/go.mod` (only if the recorder plan has not created it; module `github.com/AndreiBesliu/DevWorld/ipcam/hub`)
- Create: `ipcam/tools/spikes/upload/go.mod` (module `github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload`), `.golangci.yml`, `doc.go`
- Create: `.github/workflows/ipcam-spike-upload.yml`

**Interfaces:**
- Produces: `cd ipcam && go build ./hub/internal/storage/... ./hub/internal/outbox/... ./tools/spikes/upload/...`, lint and `go test -race` on those trees, and the CI job `upload` running lint, tests, `govulncheck` and the license check.

- [ ] **Step 1:** Create the spike module; add it (and `./hub` if needed) to `go.work`. Lint config mirrors the video spike (`version: "2"`; `govet, staticcheck, errcheck, gosec, revive`).
- [ ] **Step 2:** Write `ipcam-spike-upload.yml`: paths `ipcam/hub/internal/{storage,outbox}/**`, `ipcam/tools/spikes/upload/**`, `ipcam/go.work` and the workflow file; `concurrency` cancel-in-progress; actions pinned to SHAs; `setup-go` with `go-version-file`; job `upload` runs lint, `go test -race`, govulncheck, licenses; the conformance and outage jobs run against the fakes with **no** account secrets so fork PRs stay green.
- [ ] **Step 3: Verify.** `cd ipcam && go build ./tools/spikes/upload/... && go tool golangci-lint run ./tools/spikes/upload/... && go run github.com/rhysd/actionlint/cmd/actionlint@latest ../.github/workflows/ipcam-spike-upload.yml` — Expected: all succeed.
- [ ] **Step 4: Commit** — `git commit -m "ipcam: add upload spike module and CI"`

### Task 2: `storage.Destination` interface, `Caps` and typed errors

**Files:**
- Create: `ipcam/hub/internal/storage/{destination.go,caps.go,errors.go,types.go}` + `destination_test.go`

**Interfaces:**
- Produces (exact, §9.1):

```go
type Destination interface {
    Kind() Kind                     // LOCAL, NAS_SMB, GOOGLE_DRIVE, ONEDRIVE_PERSONAL, ONEDRIVE_BUSINESS, S3, WEBDAV, SFTP
    Capabilities() Caps
    Probe(ctx context.Context) (ProbeReport, error)
    BeginUpload(ctx context.Context, key ObjectKey, size int64, meta Meta) (UploadState, error)
    PutChunk(ctx context.Context, st *UploadState, off int64, r io.Reader, n int64) error
    Resume(ctx context.Context, st *UploadState) (nextOffset int64, err error) // detects already-complete sessions
    Commit(ctx context.Context, st *UploadState) (ObjectRef, error)             // provider id, size, provider hash
    Abort(ctx context.Context, st *UploadState) error
    Stat(ctx context.Context, ref ObjectRef) (ObjectInfo, error)
    OpenRange(ctx context.Context, ref ObjectRef, off, n int64) (io.ReadCloser, error)
    PlaybackURL(ctx context.Context, ref ObjectRef, ttl time.Duration) (string, error) // or ErrProxyRequired
    Delete(ctx context.Context, ref ObjectRef) (DeleteOutcome, error)                  // PERMANENT or RECYCLED
    Quota(ctx context.Context) (Quota, error)
    PutSmall(ctx context.Context, key ObjectKey, data []byte) (ObjectRef, error)
    List(ctx context.Context, prefix ObjectKey) iter.Seq2[ObjectInfo, error]            // reconciliation and import only
}
```

`Caps` has exactly the fields in §9.1 (`Resumable ResumeKind`, `ChunkAlign`, `PartSize`, `MaxRequest`, `EqualParts`, `MaxObject`, `Playback LinkKind`, `RangeRead`, `Delete DeleteKind`, `Quota QuotaKind`, `Hashes []HashKind`, `ServerLifecycle`, `DailyUploadCap`, `CreatesPerSec`). Typed errors: `ErrAuthExpired`, `ErrQuotaFull`, `ErrRateLimited{RetryAfter}`, `ErrDailyCap`, `ErrSessionExpired`, `ErrNotFound`, `ErrTransient`, `ErrPermanent`.

- [ ] **Step 1: Write the failing tests.**
  - `TestCaps_ZeroValueIsConservative`: an unset `Caps` reports `Resumable: NONE`, `Delete: PERMANENT`, `Quota: NONE`, empty `Hashes`.
  - `TestErrors_MapToReasons`: each typed error maps to the §7.7 reason (2100/2101/2102).
- [ ] **Step 2: Run.** `go test ./hub/internal/storage/...` — Expected: FAIL.
- [ ] **Step 3: Implement** the types; `Destination` has no implementation yet.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add storage Destination interface, Caps and typed errors"`

### Task 3: Local-disk spool driver

**Files:**
- Create: `ipcam/hub/internal/storage/local/{local.go,local_test.go}`

**Interfaces:**
- Consumes: Task 2.
- Produces a `Destination` for `Kind() == LOCAL`: `.part` write + fsync + atomic rename, SHA-256 verify, `statfs` free space, permanent delete, `OpenRange` on the local file. It is the always-present spool (S3) and the conformance baseline.

- [ ] **Step 1: Write the failing tests.**
  - `TestLocal_WriteVerifyRange`: write an object, `Stat` returns the SHA-256 and size, `OpenRange` returns a middle range exactly.
  - `TestLocal_ResumeAfterPartialWrite`: a `.part` left at offset K resumes from K, not 0.
  - `TestLocal_Quota`: `Quota` reports filesystem free space.
- [ ] **Step 2: Run.** `go test ./hub/internal/storage/local/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add local disk spool storage driver"`

### Task 4: `outbox.Queue` subset with a minimal SQLite upload-state store

**Files:**
- Create: `ipcam/hub/internal/outbox/{queue.go,store.go,backoff.go}` + tests
- Create: `ipcam/hub/internal/outbox/migrations/0001_upload_state.sql`

**Interfaces:**
- Consumes: Tasks 2–3; `modernc.org/sqlite`.
- Produces (aligned with §4.4, §9.7):

```go
package outbox
type Placement struct { ObjectKey storage.ObjectKey; LocalPath string; DestinationID string; SizeBytes int64; SHA256 string; Class string /* EVENT_CLIP|THUMBNAIL|CONTINUOUS|EXPORT */ }
type Queue interface {
    Enqueue(tx *sql.Tx, p Placement) error          // runs in the same transaction as the object (S3)
    Run(ctx context.Context) error                  // workers: upload, verify, backoff 1s→5min jitter, resume from last confirmed offset
}
func New(db *sql.DB, dests map[string]storage.Destination) Queue
```

A placement moves `pending → uploading → verified` (size + provider hash); on a transient error it backs off (1 s → 5 min, full jitter); on `kill -9` the `UploadState` resumes from the last confirmed offset, not from 0; the object key is deterministic (§9.10) so a resumed-and-complete session commits once (no duplicate).

- [ ] **Step 1: Write the failing tests** (a fake `Destination` with injectable faults):
  - `TestOutbox_EnqueueInSameTx`: a rollback of the object's transaction also rolls back the placement.
  - `TestOutbox_ResumesFromLastOffset`: a kill at offset K resumes at K; the object commits with one `Commit` call and no duplicate.
  - `TestOutbox_Backoff`: transient errors produce 1 s → 5 min with jitter (fake clock).
  - `TestOutbox_VerifyHashAndSize`: a provider hash mismatch keeps the placement `failed`, never `verified`.
- [ ] **Step 2: Run.** `go test ./hub/internal/outbox/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add outbox queue subset with SQLite upload state"`

### Task 5: Google Drive driver (`drive.file`) and `fake-gdrive`

**Files:**
- Create: `ipcam/hub/internal/storage/gdrive/{gdrive.go,gdrive_test.go}`
- Create: `ipcam/tools/spikes/upload/fakes/gdrive/{fake.go,fake_test.go}` (resumable upload, quota units, 750 GiB/day, 403 `rateLimitExceeded`)

**Interfaces:**
- Consumes: Task 2; `google.golang.org/api` Drive v3.
- Produces a `Destination` for `GOOGLE_DRIVE`: resumable session (16 MiB chunks, multiple of 256 KiB), resume with `PUT bytes */size`, `md5Checksum` + size verify, `Delete` permanent, `Quota` from `about.storageQuota`, `PlaybackURL` returns `ErrProxyRequired` (no direct link). `Caps`: `Resumable: SESSION_URI`, `ChunkAlign: 256 KiB`, `Delete: PERMANENT`, `Quota: ACCOUNT`, `Hashes: [MD5]`, `DailyUploadCap: 750 GiB`, `CreatesPerSec: 2`. **No `files.list` in normal operation**; listing only in `List` for reconciliation.

- [ ] **Step 1: Write the failing tests** (against `fake-gdrive`):
  - `TestDrive_ResumableUploadAndVerify`: upload with a mid-stream kill resumes and commits once; `md5Checksum` matches.
  - `TestDrive_ZeroListInNormalOperation` (Review Focus 3, STO-07): a full upload-verify-playback cycle makes 0 `files.list` calls; ≤ 2 creates/s.
  - `TestDrive_DailyCapPauses`: a 750 GiB/day signal returns `ErrDailyCap`.
  - `TestDrive_QuotaUnitsRecorded`: the driver exposes the quota units a download costs (200) for the report.
- [ ] **Step 2: Run.** `go test ./hub/internal/storage/gdrive/... ./tools/spikes/upload/fakes/gdrive/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add Google Drive storage driver and fake"`

### Task 6: OneDrive personal driver and `fake-graph`

**Files:**
- Create: `ipcam/hub/internal/storage/onedrive/{onedrive.go,graph.go,onedrive_test.go}`
- Create: `ipcam/tools/spikes/upload/fakes/graph/{fake.go,fake_test.go}` (320 KiB rule, `Retry-After`, recycle bin that occupies quota)

**Interfaces:**
- Consumes: Task 2; an own Graph REST client over `net/http`.
- Produces a `Destination` for `ONEDRIVE_PERSONAL`: upload session, 10 MiB fragments (32 × 320 KiB, < 60 MiB), in order, **no** `Authorization` on `uploadUrl`, resume with `GET uploadUrl`, `quickXorHash` + size verify, `Delete` → `RECYCLED`, `Quota` from `quota.remaining` with `quota.deleted` reported separately. `Caps`: `Resumable: UPLOAD_URL`, `ChunkAlign: 320 KiB`, `MaxRequest: < 60 MiB`, `Delete: RECYCLE_BIN`, `Quota: ACCOUNT`, `Hashes: [QUICKXOR]`. Retention never counts a recycle delete as freed space (§9.8 step 7).

- [ ] **Step 1: Write the failing tests** (against `fake-graph`):
  - `TestOneDrive_FragmentsAndNoAuthHeader` (STO-08): fragments are multiples of 320 KiB and < 60 MiB, with no `Authorization` on the `uploadUrl`; `Retry-After` on 429 is respected.
  - `TestOneDrive_QuickXorVerify`: `quickXorHash` + size match on commit.
  - `TestOneDrive_RecycleQuota_NoDeleteLoop` (Review Focus 4): with bytes in the recycle bin at ≥ 98 % quota, the destination goes `QUOTA_FULL` with `PAUSE_KEEP_LOCAL` and the delete loop deletes nothing beyond the catalog accounting.
- [ ] **Step 2: Run.** `go test ./hub/internal/storage/onedrive/... ./tools/spikes/upload/fakes/graph/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** The live run is gated on the free-registration decision (Global Constraints); the driver and fake tests run regardless.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add OneDrive personal storage driver and fake"`

### Task 7: Storage conformance suite (STO-01)

**Files:**
- Create: `ipcam/hub/internal/storage/conformance/{suite.go,suite_test.go}`

**Interfaces:**
- Consumes: Tasks 3, 5, 6.
- Produces `conformance.Run(t, newDest func() storage.Destination)`: kill at a random offset → resume with no duplicate, hash verified, Range read, delete semantics (permanent vs recycle), quota. Runs in CI against `local`, `fake-gdrive` and `fake-graph`; nightly against real test accounts is Faza 0-bis (paid accounts) and is listed, not run here.

- [ ] **Step 1: Write the failing tests.**
  - `TestConformance_Local`, `TestConformance_FakeDrive`, `TestConformance_FakeGraph`: each passes the suite.
  - `TestConformance_KillAtRandomOffset_NoDuplicate`: 20 random kill offsets per driver → one committed object, hash verified.
- [ ] **Step 2: Run.** `go test ./hub/internal/storage/conformance/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add storage driver conformance suite"`

### Task 8: MI5 upload-during-outage spike — G14

**Files:**
- Create: `ipcam/tools/spikes/upload/mi5/{runner.go,wanblock.go,restart.go}` + tests
- Create: `ipcam/tools/spikes/upload/cmd/uploadspike/mi5.go`
- Modify: `.github/workflows/ipcam-spike-upload.yml` (job `mi5-lab`: `toxiproxy` WAN block + mid-run restart against the fakes; uploads `phase0c-upload-mi5` artifact)

**Interfaces:**
- Consumes: Tasks 4–6. Segments come from a lightweight spike segment feeder that drops pre-coded fMP4 files into the local spool at a 5-min cadence (continuous) plus event clips, so MI5 does not hard-depend on the recorder plan and plan 2 runs on its own.
- Produces `uploadspike mi5` driving 4 fed cameras (continuous + events), a 6 h WAN block and one hub restart mid-upload:

```go
package mi5
type WanBlock interface { Block(providerHosts []string) error; Unblock() error; LanReachable() bool; LoopbackReachable() bool } // // SPIKE-ONLY
func HostFirewall() WanBlock   // owner device
func Toxiproxy() WanBlock      // lab/CI
type Mi5Result struct { Dest string; MissingSegments, Duplicates int; AllHashesVerified bool
                        RecoverySecs float64; DriveUnitsPerSegment int; OneDriveFreedAfterDelete bool; OpenSegmentReadOK bool }
func RunMi5(ctx context.Context, cfg Mi5Config) (Mi5Result, error)
```

The block covers only WAN egress to the provider hosts; LAN and loopback stay. Pass (STO-02): 0 missing, 0 duplicates, all hashes verified. The result records recovery time vs uplink, Drive quota units per segment, OneDrive `quota.deleted` freeing after delete, and whether `playback` can read a still-open segment (if not, the triggered decision is 2-min segments).

- [ ] **Step 1: Write the failing tests.**
  - `wanBlock_keepsLanAndLoopback` (Review Focus 2): `Block` leaves `LanReachable()` and `LoopbackReachable()` true; a rule that would sever either aborts the run before the 6 h clock starts.
  - `TestMI5_NoGapsNoDuplicates`: a run with a mid-upload restart commits every segment once, all hashes verified.
  - `TestMI5_RestartResumesNotRestartsUpload`: the restart resumes in-flight uploads from the last offset.
  - `TestMI5_OpenSegmentReadFallback`: when the open-segment read fails, the result flags the 2-min-segment plan B.
- [ ] **Step 2: Run.** `go test ./tools/spikes/upload/mi5/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** The owner-device WAN block uses the host firewall; the lab uses `toxiproxy`.
- [ ] **Step 4: Run** — Expected: PASS; `mi5-lab` CI green.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI5 upload-during-outage spike for G14"`

### Task 9: MI6 OAuth-on-hub spike — G15 (Phase 0 part)

**Files:**
- Create: `ipcam/tools/spikes/upload/mi6/{handoff.go,refresh.go,visibility.go,deviceflow.go}` + tests
- Create: `ipcam/hub/internal/storage/oauth/{pkce.go,oauth.go}` + tests (hub-side: verifier generated and kept on the hub; `BeginOAuth`/`CompleteOAuth`; device flow)
- Create: `ipcam/tools/spikes/upload/app/` glue (TS) wiring the handoff into the reused video spike shells; `*.test.ts`

**Interfaces:**
- Consumes: Tasks 5–6; the reused Android, Windows and web shells (Prerequisite).
- Produces (hub-side, production-aligned, T11/T34/FLW-31):

```go
package oauth
type Platform string // "android" | "windows" | "web" (ios deferred to Faza 0-bis)
type BeginResult struct { AuthURL string; RedirectURI string; State string           // native
                          VerificationURL string; UserCode string }                  // web device flow
func Begin(provider storage.Kind, p Platform) (BeginResult, error)   // verifier generated & kept on hub (RAM, 10 min)
func Complete(state, code string) (sealedRefreshToken []byte, err error) // hub repeats the exact RedirectURI (FLW-31)
func DailyRefresh(sealed []byte) (nextSealed []byte, expiry time.Time, err error)
```
```go
// mi6 (spike)
type Mi6Result struct { Provider, Platform string; HandoffOK bool; RefreshDays []bool /* days 1-6 */; CrossClientVisible bool; TokenInResults bool /* must be false */ }
```

Native platforms deliver `code`+`state` to the hub; the verifier never leaves the hub. Web × Google uses device flow started by the hub. Daily refresh is asserted for days 1–6 only (Testing mode, 7-day tokens); day 7+ is Faza 0-bis. Cross-client visibility: a file created by the hub's client is listed and read by the web, device-flow and native clients of the same project / Entra app (if it fails, F6 uses the hub's client type — written to §2.2).

- [ ] **Step 1: Write the failing tests** (a strict fake OAuth provider):
  - `handoff_verifierNeverLeavesHub` (Review Focus 5): `Begin`/`Complete` redeem the code; a redemption without the hub-held verifier returns `invalid_grant`; the verifier never appears in the app payload or logs.
  - `refresh_day7Expiry_notCountedAsFailure` (Review Focus 1): a token that expires at day 7 is marked `expected-expiry`; days 1–6 refresh pass.
  - `TestMI6_ClientTypePerPlatform` (FLW-31): a "Web application" client with no secret → refused; a mismatched `redirect_uri` → `invalid_grant`; web × Google uses device flow.
  - `TestMI6_CrossClientVisibility`: a file created by the hub client is listed by the device-flow client.
  - `TestMI6_TokenNotInResults`: the `Mi6Result` JSON contains no token bytes.
- [ ] **Step 2: Run.** `go test ./hub/internal/storage/oauth/... ./tools/spikes/upload/mi6/...` and `pnpm -C ipcam --filter spike-upload-glue test` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI6 OAuth-on-hub spike for G15"`

### Task 10: MI8 ONVIF events spike — G17

**Files:**
- Create: `ipcam/hub/internal/cameras/events/{pullpoint.go,health.go}` + tests (ONVIF PullPoint subscribe/renew, 10-min term, 8-min renew, "events healthy" in ≤ 60 s after loss; HUB-11, FLW-16)
- Create: `ipcam/tools/spikes/upload/mi8/{motionref.go,runner.go}` + tests (harness frame-diff motion reference, lab-only; coverage and silent-loss measurement)
- Create: `ipcam/tools/spikes/upload/cmd/uploadspike/mi8.go`

**Interfaces:**
- Consumes: the `cameras` ONVIF events subset (reused from Phase 0 (a) if present, else created here); `kerberos-io/onvif`.
- Produces:

```go
// cameras/events (production): HUB-11, FLW-16
type Subscription interface { Events() <-chan Event; Healthy() bool; Close() error } // PullPoint, renew at 8 min, healthy=false ≤ 60 s after loss
func Subscribe(ctx context.Context, cam CameraRef) (Subscription, error)

// mi8 (spike, lab-only motion reference)
type Episode struct { StartMs, EndMs int64 }
func MotionEpisodes(substreamRTSP string) []Episode   // frame-diff on the substream; NOT a shipped feature // SPIKE-ONLY
type Mi8Result struct { Coverage float64 /* ≥0.95 */; SilentLosses int /* 0 */; DurationHours float64 }
func RunMi8(ctx context.Context, cfg Mi8Config) (Mi8Result, error) // 72 h
```

Pass (G17): ≥ 95 % of harness motion episodes have an ONVIF event within 5 s, and 0 silent subscription losses (> 5 min without a successful `Renew`/`PullMessages`).

- [ ] **Step 1: Write the failing tests** (an ONVIF simulator with `MaxPullPoints=5`, renewals and silent-event modes):
  - `TestEvents_RenewsBefore10Min`: renewal at 8 min; after 10 rounds, 0 subscriptions left behind.
  - `TestEvents_HealthyFalseAfterLoss`: events go silent → `Healthy()` false within 60 s.
  - `TestMI8_Coverage`: a run where 96 % of episodes have an event ≤ 5 s passes; 94 % fails and records the per-brand native-adaptor / FLW-16 plan B.
  - `TestMI8_SilentLoss`: a subscription silent > 5 min counts one silent loss and fails.
- [ ] **Step 2: Run.** `go test ./hub/internal/cameras/events/... ./tools/spikes/upload/mi8/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add MI8 ONVIF events spike for G17"`

### Task 11: Report generator and verdicts (G14, G15, G17)

**Files:**
- Create: `ipcam/tools/spikes/upload/cmd/uploadreport/{main.go,metrics.go,verdict.go,render.go}` + tests and `testdata/` result sets

**Interfaces:**
- Consumes: the `Mi5Result`/`Mi6Result`/`Mi8Result` JSON from Tasks 8–10.
- Produces `uploadreport -in results/ -out docs/research`, which writes `docs/research/<date>-phase0c-upload-results.md` (Romanian owner summary; per destination a `Cod | Valoare | Prag | Trece` table; the G14, G15 (Phase 0 part) and G17 verdicts; the triggered decisions in PH0-30 format; the Faza 0-bis leftovers: OneDrive personal if registration was paid, OneDrive business, Entra P1/MI7/G16, Drive day-7+ in Production, iOS, real paid storage nightly) and `docs/research/phase0-data/phase0c-upload-*.csv`.

```go
type Verdict string // "trece" | "pică → <plan B>"
func VerdictG14(r []Result) Verdict // 0 missing, 0 dup, hashes verified
func VerdictG15(r []Result) Verdict // Phase 0 combos; cross-client visibility
func VerdictG17(r []Result) Verdict
```

- [ ] **Step 1: Write the failing tests.**
  - `TestG14_NoGaps_Trece` and `TestG14_OpenSegmentReadFails_PlanB2MinSegments`.
  - `TestG15_Day7Expiry_NotFailure` (Review Focus 1), `TestG15_VisibilityFails_HubClientType`.
  - `TestG17_Coverage94_PlanB`.
  - `TestRender_NoSecretInOutput` (Review Focus 5): a planted camera password, RTSP URL and refresh token appear 0 times in the output and CSV (PH0-22, ARH-08).
  - `TestRender_GoldenMarkdown`: matches `testdata/golden.md`.
- [ ] **Step 2: Run.** `go test ./tools/spikes/upload/cmd/uploadreport/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "ipcam: add upload spike report generator with G14,G15,G17 verdicts"`

### Task 12: Release assets and Romanian owner guide (accounts + runs)

**Files:**
- Create: `.github/workflows/ipcam-spike-upload-release.yml` (runs on tag `spike-upload-v*`; builds `uploadspike` for `windows/amd64`, `linux/arm64` and `linux/arm` (GOARM=7), `SHA256SUMS`, a Syft SBOM; creates the GitHub Release)
- Create: `ipcam/docs/spike-upload.md` (Romanian owner guide)
- Modify: `ipcam/README.md` (link to the guide)

The guide has two parts. **Account setup** (the owner does this once, step by step with screenshots): (1) create a Google Cloud project on the personal Google account, enable the Drive API, configure the OAuth consent screen in **Testing** mode with `drive.file` only, add the owner and the test account as test users; (2) create the per-platform OAuth clients (T34): an "Android" client (custom URI scheme), a "Desktop" client (loopback), and a "TVs and Limited Input" client for the web device flow — iOS is skipped (Faza 0-bis); (3) **only if it needs no payment details**, create a Microsoft app registration on the personal account with the `http://localhost:<port>/oauth/callback` redirect on the "Mobile and desktop applications" platform; if it asks for a card, stop and tell the agent (OneDrive moves to Faza 0-bis). It states explicitly that the `drive.file` Testing project gives 7-day tokens, which is fine for the spike. **Running** (3): start the hub and `uploadspike`; run MI6 (log in on the phone/PC/web and hand the code to the hub), MI5 (let the harness block the internet for 6 h), MI8 (leave it 72 h near the cameras); zip `results` and send it. It includes a time estimate, a one-line description of each run, and where secrets must never go (PH0-22).

- [ ] **Step 1:** Write the workflow; validate with `actionlint`. Push tag `spike-upload-v0.1.0`; check the assets attach and `SHA256SUMS` verifies.
- [ ] **Step 2:** Write the guide; `grep` the built binary for each Romanian on-screen label the guide quotes. Confirm no step asks for payment details anywhere (zero-cost check).
- [ ] **Step 3: Commit** — `git commit -m "ipcam: add upload spike release and owner account guide"`

### Task 13: Owner run, report and decisions (PH0-30, PH0-31, PH0-32)

**Files:**
- Create: `ipcam/docs/research/<date>-phase0c-upload-results.md` and `ipcam/docs/research/phase0-data/phase0c-upload-*.csv` (from `uploadreport`)
- Modify: `ipcam/docs/superpowers/specs/2026-10-17-ipcam-design.md`: §2.2 for triggered technical decisions (OAuth plan that won G15, cross-client visibility outcome, open-segment read / segment length, ONVIF per-brand adaptor). Each change references its gate.

- [ ] **Step 1:** Send the owner the account-setup guide and the Release link in Romanian, with the time estimate; confirm the Microsoft registration was free before planning the OneDrive run (else record OneDrive personal as a Faza 0-bis leftover).
- [ ] **Step 2:** When the results arrive (chat attachment or Drive `ipcam-teste/`), put them in a scratch directory. Run `uploadreport` and inspect the output for any token, IP, serial, SSID or RTSP URL before committing (PH0-22).
- [ ] **Step 3:** Write the triggered decisions into §2.2, each referencing its gate: G14 outcome → outbox fix / segment length; G15 → the OAuth plan that passed (A, B1 device flow, or B2 proxy) and the cross-client visibility result (if it failed, F6 uses the hub's client type); G17 → per affected brand, native event adaptor or FLW-16.
- [ ] **Step 4:** List the services, accounts and devices used, each with its `OWN` row, to confirm zero cost (PH0-32: OWN-07 Testing, OWN-08 free registration only, OWN-10 test accounts, OWN-14/15 devices, OWN-18 CI). List the Faza 0-bis leftovers with gate, estimated cost and deadline (§16.9): OneDrive personal if registration was paid, OneDrive business, Entra P1 / MI7 / G16, the Drive day-7+ refresh in Production, all iOS combinations, and the real paid-storage nightly runs.
- [ ] **Step 5: Commit and open a PR** for the owner's approval — `git commit -m "ipcam: add Phase 0 (c) upload spike results and decisions"`
