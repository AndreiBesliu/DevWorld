# Technical landscape research (2026-10-17)

> **Note:** this synthesis was written before the client-stack decision. It recommends Flutter for the clients; the owner chose **web-first TypeScript (React + Capacitor + Tauri)** after the judge panel in `2026-10-17-client-stack-judge-panel.md`. Corrections from the adversarial review in `2026-10-17-research-critique.md` take precedence over this file. The design spec is the source of truth.


**Date:** 2026-10-17
**Inputs:** seven specialist briefs covering the live video pipeline, camera discovery and control, the client framework, remote access and topology, storage destinations, recording and NVR lessons, and security, privacy and law.

**How to read this:**
- Where briefs disagree, §3.6 states the resolution and the reason.
- "(not from briefs)" marks something I added as architect. Verify it before relying on it.
- Older or low-confidence evidence is flagged where it appears and listed in the Appendix.
- Costs exclude salaries and are order-of-magnitude, roughly ±50%.

---

## 0. Bottom line

- **Architecture: a hub at the user's site plus a thin EU cloud (Architecture A).**
  - The **hub** is the only component that talks to cameras. It ingests, records, encrypts and uploads.
  - It runs as a Windows service, or as a Linux Docker container on an Intel N100 mini PC, an x86 NAS, or a Raspberry Pi 5 with an SSD.
  - The **cloud** handles accounts, pairing, signaling, presence, push and TURN credentials. It never sees plaintext video or camera passwords.
- **One transport everywhere.** Every client (Android, iOS, Windows, Web) reaches the hub over WebRTC, on the LAN and remotely. ICE finds the LAN path. TURN relays the 10–25% of sessions that cannot connect directly.
- **Phones and browsers only view and control.** They cannot record 24/7. Browsers cannot play RTSP or run discovery. Discovery therefore runs on the hub, and the apps need no iOS multicast entitlement.
- **H.264 substream is the compatibility baseline.** Onboarding sets it on each camera. H.265 goes only to clients that advertise support. Transcoding happens only on hubs with hardware encoders, never on a Pi.
- **Stack:**
  - Hub: a Go supervisor (`hubd`) running go2rtc (MIT) for live view and MediaMTX (MIT) for recording, with kerberos-io/onvif (MIT) for camera control.
  - Storage: our own drivers for local/NAS, S3, Google Drive (`drive.file` scope only) and OneDrive. rclone (MIT) covers the long tail later.
  - Backend: Go and PostgreSQL.
  - TURN: Cloudflare first, then self-hosted coturn.
  - Clients: Flutter, using flutter_webrtc for live video.
- **Same binaries, two editions:**
  - *Personal:* cloud off, LAN plus the user's own VPN.
  - *Commercial:* cloud on.
- **Infrastructure cost:**
  - About €20/month at 100 users.
  - About €200–1,400/month at 10k users. TURN dominates.
  - A full cloud NVR costs one to three orders of magnitude more, because we would pay for every camera-hour.
- **Effort:**
  - About 23–32 person-months to a public v1 on four platforms.
  - About 12–15 person-months for a LAN-only personal edition.
- **First four weeks:**
  - Buy a lab of 8–10 cameras.
  - Run seven gated spikes (§4.3).
  - Start the long-lead items: legal entity, Google app verification, Microsoft publisher verification, Apple organisation account, and a Cyber Resilience Act vulnerability process.

---

## 1. Hard constraints that shape any architecture

### 1.1 Media and codecs

| # | Constraint | Consequence | Source |
|---|---|---|---|
| M1 | Browsers cannot play RTSP. iOS AVPlayer has no RTSP support. Android Media3 RTSP plays only H.264, AAC and AC3 (no H.265, no multicast). | A gateway that repackages RTSP into WebRTC, MSE or HLS is mandatory. Native apps use the same gateway output instead of raw RTSP. | github.com/AlexxIT/go2rtc#codecs-madness ; developer.apple.com/forums/thread/71303 ; developer.android.com/media/media3/exoplayer/rtsp |
| M2 | H.264 is the only codec every client plays. H.265 over WebRTC: Chrome 136+ with hardware decode only, and Safari 18+. Edge does not offer it. Firefox WebRTC is H.264 only. H.265 outside WebRTC is hardware- and profile-dependent (caniuse rates it "partial"). | Onboarding sets an H.264 substream. H.265 goes only to clients whose SDP or reported capabilities include it. Video transcoding is the exception. | go2rtc README ; learn.microsoft.com/en-ca/answers/questions/5880331 ; caniuse.com/hevc |
| M3 | iPhone gets MSE only through ManagedMediaSource, on iOS 17.1+. | Any MSE or hls.js path needs iOS 17.1+. Live view uses WebRTC regardless. | radiantmediaplayer.com (Safari 17.1 ManagedMediaSource) ; go2rtc README |
| M4 | Android performance classes guarantee only 2, 4 or 6 concurrent 720p hardware decoders. Some devices have one. | Phone grids show 4 tiles per page (at most about 9 substreams). Larger grids use JPEG snapshots. | source.android.com/docs/compatibility/17/mpc ; developer.bitmovin.com (single-decoder limitations) |
| M5 | The Raspberry Pi 5 has no hardware H.264 encoder or decoder. Software x264 at 1080p30 uses about 60% of one core. | A Pi only repackages and records. Transcoding needs Intel QuickSync/VAAPI or NVENC (N100 class). | Raspberry Pi white paper RP-010033-WP |
| M6 | Cheap cameras allow 1–4 concurrent RTSP sessions. Reolink limits connections. Tapo runs only 2 of cloud recording, SD card and NVR at once. | Exactly one upstream connection per camera stream profile. Live view, recording and snapshots all restream from it. | home-assistant.io reolink integration docs ; tapo.com/faq/34 |
| M7 | WebRTC does not support H.264 B-frames. "Smart codecs" (H.264+/H.265+) drop keyframes. Cuts made without re-encoding snap to keyframes. | Onboarding enforces a 1–2 s keyframe interval and turns smart codecs off, or warns. | Frigate docs live.md and camera_setup.md (Oct 2026) ; MediaMTX WebRTC docs ; FFmpeg demuxers.texi |

### 1.2 Network

| # | Constraint | Consequence | Source |
|---|---|---|---|
| N1 | We cannot assume inbound connections to a home. In 2016 about 92% of cellular networks and 17–18% of fixed consumer networks used carrier-grade NAT (CGNAT). The data is old and the share is probably higher now. Starlink residential and T-Mobile Home are behind CGNAT. IPv6 carries about 43–50% of traffic, and inbound IPv6 is often filtered. | Outbound-only design: the hub keeps an outbound WebSocket open and uses ICE. No port forwarding, no UPnP. | arxiv.org/pdf/1605.05606 (2016, dated) ; natchecker.com/blog/starlink-nat-type-cgnat ; pulse.internetsociety.org (IPv6 2026) |
| N2 | 10–25% of WebRTC sessions need a TURN relay. Hole punching through symmetric NAT succeeds only about 40% of the time. These estimates come from vendors or small samples. | TURN is a recurring cost line. Log the selected ICE candidate-pair type from day one. Budget for 25% until measured. | openvidu.io/blog/2026/06/09/turn-key-considerations ; arxiv.org/pdf/2510.27500 |
| N3 | Browsers cannot send UDP, so they cannot run WS-Discovery. They cannot speak SOAP or RTSP to cameras (CORS, mixed content). Chrome Local Network Access (default from about Chrome 142, Oct 2025) prompts before a public page reaches private IPs, and drops local ICE candidates without that permission. | The web client never talks to cameras. It talks to the hub over WebRTC. On the LAN that connection may hairpin through the router or relay unless the user grants the permission. | developer.chrome.com/blog/local-network-access ; github.com/WICG/local-network-access (explainer) |
| N4 | Cloudflare forbids serving video over its proxied CDN or Tunnel bandwidth without a paid video product. ngrok bills about $0.10/GB. | No reverse tunnels for video. Cloudflare's TURN product is separate and priced, so it is fine. | developers.cloudflare.com (delivering videos with Cloudflare) |
| N5 | The home uplink is a hard ceiling. One 4 Mbps camera produces about 43 GB/day, so continuous cloud upload needs a sustained 4 Mbps of upload per camera. | Default cloud policy uploads event clips only. Continuous cloud upload is opt-in after a bandwidth check. Remote viewing defaults to the substream. | Derived arithmetic (recording brief) |

### 1.3 Client platforms

| # | Constraint | Consequence | Source |
|---|---|---|---|
| P1 | iOS suspends an app about 5 s after it goes to the background. BGContinuedProcessingTask (iOS 26) is finite and the system can kill it. Local-network operations are silently denied in the background. App Review 2.5.4 forbids misusing background modes. | iOS only views and controls. To keep watching after leaving the app, use Picture-in-Picture (iOS 15+, AVSampleBufferDisplayLayer). | developer.apple.com (extending background execution time ; TN3179 ; BGContinuedProcessingTask) ; App Review Guidelines |
| P2 | Android 15+ caps dataSync and mediaProcessing foreground services at 6 h per 24 h each. No foreground-service type is meant for recording network cameras. Each type needs a Play Console declaration with a demo video. | Android is not a 24/7 recorder in v1. | developer.android.com/develop/background-work/services/fgs/timeout ; …/fgs/service-types |
| P3 | iOS needs the Apple-approved `com.apple.developer.networking.multicast` entitlement for WS-Discovery, and `NSLocalNetworkUsageDescription` consent for any LAN traffic. Android 17 (targetSdk 37) requires `ACCESS_LOCAL_NETWORK` for all LAN traffic. Play has required targetSdk 36 since 31 Aug 2026; 37 will likely follow around Aug 2027 (inferred). | Discovery runs only on the hub, so the multicast entitlement is unnecessary. Apps still declare local-network use and fall back to relayed paths if the user denies it. | developer.apple.com (multicast entitlement) ; developer.android.com/privacy-and-security/local-network-permission ; developer.android.com/google/play/requirements/target-sdk |
| P4 | App Store rules (Guidelines updated 16 Oct 2026): in-app subscriptions must use IAP (3.1.1); reviewers need a working demo (2.1); in-app account deletion (5.1.1(v)); an equivalent privacy-preserving login if Google sign-in is offered (4.8); a recording indicator for the microphone (2.5.14). | Run a permanent demo hub with test cameras for reviewers. The pricing channel is a product decision (§6). | developer.apple.com/app-store/review/guidelines |
| P5 | Windows push needs WNS (firebase_messaging has no Windows support). MSIX-packaged apps need `privateNetworkClientServer` for LAN access. | On Windows, v1 shows notifications only while the app runs. The hub ships as a signed installer, not MSIX. | pub.dev/packages/firebase_messaging ; learn.microsoft.com (WNS quickstart ; app capability declarations) |

### 1.4 Camera ecosystem

| # | Constraint | Consequence | Source |
|---|---|---|---|
| K1 | Major brands ship ONVIF/RTSP off, or require a separate account:<br>• Hikvision: off since firmware 5.5.0.<br>• Reolink: ports off; ONVIF off on some 3.1+ firmware.<br>• Tapo: needs a separate "Camera Account", sometimes also "Third-Party Compatibility".<br>• EZVIZ: LAN Live View toggle, which resets on some models.<br>• Axis: VAPIX and ONVIF off.<br>ONVIF ports differ (80, 8000, 2020, 8899). | A guided, vendor-specific onboarding wizard with a port scan and add-by-IP is a core feature, not polish. | legacy.hikvision.com tech bulletin (2017, still current) ; support.reolink.com 360007010473 ; tapo.com/faq/724 ; help.axis.com |
| K2 | WS-UsernameToken authentication fails when the camera clock is skewed. Newer firmware may default to SHA-256 digest, while FFmpeg up to 9.0 and go2rtc support MD5 digest only. | Read the camera's time first (GetSystemDateAndTime, which needs no auth) and store an offset. Add SHA-256 digest to our go2rtc fork. | FFmpeg release/9.0 httpauth.c ; go2rtc pkg/tcp/auth.go ; agsh/onvif README |
| K3 | PTZ and events behave inconsistently despite conformance claims. Hikvision never updates MoveStatus. Reolink lacks FOV RelativeMove. Cameras cap event subscriptions (e.g. MaxPullPoints=5), and leaked subscriptions silence events until reboot. | Probe capabilities per camera. Use ContinuousMove followed by an explicit Stop. Keep PullPoint subscriptions short and renew early (Home Assistant uses a 10-minute term, renewed every 8 minutes). Show a per-camera "events healthy" indicator. | Frigate autotracking.md ; Home Assistant onvif/event_manager.py |
| K4 | Cloud-only and P2P cameras expose no RTSP or ONVIF: Ring, Blink, most Arlo, new Nest, Xiaomi, Tuya, and battery and LTE cameras. | Out of scope for v1. Official cloud APIs (Nest SDM, EZVIZ/Imou Open) come later. Never use vendor P2P SDKs (TUTK Kalay had CVE-2021-28372). | developers.google.com/nest/device-access (camera live stream) ; community.ring.com (RTSP thread) |
| K5 | ONVIF is retiring Profile S (last conformance tools June 2026) and recommends Profile T. Profile V (cameras dial out to a cloud over WebRTC) is only a release candidate (Jul 2026). Matter 1.5 cameras (Nov 2025) are barely shipping. | Support Profile S/T now, behind a protocol abstraction. Treat Profile V and Matter as connectors for 2027 onward. | onvif.org/?p=8621 ; onvif.org/?p=9145 ; csa-iot.org (Matter 1.5) |

### 1.5 Storage providers

| # | Constraint | Consequence | Source |
|---|---|---|---|
| S1 | Google Drive auth:<br>• The full `drive` scope is restricted: Google review plus a paid CASA security assessment every year.<br>• `drive.file` and `drive.appdata` are non-sensitive and work with the TV/limited-input device flow.<br>• Refresh tokens issued in Testing mode expire after 7 days.<br>• Each Google account can hold 100 refresh tokens per OAuth client.<br>• Unverified apps are capped at about 100 users (medium confidence). | Request `drive.file` only, write into an app-created folder, and publish and verify the app before beta. | developers.google.com/workspace/drive/api/guides/api-specific-auth ; developers.google.com/identity/protocols/oauth2 (and limited-input-device) |
| S2 | Google Drive limits:<br>• 750 GB of uploads per user per day; about 2 file creations per second in practice.<br>• Since May 2026 quotas are counted in quota units (a download costs 200, a list call 100).<br>• Each project has a 400M units/day billing threshold. Billing terms are due "later in 2026". | Keep our own catalog and never list Drive. Upload objects of 5 minutes or more. Download whole segments. Position Drive for small sites and event clips: one Google account takes about 17 continuous 4 Mbps cameras. | developers.google.com/workspace/drive/api/guides/limits ; …/release-notes |
| S3 | OneDrive:<br>• Upload fragments must be multiples of 320 KiB and under 60 MiB.<br>• Personal accounts have no permanent delete, so rotated files sit in the recycle bin and still count against quota.<br>• Refresh tokens issued to browser (SPA) apps last 24 h.<br>• Many business tenants block consent to unverified multitenant apps. | Tokens live on the hub. Retention counts the recycle bin (`quota.deleted`). Complete Microsoft publisher verification before onboarding business users. | learn.microsoft.com/en-us/graph/api/driveitem-createuploadsession ; …/driveitem-delete ; learn.microsoft.com/en-us/entra/identity-platform/refresh-tokens ; …/publisher-verification-overview |
| S4 | Dropbox freezes new user linking two weeks after a development app reaches 50 linked users, unless it has production approval. Writes lock the user's namespace (`too_many_write_operations`). | Dropbox ships after production approval (v1.1), with one writer per account. | docs.dropboxapi.com (developer guide ; performance) |
| S5 | S3-compatible quirks: Cloudflare R2 needs equal-size upload parts. Wasabi bills a 30- or 90-day minimum storage duration. The MinIO community repository was archived on 25 Apr 2026 and is AGPL. | Model these as capability flags in the storage driver. Warn about minimum-duration billing. Do not bundle MinIO. | developers.cloudflare.com/r2/api/error-codes ; docs.wasabi.com ; github.com/minio/minio |

### 1.6 Law and regulation

| # | Constraint | Consequence | Source |
|---|---|---|---|
| L1 | GDPR applies to a vendor that provides the means for household processing (Recital 18). A home camera that covers public space is not household processing (CJEU C-212/13 Ryneš). Breach notification is due within 72 h, and fines reach up to 2–4% of turnover. | Default to zero-knowledge: our servers never see video or camera credentials. Ship per-camera retention, privacy masks, audio off by default, and a signage helper. | eur-lex.europa.eu/eli/reg/2016/679/oj ; curia.europa.eu C-212/13 |
| L2 | Romania:<br>• Law 190/2018 Art. 5: employee video monitoring is capped at 30 days of retention and needs prior information and consultation of the union or employee representatives.<br>• Law 333/2003 + HG 301/2012: businesses obliged to have guarding need licensed installers and a minimum retention of about 20 days (low confidence).<br>• Criminal Code art. 226 punishes recording inside homes without right. | Ship retention presets (home; Romanian business with a 20–30 day window) and warn when a camera covers neighbours or public space. A Romanian lawyer must confirm the values. | legislatie.just.ro DetaliiDocument 203151, 44441, 137796, 109855 (medium/low confidence) |
| L3 | EU Cyber Resilience Act (CRA): Art. 14 reporting (24 h early warning, 72 h notification via ENISA to DNSC, Romania's national cybersecurity directorate) has applied since 11 Sep 2026. Full obligations (CE marking, SBOM, at least 5 years of security updates) apply from 11 Dec 2027. A draft Commission description puts "software intended to centrally control" home security cameras in Class I. | Set up the vulnerability-handling process now. Ship signed updates and an SBOM from v1. Budget for a third-party assessment body in case harmonised standards are late. | eur-lex.europa.eu/eli/reg/2024/2847/oj ; github.com/orcwg/cra-hub (timeline, draft input) |
| L4 | The new Product Liability Directive 2024/2853 (to be transposed by 9 Dec 2026) treats software as a product. Missing security updates and destroyed consumer data count as defects. | Buy product liability insurance. Engineer against data loss (§5). | eur-lex.europa.eu/eli/dir/2024/2853/oj |

---

## 2. Candidate end-to-end architectures

### 2.0 Cost assumptions (applied to all three)

- **Typical account:** 1 site, 4 cameras (main stream 4 Mbps H.264, substream 0.5 Mbps).
- **Usage:** about 10 GB/month of remote viewing, 20% of it relayed. Events total about 1 h per camera per day.
- **Prices:**
  - Cloudflare TURN: $0.05/GB after 1 TB/month free.
  - EU VPS: from €5.49/month with 20 TB of traffic included, about €1/TB beyond.
  - AWS egress: about $0.09/GB.
  - Cloudflare R2 storage: about $0.015/GB-month with zero egress (not from briefs).

### 2.1 Architecture A: LAN hub + thin cloud (RECOMMENDED)

```
 USER SITE (LAN)                                               OUR CLOUD (EU, 2 providers)
+-------------------------------------------------------+     +--------------------------------+
| IP cameras (ONVIF / RTSP / vendor HTTP APIs)          |     | Control plane (Go + Postgres)  |
|   |  exactly one RTSP session per stream profile      |     |  accounts, passkeys/TOTP       |
|   v                                                   |     |  pairing (QR / RFC 8628)       |
| +------------------ HUB (one install) --------------+ | WSS |  hub registry + presence       |
| | hubd (Go): supervisor, RPC API, authz, keys       |-+---->|  signaling relay               |
| |  +- ONVIF + vendor adapters: setup, PTZ, events   | | out-|  TURN credential minting       |
| |  +- go2rtc : ingest, restream, WebRTC, talk       | |bound|  push relay (opaque blobs) ----+--> FCM / APNs
| |  |     | rtsp://127.0.0.1:8554                    | |     +--------------------------------+
| |  +- MediaMTX: fMP4 record, /list, /get            | |     +--------------------------------+
| |  +- catalog (SQLite), retention, encryption       | |     | TURN: Cloudflare -> own coturn |
| |  +- upload workers -------------------------------+-+--+  | relays DTLS/SRTP ciphertext    |
| |  +- Pion data channel: RPC + playback bytes       | |  |  +----------------^---------------+
| +---------------------------------------------------+ |  |                   | ~10-25% of
|   local disk / NAS (SMB mount)                        |  |                   | sessions
+-------------------------------------------------------+  +--> Google Drive / OneDrive / S3
          ^                                                      (HTTPS, hub-held tokens,
          | LAN: ICE host candidates (direct)                     encrypted objects)
          | Remote: WebRTC peer-to-peer (~75-90%) or via TURN
 Clients: Flutter on Android / iOS / Windows; Web app (static, served from CDN)
```

**What runs where**

| Location | Runs / holds |
|---|---|
| Hub (Windows service; or Docker on Linux with host networking, which WS-Discovery and WebRTC need) | `hubd`, go2rtc, MediaMTX, an optional FFmpeg (LGPL build), the SQLite catalog, storage drivers, storage OAuth tokens, sealed camera credentials, the recording keys, and the trusted-device list |
| Our cloud (EU VPS) | Control plane API, WebSocket gateway, Postgres, push relay, TURN credential minting. The web app is static files on a CDN. |
| TURN | Cloudflare Realtime TURN first, then our own coturn in the EU |
| User's storage | Local disk or NAS; encrypted objects in Google Drive, OneDrive or S3 |
| Clients | Flutter apps and the browser. Device keys live in Keychain, Keystore, DPAPI, or non-extractable WebCrypto keys. |

**Data flows**

- **Live view, local.**
  1. The client asks the cloud to open a session.
  2. The hub relays the client's SDP offer to go2rtc and returns go2rtc's answer, signed with the hub identity key. The client checks it against the key pinned at pairing.
  3. ICE picks host candidates, so video flows camera → go2rtc → client entirely inside the LAN.
  - Native apps can also signal through a hub LAN endpoint when the internet is down. They find the hub via a cached IP or Bonjour; Bonjour needs local-network consent but not the multicast entitlement (not from briefs; verify).
  - Web needs the cloud, and may relay locally unless Chrome's local-network permission is granted (N3).
- **Live view, remote.**
  - Same signaling. ICE connects directly in about 75–90% of sessions, otherwise through TURN.
  - Media is DTLS-SRTP encrypted end to end; the relay sees ciphertext.
  - Only the viewer needs TURN credentials. TURN permissions are per IP address, so the hub normally needs only STUN. Give the hub TURN credentials only on UDP-hostile networks (not from briefs; RFC 8656 behaviour).
  - Substream is the default on cellular or relayed paths. Sessions time out when idle.
- **Recording.**
  1. Camera → (one RTSP session) → go2rtc → `rtsp://127.0.0.1` → MediaMTX.
  2. MediaMTX writes fMP4 with 1 s parts (at most about 1 s lost on a power cut) and 5-minute segments.
  3. On `runOnRecordSegmentComplete`, `hubd` indexes the segment in SQLite: camera, stream, start and end on the hub's NTP wall clock, keyframe offsets, codecs, bytes, event overlap, and upload state per destination.
  4. ONVIF events go into an events table. Pre-roll and post-roll come free from the continuous local ring buffer.
- **Upload to storage.**
  1. Each destination has its own persistent outbox.
  2. Event mode cuts a clip with MediaMTX `/get?start&duration&format=mp4` (keyframe-aligned) as soon as post-roll ends. Continuous mode uploads whole segments.
  3. Each object is encrypted in the rclone-crypt format.
  4. Upload is resumable, with chunks aligned per provider: Drive 256 KiB, Graph 320 KiB, S3 parts of 5 MiB or more.
  5. The provider hash is verified, the object is marked uploaded, and the local copy becomes evictable by policy.
  - Day folders: `/{App}/{site}/{camera}/YYYY-MM-DD/`. Bandwidth is capped. An internet outage only delays the queue.
- **Playback.**
  1. The client calls the RPC `timeline(camera, range)` and gets segments, events and thumbnails.
  2. On `play`, the hub serves HLS playlists (fMP4, `EXT-X-MAP` plus byte ranges into MediaMTX's segment files).
  3. Bytes come from local disk. If the local copy was evicted, the hub fetches it from the cloud and decrypts it.
  4. Bytes travel over the hub data channel. On web they feed hls.js through a custom loader. On native they go through a small HTTP proxy on 127.0.0.1 that standard players read.
  - Export: MP4 via `/get?format=mp4`.
- **Hub offline or stolen.**
  - No live view or playback.
  - Event clips already uploaded survive in the user's cloud.
  - v2: clients read and decrypt the cloud copies directly, using an index stored in the cloud.

**Pros**
- Works with any ONVIF or RTSP camera. Video and camera passwords stay on site, so a breach of our cloud leaks no footage. This avoids the failures seen at Ring, Verkada and Wyze.
- Cheapest to run: we pay for signaling plus the relayed share only.
- Survives internet outages: recording is local and uploads queue.
- One binary serves both personal and commercial editions. The pattern is proven by Home Assistant with Nabu Casa TURN, and by the go2rtc/Frigate ecosystem.
- Fits every platform limit in §1.3. Small GDPR footprint. No restricted Google scopes.

**Cons**
- Needs an always-on device on site, and installation and support across Windows, Docker, NAS and Pi.
- Remote viewing is bounded by the site's upload bandwidth.
- If the hub is offline, remote access stops. If it is stolen, footage not yet uploaded is lost. Mitigation: upload event clips quickly.
- End-to-end key recovery UX is hard. AI and search must run on the hub.
- The hub is probably a CRA Class I product.

**Monthly infrastructure cost**

| Users | Backend | TURN (20% relayed, about 2 GB per user-month) | Total |
|---|---|---|---|
| 100 | €10–20 (one VPS) | within the 1 TB free tier | **≈ €20** |
| 1,000 | €30–80 | about 2 TB, ≈ $50 | **≈ €80–130** |
| 10,000 | €150–400 | about 20 TB: ≈ $950 on Cloudflare, or ≈ €50–150 self-hosted | **≈ €200–1,400** (€0.02–0.14 per user) |

Fixed costs on top:
- Apple Developer: $99/year.
- Google Play: $25 once.
- Windows code signing: a few hundred € per year (not from briefs).
- Microsoft publisher verification and Google brand verification: free. CASA is avoided by using `drive.file`.

**Build effort:** about 23–32 person-months to a public v1 (3–4 engineers over roughly 8–11 months). The LAN-only personal edition is about 12–15 person-months.

**Fit:**
- *Personal:* yes. Run the hub on your own PC with the cloud off, and reach it remotely over your own VPN.
- *Commercial:* yes. This is the core. Cloud features can be added later as paid tiers.

### 2.2 Architecture B: Full cloud NVR

```
 USER SITE                                OUR CLOUD (EU)
+--------------------------------+      +------------------------------------------------------+
| cameras --RTSP--> forwarder    | SRT/ |  ingest + recorder fleet (MediaMTX, 1 path/camera)   |
| (hub-lite: ONVIF, restream,    | WHIP/|      |                                 |             |
|  pushes EVERY camera 24/7)     | RTSP |      v                                 v             |
|                                |----->|  object storage (R2/S3)          SFU (LiveKit) /     |
+--------------------------------+      |  + timeline DB                   WebRTC edge         |
| Hikvision ISUP / Dahua Auto    |----->|      |   copy (export, egress again)  |             |
| Register / Matter 1.5 cameras  |dial- |      +--> user's Drive/OneDrive       |             |
+--------------------------------+ out  |  control plane, token broker, AI workers, billing    |
                                        +------------------------------------------------------+
                                               ^ presigned playback               ^ live: 100% relayed
                                               +------------- clients ------------+
```

**What runs where:** recording, storage, AI, live fan-out and storage tokens all live in our cloud. The site runs a forwarder, which is unavoidable for generic ONVIF/RTSP cameras because they cannot push streams.

**Data flows**
- **Live, local:** goes through the cloud, unless an A-style local path is added.
- **Live, remote:** camera → forwarder → our ingest → SFU → client. We pay for 100% of the egress.
- **Recording:** a cloud recorder per camera.
- **Upload:** from our bucket to the user's Drive or OneDrive using tokens held on our servers. This needs a backend token broker and serialised refreshes (Box tokens are single-use).
- **Playback:** presigned range URLs from R2 or S3. This works even when the site is offline.

**Pros**
- No site device needed for cameras that can push (ISUP, Dahua Auto Register, Matter).
- Footage survives theft or loss of the site equipment.
- Server-side AI, search and sharing. Easy multi-viewer.
- Recurring revenue is straightforward.

**Cons**
- Most cameras still need the forwarder, so B is effectively A plus a cloud media plane.
- The user's uplink must carry every camera 24/7, at about 2 Mbps each.
- We become a large-scale video processor: a data processing agreement (DPA) with every business customer, a likely mandatory DPO, DPIA support, and data-subject access requests over footage.
- We carry the insider-access and tenant-isolation risk behind the Ring, Verkada and Wyze incidents.
- Holding storage tokens server-side increases liability. Business customers will expect ISO 27001 or SOC 2.

**Monthly infrastructure cost** (2 Mbps cloud recording, derived estimates)

| Cost item | Per camera-month |
|---|---|
| Ingest and recorder (about 300–400 cameras per 1 Gbps node) | ≈ $0.15–0.5 |
| Storage, event-only (about 27 GB) | ≈ $0.4 |
| Storage, continuous 30 days (about 650 GB) | ≈ $9.7 |
| Live egress through the SFU | ≈ $0.13 |
| **Total, event-only** | **≈ $0.7–1.5** |
| **Total, continuous** | **≈ $10–11** |

| Users (4 cameras each) | Event-only | Continuous, 30-day retention |
|---|---|---|
| 100 | ≈ €0.5–1k, including fixed SFU and HA minimum | ≈ $4–5k |
| 10,000 (40k cameras) | ≈ $28–60k | ≈ $400k |

**Build effort:** about 40–55 person-months plus 24/7 on-call operations. That includes A's forwarder anyway.

**Fit:**
- *Personal:* no.
- *Commercial:* only as a premium tier added on top of A, such as a cloud SFU for more than 2 viewers or managed retention storing hub-encrypted objects so we stay zero-knowledge.

### 2.3 Architecture C: Client-only / local-first (no hub, no cloud)

```
 USER SITE (LAN only)
+-------------------------------------------------------------------------+
|  IP cameras <------ RTSP / ONVIF (camera passwords on EVERY device) ----+
|      ^                      ^                          ^                 |
|  Windows app            Android / iOS app          Web browser           |
|  live + records ONLY    live via media_kit/fvp,    cannot: no RTSP,      |
|  while running -> disk  PTZ; no recording when     no UDP multicast,     |
|  (+ upload to Drive)    backgrounded               LNA prompts           |
+-------------------------------------------------------------------------+
 Remote access: none, or bring-your-own VPN (Tailscale/WireGuard). Port forwarding: rejected.
```

**Data flows**
- **Live, local:** app → camera RTSP directly.
- **Live, remote:** only over the user's own VPN.
- **Recording:** only in the Windows app, and only while it runs.
- **Upload:** from the Windows app.
- **Playback:** only on the Windows PC.

**Pros**
- Zero infrastructure.
- Lowest LAN latency.
- Simplest legal position.

**Cons**
- Fails three stated requirements: web viewing (M1, N3), remote access (N1), and recording when the app is not open (P1, P2).
- Camera passwords sit on every device.
- Every client needs the iOS multicast entitlement and the Android 17 local-network permission.
- easy_onvif, the Flutter ONVIF library, has no events support.
- A Windows app that records 24/7 and serves other devices is a hub in disguise, which is Architecture A.

**Monthly cost:** about €0–10.

**Build effort:** about 10–14 person-months, and the requirement gaps remain.

**Fit:**
- *Personal:* partial, as a LAN viewer.
- *Commercial:* no.

### 2.4 Comparison and verdict

| | A: LAN hub + thin cloud | B: Full cloud NVR | C: Client-only |
|---|---|---|---|
| Meets all stated requirements | Yes | Yes, but still needs a forwarder on site | No (web, remote, background recording) |
| Infrastructure at 100 / 10k users | ≈ €20 / ≈ €0.2–1.4k | ≈ €0.5–5k / ≈ €28–400k | ≈ €0–10 |
| Who stores video | User (disk, NAS, own cloud) | Us | User |
| GDPR role | Small processor (accounts, relay metadata) | Large-scale video processor; DPO likely | None |
| What the user must provide | Always-on device | About 2 Mbps of upload per camera, 24/7 | Nothing |
| Build effort to v1 | 23–32 person-months | 40–55 person-months + operations | 10–14 person-months |
| Personal fit / commercial fit | Yes / Yes | No / premium tier only | Partial / No |

**Verdict:** build A. C is A's personal edition: the hub runs on the user's PC with the cloud off. B's features become optional paid tiers on top of A, and those tiers store only hub-encrypted objects.

---

## 3. Recommended technology per component

### 3.1 Component choices

| Component | Choice (pinned Oct 2026) | License | Why | Notes and fallback |
|---|---|---|---|---|
| Hub runtime | Go; one static binary `hubd` per OS/arch that supervises the sidecars | BSD-3 | Same language and libraries as go2rtc, MediaMTX, Pion and gortsplib. Cross-compiles to Windows amd64 and Linux amd64/arm64. | Docker images target Linux hosts. Docker Desktop on Windows lacks the host networking that discovery and WebRTC need, so Windows runs `hubd` as a native service. |
| Camera ingest and live gateway | **go2rtc v1.9.14**, as a vendored fork | MIT | One camera connection serves every viewer. It picks WebRTC, MSE, HLS or MJPEG per client, has the widest two-way audio and vendor support, and is deployed in Home Assistant and Frigate. | Bind the API and RTSP to 127.0.0.1; only the WebRTC media port (8555) is reachable on the LAN. Remove the `exec`, `echo` and `expr` sources and API config writes. Add SHA-256 digest. Use go2rtc's own WebRTC format, not WHEP, so two-way audio keeps working. If the fork stalls, fall back to MediaMTX plus gortsplib. |
| Recorder and playback source | **MediaMTX v1.21.2** | MIT | fMP4 recording with 1 s parts, segment hooks, `/list` and `/get?format=mp4` (clip cutting). New releases every 2–4 weeks. | Pulls from go2rtc's local restream. Playback and API bound to localhost. Wrapped behind a `Recorder` interface so it can be replaced by our own gortsplib recorder if we need RAM pre-roll (to avoid continuous disk writes) or dual-stream recording. |
| Low-level media | gortsplib v5.6.7, mediacommon, **Pion WebRTC v4.2.24**, pion/turn | MIT | The hub's own data-channel session, HLS playlist generation, and a later merge of all streams into a single WebRTC connection | — |
| Transcoding, snapshots, audio | **FFmpeg 9.0.x, LGPL build**, run as a separate process | LGPL-2.1+ | Hardware encoders (QSV, VAAPI, NVENC), keyframe-only thumbnails (`skip_frame nokey`), AAC → Opus | Never build with `--enable-gpl` (no libx264 or libx265). For software H.264 encoding use OpenH264 (BSD-2; not from briefs, verify). |
| ONVIF | **kerberos-io/onvif v1.2.3** | MIT | Active (Oct 2026). Device, Media, PTZ, Event and Discovery services. Falls back from UsernameToken to Digest auth. | 0x524a/onvif-go v1.2.0 (MIT) is richer but young; its virtual camera server is useful for tests. |
| Vendor adapters | Own Go code: Hikvision ISAPI, Dahua CGI and RPC2, Reolink `api.cgi` and Baichuan; Tapo, Xiaomi, Wyze and Tuya through go2rtc | Own | PTZ, events and settings where ONVIF is weak | Use reolink_aio, pyHik and pytapo (all MIT) as protocol references. |
| Hub database | SQLite via modernc.org/sqlite (pure Go) | Public domain / BSD-3 | Catalog, events, upload outbox. No CGO, so cross-compiling stays easy (not from briefs). | — |
| Storage | **Own `Destination` interface** (§3.2). Native drivers: local/NAS path, S3 (aws-sdk-go-v2), Google Drive (google-api-go-client, `drive.file`), OneDrive (Graph REST). Dropbox native in v1.1. **rclone v1.75.2** in-process for WebDAV, SFTP, FTP, pCloud and Box later. | Apache-2.0 / BSD-3 / MIT | Provider differences leak into product behaviour (chunk alignment, links, deletes, token lifetimes), so they must be modelled explicitly | — |
| Recording encryption | **rclone-crypt file format**: XSalsa20-Poly1305, 64 KiB blocks, seekable. A random per-site key is wrapped to each trusted device's X25519 key. The recovery kit is the rclone passphrase. | MIT (format and code) | Users can always recover footage with stock rclone. Seeking still works for playback. | Default on for cloud destinations. Toggle per destination (§6). |
| Remote transport and API | WebRTC on the LAN and remotely. Protocol Buffers RPC and byte streams over a data channel. | BSD-3 (protobuf) | The only transport that works through NAT on all four targets, with end-to-end DTLS | Last resort in v1.1: MSE/fMP4 through a metered cloud WebSocket relay. |
| TURN | **Cloudflare Realtime TURN**, then **coturn 4.14+** or pion/turn on 2–3 EU VPS from two providers | Service / BSD-3 / MIT | Cloudflare: $0.05/GB with 1 TB free, anycast, TLS on 443. Self-hosted costs about €1/TB beyond included traffic. | Switch to self-hosted at 2–5 TB/month relayed. Credentials short-lived. Deny private and ULA peer addresses. Don't use Twilio ($0.40/GB). AWS KVS caps connections at 1 h and viewers at 10 per channel. |
| Cloud backend | **Go + PostgreSQL + WebSocket gateway**; FCM HTTP v1 and APNs; EU VPS; Docker Compose, which doubles as the self-host edition | Own / PostgreSQL License | No per-message billing for chatty presence and signaling. About €10–20/month at 100 users, €150–400 at 10k. | Supabase Pro for auth and DB only, if speed outweighs control. Never use Supabase Realtime or Firebase RTDB for hub heartbeats: 10k hubs × 1 message/30 s ≈ $2.1k/month on Supabase, and the Firebase Spark plan caps at 100 connections. |
| Identity and pairing | Ed25519 hub identity key created at install. QR pairing on the LAN, or an RFC 8628 device code. Passkeys or TOTP for accounts. The hub keeps its own trusted-device list. | Own | Clients pin the hub key, so our cloud cannot intercept sessions. New devices are admitted only via QR on the LAN or approval from an already-trusted device, so a compromised cloud cannot add viewers. | — |
| Client framework | **Flutter 3.47.x** for Android, iOS, Windows and Web | BSD-3 | The only stack with maintained video paths on all four targets | If the Flutter web grid fails its spike, ship a thin TypeScript web viewer (browser WebRTC plus hls.js) on the same hub API. |
| Live player | **flutter_webrtc 1.6.2** (libwebrtc m150 on native; browser RTCPeerConnection on web) | MIT | One live path everywhere | Verify web support for the pinned version. H.265 over native libwebrtc is undocumented; test it. |
| Recorded playback | Android/iOS: video_player 2.14 (ExoPlayer/AVPlayer, OS decoders). Windows: **fvp 0.39** (libmdk, hardware decoders). Web: **hls.js**. | BSD-3 / BSD-3 plus libmdk key (free for Flutter, commercial use allowed) / Apache-2.0 | Using OS and browser decoders avoids HEVC patent exposure and GPL libmpv builds. | On Windows, fall back to media_kit (MIT) with a non-GPL libmpv build. |
| Push | FCM (Android, web PWA) and APNs (iOS). Payloads are encrypted by the hub per device; the backend relays opaque blobs. WNS in v1.1. | Services | Free. Avoids leaking metadata to Apple and Google. | iOS uses a Notification Service Extension to decrypt. |
| AI (v2) | ONNX Runtime or OpenVINO with YOLOX, RF-DETR or D-FINE (Apache-2.0) or the MultimediaTechLab YOLO (MIT) | MIT / Apache-2.0 | N100-class hubs have Intel iGPU or NPU (about 6–30 ms per inference) | Frigate no longer recommends Coral for new installs. |
| Supply chain | SPDX license allowlist in CI; SBOM per release; signed auto-updates with rollback | — | CRA readiness and protection against copyleft contamination | — |

### 3.2 Storage driver contract (sketch)

```go
type Destination interface {
    // Caps reports: resumable upload, part alignment/min/max, size needed upfront, max object size,
    // server lifecycle rules, link type (presigned|preauth|temporary|proxy-only), range reads,
    // permanent delete vs recycle bin, quota query, app-scoped folder, hash types.
    Capabilities() Caps
    BeginUpload(ctx context.Context, key string, size int64, meta Meta) (UploadState, error) // serialisable
    PutChunk(ctx context.Context, st *UploadState, off int64, r io.Reader, n int64) error
    Resume(ctx context.Context, st *UploadState) (nextOffset int64, err error)
    Commit(ctx context.Context, st *UploadState) (ObjectRef, error) // provider id, size, provider hash
    OpenRange(ctx context.Context, ref ObjectRef, off, n int64) (io.ReadCloser, error)
    PlaybackURL(ctx context.Context, ref ObjectRef, ttl time.Duration) (string, error) // or ErrProxyRequired
    Delete(ctx context.Context, ref ObjectRef, permanent bool) error
    Quota(ctx context.Context) (Quota, error)
    List(ctx context.Context, prefix string) ([]ObjectRef, error) // reconciliation only, never the timeline
}
```

`UploadState` holds the Drive session URI, the Graph `uploadUrl`, the Dropbox `session_id` and offset, or the S3 `uploadId` and parts. It is persisted in SQLite so uploads survive restarts.

**Per-provider rules**
- **Google Drive:** create at most about 2 files/s; pause the destination for 24 h when the 750 GB/day cap is hit.
- **OneDrive:** honour `Retry-After`.
- **Dropbox:** one writer per account, plus `finish_batch`.
- **S3, R2, B2:** also configure bucket lifecycle rules so retention runs even when the hub is offline.

**OAuth on the hub**
- Google: device flow with `drive.file`.
- Microsoft: device-code flow with `Files.ReadWrite.AppFolder` for personal accounts and `Files.ReadWrite` for work accounts (verify the device-code flow with AppFolder in Phase 0).
- Dropbox: PKCE started on the phone, with the authorization code passed to the hub over the encrypted channel.
- Use one Google Cloud project for every first-party client. `drive.file` only sees files created by the same project.

### 3.3 Hub hardware tiers (validate in Phase 0; capacities are community estimates)

| Hub host | Cameras (repackage + record, H.264) | Transcoding | Notes |
|---|---|---|---|
| Existing Windows 10/11 PC | About 8–16 | Only with an iGPU (QSV) or NVENC | Sleep must be disabled. Laptops are unreliable hubs. |
| Intel N100/N150 mini PC (≈ €150–250), Windows or Docker | About 16–32 | About 4–8 × 1080p (unverified) | Recommended appliance. |
| Raspberry Pi 5 + SSD (Docker) | About 8–10 | None | Recording to SD card is not supported. |
| x86 NAS with Docker | About 8–16 | Depends on model | Needs host networking. |

### 3.4 Delivery and codec policy

| Situation | Transport | Stream | Codec rule |
|---|---|---|---|
| Grid tiles (desktop up to 16; phone 4 per page, about 9 max) | WebRTC from go2rtc, one connection per visible tile | Substream: about 640×360, 10–15 fps, about 512 kbps, 1 s keyframe interval | H.264 |
| Fullscreen, PTZ, two-way talk | WebRTC | Main stream | H.264. H.265 only if the client's SDP offers it (Chrome 136+ with hardware, Safari 18+). Never Firefox or Edge. |
| Large phone grids, thumbnails, poor links | JPEG snapshots at 1–2 fps over the hub data channel | Substream keyframes | — |
| Recorded playback | Hub-generated HLS (fMP4 + byte ranges) over the data channel | Recorded main stream (substream recording in v1.1) | H.265 recordings are flagged as not playable on Firefox or Edge unless the hub can transcode in hardware. |
| UDP blocked | TURN over TLS on port 443 | As above | — |
| Everything else failed (v1.1) | MSE/fMP4 through a metered cloud WebSocket relay | Substream | — |
| Audio | G.711 passed through. AAC is transcoded to Opus (FFmpeg) only for WebRTC. | — | — |

### 3.5 GPL, AGPL and other licensing traps

| Trap | Where it would bite | Do instead |
|---|---|---|
| FFmpeg built with `--enable-gpl` (libx264, libx265) turns the whole build GPL | Hub transcoding, app players | LGPL build in a separate process; hardware encoders or OpenH264 |
| media_kit's `encoders-gpl` libmpv build (GPLv3). Bundled software HEVC decoders (libmpv, VLC, FFmpeg) may owe Access Advance royalties. | Flutter native playback | OS decoders via video_player; hardware decoding in fvp |
| Janus (GPL-3.0); GStreamer "ugly" and GPL plugins | Gateway | go2rtc and MediaMTX (MIT) |
| vlcj (GPLv3); libVLC (LGPL) statically linked on iOS is a legal grey area | Desktop and iOS players | Not needed with the stack above |
| nginx-vod-module (AGPL-3.0) inside Frigate's MIT image; Frigate+ models need a commercial license for hosted services; the Frigate trademark is not licensed | Copying Frigate's playback stack | Hub-generated HLS; our own models |
| ZoneMinder (GPLv2); motionEye, moonfire-nvr, LightNVR (GPLv3); Shinobi (paid commercial license) | Reusing an NVR engine | Use as design references only |
| Ultralytics YOLOv8/YOLO11 (AGPL-3.0); original YOLOv9 (GPL-3.0); YOLO-NAS weights (non-commercial) | AI | YOLOX, RF-DETR, D-FINE (Apache-2.0); MultimediaTechLab YOLO (MIT) |
| python-amcrest (GPLv2); gSOAP-based ONVIF stacks (GPLv2 or commercial) | Camera control | MIT Go ONVIF libraries |
| NetBird servers (AGPLv3 since v0.53.0); ZeroTier SDK (BSL 1.1); EMQX 5.9+ clusters (BSL 1.1) | Remote access, messaging | WebRTC with coturn |
| MinIO (AGPLv3, archived, no binaries) | Self-hosted object storage | Any S3-compatible server; don't bundle one |
| libmdk (behind fvp) is proprietary and key-licensed, from a single vendor | Windows playback | Keep media_kit as the fallback |
| Vendor P2P SDKs (TUTK Kalay), Hikvision ISUP SDK terms | Cloud-only cameras | Avoid; if ever needed, use official cloud APIs |

### 3.6 Contradictions between briefs and their resolution

| Topic | Disagreement | Resolution and reason |
|---|---|---|
| Grid transport | The live-video brief recommends MSE for grids, following Frigate. The remote-access brief says WebRTC everywhere. | **WebRTC per tile**, as Home Assistant does with go2rtc. Frigate's MSE choice assumes LAN or reverse-proxy viewing. For us remote is the default, and remote MSE means either paying for a cloud byte relay or tunnelling it. MSE stays for playback and as the last-resort relay. |
| Live engine | One brief puts MediaMTX in charge of live and recording; others pair go2rtc with MediaMTX. | **go2rtc for live, MediaMTX for recording only**, pulling go2rtc's restream. go2rtc covers two-way audio (MediaMTX cannot proxy it; issue open since 2022), vendor protocols and per-client format choice. |
| Segment length | MediaMTX defaults to 1 h segments; the storage brief says 1–2 min local and 5–10 min cloud; the recording brief says 1–2 min plus 5-minute bundles. | **5-minute segments with 1 s parts.** Crash loss depends on the part size, not the segment size. Event clips are cut with `/get`, so nothing needs re-bundling. Check `/get` on segments still being written in Phase 0; if it fails, drop to 2 minutes. |
| Clients talking to cameras directly | The client brief proposes native RTSP on the LAN (media_kit or fvp). The camera and security briefs say only the hub talks to cameras. | **Hub only in v1.** Camera credentials never leave the hub. This avoids the iOS multicast entitlement and Android 17 permission on clients. |
| Native player | The client brief offers media_kit or fvp. The live-video brief warns about bundled software HEVC decoders. | **video_player (OS decoders) plus fvp on Windows.** media_kit is the fallback only. |
| Storage tokens in SaaS mode | The storage brief puts a token broker in the backend. The security brief keeps tokens on the hub. | **Tokens stay on the hub** via device flows. The backend never holds storage tokens unless a future managed-cloud tier needs it. |
| Encryption design | The storage brief proposes the rclone-crypt format with a passphrase. The security brief proposes per-segment keys wrapped to device keys. | **rclone-crypt format with a random site key wrapped to each device's key.** Recovery is the passphrase, so users stay free of lock-in and can still distribute keys to multiple devices. |
| OneDrive scope | `Files.ReadWrite` or AppFolder? | **AppFolder for personal accounts; `Files.ReadWrite` for work and school accounts**, with publisher verification. AppFolder is labelled "preview", so track it. |
| Backend | Go + Postgres or Supabase? | **Go + Postgres.** Self-hostable, same language as the hub, no per-message billing. Supabase is acceptable for auth and DB only. |
| ONVIF library | use-go/onvif is listed as MIT-acceptable in one brief but has been unmaintained since its 2022 tag. | **kerberos-io/onvif.** |
| MediaMTX version and license | A third-party listing showed 1.19.2 with "verify license". Two briefs verified v1.21.2 and MIT. | **v1.21.2, MIT.** |
| Chrome WebRTC H.265 | MediaMTX docs say Windows only with a capable GPU. go2rtc says Chrome 136+, including Android. | It is hardware-gated and varies by platform. **Never assume it; negotiate per client.** The H.264 baseline makes this moot. |
| Firefox HEVC | go2rtc's table says no H.265 via MSE. caniuse says partial support from Firefox 137 with hardware decoding. | **Plan for Firefox as H.264-only.** |
| Chrome Local Network Access version | Briefs cite 141, 142 and 145. | **Irrelevant.** The design assumes it is enforced. |
| flutter_webrtc on web | pub.dev analysis of v1.2.0 shows no web support; the 1.6.2 README lists web. | **Verify in the spike.** Fallback is browser RTCPeerConnection through JS interop. |
| Android as a hub | The remote-access brief includes an Android hub mode. Other briefs cite foreground-service limits. | **Not in v1.** Later experiment with the `connectedDevice` service type and the Play declaration. |
| Apple multicast entitlement | Three briefs say request it early. | **Not needed**, because discovery stays on the hub. Request it only if in-app discovery is wanted later. |
| Managed cloud copy (storage brief option E) vs zero-knowledge | — | **Not in v1.** If added later as a paid tier, store hub-encrypted objects only. |

---

## 4. Sub-projects, dependency order and MVP cut

### 4.1 Independently buildable sub-projects

| ID | Sub-project | Scope | Depends on | Testable in isolation with | Effort (person-months) |
|---|---|---|---|---|---|
| SP0 | Lab and spikes | 8–10 cameras (Hikvision, Dahua/Amcrest, Reolink wired, Reolink battery + Home Hub, Tapo C2xx, VIGI, EZVIZ, Imou, Xiongmai/iCSee, Axis or Uniview), an N100, a Pi 5 + SSD; 7 gated spikes (§4.3) | — | — | 1–1.5 (+ about €2–3k of hardware) |
| SP1 | Camera connector (Go library) | WS-Discovery plus unicast port scan (80/443/554/8000/2020/8899/34567/37777/9000); ONVIF wrapper with clock offset, auth fallback and SHA-256 digest; capability probe; PTZ; imaging; PullPoint lifecycle; vendor adapters; vendor fingerprints and onboarding steps; H.264 substream configuration via Media2 | SP0 lab | onvif-go virtual camera, recorded SOAP fixtures, nightly runs against the lab | 3–4 |
| SP2 | Media core | Supervising the go2rtc fork and MediaMTX; config generation; one-connection rule; codec and keyframe checks; snapshots; hardware transcoding policy; LGPL FFmpeg build | SP1 (stream URIs) | Lab cameras, synthetic RTSP sources | 2–3 |
| SP3 | Recording catalog and policy | Segment hook → SQLite; events → keep; pre-roll and post-roll; retention; emergency oldest-first cleanup; reconciliation after power loss (abort if more than 50% would be deleted); HLS byte-range playlists; MP4 export; thumbnails | SP2 | Synthetic fMP4 files, a power-cut rig | 2–3 |
| SP4 | Storage engine | `Destination` interface; local/NAS, S3, Drive and OneDrive drivers; device-flow OAuth; resumable outbox; hash verification; quota; retention per destination; rclone-crypt | — (uses fake segments) | Provider test accounts, a local S3-compatible server | 3–4 |
| SP5 | Hub session and API | Protobuf API; Pion data-channel server; go2rtc signaling bridge with hub-signed SDP; trusted-device list and grants; LAN offline endpoint; local playback proxy protocol | Interfaces from SP1–SP4 | CLI client, mock hub | 2–3 |
| SP6 | Cloud control plane | Accounts (passkeys, TOTP); pairing; hub WebSocket gateway and presence; signaling relay; TURN credentials; sharing ACLs; push relay; audit log; Docker Compose | SP5 protocol | Load test with 10k simulated hubs | 2.5–3.5 |
| SP7 | Clients (Flutter) | Live grid, fullscreen and PTZ; snapshots; timeline and export; onboarding wizard UI; storage setup; sharing; notifications; four platforms | SP5 API (mocked first) | Mock hub | 5–7 |
| SP8 | Packaging and updates | Windows service, installer, firewall rules and tray app; Docker multi-arch with host networking; signed auto-update with rollback; SBOM; license gate | — | CI | 1.5–2 |
| SP9 | Compliance and store readiness | Legal entity; privacy policy and DPA template; Google verification; Microsoft publisher verification; Apple and Play submissions with the demo hub; CRA vulnerability process; Romanian counsel on retention | — | — | 1–1.5 + external counsel |
| | **Total** | | | | **≈ 23–32** |

### 4.2 Dependency order

```
SP0 lab + spikes (weeks 0-4; gates below)
 |-> SP1 camera connector --> SP2 media core --> SP3 recording catalog --+
 |-> SP4 storage engine (fake segments) ---------------------------------+--> SP5 hub session/API --> SP6 cloud control plane
 |                                                                                ^
 |   SP5 API schema frozen by week 4 ----> SP7 clients (against mock hub) -------+
 |-> SP8 packaging/updates  (continuous from week 2)
 '-> SP9 compliance/store   (continuous; long-lead items in week 1)
```

**Long-lead items for week 1:**
- Romanian legal entity (SRL). Apple's organisation account, Microsoft Partner Center and DPAs all need it.
- Apple organisation account (needs a D-U-N-S number).
- Google Cloud project: publish the app out of Testing mode and start brand verification.
- Microsoft AI Cloud Partner Program and publisher verification.
- `security.txt` and a vulnerability disclosure policy (CRA Art. 14 already applies).
- Counsel review of Romanian retention law (L2).

### 4.3 Phase-0 spikes and gates

| # | Spike | Pass criterion | If it fails |
|---|---|---|---|
| G1 | Flutter web grid of 9 and 16 WebRTC substream tiles on Chrome, Safari and Firefox | Stable for 30 min on a mid-range laptop, with no player restarts when the layout changes | Keep Flutter for Android, iOS and Windows; build the web viewer in TypeScript |
| G2 | go2rtc + MediaMTX with 8 cameras on Windows 11, Pi 5 + SSD, and N100 | Acceptable CPU and RAM; 20 hard power cuts each lose ≤ 2 s and the catalog reconciles | Adjust the hardware tiers; consider our own gortsplib recorder |
| G3 | ONVIF events per lab camera | 72 h without silent subscription loss after renewal handling | Pull vendor-native events or hub-side motion detection into v1 |
| G4 | Remote WebRTC over Romanian carriers (Digi, Orange, Vodafone, Telekom) and CGNAT lines | Record candidate-pair types; relay share known | Re-plan the TURN budget |
| G5 | Drive and OneDrive upload during a simulated 6 h outage, then resume | No gaps, no duplicates, hashes verified | Fix the outbox design |
| G6 | An H.265 camera through every client path | Known support matrix | Tighten onboarding enforcement |
| G7 | flutter_webrtc 4-tile grid on a mid-range Android phone and an iPhone; HLS through the local proxy | Smooth playback; scrubbing works | Lower tile caps; use snapshot grids |

### 4.4 MVP cut

| Area | v1 (public MVP) | v1.1 | v2+ / watch |
|---|---|---|---|
| Hub hosts | Windows 10/11 service; Docker amd64/arm64 (N100, x86 NAS, Pi 5 + SSD) | Pi OS image; Synology/QNAP packages | Android hub mode (if Play accepts it); a sold hub box (CRA, RED and WEEE implications) |
| Cameras | ONVIF S/T and manual RTSP; tier-1 list verified in the lab (Dahua/Amcrest, Hikvision, Reolink wired, Tapo wired, VIGI, Axis/Uniview); guided wizard; H.264 substream enforcement | Tier-2 beta via go2rtc (Tapo talk, Xiaomi, Wyze, Tuya, DVRIP with a security warning) | Nest SDM, EZVIZ/Imou Open; ISUP and Dahua Auto Register; Matter 1.5 and ONVIF Profile V. Never: Ring, Blink, Arlo |
| Live view | Grid (substream) and fullscreen (main); WebRTC on LAN and remote; snapshot grids on phones | Two-way talk; Picture-in-Picture on iOS and Android; MSE cloud relay as last resort | Cloud SFU for more than 2 concurrent viewers |
| Control | PTZ continuous move + stop, zoom, presets; IR/day-night; reboot | Patrols, motion-zone configuration, siren and floodlight (vendor APIs) | — |
| Recording | Continuous local recording; event retention from ONVIF events with pre-roll and post-roll; per-camera retention presets; timeline; MP4 export; event thumbnails | Hub-side pixel-diff motion detection; substream recording | AI detection (person, vehicle, animal); smart search. No face or licence-plate recognition |
| Storage | Local disk and mounted NAS; S3-compatible (AWS, R2, B2, Wasabi, Hetzner); Google Drive (`drive.file`); OneDrive personal and business; optional encryption; event clips by default, continuous opt-in | Dropbox (after approval); WebDAV/Nextcloud and SFTP via rclone | Box, pCloud, FTP; managed retention tier; playback straight from the cloud while the hub is offline |
| Remote and accounts | Accounts with passkeys/TOTP; QR pairing; cloud signaling; Cloudflare TURN; family sharing (owner, operator, viewer); audit log | Self-hosted coturn; time-limited shares | Multi-site business features, SSO |
| Notifications | Encrypted FCM and APNs (Android, iOS, web PWA); Windows in-app only | WNS; rich snapshots via the iOS Notification Service Extension | — |
| Clients | Android (target API 36), iOS 17.1+, Windows 10/11, Web (Chrome, Edge, Safari; Firefox H.264 only) | — | — |
| Editions | Personal (cloud off, LAN plus your own VPN) and cloud-connected, from the same binaries | — | Possibly an open-source hub (§6) |

### 4.5 Milestones (3–4 engineers)

- **M0, weeks 0–4:** lab, spike gates G1–G7, API schema frozen.
- **M1, about month 4:** LAN alpha. Windows hub with Windows and Android clients: add a camera, live view, PTZ, local recording, playback. The team uses the personal edition daily.
- **M2, about month 6:** S3, Drive and OneDrive destinations; events and retention; encryption.
- **M3, about month 8:** cloud control plane, remote WebRTC with TURN, iOS and web clients, push.
- **M4, about months 9–11:** closed beta (Google verification done, relay ratio measured), store submissions, then v1.

---

## 5. Top risks and mitigations

| # | Risk | Mitigation |
|---|---|---|
| R1 | **Long tail of camera compatibility** (K1–K3): features off by default, PTZ and event quirks, SHA-256 digest, clock skew, leaked event subscriptions. This drives support volume. | Publish a tiered compatibility list backed by the lab. Probe capabilities and never trust what the camera advertises. Show per-camera "stream healthy" and "events healthy" indicators. Run the conformance smoke test nightly against the lab. Collect failure codes through opt-in telemetry. |
| R2 | **Cameras that default to H.265** produce black tiles or CPU spikes (M2). | Set an H.264 substream at onboarding (with consent). Negotiate codecs per client. Flag H.265 recordings. Transcode only on hubs with hardware encoders, never on a Pi. |
| R3 | **Users resist needing an always-on device.** | Make "This PC is my hub" a 5-minute install. Recommend an N100 appliance. Be explicit in marketing. Consider vendor-cloud connectors in v2 and, later, a sold box. |
| R4 | **Upload bandwidth and provider caps** (N5, S2, S3). | Upload event clips by default. Run a bandwidth calculator at onboarding (measured uplink against cameras × bitrate). Throttle uploads. Pause Drive for 24 h at the cap. Warn about Wasabi's minimum-duration billing. Never silently drop segments that have not been uploaded. |
| R5 | **More sessions relay than modelled**, raising TURN cost. CGNAT data is from 2016. | Measure candidate-pair types. Use the substream on relayed paths. Time out idle sessions. Self-host coturn past 2–5 TB/month. Offer a paid tier with a relay allowance. |
| R6 | **Single-maintainer dependencies:** go2rtc has had no tagged release since Jan 2026; fvp/libmdk; media_kit releases slowly. | Vendor and fork go2rtc, with CI running against the lab. Put go2rtc, MediaMTX and the player behind internal interfaces (`LiveGateway`, `Recorder`, `Player`). Keep fallbacks identified (MediaMTX/gortsplib, media_kit). |
| R7 | **Hub compromise.** The hub holds camera credentials, and go2rtc has a history of remote-code-execution bugs (CVE-2024-29192, ZDI-26-560/561). | Bind sidecars to localhost and remove the exec sources. `hubd` and the WebRTC port are the only listeners. Signed auto-updates with rollback; patch critical issues within 72 h. Fuzz the RTSP and ONVIF parsers. Never use UPnP or port forwarding. Recommend a camera VLAN with no internet access. |
| R8 | **OAuth and provider policy** (S1–S4): Google verification and its 7-day Testing tokens, quota-unit billing still to be announced, OneDrive recycle bin, Microsoft consent blocking, Dropbox's 50-user freeze. | Use `drive.file` only. Verify before beta. Keep our own catalog and download whole segments. Make retention recycle-bin aware. Start Partner Center in week 1. Ship Dropbox after approval. |
| R9 | **Flutter web grid performance.** | Gate G1. A TypeScript web viewer is ready as the fallback. |
| R10 | **Platform permission changes** (P3, N3): Android 17 enforcement, iOS local-network consent, Chrome Local Network Access. | No client ever talks to a camera. If permission is denied, fall back to a relayed WebRTC path. Show an explainer screen before each permission request. |
| R11 | **Losing recordings:** power cuts, full disks, SD-card wear, Windows antivirus or file locks, hub theft. | fMP4 with 1 s parts. Reconcile files and database at startup. Emergency oldest-first cleanup. Require an SSD or HDD. The installer excludes the recording folder from antivirus scanning. Upload event clips quickly. |
| R12 | **Regulation** (L1–L4): CRA reporting already applies and the hub may be Class I; GDPR; Romanian Laws 190 and 333; the Product Liability Directive. | Disclosure policy and a 24 h/72 h reporting runbook now. SBOM. A public 5-year support commitment. Counsel-reviewed retention presets. Privacy masks and audio off by default. No face recognition. Liability insurance. Budget for a third-party assessment. |
| R13 | **Over-claiming privacy** (as Eufy did) or **insider access to video** (as at Ring and Verkada). | Keep the architecture zero-knowledge so staff have no path to video. Check marketing claims against the architecture before publishing. Passkeys and MFA. Protection against credential stuffing. |
| R14 | **Users lose their encryption keys** and with them their recordings. | Recovery kit at setup (an rclone-compatible passphrase). Wrap keys for multiple devices. Clear warnings before enabling encryption. |
| R15 | **App store rejection or margin loss:** demo requirement (2.1), IAP (3.1.1), "monitoring app" positioning. | Keep a permanent demo hub for reviewers. Make the pricing-channel decision (§6). Position as property monitoring with visible indicators. |
| R16 | **Planning numbers are estimates:** N100/Pi capacity, relay ratio, cost model. | Measure in Phase 0 and in beta telemetry. Re-baseline the budget at M4. |

---

## 6. Open decisions for the product owner

| # | Decision | Options | What it changes | Default if undecided |
|---|---|---|---|---|
| D1 | Business model and first release | Personal or self-hosted / multi-tenant SaaS / both | Whether a backend is needed, whether relay is a paid feature, and CRA status (a non-monetised open-source release escapes; monetised software is a "manufacturer") | Both, from one codebase. Free personal edition; paid remote and cloud features. |
| D2 | Primary customer | Home and prosumer / SMB (shops, offices, apartment associations, businesses under Law 333) | Retention presets, roles, DPA, multi-site, audit, Romanian legal exposure | Home and prosumer first; SMB features in v2 |
| D3 | Is a hub acceptable, and on what? | Windows PC / Docker on NAS or mini PC / a box we sell / must work with "cameras only" | Whether cloud-only connectors are needed at v1; CRA, RED and WEEE obligations if we sell hardware | Hub required on Windows or Docker; no hardware sales in v1 |
| D4 | Cameras at launch | Which 5–10 models; whether reverse-engineered protocols (Xiaomi, Tapo cloud password, Wyze) are allowed | Lab purchases, support tiers, terms-of-service and legal risk | ONVIF/RTSP tier 1 only; proprietary protocols as a flagged beta later |
| D5 | Storage and cloud-recording policy | Provider order; continuous vs event-only to cloud; encryption default; whether recordings must be readable without our app; whether an app-owned folder is acceptable | Google scope (`drive.file` vs restricted scope with yearly CASA), bandwidth, user lock-in | Local/NAS, S3, Drive and OneDrive; event clips to cloud; encryption on (rclone-compatible); app-owned folders |
| D6 | Control features in v1 | PTZ depth, two-way audio, imaging, siren and floodlight | Vendor adapters, microphone permissions and review | PTZ, presets, zoom and IR in v1; talk in v1.1 |
| D7 | Remote-access promise | Zero-config relay included / bring your own VPN; latency target; concurrent remote viewers per camera | TURN cost; whether a cloud SFU is needed (above about 2 viewers) | Zero-config WebRTC with TURN; up to 2 concurrent remote viewers per camera in v1 |
| D8 | Minimum devices | iOS 17.1+? Android minimum? Firefox? Maximum tiles per device? | Player paths, test matrix | iOS 17.1+, Android 10+ (assumption; check Romanian device statistics), Firefox H.264 only, 16 tiles on desktop and 4 per page on phones |
| D9 | How subscriptions are sold | IAP inside the apps (Apple 3.1.1) / web-only purchase with a companion app that has no purchase prompts | Margins, store review | Decide with counsel before M3 |
| D10 | Team languages | Go + Dart / TypeScript-heavy / C# | A TypeScript team points to a web-first client (React + Capacitor + Tauri) | Go + Flutter |
| D11 | Infrastructure dependencies and residency | Cloudflare TURN acceptable / fully self-hostable from day one; EU-only | Ops effort, cost curve | Cloudflare TURN + EU VPS, with self-hosted coturn ready |
| D12 | Legal entity and commitments | SRL timing; CRA support period (at least 5 years); security contact for ANSPDCP and DNSC | Store accounts, DPAs, CRA compliance | SRL before beta; publish a 5-year support statement |
| D13 | AI scope | None / person and vehicle detection / face or licence-plate recognition | GDPR Art. 9, AI Act and DPIA duties; hub hardware | None in v1; person and vehicle in v2; never face recognition |
| D14 | Open-source posture | Closed hub / open-source hub with a paid cloud | Community trust, CRA "steward" role, competitive moat | Closed in v1; revisit after launch |
| D15 | Windows distribution | Direct signed installer / Microsoft Store (client only) | Easier WNS push via Store; service hosting | Direct installer for hub and client; Store optional for the client |
| D16 | Retention defaults | Home (e.g. 72 h continuous, 14–30 days for events); Romanian business preset of 20–30 days | GDPR proportionality, Romanian law | As listed, confirmed by counsel |

---

## Appendix: Evidence to re-verify before committing

**Dated sources**
- CGNAT prevalence (IMC 2016).
- ONVIF Profile M material (2020–21).
- Google Play stalkerware policy text (2020).
- Hikvision ONVIF-off bulletin (2017; still consistent with Frigate's Oct 2026 docs).
- Amcrest/Dahua API documentation (2017).

**Low confidence or unverified**
- CASA tiers and pricing.
- HG 301/2012 minimum retention and the scope of Law 333/2003.
- ANSPDCP DPIA list details.
- Capacity estimates for the N100 and Pi.
- The exact Chrome version that enforces Local Network Access by default.
- HEVC support in WebView2.
- flutter_webrtc web support at the pinned version.
- 2026 pricing for Tailscale and ngrok.
- The competitor analysis (Blue Iris, tinyCam, Alfred, Camect, Eufy; vendor sites were unreachable).
- Google Drive's "1 TB/day egress" line, which conflicts with the quota-unit threshold.
- Safari WebCodecs audio support.
- Whether MediaMTX `/get` works on segments still being written.
- Microsoft device-code flow combined with `Files.ReadWrite.AppFolder`.
- Bonjour working without the multicast entitlement.
- R2 pricing and OpenH264 licensing (not from briefs).

**Moving targets to re-check at launch**
- Hetzner pricing (raised twice in 2026).
- Google Drive billing terms ("later in 2026").
- Play target API 37 (inferred for about Aug 2027).
- Final CRA implementing-act wording on "software intended to centrally control" cameras.
- The EU-US Data Privacy Framework appeal.
- The AI Act Digital Omnibus delay.
- Product Liability Directive transposition (due 9 Dec 2026).
- go2rtc release cadence.
- Whether ONVIF Profile V is finalised.
- `Files.ReadWrite.AppFolder` leaving "preview".