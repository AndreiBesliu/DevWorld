# Client stack judge panel (2026-10-17)

Three independent judges compared the client stack options. The owner chose **B (web-first TypeScript)** based on this panel.

Options: **A** Flutter everywhere · **B** web-first TypeScript (React SPA/PWA + Capacitor + Tauri) · **C** hybrid (Flutter mobile/Windows + separate TS web).

## Lens: AI maintainability and verification

Scores: A=5, B=8, C=3 — winner **B** (high confidence)

**Decisive reasons**
- How close the tested code is to the shipped code. In B, the SPA an agent runs in Chromium under Playwright, with real WebRTC from a containerised go2rtc and hub, is essentially the code and engine that ship in the Android WebView (Chromium) and Windows WebView2 (Chromium). In A, the only target runnable in the container (Flutter web) uses a different video path (HtmlElementView) from the native libwebrtc textures that ship on Android, iOS and Windows. Agents would be checking a stand-in, not the product.
- Agents can inspect failures. B produces DOM, console, network and trace artefacts that agents read well. A's Flutter web is a canvas that needs a semantics workaround before automation can see it (docs.flutter.dev web-accessibility; flutter/flutter#177670), and A's native video bugs show up as C++/platform crashes that cannot be reproduced in the container.
- AI models know TypeScript/React far better than Dart/Flutter, which lowers the API-hallucination rate. Dart's stronger type system does not make up for this, because strict TS, generated hub-API types and zod close most of the gap.
- Fewer moving parts in the native layer. B has thin Kotlin/Swift/Rust islands around one TS codebase. A has Dart plus single-maintainer native video plugins (fvp/libmdk, media_kit). C doubles the UI work and is ruled out by this lens.
- Builds are more reachable from Linux. Capacitor Android builds on Linux, and Tauri can cross-build NSIS installers with cargo-xwin. Flutter Windows needs a Windows host. iOS needs macOS CI under every option.

**What would change this verdict**

Four results would shift me toward A or C:

(1) A week-1 spike shows a Capacitor app on a mid-range Android phone, or WKWebView on iOS 17+, cannot sustain 4-9 substream WebRTC/MSE tiles, or cannot do PiP and background audio without large custom native plugins. If the Swift/Kotlin plugin code would exceed about 15-20% of client code, B's single-language advantage is gone.

(2) Flutter web passes a 16-tile Chromium/Safari/Firefox grid spike, and a Flutter Linux-desktop build with flutter_webrtc runs reliable headless integration tests in the container as a credible stand-in for the Windows and Android native paths. Then A's single strongly typed codebase and golden tests would win.

(3) Apple rejects the Capacitor build under guideline 4.2 and fixing it needs substantial native UI.

(4) Measured agent defect rates over a 2-4 week pilot are clearly lower in Dart than in TypeScript on this codebase.

**Must-do items if the winner is chosen**
- Week-1 spike, before writing feature code: a 9-tile substream grid plus fullscreen WebRTC, MSE fallback and fMP4 playback, run in Chromium (Playwright), the Capacitor Android WebView on a mid-range device (Firebase Test Lab/BrowserStack), Capacitor iOS WKWebView on iOS 17.1+ (ManagedMediaSource) via macOS CI, and Tauri/WebView2 on a Windows runner. Record CPU, framesDropped and latency figures.
- Containerised E2E harness: docker-compose with the Go hub, go2rtc and ffmpeg synthetic RTSP cameras. The cameras use testsrc with a burned-in timestamp and frame counter, and include H.264, H.265 and B-frame variants. Playwright tests assert that video actually plays (`getStats` framesDecoded increasing, `requestVideoFrameCallback`, latency under 1 s), check PTZ data-channel round-trips, check timeline seeking, and take visual snapshots. Every agent PR must pass this suite.
- Single source of truth for the hub/client API (protobuf or OpenAPI), generating both Go and TypeScript types, with zod/runtime validation at the boundary. TypeScript `strict` plus `noUncheckedIndexedAccess`, ESLint type-aware rules, no `any`.
- Keep the video player as one framework-agnostic TS module (adapted from go2rtc's MIT video-rtc.js), with its own unit and E2E tests. The React UI talks only to its interface.
- Keep the native layer thin: prefer official @capacitor/* plugins, and write each custom plugin (local-network permission, PiP) as a small, separately tested module. Use the Tauri shell only for window and tray handling, sidecar hub lifecycle and the updater. No business logic in Rust/Kotlin/Swift.
- CI matrix: Linux (unit, E2E Chromium/Firefox, Android build plus emulator on a KVM-enabled runner), Windows runner (Tauri build, Playwright `connectOverCDP` against the real WebView2 app), macOS runner (iOS build, simulator smoke test via Appium/XCUITest or Safari inspection of WKWebView). The WebKit-on-Linux project is advisory only.
- Upgrade discipline: Renovate with a release-age delay (for example 7-14 days) and grouped updates. A scheduled quarterly upgrade task for React/Vite/Tauri minors. A planned yearly Capacitor major using `npx cap migrate` and the official guide. A calendar entry for the Play target-API deadline each August. Pin exact versions, commit lockfiles, and run a licence allowlist check in CI.
- Agent ergonomics: an AGENTS.md/CLAUDE.md giving one-command dev, test, E2E and screenshot workflows, the rule that no PR merges without an E2E video assertion and screenshot artefact, and Playwright traces uploaded on failure so later agents can debug from the artefacts.
- Mitigate the App Store 4.2 risk early: native PiP, push, local-network permission, haptics and a native-feeling navigation shell, plus a demo camera stream for reviewers. Do a TestFlight review dry run before the feature work grows.

### Option A (5)
Strengths:
- One UI codebase in one language (Dart). It has sound null safety and a strict analyzer (`dart analyze`), so many mistakes an AI makes are caught before anything runs.
- Flutter's own test tooling is very good. Widget tests and golden (screenshot) tests run headless on Linux with `flutter test`, with no device needed. That is a fast, deterministic feedback loop for UI logic.
- flutter_webrtc also supports Linux desktop. In principle an agent could run integration tests of the native libwebrtc path under Xvfb as a rough stand-in for Windows.
- It is the framework the earlier client-framework brief recommended, and it has a maintained video path on all four targets (brief: flutter_webrtc released 2026-10-07, media_kit/fvp).
Weaknesses:
- The core feature, live video, takes different code paths depending on platform. Android, iOS and Windows render native libwebrtc output into textures. Web uses HtmlElementView platform views wrapping an HTML <video>. So the one target an agent can fully run in a Linux container (Flutter web in Chromium) does not exercise the video path that ships on 3 of the 4 platforms. Bugs in the native paths show up as C++/ObjC/JNI crashes that an agent cannot reproduce in the container.
- Flutter web draws to a canvas, so Playwright has no DOM to query. Semantics must be enabled first, through the hidden 'Enable accessibility' control or `SemanticsBinding.ensureSemantics()`. There are known automation problems with that placeholder (flutter/flutter#177670). Visual checks of the real web UI come down to comparing screenshots. Sources: docs.flutter.dev/ui/accessibility/web-accessibility, github.com/flutter/flutter/issues/177670.
- A Flutter web grid of 9-16 HtmlElementView video tiles is unproven. The earlier brief flags it as the first de-risking task: platform views are expensive, and moving them in the DOM restarts players. When it fails, the cause sits in the gap between Flutter's engine and the browser, which is hard for an agent to diagnose.
- `flutter build windows` must run on a Windows host, so every Windows build and test needs a Windows CI runner. Android emulators in cloud containers usually lack KVM. In practice an agent verifies only web plus unit and golden tests locally.
- Ongoing upgrade work: Flutter ships about quarterly stable releases (the brief notes 3.47, with 3.50 due in Nov 2026). The plugin ecosystem lags behind those releases. There is a forced dart:html to package:web/js_interop migration for Wasm, which is blocked if any plugin still imports dart:html (docs.flutter.dev/platform-integration/web/wasm). The Android Gradle/Kotlin/AGP bumps apply here as well.
- AI models have seen far less Dart/Flutter than TypeScript/React. Many key packages have a single maintainer (fvp/libmdk; media_kit has slow releases per the brief), which raises the risk of AI-generated code drifting from the real API.

### Option B (8)
Strengths:
- What an agent tests in the container is almost exactly what ships. The Android WebView is Chromium and Windows WebView2 is Chromium/Edge, and the same SPA runs in Chromium under Playwright. A Linux container can run the whole real chain: go2rtc plus ffmpeg synthetic RTSP cameras, then the hub, then WebRTC/MSE, then the real React UI in Chromium. Playwright can assert that frames actually decode (`getStats` framesDecoded, `requestVideoFrameCallback`, latency read from a burned-in timestamp) and take screenshots. That covers about 3 of the 4 targets with real video.
- Diagnostics are the best of the three. Playwright traces record DOM snapshots, console, network and timing. Stack traces are readable through source maps. WebRTC stats are available. Failures are DOM and JS errors, not engine-internal rendering problems.
- TypeScript/React is the stack AI models have seen most, so agents are least likely to hallucinate APIs and most likely to get them right first time. Test tooling is mature: Vitest, Testing Library, Playwright, and visual snapshot comparison.
- Most of the code is in one language (strict TypeScript). Native code is limited to small islands: Capacitor plugins in Kotlin/Swift for push, PiP and local-network permission, and a thin Rust shell in Tauri. Those islands mostly use official plugins.
- The real Windows app can be tested with Playwright using `connectOverCDP` against WebView2 on a Windows CI runner (playwright.dev/docs/webview2). Tauri 2 can cross-build NSIS installers from Linux with cargo-xwin (v2.tauri.app/distribute/windows-installer), and it can bundle the Go hub as a sidecar.
- Upgrades are mechanical and documented. Capacitor majors come with `npx cap migrate` and written guides (capacitorjs.com/docs/updating/8-0). React and the browser APIs change slowly. Tauri has been on 2.x since late 2024. The go2rtc browser player is already JS, so it can be reused instead of reimplemented.
Weaknesses:
- iOS WKWebView is WebKit, and Playwright's Linux WebKit build is not a reliable stand-in for WebRTC/H.264/MSE, since codec support depends on the GStreamer build. iOS needs macOS CI with a simulator or Safari Web Inspector. MSE on iOS needs ManagedMediaSource (iOS 17.1+). This is also true of A.
- There are several upgrade treadmills: Capacitor ships a yearly major (Capacitor 9 raises the floor to iOS 16 and Xcode 27, per capacitorjs.com/docs/next/updating/9-0), Tauri, Vite/React, and the Play target-API ratchet.
- TypeScript's type system is unsound and less strict than Dart's. It needs `strict` mode, generated API types and runtime validation (zod) at the hub boundary.
- The npm dependency surface brings supply-chain risk (for example the 2025 npm worm incidents). It needs lockfiles, a release-age delay and a licence/allowlist check in CI.
- There is a risk of App Store 4.2 rejection for a 'thin webview' app. Grid performance in WebViews on mid-range Android phones is unmeasured.

### Option C (3)
Strengths:
- Each half uses its strongest stack. The web gets a native-DOM, Playwright-friendly SPA, and the mobile and desktop apps get Flutter's native video path.
- It avoids Flutter web's canvas and platform-view problems entirely.
Weaknesses:
- Two UI codebases in two languages, with two state models, two test suites and two dependency treadmills. Every feature, bug fix and API change has to be made twice, and an agent fixing one side routinely forgets the other.
- It combines A's main verification gap (native Flutter video paths can't be tested in a Linux container; Windows needs a Windows host) with B's churn on the web side. It gets B's container-fidelity advantage only for the web target.
- Agents need two sets of context, two sets of conventions and two pipelines. Over years, the two UIs will drift apart in behaviour and look. Detecting that drift requires parity tests across both stacks.
- Its only real advantage is as a fallback if A's web spike fails. As a deliberate starting architecture it is the worst choice for long-term AI maintenance.

## Lens: END-USER EXPERIENCE & VIDEO PERFORMANCE on Web, Android, iOS and Windows: multi-camera grids, startup and latency, PiP, push-to-live, PTZ gestures, timeline scrubbing, LAN/offline behaviour, native feel, accessibility and store acceptance

Scores: A=6.5, B=6, C=8 — winner **C** (medium confidence)

**Decisive reasons**
- Mobile is the primary surface: push, tap, live view, PTZ and PiP, on Android-dominated Romania with mostly mid-range phones. Native libwebrtc in Flutter is the safest path to hardware decode with software fallback and a working iOS background PiP (AVSampleBufferDisplayLayer fed from native frames). WebView WebRTC fails here: forum reports say PiP breaks in the background (developer.apple.com/forums/thread/812088, /thread/819235), and Android WebView's hardware decode is unverified (Chromium disabled it in 2015: codereview.chromium.org/928613004).
- On the web, the browser's own <video> with go2rtc's player beats Flutter web on every UX axis. Flutter web adds a per-frame canvas copy per tile (flutter_webrtc PR #1911, now default) or overlay-limited platform views (api.flutter.dev HtmlElementView). Its accessibility is opt-in (docs.flutter.dev/ui/accessibility/web-accessibility), and it has a heavier first load and non-native browser behaviour.
- Option C is the only option with no weak platform for users. A has a weak web, B has a weak iOS (PiP, 4.2 review risk, webview feel) and an uncertain Android grid. Windows is a slight minus for C and A compared with WebView2, but desktop CPUs absorb 16 software-decoded substreams.
- Store acceptance: Flutter apps carry no Guideline 4.2 webview-wrapper risk. Capacitor apps carry a moderate one, and rejection loops are especially costly when an AI agent iterates with no physical device and macOS only in CI.

**What would change this verdict**

(1) A device-farm spike shows Capacitor on a mid-range Android (e.g. Galaxy A35, Redmi Note 13) running a 9-tile H.264 360p WebRTC grid at 15 fps or more, with hardware decode confirmed in chrome://media-internals or webrtc-internals and acceptable heat after 10 min, plus a working iOS background PiP from a Capacitor app via a native plugin. Then B rises to about 8 and wins, because a single UI stack also gives the best web and Windows. (2) A Flutter-web spike shows 16 texture-mode tiles at full fps in Chrome, Safari and Firefox on a mid-range laptop at under 25% CPU, with acceptable forced-semantics accessibility. Then A rises to about 8 and wins on consistency. (3) If the product decides the web is only a minor 'settings and occasional view' surface, A becomes close to C on UX and simpler overall.

**Must-do items if the winner is chosen**
- Generate one shared API and protocol contract (protobuf or OpenAPI for hub, cloud and data channel RPC) into both Dart and TypeScript clients. Keep a single feature/UX spec with a per-platform parity checklist enforced in CI so the web does not drift.
- Web: wrap go2rtc's video-rtc.js (WebRTC first, MSE/fMP4-over-WS fallback) rather than writing a player. Grid pages of 9 or 16 tiles on substreams, with IntersectionObserver pausing off-screen tiles. Playwright end-to-end tests run against a fake hub that streams test fMP4/H.264, asserting first-frame time, frame rate from getVideoPlaybackQuality, and fallback.
- Mobile: use flutter_webrtc with a cap on concurrent decoders (default 4-tile pages, up to 9 on substreams) and automatic snapshot/MJPEG tiles beyond the cap. Switch to the main stream only in fullscreen. Log decoder type and hardware vs software fallback in telemetry.
- iOS PiP: build or adopt a Swift plugin that feeds RTCVideoTrack frames to AVSampleBufferDisplayLayer and AVPictureInPictureController, with the correct audio session. Android PiP uses activity PiP mode. Validate both on macOS CI simulators plus a real-device farm (BrowserStack, AWS Device Farm or Firebase Test Lab), since there are no physical phones.
- Push-to-live: on notification tap, deep-link to the camera, start ICE gathering and TURN credential fetch in parallel with UI build, and target <2 s remote first frame. Set the camera I-frame interval to fps during onboarding so first frames arrive quickly.
- Windows spike in week 1: measure flutter_webrtc CPU at 16 x 360p and 1 x 4K on a mid-range laptop. If software decode is too costly, render the Windows grid through media_kit (libmpv with D3D11 hardware decode) over the hub's fMP4 stream and keep WebRTC for single-camera, PTZ and talk.
- Timeline: have the hub generate thumbnail sprites per segment and serve fMP4 with HTTP Range. Play through media_kit or fvp on native and MSE on web. Scrubbing shows sprites and seeks on release.
- Accessibility: add Flutter Semantics labels on tiles, PTZ controls and the timeline, plus TalkBack/VoiceOver checks in integration tests. Use DOM ARIA on web with axe-core in Playwright.
- LAN/offline: mobile discovers the hub via mDNS and signals directly to its LAN IP when the cloud is unreachable (ATS NSAllowsLocalNetworking, Android cleartext config for RFC1918, local-network permission prompts on iOS and Android 17). Web shows a clear 'needs internet or hub HTTPS' message instead of failing silently.

### Option A (6.5)
Strengths:
- Android/iOS: flutter_webrtc renders video with native libwebrtc into Flutter textures, with no WebView in between. Libwebrtc's hardware decoder factory uses MediaCodec/VideoToolbox and falls back to software decoding when a phone runs out of hardware decoder instances (phones only guarantee 2-6), so a 9-tile substream grid degrades smoothly instead of showing black tiles.
- iOS PiP is realistic. The decoded frames are already in native code, so a small Swift plugin can feed RTCVideoTrack frames to AVSampleBufferDisplayLayer plus AVPictureInPictureController (iOS 15+). A community package already uses this approach (pip_webrtc), as do commercial SDKs (VideoSDK Flutter guide). Android PiP is activity-level and trivial.
- The mobile app feels native: 60/120 Hz gestures for pinch-zoom and joystick PTZ, haptics, and native scroll physics. Startup is AOT-compiled with no WebView boot. Accessibility maps to TalkBack/VoiceOver properly.
- There is no Apple 4.2 'webview wrapper' exposure.
- flutter_webrtc is actively maintained (1.6.2+hotfix.4, 2026-10-07, libwebrtc m150 on Windows) and added H.265 on Apple/Android in 1.1.0 (CHANGELOG).
- Recorded playback on native can use media_kit or fvp, which give hardware decode, accurate seeks on fMP4 and thumbnail scrubbing.
Weaknesses:
- Web is the weakest target. Since flutter_webrtc 1.1.0 (PR #1911), web video is drawn by copying every frame of a hidden <video> into the Flutter canvas with createImageFromTextureSource. That is an extra GPU copy per tile per frame, with no published benchmarks. The old HtmlElementView path costs one overlay per tile, and browsers cap overlays (Flutter HtmlElementView docs). A 16-tile grid on a low-end laptop or in Safari is unproven.
- On web, accessibility is opt-in: there is a hidden 'Enable accessibility' button unless ensureSemantics is forced, which costs performance (Flutter web-accessibility docs). The first download is heavy (CanvasKit/skwasm), and Ctrl+F, text selection and browser feel are non-native. A support or installer audience on the web will notice.
- Windows: libwebrtc outside Chrome most likely decodes H.264 in software, and flutter_webrtc on desktop hands frames over as CPU pixel buffers. 16 substreams are fine on a desktop CPU, but fullscreen 4K main streams and older laptops will run hot. Chromium/WebView2 would use Media Foundation hardware decode instead. I could not confirm this from a primary source; it needs a spike.
- The MSE/fMP4 fallback has no battle-tested player on native Flutter. go2rtc's video-rtc.js only helps on web, and native relies on libmpv/fvp tuning of HTTP fMP4, which typically adds 1-2 s of latency.

### Option B (6)
Strengths:
- The web is the best possible: a native <video> fed by go2rtc's own video-rtc.js WebRTC/MSE player, the same player Frigate and Home Assistant use. There is no canvas copy, accessibility uses real DOM semantics, and the bundle is small.
- Windows via Tauri/WebView2 inherits Chromium's WebRTC and MSE with Media Foundation hardware decode. 16 tiles of substreams run at low CPU. HEVC over MSE works where the OS extension and hardware exist.
- Grid, timeline and fallback logic exist once and behave identically on every platform. MSE over WebSocket is a first-class fallback everywhere.
- Timeline scrubbing in the browser is MSE plus hub thumbnail sprites, a well-trodden pattern.
Weaknesses:
- iOS PiP is the decisive gap. Apple Developer Forums threads report that WebRTC/HTML video in WKWebView keeps PiP only while the app is in the foreground, that autopictureinpicture is ignored for inline playback, and that webkitSetPresentationMode('picture-in-picture') fires events but no window appears (threads 812088, 819235). A real fix needs a second, native video stack, either native WebRTC or AVPlayer over HLS, which removes the 'one player' benefit.
- Android (Romania is heavily Android, mostly mid-range): whether System WebView uses hardware decode for WebRTC is unverified. Chromium disabled WebRTC hardware decode in WebView in 2015 (codereview.chromium.org/928613004), and I found no primary source saying it was re-enabled. If decoding is software, a 9-tile grid on a Helio/Snapdragon 6-class phone means heat, battery drain and dropped frames. This must be measured before committing.
- The mobile app feels like a webview: touch latency on the PTZ joystick, scroll physics, cold-start WebView boot (roughly 1-2 s extra on mid-range Android), and the Ionic forum's reports of WKWebView live streams freezing without errors. Native gestures and haptics need plugins.
- Apple 4.2 risk is moderate rather than zero. Reviewers probe for thin, website-like apps and offline behaviour (Apple forum thread 812889 on a Capacitor 4.2 rejection; MobiLoud guidance). Native push, PiP and local-network features plus a real hub flow probably pass, but rejection cycles delay launch.
- On iOS, ManagedMediaSource is required for the MSE fallback (iOS 17.1+). Its behaviour inside WKWebView, as opposed to Safari, is not documented, so the fallback is uncertain on iPhone.

### Option C (8)
Strengths:
- It takes the best surface for each platform. Mobile gets Flutter's native libwebrtc textures, native PiP feasibility, native gestures and accessibility, and no 4.2 exposure. Web gets go2rtc's DOM <video> player, real DOM accessibility and a light bundle.
- The web is free to stay a thin viewer and dashboard built on the browser's own player, so the riskiest component, the Flutter-web multi-video grid, is never used.
- Push-to-live on mobile can pre-warm the RTCPeerConnection natively when a notification is tapped, which is easier to tune for <2 s remote first frame than a WebView that must boot first.
- On mobile, LAN mode can talk HTTP/WebSocket signaling straight to the hub's LAN IP when the internet is down, with no mixed-content or WKWebView ATS limits in the way.
Weaknesses:
- The Windows weaknesses are the same as Option A's (likely software decode and CPU pixel-buffer textures in flutter_webrtc on desktop).
- Two UIs risk feature-parity drift and inconsistent UX: the web may lag mobile on features like timeline or PTZ, and users who switch devices will notice. It is the same video pipeline, but the UI behaviour differs.
- Two sets of UI bugs to fix means slower UX fixes overall. This belongs to the maintainability lens, but users feel it as fixes arriving more slowly.

## Lens: Risk and long-term viability: worst realistic failure mode per option, how likely it is, and what it costs to mitigate

Scores: B=7.5, A=5, C=3 — winner **B** (medium confidence)

**Decisive reasons**
- B fails gracefully and A does not. B's worst case (an iOS WKWebView media gap or a 4.2 rejection) stays on one platform and can be fixed at bounded cost: the hub already produces fMP4, so HLS playback is native in WKWebView, and a native iOS video plugin is the last resort. A's worst case (Flutter web grid fails, or flutter_webrtc/media_kit stall) forces a web rewrite into C, or leaves the agents maintaining libwebrtc/libmpv native builds they cannot test.
- Licensing and patent exposure: B ships no codec binaries. A and C need media_kit (LGPL FFmpeg/libmpv, possible HEVC software decode) or fvp (proprietary libmdk key, single maintainer) for recorded playback on Windows.
- Agents can verify their own work. Chromium is the engine shipped in Android WebView and WebView2 and is also what Playwright drives in the Linux container, so B's critical video code gets automated end-to-end tests against a test hub. A and C's native video paths can only be verified on hardware or macOS CI.
- Swapping a dead dependency is cheap: if Capacitor or Tauri dies, only a thin shell is replaced and the UI stays. Losing Flutter or flutter_webrtc means rewriting the client or forking the native media stack.
- C combines the risks of A and B and doubles the long-term maintenance surface, which is the opposite of what an AI-maintained product needs.

**What would change this verdict**

I would switch to A if a week-1 spike shows a Flutter-web 16-tile WebRTC/MSE substream grid holding up on Chrome, Safari and Firefox for 8 or more hours without memory growth or tile restarts during layout changes, AND recordings can be played without media_kit/fvp (e.g. the hub replays fMP4 over WebRTC, or the first-party video_player gains a maintained Windows implementation), AND flutter_webrtc shows multi-org maintainership (e.g. LiveKit or Stream committing upstream). I would also move away from B if a real-iPhone spike via macOS CI or a device farm shows WKWebView cannot play hub WebRTC over TURN-TLS/443 reliably and cannot play fMP4 HLS for recordings on iOS 16+, or if Apple rejects the first build under 4.2 despite native push, PiP and Face ID features. Either result would mean the iOS client needs a native video stack in B anyway, which removes most of B's advantage.

**Must-do items if the winner is chosen**
- Week-1 iOS de-risk spike on a real iPhone (macOS CI runner plus a device farm such as BrowserStack). Test: WKWebView WebRTC to the hub over LAN, STUN and TURN-TLS:443 on a Romanian mobile carrier; a 4-tile grid; fullscreen; PiP; background/foreground resume; and recordings played as fMP4 HLS. Go/no-go before any UI work.
- Make the player transport-agnostic, with one TypeScript interface over WebRTC, MSE-over-WebSocket and HLS(fMP4). Feature-detect ManagedMediaSource and MediaSource, and on iOS WKWebView fall back to native HLS. The hub must serve an HLS playlist from its fMP4 segment catalog.
- Webview security baseline: all assets bundled in the app with no remote server.url and no hub-served HTML; a strict CSP (script-src 'self', no inline or eval); a least-privilege Tauri capabilities file; a minimal Capacitor plugin set; deep-link and universal/app-link payloads validated against an allowlist and never auto-executed; tokens only in Keychain/Keystore/DPAPI via a vendored plugin and never in localStorage; HttpOnly SameSite cookies plus refresh rotation on web.
- Supply-chain controls in CI: committed lockfile; pnpm or npm with a minimum release age before new versions are adopted; Renovate with a cooldown; osv-scanner/npm audit; npm provenance checks; and a license allowlist (MIT/BSD/Apache/MPL/ISC) that fails the build on GPL/AGPL/BSL.
- Keep the push, secure storage, PiP, local-network permission and keep-awake plugins in-repo as small Swift/Kotlin plugins (a few hundred lines each, with tests) rather than depending on single-maintainer community plugins.
- Avoid an App Store 4.2 rejection by shipping real native value: push with time-sensitive notifications, native PiP, biometric app lock, an offline-capable bundled UI, and a demo camera account for reviewers.
- Playwright end-to-end suite in CI against a containerised test hub with synthetic RTSP cameras, run on Chromium, Firefox and WebKit, covering live-view start time (<1 s LAN), grid paging, timeline scrubbing and the PTZ data channel. Add a macOS CI job that runs the iOS build plus an XCUITest smoke test on a simulator.
- Pin minimum runtimes and monitor them: iOS 16+ (17.1+ preferred for MMS), an Android System WebView minimum Chromium version check at startup with an update prompt, and WebView2 Evergreen. Plan for Windows 10 WebView2 updates ending around Oct 2028.
- Keep the Tauri Rust layer under about 300 lines (sidecar launch for the Go hub, updater, tray) and document an Electron fallback. Code-sign both the shell and the hub sidecar.
- Enforce the H.264 substream policy in hub onboarding, because WebView2/Edge will not decode HEVC over WebRTC. Pass H.265 through only when the client's capability probe says it is supported.

### Option B (7.5)
Strengths:
- Licensing and patents carry the least risk. Every decode uses the OS or browser engine (Chromium in Android System WebView and WebView2, WebKit on iOS), so no LGPL FFmpeg/libmpv is shipped and the app bundles no software HEVC decoder that would need patent licensing. The brief warns against bundling FFmpeg/libmpv/VLC until LGPL and HEVC exposure are reviewed (research brief, video pipeline RECO 4 and RISKS).
- Replacing any one wrapper is cheap. The SPA survives if a wrapper dies: Capacitor can be swapped for a small native WKWebView or Android WebView shell (or a TWA/PWA on Android), and Tauri for Electron or a C# WebView2 shell. Each swap is weeks of work, not a rewrite of the app.
- Backers are stable and permissive. Capacitor is MIT and backed by OutSystems (which bought Ionic in 2022). Capacitor 8 shipped in Dec 2025 with SPM as the default, and Capacitor 9 is planned for Nov 2026 (plushcap summary of Ionic's 'Road to Capacitor 9'). Tauri is MIT/Apache. On Windows 10, the WebView2 Evergreen runtime keeps receiving updates until at least Oct 2028, independent of ESU (pureinfotech, citing Microsoft).
- AI agents can test the code that actually ships. The video player that runs in Android WebView and WebView2 is Chromium, the same engine Playwright drives in the Linux container. Playwright's WebKit build covers part of the iOS behaviour. Under A or C most of the native video path can't be exercised in the container at all.
- It reuses the hub's existing proven browser player (the go2rtc-style WebRTC → MSE fallback that Frigate uses), so the client does not need its own native media stack.
- TypeScript/React has by far the largest training corpus and ecosystem of any option here, which lowers how often AI agents make mistakes when maintaining it.
Weaknesses:
- The worst realistic failure is on iOS WKWebView. MSE/ManagedMediaSource availability inside WKWebView is uncertain: nanoStream reports MMS access is denied for locally loaded pages in WKWebView. WebRTC has only worked in WKWebView since iOS 14.3, and there are field reports of ICE/UDP and TURN problems on cellular. PiP of a MediaStream video inside WKWebView is unverified. If any of these break, iOS needs a native video plugin, which can only be built and tested on macOS CI.
- App Store guideline 4.2 ('minimum functionality' / repackaged website) is a recurring rejection risk for wrapped web apps (research brief, framework RISKS).
- The security surface is larger. Any XSS in the webview can reach every native bridge plugin. Capacitor and deep-link handlers need strict validation. npm supply-chain risk is real: large worm-style compromises of popular npm packages happened in 2025.
- Community Capacitor plugins (push, PiP, secure storage, local-network permission) are each a bus-factor risk. Tauri adds a Rust layer the agents must also maintain.
- Codec ceilings are set by the engine. Edge/WebView2 does not offer HEVC over WebRTC, so H.264 substreams or hub transcoding are mandatory on Windows. This is mitigated by the already-decided H.264 substream policy.

### Option A (5)
Strengths:
- A single UI codebase, and no browser bridge exposed to app JS on mobile or desktop: Dart compiles to AOT, so XSS cannot reach native APIs.
- flutter_webrtc is actively released: 1.6.2+hotfix.4 came out about 10 days ago under the verified publisher flutter-webrtc.org (pub.dev versions page). Commercial forks also exist, such as stream_webrtc_flutter with 7 maintainers (depscope), which partly offsets its bus factor.
- Flutter itself is BSD-licensed and production-proven on Android, iOS and Windows. Google's 2026 roadmap reaffirms investment and says non-Google contributors now outnumber Google staff (flutter.googlesource.com roadmap).
Weaknesses:
- Worst realistic failure: the required WEB client fails. Each Flutter-web video tile is an HtmlElementView platform view. The 9-16 tile grid is unproven (research brief), and Flutter has had to re-architect web platform views because DOM moves restarted video players (docs.flutter.dev 'platform views using HTML slots'). If the grid fails, the product is forced into Option C, rewriting the web client and keeping two UIs for good.
- Recorded fMP4 playback on Windows depends on media_kit or fvp. media_kit had 1 active maintainer and 52% author dominance (DepScope, Dec 2025), slow pub releases, and bundles libmpv/FFmpeg, which brings LGPL compliance work (static linking on iOS) and possible HEVC patent exposure. fvp relies on libmdk, a proprietary-key binary from a single maintainer.
- flutter_webrtc ships prebuilt libwebrtc for each platform. If it stalls, rebuilding and patching libwebrtc for Windows, Android and iOS is far beyond what AI agents can do in Linux containers (iOS needs macOS). Its frequent hotfix cadence (4 hotfixes in about 40 days) points to regression churn on exactly the component video depends on.
- Most of the critical video path can't be tested in the container: Linux has no Android, iOS or Windows runtime, and the web build takes a different code path (HTML video) from the native builds (libwebrtc textures). Bugs would surface per platform after release.
- Framework longevity carries a moderate tail risk: the 2024 Google cuts and the Flock community fork were motivated by desktop being deprioritised (okoone, walturn). Desktop is a required target here.
- Dart has a smaller corpus than TypeScript, so AI agents make more mistakes in it, and Flutter's plugin ecosystem (e.g. flutter_secure_storage) is also community-maintained.

### Option C (3)
Strengths:
- It avoids the Flutter-web multi-tile risk by serving the web with a native browser player.
- The native apps avoid the WKWebView MSE and App Store 4.2 webview risks.
Weaknesses:
- It takes on both risk sets: flutter_webrtc and media_kit/fvp bus factor and LGPL/HEVC exposure on the native side, plus npm supply chain and browser quirks on the web side.
- Worst realistic failure is permanent parity drift. Every feature, bug fix, auth flow, timeline and PTZ UI has to be built twice in two languages, and over years AI agents will let them diverge. Two separate upgrade treadmills (Flutter/Dart releases and React/npm) double the maintenance work.
- Like A, its native video path can't be tested in Linux containers. Only the web half gets Playwright coverage.
- For a non-developer owner who can't arbitrate between two codebases, this has the highest coordination and security-review surface for the same product.

