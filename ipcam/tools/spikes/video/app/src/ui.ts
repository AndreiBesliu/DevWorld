// The start page: the hub's address, a connection check that says why the
// phone cannot reach the hub (Review Focus 1), the scenarios with a start
// button and a status each, the results not yet uploaded, and the stage on
// which the scenarios show their video. Romanian by default, English one tap away.
import { createPlayer } from '@ipcam/player';
import { autorun, type AutorunRequest, type ErrorsTarget } from './autorun';
import { readBand } from './band';
import { fetchCameras, simCameras } from './cameras';
import { captureForDecoderInfo } from './decoderinfo';
import { detectDevice, type DeviceInfo, type Platform } from './device';
import { ConnectError } from './hubhttp';
import { createI18n, type MessageKey, type Translate } from './i18n';
import { sampleStats } from './metrics';
import { readNativeMetrics, spikeNative } from './native';
import { publish, ResultsQueue, SCENARIO_IDS, type KeyValueStore, type ScenarioId, type ScenarioResult } from './results';
import type { Clock, ScenarioEnv, Stage } from './scenarios/context';
import { runScenario } from './scenarios/index';
import { openSession } from './session';
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

/** The video area: a grid of tiles, a fullscreen element over it, a line of text, a yes/no question. */
class DomStage implements Stage {
  readonly root = el('section', 'stage');
  private readonly gridEl = el('div', 'grid');
  private readonly fullEl = el('div', 'full');
  private readonly message = el('p', 'message');
  private readonly question = el('div', 'question');
  private readonly pool: HTMLVideoElement[] = [];
  private big: HTMLVideoElement | undefined;

  constructor() {
    this.root.append(this.message, this.question, this.gridEl, this.fullEl);
    this.reset();
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
    this.gridEl.style.setProperty('--columns', String(Math.max(1, Math.ceil(Math.sqrt(n)))));
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
  }
}

type Status = { kind: 'idle' } | { kind: 'running' } | { kind: 'done'; result: ScenarioResult } | { kind: 'failed'; detail: string };

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
  (globalThis as { __spikeResults?: ScenarioResult[] }).__spikeResults ??= [];
  (globalThis as ErrorsTarget).__spikeErrors ??= [];
  const clock: Clock = {
    now: () => performance.now(),
    setTimer: (fn, ms) => {
      const id = setTimeout(fn, ms);
      return () => {
        clearTimeout(id);
      };
    },
  };

  // Older browsers and some WebViews have no navigator.wakeLock, whatever lib.dom says.
  const wakeLock = (): ScreenWakeLock | undefined => ('wakeLock' in navigator ? navigator.wakeLock : undefined);

  const environment = async (hubUrl: string): Promise<ScenarioEnv> => {
    const cameras = await fetchCameras(hubUrl);
    safeSet(storage, HUB_KEY, hubUrl);
    return {
      hubUrl,
      cameras,
      device,
      t,
      stage,
      ...(unattended ? {} : { ask: (question: string) => stage.ask(question, t('yes'), t('no')) }),
      ...(native ? { native } : {}),
      doc: document,
      clock,
      openSession,
      createPlayer,
      readBand: (video) => readBand(video, canvas),
      sampleStats,
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
    header.append(el('h1', '', t('appTitle')), language);

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
    hubSection.append(label, check, connectionBox, el('p', 'device', t('device', { device: deviceLine })));

    const list = el('section', 'scenarios');
    list.append(el('h2', '', t('scenarios')));
    for (const id of SCENARIO_IDS) {
      const status = statuses.get(id) ?? { kind: 'idle' };
      const row = el('div', `scenario ${status.kind}`);
      row.dataset.scenario = id;
      const repeat = status.kind === 'done' && status.result.interrupted;
      const button = el('button', '', t(repeat ? 'repeat' : 'start'));
      button.disabled = busy;
      button.addEventListener('click', () => {
        void runFromPage(id);
      });
      row.append(el('strong', '', id), el('span', 'what', t(`desc${id}` as MessageKey)), el('span', 'status', statusText(status, t)), button);
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
    controls.replaceChildren(header, hubSection, list, pendingSection);
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
      deviceLine = metrics === 'n/a' ? `${describeDevice(device)}, ${t('nativeNa')}` : describeDevice(device, metrics);
      connection = [
        t('connected', { cameras: simCameras(cameras).length, pair: pair === 'n/a' ? 'n/a' : `${pair.local}/${pair.remote}` }),
      ];
      void queue.flush().then(render);
    } catch (error) {
      connection = connectErrorLines(error, t);
      connectionFailed = true;
    } finally {
      busy = false;
      render();
    }
  };

  const run = async (id: ScenarioId, go: () => Promise<ScenarioResult>): Promise<void> => {
    busy = true;
    statuses.set(id, { kind: 'running' });
    connection = [];
    connectionFailed = false;
    render();
    try {
      statuses.set(id, { kind: 'done', result: await go() });
    } catch (error) {
      statuses.set(id, { kind: 'failed', detail: error instanceof ConnectError ? error.kind : String(error) });
      connection = connectErrorLines(error, t);
      connectionFailed = true;
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

  const controls = el('div', 'controls');
  root.replaceChildren(controls, stage.root);
  render();
  if (hub() !== '' && queue.pending() > 0) void queue.flush().then(render); // what the last launch could not send

  return {
    autorun(request) {
      unattended = true;
      hubInput.value = request.hub;
      return run(request.scenario, () => autorun(request, { connect: environment, queue }));
    },
  };
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

function statusText(status: Status, t: Translate): string {
  switch (status.kind) {
    case 'idle':
      return t('statusIdle');
    case 'running':
      return t('statusRunning');
    case 'failed':
      return t('statusFailed', { detail: status.detail });
    case 'done':
      return status.result.interrupted ? t('statusInterrupted') : t('statusDone', { seconds: Math.round(status.result.durationS) });
  }
}
