// The start page: the hub's address, a connection check that says why the
// phone cannot reach the hub (Review Focus 1), the scenarios with a start
// button and a status each, the results not yet uploaded, and the stage on
// which the scenarios show their video. Romanian by default, English one tap away.
import { createPlayer } from '@ipcam/player';
import { createAttention, type AudioOut } from './attention';
import { autorun, type AutorunRequest, type ErrorsTarget } from './autorun';
import { exposeBandReader, readBand, type BandReaderTarget } from './band';
import { fetchCameras, simCameras } from './cameras';
import { watchCharger } from './charger';
import { captureForDecoderInfo, grantCamera } from './decoderinfo';
import { detectDevice, type DeviceInfo, type Platform } from './device';
import { APP_VERSION, planFor, scenariosOf, type DevicePlan } from './deviceplan';
import { askOnce, reasonOf, runQueue, type QueueEvent, type QueueOutcome } from './devicerun';
import { decodesH264 } from './h264check';
import { ConnectError } from './hubhttp';
import { FIND_TIMEOUT_MS, searchHubs, type FindState, type Search } from './hubfinder';
import { HubControl, LOCAL_HUB, type HubState } from './hubprocess';
import { createI18n, minutesAndSeconds, type MessageKey, type Translate } from './i18n';
import { sampleLoss, sampleStats } from './metrics';
import { readNativeMetrics, spikeNative, type NativeMetrics } from './native';
import { completeFor, fetchProgress, remainingLines, type Progress } from './progress';
import { publish, ResultsQueue, SCENARIO_IDS, type InterruptReason, type KeyValueStore, type ScenarioId, type ScenarioResult } from './results';
import type { Clock, IntersectionWatch, ScenarioEnv, Stage } from './scenarios/context';
import { runScenario } from './scenarios/index';
import { NotStarted } from './scenarios/runner';
import { openSession } from './session';
import { checkForUpdate, RECHECK_MS, type UpdateOffer } from './update';
import { holdWakeLock, type ScreenWakeLock } from './wakelock';

/** Where the shells remember the hub's address. */
export const HUB_KEY = 'spike.hub';

/**
 * The address the field starts with. In a browser the app comes from the hub
 * itself, so it is the page's origin. The shells load the app from their own
 * origin (http://localhost, tauri.localhost), so they offer the address that
 * worked last time, or nothing.
 */
export function defaultHubUrl(platform: Platform, origin: string, remembered: string | null): string {
  if (platform === 'android' || platform === 'windows-tauri') return remembered ?? '';
  return origin;
}

/**
 * What the page shows when it cannot reach the hub: the address it tried, the
 * kind of failure and the hint for the usual causes (guest Wi-Fi, AP isolation,
 * a firewall on "Public"). Only the screen sees the address, never a result.
 */
export function connectErrorLines(error: unknown, t: Translate): string[] {
  if (!(error instanceof ConnectError)) {
    return [t('otherError', { detail: error instanceof Error ? error.message : String(error) })];
  }
  const kind =
    error.kind === 'timeout'
      ? t('kindTimeout')
      : error.kind === 'refused'
        ? t('kindRefused')
        : t('kindRejected', { detail: error.detail });
  return [t('connectFailed'), t('triedUrl', { url: error.url }), kind, t('connectHint')];
}

/** localStorage, or nothing where the page may not use it. */
function localStore(): KeyValueStore | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className = '', text = ''): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text) node.textContent = text;
  return node;
}

/**
 * The video area. While a run lasts it covers the viewport above the start
 * page (the `active` class, index.html): the status line, the text and the
 * yes/no question on top, the grid of tiles in the rest, the fullscreen element
 * over all of it.
 */
class DomStage implements Stage {
  readonly root = el('section', 'stage');
  private readonly banner = el('p', 'notice');
  private readonly status = el('p', 'status');
  private readonly gridEl = el('div', 'grid');
  private readonly fullEl = el('div', 'full');
  private readonly message = el('p', 'message');
  private readonly question = el('div', 'question');
  private readonly pool: HTMLVideoElement[] = [];
  private big: HTMLVideoElement | undefined;

  constructor() {
    this.root.append(this.banner, this.status, this.message, this.question, this.gridEl, this.fullEl);
    this.notice('');
    this.reset();
  }

  /**
   * The automatic run's banner over the stage (an interrupted scenario that
   * started again). It stays from run to run, until the automatic run clears it.
   */
  notice(text: string): void {
    this.banner.textContent = text;
    this.banner.hidden = text === '';
  }

  open(status: string): void {
    this.status.textContent = status;
    this.root.classList.add('active');
  }

  private video(): HTMLVideoElement {
    const video = el('video');
    video.muted = true;
    video.playsInline = true;
    return video;
  }

  grid(n: number): HTMLVideoElement[] {
    while (this.pool.length < n) this.pool.push(this.video());
    const shown = this.pool.slice(0, n);
    this.gridEl.replaceChildren(...shown);
    const columns = Math.max(1, Math.ceil(Math.sqrt(n)));
    this.gridEl.style.setProperty('--columns', String(columns));
    this.gridEl.style.setProperty('--rows', String(Math.max(1, Math.ceil(n / columns))));
    this.gridEl.hidden = n === 0;
    return shown;
  }

  fullscreen(): HTMLVideoElement {
    this.big ??= this.video();
    this.fullEl.replaceChildren(this.big);
    this.fullEl.hidden = false;
    return this.big;
  }

  exitFullscreen(): void {
    this.fullEl.hidden = true;
  }

  say(text: string): void {
    this.message.textContent = text;
    this.message.hidden = text === '';
  }

  ask(question: string, yes: string, no: string): Promise<boolean> {
    return new Promise((resolve) => {
      const answer = (value: boolean) => () => {
        this.question.replaceChildren();
        this.question.hidden = true;
        resolve(value);
      };
      const yesButton = el('button', '', yes);
      const noButton = el('button', '', no);
      yesButton.addEventListener('click', answer(true));
      noButton.addEventListener('click', answer(false));
      this.question.replaceChildren(el('p', '', question), yesButton, noButton);
      this.question.hidden = false;
    });
  }

  reset(): void {
    this.grid(0);
    this.exitFullscreen();
    this.say('');
    this.question.replaceChildren();
    this.question.hidden = true;
    this.status.textContent = '';
    this.root.classList.remove('active');
  }
}

/** How much of `element` is inside the viewport, from an IntersectionObserver; n/a where there is none. */
function watchIntersection(element: HTMLElement): IntersectionWatch | 'n/a' {
  if (typeof IntersectionObserver !== 'function') return 'n/a';
  let ratio = 0;
  const observer = new IntersectionObserver(
    (entries) => {
      const last = entries.at(-1);
      if (last) ratio = last.isIntersecting ? last.intersectionRatio : 0;
    },
    { threshold: [0, 0.25, 0.5, 0.75, 1] },
  );
  observer.observe(element);
  return {
    ratio: () => ratio,
    stop: () => {
      observer.disconnect();
    },
  };
}

/**
 * A scenario's state on the start page. `refused`: it did not start, `key`
 * says why (S3 on the charger, R48); `failed`: it threw (the hub could not be
 * reached, for example). `queued`: the automatic run ran it, and starts it
 * again by itself when it was interrupted.
 */
export type Status =
  | { kind: 'idle' }
  | { kind: 'running' }
  | { kind: 'done'; result: ScenarioResult; queued?: true }
  | { kind: 'failed'; detail: string }
  | { kind: 'refused'; key: MessageKey };

export interface App {
  autorun(request: AutorunRequest): Promise<void>;
}

/** Builds the start page in `root` and returns what main.ts drives. */
export function startApp(root: HTMLElement): App {
  const storage = localStore();
  const i18n = createI18n({ storage });
  const { t } = i18n;
  const device = detectDevice();
  const native = spikeNative();
  const stage = new DomStage();
  const canvas = el('canvas');
  const hubInput = el('input');
  hubInput.type = 'url';
  hubInput.autocomplete = 'off';
  hubInput.value = defaultHubUrl(device.platform, location.origin, safeGet(storage, HUB_KEY));
  const hub = (): string => hubInput.value.trim().replace(/\/+$/, '');
  const queue = new ResultsQueue({ storage, hubUrl: hub });
  const statuses = new Map<ScenarioId, Status>(SCENARIO_IDS.map((id) => [id, { kind: 'idle' }]));
  let connection: string[] = [];
  let connectionFailed = false;
  let busy = false;
  let unattended = false;
  let deviceLine = describeDevice(device);
  // The test app's own updates (update.ts): the newer release GitHub lists, and how its install goes.
  let updateOffer: UpdateOffer | undefined;
  let updateLine = '';
  let updating = false;
  // SPIKE-ONLY (hubprocess.ts): the hub that the Windows shell starts, and its panel, built once so that
  // a new line of the hub neither rebuilds the page nor takes the focus from the folder field.
  const hubControl = native?.hub
    ? new HubControl({
        shell: native.hub,
        storage,
        onChange: () => {
          drawHub();
        },
        onListening: () => {
          if (hub() === '') hubInput.value = LOCAL_HUB;
          if (!busy) void checkConnection();
        },
      })
    : undefined;
  const hubPanel = el('section', 'hub-process');
  const hubFolderInput = el('input');
  hubFolderInput.type = 'text';
  hubFolderInput.autocomplete = 'off';
  hubFolderInput.spellcheck = false;
  const hubStartButton = el('button', 'hub-start');
  const hubStopButton = el('button', 'hub-stop');
  const hubStatus = el('p', 'hub-status');
  const hubLog = el('pre', 'hub-log');
  // The automatic run (post-release task A): the device's list, what spikehub
  // holds, and what the run says on the page.
  const plan = planFor(device);
  let progress: Progress | undefined;
  let runLines: string[] = [];
  // SPIKE-ONLY (hubfinder.ts): Android looks for spikehub in the phone's Wi-Fi and lists what it finds.
  let findState: FindState | undefined;
  let search: Search | undefined;
  /** The box that shows runLines, so that a countdown can write its line in place. */
  let runStatus: HTMLElement | undefined;
  let onContinue: (() => void) | undefined;
  /** «Sari peste așteptare» while a wait of the run is offered to be skipped (devicerun.ts, offerSkip). */
  let onSkip: (() => void) | undefined;
  const attention = createAttention({
    audio: () => {
      const Context = (globalThis as { AudioContext?: new () => AudioOut }).AudioContext;
      return Context ? new Context() : undefined;
    },
    ...('vibrate' in navigator ? { vibrate: (pattern: number[]) => navigator.vibrate(pattern) } : {}),
    ...(native ? { native } : {}),
  });
  (globalThis as { __spikeResults?: ScenarioResult[] }).__spikeResults ??= [];
  (globalThis as ErrorsTarget).__spikeErrors ??= [];
  exposeBandReader(globalThis as BandReaderTarget, el('canvas'));
  const clock: Clock = {
    now: () => performance.now(),
    setTimer: (fn, ms) => {
      const id = setTimeout(fn, ms);
      return () => {
        clearTimeout(id);
      };
    },
  };
  // What the shell tells of itself: its model, which the run and the page's
  // marks count by (a results folder may hold another phone's or the
  // emulator's runs), and the charger, which an S3 of the phone waits on
  // (charger.ts). Read at the start, at the connection check and at the button.
  let shellModel: string | undefined;
  const chargerWatch = watchCharger(() => clock.now());
  const seeShell = (metrics: NativeMetrics | 'n/a'): void => {
    if (metrics === 'n/a') return;
    shellModel = metrics.model ?? shellModel;
    chargerWatch.see(metrics.charging);
  };
  const shellRead: Promise<void> = native ? readNativeMetrics(native, clock.setTimer).then(seeShell) : Promise.resolve();

  // Older browsers and some WebViews have no navigator.wakeLock, whatever lib.dom says.
  const wakeLock = (): ScreenWakeLock | undefined => ('wakeLock' in navigator ? navigator.wakeLock : undefined);

  /** The environment of a run against `hubUrl`; `queued` gives the automatic run's question and patience. */
  const environment = async (
    hubUrl: string,
    queued?: { ask: (question: string) => Promise<boolean> },
  ): Promise<ScenarioEnv> => {
    const cameras = await fetchCameras(hubUrl);
    safeSet(storage, HUB_KEY, hubUrl);
    return {
      hubUrl,
      cameras,
      device,
      t,
      stage,
      ...(unattended ? {} : { ask: queued?.ask ?? ((question: string) => stage.ask(question, t('yes'), t('no'))) }),
      ...(queued ? { waitForPersonMs: Infinity } : {}),
      ...(native ? { native } : {}),
      doc: document,
      clock,
      openSession,
      createPlayer,
      readBand: (video) => readBand(video, canvas),
      sampleStats,
      sampleLoss,
      watchIntersection,
      captureForDecoderInfo: () => captureForDecoderInfo(),
      holdWakeLock: () => holdWakeLock({ ...withWakeLock(wakeLock()), ...(native ? { native } : {}), doc: document }),
      jsHeapMb: () => {
        const memory = (performance as { memory?: { usedJSHeapSize?: number } }).memory;
        return memory?.usedJSHeapSize === undefined ? undefined : memory.usedJSHeapSize / 2 ** 20;
      },
      random: () => Math.random(),
    };
  };

  const render = (): void => {
    document.documentElement.lang = i18n.lang();
    const header = el('header');
    const language = el('button', 'language', t('otherLanguage'));
    language.disabled = busy;
    language.addEventListener('click', () => {
      i18n.setLang(i18n.lang() === 'ro' ? 'en' : 'ro');
      render();
    });
    const title = el('h1', '', t('appTitle'));
    title.append(' ', el('small', 'version', t('version', { version: APP_VERSION })));
    header.append(title, language);
    const updateSection = renderUpdate();
    drawHub();

    const hubSection = el('section', 'hub');
    const label = el('label', '', t('hubLabel'));
    label.append(hubInput);
    const check = el('button', '', t('check'));
    check.disabled = busy;
    check.addEventListener('click', () => {
      void checkConnection();
    });
    const connectionBox = el('div', connectionFailed ? 'connection failed' : 'connection');
    connectionBox.append(...connection.map((line) => el('p', '', line)));
    hubSection.append(label, check);
    if (native?.lanAddresses) {
      const find = el('button', 'find-hub', t('findHub'));
      find.disabled = busy || search !== undefined;
      find.addEventListener('click', () => {
        findHubs(false);
      });
      hubSection.append(find);
    }
    if (findState) {
      const found = el('div', 'found-hubs');
      found.append(...findLines(findState, t).map((line) => el('p', '', line)));
      for (const url of findState.kind === 'searching' || findState.kind === 'done' ? findState.found : []) {
        const pick = el('button', 'found-hub', url);
        pick.disabled = busy;
        pick.addEventListener('click', () => {
          useFoundHub(url);
        });
        found.append(pick);
      }
      hubSection.append(found);
    }
    hubSection.append(connectionBox, el('p', 'device', t('device', { device: deviceLine })));

    const completeInHub = progress ? completeFor(progress, device, shellModel) : new Set<ScenarioId>();
    const list = el('section', 'scenarios');
    list.append(el('h2', '', t('scenarios')), renderAutorun(completeInHub));
    for (const id of SCENARIO_IDS) {
      const status = statuses.get(id) ?? { kind: 'idle' };
      // A run that failed or was interrupted is marked like a failure: it needs another try.
      const row = el('div', `scenario ${offersRepeat(status) || status.kind === 'refused' ? 'failed' : status.kind}`);
      row.dataset.scenario = id;
      const button = el('button', '', t(offersRepeat(status) ? 'repeat' : 'start'));
      button.disabled = busy;
      button.addEventListener('click', () => {
        void runFromPage(id);
      });
      const text = statusText(status, t, completeInHub.has(id));
      row.append(el('strong', '', id), el('span', 'what', t(`desc${id}` as MessageKey)), el('span', 'status', text), button);
      list.append(row);
    }

    const pending = queue.pending();
    const pendingSection = el('section', 'pending');
    if (pending > 0) {
      const resend = el('button', '', t('resend'));
      resend.disabled = busy;
      resend.addEventListener('click', () => {
        void queue.flush().then(render);
      });
      pendingSection.append(el('p', '', t('pendingResults', { n: pending })), resend);
    }
    // The stage stays where it is: moving a playing video element would disturb it.
    controls.replaceChildren(header, updateSection, ...(hubControl ? [hubPanel] : []), hubSection, list, pendingSection);
  };

  /**
   * Brings the hub's panel up to date. Its log only outside a run: during one
   * the stage covers the page, and a scenario's measurements see no extra
   * layout; the render at the run's end draws it.
   */
  const drawHub = (): void => {
    if (!hubControl) return;
    const state = hubControl.state();
    hubPanel.querySelector('h2')?.replaceChildren(t('hubTitle'));
    hubPanel.querySelector('label > span')?.replaceChildren(t('hubFolderLabel'));
    hubStartButton.textContent = t('hubStart');
    hubStopButton.textContent = t('hubStop');
    hubStartButton.disabled = busy || hubControl.busy();
    hubStopButton.disabled = busy || (state.kind !== 'running' && state.kind !== 'starting');
    hubFolderInput.disabled = hubControl.busy();
    hubStatus.textContent = hubStatusText(state, t);
    hubStatus.className = `hub-status ${hubFailed(state) ? 'failed' : ''}`.trim();
    if (busy) return;
    const lines = hubControl.log.lines();
    const atEnd = hubLog.scrollTop + hubLog.clientHeight >= hubLog.scrollHeight - 4;
    hubLog.textContent = lines.length > 0 ? lines.join('\n') : t('hubLogEmpty');
    if (atEnd) hubLog.scrollTop = hubLog.scrollHeight;
  };

  /** The offer of a newer release, under the title; empty without one. Not during a run: installing ends the app. */
  const renderUpdate = (): HTMLElement => {
    const block = el('section', 'update');
    const updater = native?.update;
    if (!updateOffer || !updater) {
      block.hidden = true;
      return block;
    }
    const offer = updateOffer;
    const install = el('button', '', t('updateNow'));
    install.disabled = busy || updating;
    install.addEventListener('click', () => {
      updating = true;
      updateLine = t('updateDownloading');
      render();
      // The installer ends the app; the hub it started goes first, so that none is left running.
      (hubControl ? hubControl.stop() : Promise.resolve()).then(() => updater.install(offer)).then(
        (outcome) => {
          updating = false;
          updateLine = t(outcome === 'permission' ? 'updatePermission' : device.platform === 'android' ? 'updateConfirm' : 'updateInstalling');
          render();
        },
        (error: unknown) => {
          updating = false;
          console.warn('the update failed', error);
          updateLine = t('updateFailed', { detail: error instanceof Error ? error.message : String(error) });
          render();
        },
      );
    });
    block.append(el('p', '', t('updateAvailable', { version: offer.version, current: APP_VERSION })), install);
    if (updateLine) block.append(el('p', 'update-status', updateLine));
    return block;
  };

  /** Asks GitHub for a newer release; quietly does nothing when it cannot (offline, rate limit). */
  const lookForUpdate = async (): Promise<void> => {
    const updater = native?.update;
    if (!updater || unattended || updating) return;
    try {
      const offer = await checkForUpdate((url, init) => fetch(url, init), APP_VERSION, updater.asset);
      if (offer?.version !== updateOffer?.version) {
        updateOffer = offer;
        updateLine = '';
        render();
      }
    } catch (error) {
      console.warn('the update check failed', error);
    }
  };

  /** The automatic run's block, at the top of the scenario list; `completeInHub` is this device's, as its run counts it. */
  const renderAutorun = (completeInHub: ReadonlySet<ScenarioId>): HTMLElement => {
    const block = el('div', 'autorun');
    block.append(el('h3', '', t('autoTitle')));
    if (plan === undefined) {
      block.append(el('p', 'unknown', t('autoUnknown')));
    } else {
      const runAll = el('button', 'run-all', t('runAll'));
      const rerunAll = el('button', 'rerun-all', t('rerunAll'));
      runAll.disabled = busy;
      rerunAll.disabled = busy;
      runAll.addEventListener('click', () => {
        void runDevice(plan, false);
      });
      rerunAll.addEventListener('click', () => {
        void runDevice(plan, true);
      });
      block.append(
        el('p', 'plan', t('autoDevice', { label: plan.label, scenarios: scenariosOf(plan).join(', ') })),
        runAll,
        rerunAll,
        el('p', 'advice', t(plan.pc ? 'advicePc' : 'advicePhone')),
      );
    }
    if (progress) {
      const own = plan === undefined ? undefined : { plan, complete: completeInHub };
      block.append(...remainingLines(progress, t, own).map((line) => el('p', 'remaining', line)));
    }
    const lines = el('div', 'run-status');
    lines.append(...runLines.map((line) => el('p', '', line)));
    runStatus = lines;
    block.append(lines);
    if (onSkip) {
      const skip = onSkip;
      const button = el('button', 'skip-wait', t('skipWait'));
      button.addEventListener('click', () => {
        skip();
      });
      block.append(button, el('p', 'guide', t('skipWaitGuide')));
    }
    if (onContinue) {
      const proceed = onContinue;
      const go = el('button', 'continue', t('continue'));
      go.addEventListener('click', () => {
        proceed();
      });
      block.append(el('p', 'guide', t('s6Guide')), go);
    }
    return block;
  };

  /** Reads what spikehub holds, for «Rămase pe acest PC:»; nothing when it cannot be reached. */
  const refreshProgress = async (): Promise<void> => {
    if (hub() === '') return;
    try {
      await shellRead; // the shell's model first: the marks count by it
      progress = await fetchProgress(hub());
    } catch {
      return; // the connection check and the runs say why
    }
    render();
  };

  /** What the automatic run says on the page, and the banner over the stage. */
  const onQueueEvent = (event: QueueEvent): void => {
    const lines = queueEventLines(event, t);
    if (event.kind === 'resumed' || event.kind === 'retry') stage.notice(lines?.[0] ?? '');
    if (event.kind === 'result' && !event.result.interrupted) stage.notice('');
    if (lines === undefined) return;
    runLines = event.kind === 'resumed' || event.kind === 'retry' ? [...runLines.slice(-1), ...lines] : lines;
    // A countdown changes its line every second: written in place, since a
    // page drawn again under the finger would lose a tap on «Sari peste așteptare».
    const shown = runStatus?.isConnected ? [...runStatus.children] : [];
    if ((event.kind === 'pause' || event.kind === 'cold') && shown.length === runLines.length) {
      runLines.forEach((line, i) => {
        const p = shown[i];
        if (p) p.textContent = line;
      });
      return;
    }
    render();
  };

  // Picture-in-picture (S6, Android): the run waits until the person brings the app back.
  let inPip = false;
  const pipWaiters: (() => void)[] = [];
  native?.onPip?.((on) => {
    inPip = on;
    if (!on) for (const resolve of pipWaiters.splice(0)) resolve();
  });

  /**
   * Runs one scenario of the automatic run as the start page does, and shows
   * its status. `notes` (the waits skipped before it) join the result's notes
   * before it is stored.
   */
  const runQueued = async (
    id: ScenarioId,
    hubUrl: string,
    ask: (question: string) => Promise<boolean>,
    notes: readonly string[],
  ): Promise<ScenarioResult> => {
    statuses.set(id, { kind: 'running' });
    render();
    try {
      const measured = await runScenario(id, await environment(hubUrl, { ask }));
      const result = notes.length === 0 ? measured : { ...measured, notes: [...measured.notes, ...notes] };
      statuses.set(id, { kind: 'done', result, queued: true });
      await publish(result, queue);
      return result;
    } catch (error) {
      if (error instanceof NotStarted) statuses.set(id, { kind: 'refused', key: error.key });
      else statuses.set(id, { kind: 'failed', detail: error instanceof ConnectError ? error.kind : String(error) });
      throw error;
    } finally {
      render();
    }
  };

  /** «Rulează tot pentru acest dispozitiv», or with `rerunAll` «Rulează din nou tot» (devicerun.ts). */
  const runDevice = async (devicePlan: DevicePlan, rerunAll: boolean): Promise<void> => {
    attention.prime(); // in the tap: the sound of S5 and S6 comes later, without one
    search?.stop(); // its requests would share the Wi-Fi with the measurements
    busy = true;
    connection = [];
    connectionFailed = false;
    runLines = [t('autoStarting')];
    render();
    const hubUrl = hub();
    try {
      progress = await fetchProgress(hubUrl);
      safeSet(storage, HUB_KEY, hubUrl);
      if (native) seeShell(await readNativeMetrics(native, clock.setTimer));
      const complete = completeFor(progress, device, shellModel);
      const ask = askOnce((question) => stage.ask(question, t('yes'), t('no')));
      const outcome = await runQueue({
        plan: devicePlan,
        complete,
        rerunAll,
        doc: document,
        clock,
        runScenario: (id, notes) => runQueued(id, hubUrl, ask, notes),
        requestCamera: async () => {
          runLines = [t('autoCamera'), t(device.platform === 'android' ? 'mv4ExplainAndroid' : 'mv4Explain')];
          render();
          await grantCamera();
        },
        ...(devicePlan.platform === 'firefox' ? { preflight: async () => decodesH264(await environment(hubUrl)) } : {}),
        holdWakeLock: () => holdWakeLock({ ...withWakeLock(wakeLock()), ...(native ? { native } : {}), doc: document }),
        alert: () => {
          attention.alert();
        },
        confirm: () =>
          new Promise<void>((resolve) => {
            onContinue = () => {
              onContinue = undefined;
              render();
              resolve();
            };
            render();
          }),
        ...(native?.onPip
          ? {
              pip: {
                active: () => inPip,
                ended: () => (inPip ? new Promise<void>((resolve) => pipWaiters.push(resolve)) : Promise.resolve()),
              },
            }
          : {}),
        ...(native && devicePlan.platform === 'android'
          ? {
              charger: async () => {
                seeShell(await readNativeMetrics(native, clock.setTimer));
                return chargerWatch.state();
              },
            }
          : {}),
        offerSkip: () => {
          const pressed = new Promise<void>((resolve) => {
            onSkip = resolve;
          });
          render();
          return {
            pressed,
            withdraw: () => {
              onSkip = undefined;
              render();
            },
          };
        },
        show: onQueueEvent,
      });
      runLines = queueOutcomeLines(outcome, t);
    } catch (error) {
      if (error instanceof NotStarted) {
        runLines = [t(error.key), t('autoStopped')];
      } else {
        connection = connectErrorLines(error, t);
        connectionFailed = true;
        runLines = [t('autoStopped')];
      }
    } finally {
      stage.notice('');
      onContinue = undefined;
      onSkip = undefined;
      busy = false;
      render();
      void refreshProgress();
    }
  };

  /** A hub that the search found: it goes into the field, the search stops, and the connection is checked. */
  const useFoundHub = (url: string): void => {
    search?.stop();
    hubInput.value = url;
    void checkConnection();
  };

  /**
   * Looks for spikehub in the phone's Wi-Fi (hubfinder.ts). `atLaunch`: the
   * search that the app starts by itself, with an empty field, which takes the
   * hub when it finds exactly one and the field is still empty.
   */
  const findHubs = (atLaunch: boolean): void => {
    const lanAddresses = native?.lanAddresses;
    if (!lanAddresses || search) return;
    const started = searchHubs({
      lanAddresses: () => lanAddresses(),
      ask: async (url) => {
        await fetchProgress(url, FIND_TIMEOUT_MS);
      },
      show: (state) => {
        findState = state;
        render();
      },
    });
    search = started;
    render();
    void started.done.then((found) => {
      search = undefined;
      const [only] = found;
      if (atLaunch && found.length === 1 && only !== undefined && hub() === '' && !busy) useFoundHub(only);
      else render();
    });
  };

  const checkConnection = async (): Promise<void> => {
    busy = true;
    connection = [t('checking')];
    connectionFailed = false;
    render();
    try {
      const cameras = await fetchCameras(hub());
      const session = await openSession(hub(), 0);
      const pair = await session.pair();
      session.close();
      safeSet(storage, HUB_KEY, hub());
      const metrics = native ? await readNativeMetrics(native, clock.setTimer) : undefined;
      if (metrics !== undefined) seeShell(metrics);
      deviceLine = metrics === 'n/a' ? `${describeDevice(device)}, ${t('nativeNa')}` : describeDevice(device, metrics);
      connection = [
        t('connected', { cameras: simCameras(cameras).length, pair: pair === 'n/a' ? 'n/a' : `${pair.local}/${pair.remote}` }),
      ];
      void queue.flush().then(render);
      void refreshProgress();
    } catch (error) {
      connection = connectErrorLines(error, t);
      connectionFailed = true;
    } finally {
      busy = false;
      render();
    }
  };

  const run = async (id: ScenarioId, go: () => Promise<ScenarioResult>): Promise<void> => {
    search?.stop(); // its requests would share the Wi-Fi with the measurements
    busy = true;
    statuses.set(id, { kind: 'running' });
    connection = [];
    connectionFailed = false;
    render();
    try {
      statuses.set(id, { kind: 'done', result: await go() });
    } catch (error) {
      if (error instanceof NotStarted) {
        statuses.set(id, { kind: 'refused', key: error.key });
      } else {
        statuses.set(id, { kind: 'failed', detail: error instanceof ConnectError ? error.kind : String(error) });
        connection = connectErrorLines(error, t);
        connectionFailed = true;
      }
    } finally {
      busy = false;
      render();
    }
  };

  const runFromPage = (id: ScenarioId): Promise<void> =>
    run(id, async () => {
      const result = await runScenario(id, await environment(hub()));
      await publish(result, queue);
      return result;
    });

  if (hubControl) {
    const folderLabel = el('label');
    folderLabel.append(el('span'), hubFolderInput);
    hubStartButton.addEventListener('click', () => {
      void hubControl.start(hubFolderInput.value);
    });
    hubStopButton.addEventListener('click', () => {
      void hubControl.stop();
    });
    hubPanel.append(el('h2'), folderLabel, hubStartButton, hubStopButton, hubStatus, hubLog);
    // A remembered folder starts the hub by itself; not under autorun (CI), which brings its own hub.
    void hubControl.load().then(() => {
      hubFolderInput.value = hubControl.folder();
      if (!unattended && hubControl.remembered()) void hubControl.start(hubFolderInput.value);
    });
  }
  const controls = el('div', 'controls');
  root.replaceChildren(controls, stage.root);
  render();
  if (hub() !== '' && queue.pending() > 0) void queue.flush().then(render); // what the last launch could not send
  // With an empty field, the phone looks for the hub by itself; after the autorun's start, which brings its own hub.
  if (native?.lanAddresses) {
    setTimeout(() => {
      if (!unattended && hub() === '') findHubs(true);
    }, 0);
  }
  void refreshProgress();
  if (native?.update) {
    // After the start (an autorun sets `unattended` first), then every few hours while the app stays open.
    setTimeout(() => void lookForUpdate(), 3000);
    setInterval(() => void lookForUpdate(), RECHECK_MS);
  }

  return {
    autorun(request) {
      unattended = true;
      hubInput.value = request.hub;
      return run(request.scenario, () => autorun(request, { connect: environment, queue }));
    },
  };
}

/** What the page says of the search for the hub; the hubs it found follow as buttons. */
export function findLines(state: FindState, t: Translate): string[] {
  switch (state.kind) {
    case 'searching':
      return [t('findSearching', { networks: state.networks.join(', ') })];
    case 'done':
      return state.found.length === 0 ? [t('findNone', { networks: state.networks.join(', ') })] : [t('findFound')];
    case 'no-network':
      return [t('findNoNetwork')];
    case 'failed':
      return [t('findFailed', { detail: state.detail })];
  }
}

/** What the hub's panel says of its state. */
export function hubStatusText(state: HubState, t: Translate): string {
  switch (state.kind) {
    case 'stopped':
      return t('hubStopped');
    case 'starting':
      return t('hubStarting');
    case 'running':
      return t('hubRunning');
    case 'stopping':
      return t('hubStopping');
    case 'ended':
      return t('hubEnded', { code: state.code ?? '?' });
    case 'missing':
      return t('hubMissing');
    case 'failed':
      return state.detail === '' ? t('hubNoFolder') : t('hubFailed', { detail: state.detail });
  }
}

/** Whether the hub's state asks the person to do something. */
function hubFailed(state: HubState): boolean {
  return state.kind === 'ended' || state.kind === 'missing' || state.kind === 'failed';
}

function withWakeLock(wakeLock: ScreenWakeLock | undefined): { wakeLock?: ScreenWakeLock } {
  return wakeLock ? { wakeLock } : {};
}

function safeGet(storage: KeyValueStore | undefined, key: string): string | null {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function safeSet(storage: KeyValueStore | undefined, key: string, value: string): void {
  try {
    storage?.setItem(key, value);
  } catch {
    // Not remembered: the field starts empty next time.
  }
}

function describeDevice(device: DeviceInfo, metrics?: { model?: string; os?: string; webview?: string }): string {
  return [device.platform, metrics?.model, metrics?.os ?? device.os, metrics?.webview ?? device.webview]
    .filter((part): part is string => part !== undefined && part !== '')
    .join(', ');
}

/** The longest reason a «eșuat: …» status shows. */
const FAILED_DETAIL_MAX = 120;

/**
 * The reason of a run that ended with a `failed: <Name>: <message>` note (the
 * scenario threw, client-failed-shown-done): its message, shortened.
 */
function failedDetail(result: ScenarioResult): string | undefined {
  const note = result.notes.find((text) => text.startsWith('failed: '));
  if (note === undefined) return undefined;
  const detail = note.slice('failed: '.length).replace(/^\w*Error: /, '');
  return detail.length > FAILED_DETAIL_MAX ? `${detail.slice(0, FAILED_DETAIL_MAX - 1)}…` : detail;
}

/** Why a run was interrupted, in the person's language. */
function interruptionText(result: ScenarioResult, t: Translate): string {
  const reason = result.interruption;
  switch (reason?.kind) {
    case 'session-lost':
      return t('interruptedSessionLost');
    case 'charging':
      return t('interruptedCharging');
    case 'early-return':
      return t('interruptedEarlyReturn', { time: minutesAndSeconds(reason.backgroundS) });
    case 'pip-left':
      return t('interruptedPipLeft', { time: minutesAndSeconds(reason.pipS) });
    default:
      return t('statusInterrupted');
  }
}

/**
 * The status line of a scenario on the start page. A run that failed or was
 * interrupted never reads «gata». `completeInHub`: spikehub holds a complete
 * result of this device for it (from an earlier session or release), which an
 * idle row says.
 */
export function statusText(status: Status, t: Translate, completeInHub = false): string {
  switch (status.kind) {
    case 'idle':
      return t(completeInHub ? 'statusDoneInHub' : 'statusIdle');
    case 'running':
      return t('statusRunning');
    case 'failed':
      return t('statusFailed', { detail: status.detail });
    case 'refused':
      return t(status.key);
    case 'done': {
      if (status.result.interrupted) {
        // The automatic run starts it again by itself, or its stop line says what to do: no «Repetă» here.
        if (status.queued) return t('statusInterruptedAuto', { reason: queueReason(reasonOf(status.result), t) });
        return interruptionText(status.result, t);
      }
      const failed = failedDetail(status.result);
      return failed === undefined ? t('statusDone', { seconds: Math.round(status.result.durationS) }) : t('statusFailed', { detail: failed });
    }
  }
}

/** Whether the scenario's button reads «Repetă»: its run was interrupted or failed. */
export function offersRepeat(status: Status): boolean {
  return status.kind === 'done' && (status.result.interrupted || failedDetail(status.result) !== undefined);
}

/**
 * Why a scenario of the automatic run was interrupted, for its banner and the
 * line that stops the run. Without the start page's «Repetă» advice
 * (interruptionText): the run starts the scenario again by itself.
 */
function queueReason(reason: InterruptReason, t: Translate): string {
  switch (reason.kind) {
    case 'hidden':
      return t('autoReasonHidden');
    case 'session-lost':
      return t('autoReasonSessionLost');
    case 'charging':
      return t('autoReasonCharging');
    case 'early-return':
      return t('autoReasonEarlyReturn', { time: minutesAndSeconds(reason.backgroundS) });
    case 'pip-left':
      return t('autoReasonPipLeft', { time: minutesAndSeconds(reason.pipS) });
  }
}

/** What to do before pressing the button again, after a run stopped by the third interruption of `reason`. */
function againAfter(reason: InterruptReason): MessageKey {
  switch (reason.kind) {
    case 'hidden':
      return 'autoAgainHidden';
    case 'session-lost':
      return 'autoAgainSessionLost';
    case 'charging':
      return 'autoAgainCharging';
    case 'early-return':
    case 'pip-left':
      return 'autoAgainPerson';
  }
}

/**
 * What the page says for an event of the automatic run; undefined for an event
 * that changes no line (a result: the scenario's row shows it).
 */
export function queueEventLines(event: QueueEvent, t: Translate): string[] | undefined {
  switch (event.kind) {
    case 'preflight':
      return [t('autoPreflight')];
    case 'camera':
      return [t('autoCamera')];
    case 'scenario':
      return [t('autoScenario', { id: event.id, index: event.index, total: event.total })];
    case 'result':
      return undefined;
    case 'interrupted':
      return [t('autoInterrupted', { id: event.id, reason: queueReason(event.reason, t) })];
    case 'resumed':
      return [t('autoResumed', { id: event.id, count: event.count, reason: queueReason(event.reason, t) })];
    case 'retry':
      return [t('autoRetry', { id: event.id })];
    case 'pause':
      return [t('autoPause', { time: minutesAndSeconds(event.leftMs / 1000) })];
    case 'cold':
      return [t('autoCold', { id: event.id, time: minutesAndSeconds(event.leftMs / 1000) })];
    case 'attention':
      return [t('autoAttention', { id: event.id })];
    case 'confirm':
      return [t('autoAttention', { id: event.id })];
    case 'pip':
      return [t('autoPip')];
  }
}

/** What the page says when the automatic run ends. */
export function queueOutcomeLines(outcome: QueueOutcome, t: Translate): string[] {
  switch (outcome.kind) {
    case 'done':
      if (outcome.ran.length === 0) return [t('autoNothingLeft')];
      return outcome.failed.length === 0 ? [t('autoDone')] : [t('autoDone'), t('autoDoneFailed', { ids: outcome.failed.join(', ') })];
    case 'interrupted-too-often':
      return [t('autoTooManyInterruptions', { id: outcome.id, reason: queueReason(outcome.reason, t) }), t(againAfter(outcome.reason))];
    case 'no-h264':
      return [t('openh264'), `1. ${t('openh264Step1')}`, `2. ${t('openh264Step2')}`, `3. ${t('openh264Step3')}`];
  }
}
