# Adversarial review: IP camera platform technical landscape (as of 2026-10-17)

**Verification limits.** The shared WebSearch budget for this run was already used up, so I could not run any web searches. WebFetch also could not resolve these domains from this sandbox: developers.google.com, learn.microsoft.com, developer.chrome.com, chromestatus.com, developers.cloudflare.com, support.google.com, source.android.com, onvif.org, docs.docker.com, docs.hetzner.com.

I checked claims against primary sources I could reach: GitHub release pages, raw vendor doc repos and source code, developer.android.com, developer.apple.com (its JSON doc endpoint and the guidelines page), and pub.dev. Anything I correct from memory is labelled "(from memory)".

## Verified OK

| # | Claim in synthesis | What the source says |
|---|---|---|
| 1 | go2rtc has had no tagged release since Jan 2026 (R6) | Latest tag is v1.9.14 (2026-01-19). Before it: v1.9.13 (2025-12-14), v1.9.12 (2025-11-16), v1.9.11 (2025-10-21), v1.9.10 (2025-09-24). The monthly cadence stopped about 9 months ago. [go2rtc releases](https://github.com/AlexxIT/go2rtc/releases) |
| 2 | MediaMTX v1.21.2, MIT, releases every 2–4 weeks | v1.21.2 (08 Oct), v1.21.1 (20 Sep), v1.21.0 (05 Sep), v1.20.1 (18 Aug), v1.20.0 (05 Aug). LICENSE is MIT. [releases](https://github.com/bluenviron/mediamtx/releases), [LICENSE](https://raw.githubusercontent.com/bluenviron/mediamtx/main/LICENSE) |
| 3 | MediaMTX recording and playback features | Part duration equals the RPO ("the last part gets lost"). `recordSegmentDuration` is a *minimum*. `runOnRecordSegmentComplete` fires when a segment is finalized, and `runOnInit` is needed to sync after a crash. `/get` takes `format=fmp4` (default) or `mp4`. [09-record.md](https://raw.githubusercontent.com/bluenviron/mediamtx/main/docs/2-features/09-record.md), [10-playback.md](https://raw.githubusercontent.com/bluenviron/mediamtx/main/docs/2-features/10-playback.md) |
| 4 | Cloudflare TURN costs $0.05/GB with 1 TB free; the hub normally needs only STUN | "$0.05 per GB", "free tier of 1,000 GB", billed only on "data sent from the Cloudflare edge to the TURN client" (ingress is free). TURN↔SFU traffic is free, STUN is free, and credentials last up to 48 h. Hub→relay traffic is therefore free, and the ≈$950 figure at 20 TB is correct. [CF TURN FAQ (docs repo)](https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/docs/realtime/turn/faq.mdx) |
| 5 | Android 17 / targetSdk 37 requires ACCESS_LOCAL_NETWORK | "Mandatory and enforced for apps targeting Android 17 or higher." It gates TCP in and out, UDP unicast/multicast/broadcast, and `.local` resolution across all networking APIs. Apps below 37 with INTERNET get a temporary implicit grant. [dev.android](https://developer.android.com/privacy-and-security/local-network-permission) |
| 6 | Play has required targetSdk 36 since 31 Aug 2026 | Confirmed, with an extension available to 1 Nov 2026. [dev.android](https://developer.android.com/google/play/requirements/target-sdk) |
| 7 | Android 15+ caps dataSync and mediaProcessing at 6 h per 24 h each | Confirmed. These are the only two types with the cap, each tracked separately, and the cap resets when the app comes to the foreground. [dev.android](https://developer.android.com/develop/background-work/services/fgs/timeout) |
| 8 | iOS multicast needs an entitlement; Bonjour needs local-network consent but not the multicast entitlement (§2.1 "verify") | Confirmed, with one caveat (see C3). TN3179: sending or receiving multicast or broadcast needs `com.apple.developer.networking.multicast`, which is not needed on macOS. "All Bonjour operations require local network access." [TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy) |
| 9 | `Files.ReadWrite.AppFolder` is "preview" and works for personal accounts | Delegated, "(preview)", no admin consent required, and available for consent in personal accounts. [Graph permissions reference](https://raw.githubusercontent.com/microsoftgraph/microsoft-graph-docs-contrib/main/concepts/permissions-reference.md) |
| 10 | CRA timeline | In force 11 Dec 2024. Notified bodies from 11 Jun 2026. Art. 14 reporting from 11 Sep 2026. All other obligations from 11 Dec 2027. [ORC WG CRA hub](https://raw.githubusercontent.com/orcwg/cra-hub/main/faq/cra-itself/timeline.md) |
| 11 | MinIO was archived 25 Apr 2026 and is AGPL | Confirmed: "no longer maintained", source only, AGPLv3. [github.com/minio/minio](https://github.com/minio/minio) |
| 12 | flutter_webrtc web support is unclear | 1.6.2+hotfix.4 is current (about 10 days old). The pub.dev platform tags list Android, iOS, Linux, macOS and Windows, with **no Web tag**, while the README and feature table claim Web. This is still unresolved, so the G1 spike is required. [pub.dev](https://pub.dev/packages/flutter_webrtc) |
| 13 | Drive limits from rclone | 750 GiB/day upload, about 2 files/s, and the shared rclone client_id "will stop working during 2026". Testing-mode grants "expire after a week". `drive.file` sees only files the app created. [rclone drive.md](https://raw.githubusercontent.com/rclone/rclone/master/docs/content/drive.md) |
| 14 | App Review 2.5.4 | Text confirmed. [guidelines](https://developer.apple.com/app-store/review/guidelines/) |

**Not re-verified because the domains were unreachable.** These still rest on the briefs:
- Drive quota units and the 400M units/day threshold.
- The Chrome version that enforces Local Network Access (LNA) by default.
- Chrome 136 WebRTC HEVC, and the Edge Q&A.
- ONVIF Profile S end and the Profile V release candidate.
- Hetzner's 20 TB allowance, €1/TB and the 2026 price rises.
- Google's device-flow scope list.
- CASA pricing.

## Corrections

**C1. P4, D9, R15: "in-app subscriptions must use IAP (3.1.1)".**
This is incomplete for the target market.
- **US storefront:** 3.1.1(a) says entitlements "are not required for developers to include buttons, external links, or other calls to action in their United States storefront apps."
- **EU:** Apple's DMA page (updated 18 Aug 2026, new terms in force 1 Oct 2026 under Attachment 14) allows alternative payment processing in the app, plus out-of-app offers with an actionable link. Rates I read off the page:
  - IAP: 26% standard, 15% reduced.
  - Alternative payment processor: 20% / 10%.
  - Store services commission on link-outs: 15% / 10%, on sales within 7 days of the tap.
  - The payment choice must be kept for 12 months, and minors must go through parental gates.
- **Impact:** D9 is a choice among three EU channels, not "IAP or a web-only companion app". Confirm the exact fees in Attachment 14.
- Sources: [App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/), [Apple DMA EU page](https://developer.apple.com/support/dma-and-apps-in-the-eu/).

**C2. P1: "Local-network operations are silently denied in the background."**
This is overstated. TN3179 says the denial without an alert happens when the Local Network privilege is *undetermined*. Once the user has granted it, the note does not describe background denial.
- **Impact:** Picture-in-Picture over the LAN path can work. Request the permission in the foreground at the first LAN connection.
- Source: [TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy).

**C3. §2.1: "Bonjour needs local-network consent but not the multicast entitlement."**
This is true only if the app declares `NSBonjourServices`. TN3179: "add a list of service types to the NSBonjourServices property". Browsing arbitrary or all service types does need the multicast entitlement.
- **Impact:** the hub must advertise a fixed service type (for example `_ourhub._tcp`) and list it in the app's Info.plist.

**C4. M1: "Android Media3 RTSP plays only H.264, AAC, AC3 (no H.265)".**
The docs page still says this, but the androidx/media source maps `VIDEO_H265` to `RtpH265Reader`, along with H.263, MPEG-4, VP8, VP9, Opus, AMR and G.711. The docs are stale.
- **Impact:** low, because the design uses WebRTC. Drop this argument from the rationale.
- Sources: [Media3 RTSP docs](https://developer.android.com/media/media3/exoplayer/rtsp), [DefaultRtpPayloadReaderFactory.java](https://raw.githubusercontent.com/androidx/media/release/libraries/exoplayer_rtsp/src/main/java/androidx/media3/exoplayer/rtsp/reader/DefaultRtpPayloadReaderFactory.java).

**C5. N3, R10: "Chrome LNA … drops local ICE candidates without that permission."**
I could not confirm this has shipped.
- The WICG explainer only *proposes* it ("Local connection attempts that use WebRTC should also be gated…"; "we won't add the candidate to the ICE Agent"). It also says the proposal "has not been approved to ship in Chrome". I could not reach chromestatus.
- **Impact:** the design tolerates either outcome. G1 and G4 should test LAN web viewing on current stable Chrome and record whether a prompt appears or a relay is used.
- Related (from memory): browsers hide their own host candidates behind mDNS `.local` names. The hub's Pion/go2rtc must resolve mDNS or rely on peer-reflexive candidates for LAN-direct connections.
- Source: [LNA explainer](https://raw.githubusercontent.com/WICG/local-network-access/main/explainer.md).

**C6. M2, §3.4: "H.265 … Never Firefox or Edge."**
go2rtc's own matrix lists "Desktop Chrome 136+, Desktop Edge, Android Chrome 136+: WebRTC H264, H265*". That contradicts the Microsoft Q&A cited for M2, and §3.6 does not resolve it.
- **Fix:** no user-agent rules. Send H.265 only if the client's SDP offer includes it *and* a first frame decodes within N seconds; otherwise fall back to H.264.
- Source: [go2rtc README](https://raw.githubusercontent.com/AlexxIT/go2rtc/master/README.md).

**C7. M4: "performance classes guarantee only 2, 4 or 6 concurrent 720p decoders (class 10/20/30)".**
This looks garbled. From memory (source.android.com was unreachable), media performance class requires at least 6 concurrent hardware decoder sessions: 720p for the R class and 1080p from S onward. There are no "class 10/20/30".
- **Impact:** the conclusion still holds (low-end phones may have 1–2 decoders, so paginate at 4 tiles), but the cited guarantee should not be repeated.

**C8. S1: "Unverified apps are capped at about 100 users."**
From memory (support.google.com was unreachable): the user cap applies to unverified apps that request *sensitive or restricted* scopes. With only `drive.file` and `drive.appdata`, which are non-sensitive, the real constraints are:
- leaving Testing mode, which issues 7-day tokens (confirmed in the rclone docs);
- brand verification, if the consent screen shows our name or logo.

The plan to verify before beta is unchanged.

**C9. D1, L3: "a non-monetised open-source release escapes [the CRA]".**
That is true only for genuinely non-commercial open source.
- A *free personal edition* from the company that sells the cloud edition is made available in the course of commercial activity. Assume it is in scope.
- Art. 14 duties attach to products on the market, including products placed before Dec 2027 (Art. 69(3), from memory). The runbook must exist before the first public beta, but there is no legal reporting duty in "week 1".
- From memory: the Commission's implementing regulation on technical descriptions of important products appears to have been adopted in late 2025, so it is no longer a "draft". Check whether the final text keeps "software intended to centrally control". The ORC WG hub still tracks only the March 2025 draft ([README](https://raw.githubusercontent.com/orcwg/cra-hub/main/product-definitions/README.md)).

**C10. §3.1: "For software H.264 encoding use OpenH264 (BSD-2)."**
The BSD licence covers copyright, not patents. From memory: Cisco pays the AVC royalties only for *its own binary, downloaded at runtime*. A bundled or self-built OpenH264, or the H.264/HEVC decoders in an LGPL FFmpeg build, leaves the patent licensing with us. §3.5 lists only the HEVC exposure.
- **Impact:** prefer hardware encoders and OS decoders, and have counsel check AVC exposure.

**C11. §3.1, §3.6: "rclone-crypt format with a random site key wrapped to each device's key; recovery is the rclone passphrase."**
This only works if the random site key *is* the rclone password. rclone-crypt derives every key from that password with scrypt, so one key encrypts every file.
- Consequences the synthesis does not state: no per-segment keys and no forward secrecy. A revoked device or a leaked recovery kit exposes all footage, past and future, until everything is re-encrypted.
- The security brief's per-segment keys and the "recoverable with stock rclone" promise are mutually exclusive. The synthesis presents them as reconciled.
- **Fix:** make this an explicit D5 decision.
- Source: [rclone crypt.md](https://github.com/rclone/rclone/blob/master/docs/content/crypt.md).

**C12. §0, §2.1: "1 s parts (at most about 1 s lost on a power cut)".**
MediaMTX's docs say the last part is lost on a failure, but say nothing about fsync. On a real power cut, OS write-back caching (ext4 commit interval, the NTFS lazy writer) can lose several seconds.
- **Impact:** G2's "≤ 2 s" gate may fail as configured. Plan for fsync on part close (a patch or a setting).

**C13. §3.1 hub runtime: "Docker Desktop on Windows lacks host networking."**
From memory, Docker Desktop 4.34+ (2024) does offer host networking for Linux containers, but only at layer 4 inside its VM. WS-Discovery multicast and LAN ICE host candidates still don't behave as they do on Linux. The conclusion (run a native Windows service) is right; the wording is outdated.

**C14. §4.4 MVP, Notifications: "encrypted FCM and APNs" in v1, but the iOS Notification Service Extension in v1.1.**
These contradict each other. Encrypted APNs payloads need the Notification Service Extension to show any useful text, so either move it to v1 or send generic unencrypted text in v1 and word the privacy claims to match.

**C15. §0, §2.1: "Personal edition = cloud off, LAN plus your own VPN."**
With the cloud fully off:
- APNs and FCM push cannot work, because our push credentials cannot be shipped to self-hosters.
- The web client has no signaling path unless the hub serves it over LAN HTTPS with a trusted certificate.

Either keep a minimal cloud (signaling and push) on for the personal edition, or document "no push, no web" for it.

**C16. §2.1, R13: "our cloud cannot intercept sessions / a compromised cloud cannot add viewers."**
This holds for native apps that pin the hub key. It does **not** hold for the web app, because its JavaScript comes from our CDN, so a compromised cloud can ship JS that skips the pinning. The same applies to the hub auto-update channel: whoever holds the update-signing key can read every user's footage.
- **Impact:** scope the "zero-knowledge" marketing claims accordingly (the Eufy lesson).

**C17. §2.4: "GDPR role: small processor".**
This is incomplete. For accounts, billing, telemetry, ICE logs (which contain IP addresses) and opt-in failure codes, we are a **controller**. That needs our own privacy notice and record of processing.

**C18. Effort: 23–32 person-months.**
The arithmetic is internally consistent. But it leaves out QA and the device test matrix, UX design, security testing, billing/VAT, support, and upkeep of the go2rtc fork. SP7 (four clients with a per-vendor wizard, timeline, PiP and push) at 5–7 person-months is optimistic. My judgement, not sourced: budget +30–50%.

**C19. S3 / §3.2: device-code flow with `Files.ReadWrite` for work accounts.**
From memory, unverified this session: after the 2025 device-code phishing campaigns, many tenants block device-code flow through Conditional Access. Microsoft also tightened default user consent for file and site permissions in 2025.
- **Fix:** run the Dropbox pattern (PKCE on the phone, code handed to the hub) for Microsoft work accounts too, and test it against a tenant that has Conditional Access in G5.

## Missing topics

1. **EU tax and payment operations.** A Romanian SRL selling business-to-consumer digital subscriptions across the EU needs VAT OSS registration, or a merchant-of-record provider. This ties into the Apple and Google EU alternative-billing choice (C1).
2. **Android developer verification.**
   - It launches in Aug 2026. Enforcement starts 30 Sep 2026 in Brazil, Indonesia, Singapore and Thailand on certified devices, and goes global in 2027.
   - Any APK distributed outside Play must be registered: a beta APK, a direct download, or a future Android hub mode.
   - From memory: new *personal* Play accounts must run a 12-tester, 14-day closed test before production, and organisation accounts are exempt. So register Play under the SRL in week 1.
   - Source: [dev.android developer verification](https://developer.android.com/developer-verification).
3. **LAN HTTPS for the hub.** The LAN offline endpoint and any hub-served web UI need a publicly trusted certificate (for example a per-hub subdomain with DNS-01 issuance, as Plex does). Without one, secure-context APIs fail: WebCrypto non-extractable keys, and getUserMedia for two-way talk.
4. **H.264 profile and level negotiation.** Cameras send Main or High profile, and 4K streams can be Level 5.x. Browsers' `profile-level-id` offers differ (from memory, Firefox via OpenH264 has historically been weaker on High profile). Add this to the G1/G6 matrix.
5. **Browser mDNS host candidates** and how the hub resolves them (C5).
6. **Update-signing key custody.** Use an HSM or offline root, threshold signing, reproducible builds and a transparency log. This key is the real crown jewel of the zero-knowledge model and a CRA expectation.
7. **Hub migration, backup and disaster recovery.** Moving to a new PC (catalog, keys, OAuth tokens, camera credentials), and what happens when the hub's disk dies.
8. **Encryption at rest on the hub.** A stolen hub exposes local footage and camera credentials whatever the cloud encryption. Give BitLocker/LUKS guidance and seal credentials with the TPM.
9. **Windows-service specifics.** Services cannot see users' mapped drives, so NAS access needs UNC paths plus stored credentials. Also handle Windows Update reboots and power plans.
10. **One PeerConnection per tile plus a separate Pion data PeerConnection.** On relayed paths a 16-tile grid means 16 TURN allocations, with CPU and battery cost on phones. Merge streams into one PeerConnection in v1, not "later".
11. **Playback over the data channel.** SCTP-over-DTLS throughput and head-of-line blocking at higher scrub speeds need a benchmark in G7.
12. **Cost-model sensitivity.** 10 GB/month of remote viewing is light use: about 44 h of the 0.5 Mbps substream, or about 5.5 h of the 4 Mbps main stream. Business users watching main streams could be 10×. Add a heavy-user scenario and a relay allowance per plan.
13. **Remote diagnostics and fleet telemetry.** A privacy-preserving support bundle, hub version distribution and crash reporting are needed for R1 support volume and for CRA vulnerability handling.
14. **Other EU rules (from memory).** The European Accessibility Act (since 28 Jun 2025) may apply to consumer e-commerce flows, with an exemption for micro-enterprises. NIS2 (Romania: OUG 155/2024) is probably out of scope at startup size, but re-check if the company grows into managed services.
15. **Warnings about the cameras themselves.** Flag default passwords, exposed ports and known firmware CVEs during onboarding. This reduces liability and is a differentiator.
16. **Localisation** (Romanian and English) and tooling for data-subject requests against our own account data.

## Net effect on the recommendation

- **Architecture A still stands.** Nothing I verified or corrected undermines the hub-plus-thin-cloud design with WebRTC everywhere. Several findings reinforce it:
  - Android 17's local-network rule and the 6-hour foreground-service caps.
  - go2rtc's stalled releases, which back the decision to fork it.
  - MinIO's archival.
  - Cloudflare TURN billing only edge-to-client traffic.
- **Decisions to re-open:**
  - D9 monetisation, now that EU alternative payments and link-outs and US link-outs are allowed (C1). Add VAT OSS.
  - D5 encryption: rclone-compatible single key versus per-segment keys (C11).
  - D1: whether the personal edition keeps a minimal cloud for push and signaling (C15), and the assumption that it is CRA-exempt (C9).
- **Claims to scope:** "zero-knowledge" covers native apps; the web app and the update channel rely on trusting our servers (C16). Protect the update-signing key as a top-tier asset.
- **Phase 0 gates to add:**
  - G1/G4: Chrome LNA prompt and relay behaviour on the LAN, and mDNS candidates.
  - G2: fsync and the real power-cut loss.
  - G5: Microsoft work account with Conditional Access blocking device-code flow.
  - G6: H.264 profile/level matrix and Edge HEVC.
- **Week-1 items to add:**
  - Register Play Console as an organisation.
  - Plan Android developer verification for any APK distributed outside Play.
  - Register for EU VAT OSS (or pick a merchant of record).
  - Add the NSBonjourServices entry and fixed service type to the iOS checklist.
- **Planning numbers:** budget about 30–50% more effort (roughly 30–45 person-months to v1), and add a heavy-viewing scenario to the TURN cost model. The €/user figures at the stated assumptions are arithmetically correct.

**Sources**
- [go2rtc releases](https://github.com/AlexxIT/go2rtc/releases) · [go2rtc README](https://raw.githubusercontent.com/AlexxIT/go2rtc/master/README.md)
- [MediaMTX releases](https://github.com/bluenviron/mediamtx/releases) · [MediaMTX record docs](https://raw.githubusercontent.com/bluenviron/mediamtx/main/docs/2-features/09-record.md) · [MediaMTX playback docs](https://raw.githubusercontent.com/bluenviron/mediamtx/main/docs/2-features/10-playback.md) · [MediaMTX LICENSE](https://raw.githubusercontent.com/bluenviron/mediamtx/main/LICENSE)
- [Cloudflare TURN FAQ (docs repo)](https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/docs/realtime/turn/faq.mdx)
- [Android local network permission](https://developer.android.com/privacy-and-security/local-network-permission) · [Play target SDK](https://developer.android.com/google/play/requirements/target-sdk) · [FGS timeout](https://developer.android.com/develop/background-work/services/fgs/timeout) · [Android developer verification](https://developer.android.com/developer-verification)
- [Media3 RTSP docs](https://developer.android.com/media/media3/exoplayer/rtsp) · [Media3 RTP reader factory source](https://raw.githubusercontent.com/androidx/media/release/libraries/exoplayer_rtsp/src/main/java/androidx/media3/exoplayer/rtsp/reader/DefaultRtpPayloadReaderFactory.java)
- [Apple TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy) · [App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/) · [Apple DMA/EU page](https://developer.apple.com/support/dma-and-apps-in-the-eu/)
- [Graph permissions reference](https://raw.githubusercontent.com/microsoftgraph/microsoft-graph-docs-contrib/main/concepts/permissions-reference.md) · [Graph permanentDelete](https://raw.githubusercontent.com/microsoftgraph/microsoft-graph-docs-contrib/main/api-reference/v1.0/api/driveitem-permanentdelete.md) · [Entra user consent docs](https://raw.githubusercontent.com/MicrosoftDocs/entra-docs/main/docs/identity/enterprise-apps/configure-user-consent.md)
- [ORC WG CRA timeline](https://raw.githubusercontent.com/orcwg/cra-hub/main/faq/cra-itself/timeline.md) · [ORC WG product definitions](https://raw.githubusercontent.com/orcwg/cra-hub/main/product-definitions/README.md)
- [WICG Local Network Access explainer](https://raw.githubusercontent.com/WICG/local-network-access/main/explainer.md)
- [MinIO repo](https://github.com/minio/minio)
- [flutter_webrtc on pub.dev](https://pub.dev/packages/flutter_webrtc)
- [rclone Drive docs](https://raw.githubusercontent.com/rclone/rclone/master/docs/content/drive.md) · [rclone crypt docs](https://github.com/rclone/rclone/blob/master/docs/content/crypt.md)