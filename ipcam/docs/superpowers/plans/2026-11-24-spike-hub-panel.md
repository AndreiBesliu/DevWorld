# Spike: ipcam-desktop starts spikehub and shows its log (v0.1.2)

**Pe scurt pentru proprietar.** Ați cerut pe 2026-11-24 ca aplicația de test de pe Windows (ipcam-desktop) să pornească singură spikehub și să arate ce scrie el, într-un panou al ei, fără fereastra terminalului. De la v0.1.2:

- pagina aplicației are panoul «Hub-ul pe acest PC (spikehub)», cu folderul spikehub, butoanele «Pornește hub-ul» și «Oprește hub-ul» și ultimele rânduri scrise de hub;
- hub-ul pornește ascuns, fără fereastră neagră și fără să deschidă browserul;
- folderul se alege o dată (implicit `Descărcări\spikehub-windows`), iar aplicația îl ține minte și pornește hub-ul singură la următoarele deschideri;
- când închideți aplicația sau o actualizați, hub-ul se oprește și el.

Rezultatele rămân în același folder `results`, deci nimic din ce ați rulat nu se pierde. Pasul 6 (camerele reale) rămâne în fereastra lui, pentru că acolo adresele se tastează ascuns.

**Goal:** The Windows shell starts `spikehub.exe` from the owner's spikehub folder as a hidden child process and the spike app shows the hub's output in a panel, so that no console window is needed on the PC. Owner's request of 2026-11-24 (thread "Rulează testul video pe Windows"), approved on a decision card the same day.

**Base:** PR #7 (`claude/project-thread-pcvwvf`, the test apps' own updates, v0.1.2). This work ships in the same v0.1.2.

## Design

- **No new Rust.** The shell's Rust is at 291 of the 299 lines APP-01 allows. `tauri-plugin-shell` 2.6.0, which PR #7 registered, already spawns a child without a console window (`CREATE_NO_WINDOW`), streams its stdout and stderr line by line, puts it in a job object with `processGroup` so that a kill ends its whole tree, and kills its children when the app exits (`RunEvent::Exit`).
- **One allowed command.** `capabilities/spike-hub.json` allows exactly `cmd /d /c .\spikehub.exe -browser=false`, with fixed arguments, plus `shell:allow-kill` and `core:path:allow-resolve-directory` (for the Downloads folder). The page chooses only the working directory: the folder that holds `spikehub.exe`, `media` and `results`, so spikehub's defaults (`./media`, `./results`) keep the owner's earlier results. `cmd` is there because Windows does not look for a relative program in the child's working directory; the job object ends `cmd` and spikehub together. `-browser=false`: the app is the hub's client, so no browser tab opens.
- **spikehub is unchanged.** It waits for Enter only when stdin is a terminal (`waitForEnter`), so a hub started with a pipe ends by itself after a start error, and the panel shows why. `-real` (step 6 of the guide) still needs a terminal and keeps its `.cmd` file.
- **The page** (`hubprocess.ts`, `ui.ts`): the panel is shown only where the shell offers `spikeNative.hub` (the Windows shell). It keeps the last 400 lines and draws new ones only outside a run, so a scenario's measurements see no extra layout. When the hub says `spikehub listens`, an empty hub address becomes `http://127.0.0.1:7080` and the page checks the connection. The folder is remembered (`spike.hubFolder`); with a remembered folder the hub starts by itself at launch, except under `--autorun` (CI). «Oprește hub-ul» is off during a run. An update stops the hub before the installer runs.
- **Measurements.** `spike_metrics` counts only `msedgewebview2.exe` processes under the app (its unit test already has a non-WebView2 child), so the hub as a child of the app does not enter MV6 or MV7; `cpuPctTotal` already included the hub, which ran on the same PC.

## Tasks

- [x] `native.ts`: the `ShellHub` facade (start a hub in a folder, its lines, its end, stop; the default folder).
- [x] `native-tauri.ts`: `HUB_COMMAND = 'spike-hub'`, spawned with `cwd`, `encoding: 'utf-8'`, `processGroup: true`; exit code 9009 (`cmd`: not found) and spawn errors become owner-facing messages. Unit tests, including the capability file's exact arguments.
- [x] `hubprocess.ts`: the log (line split, 400-line cap), the controller (state, remembered folder, auto-start, "listens" detection). Unit tests.
- [x] `ui.ts`, `i18n.ts`, `index.html`: the panel, Romanian and English texts, styles; stop the hub before an update.
- [x] `capabilities/spike-hub.json`.
- [x] CI (`windows-smoke.ps1`, `ci/page-hub.ts`): on the runner, a spikehub folder; the app started without arguments; the page's «Pornește hub-ul» clicked through the DevTools port; the hub must answer on port 7080 within 30 s as the child of the app's `cmd`, with no window, and the panel must show `spikehub listens`; «Oprește hub-ul» must end spikehub; started again, closing the app must end it too.
- [x] Guide (`docs/spike-video.md`) and spec (O18, and APP-24 for the product's hub panel). O17 is taken by the recorder spike's branch, which has not merged yet.
