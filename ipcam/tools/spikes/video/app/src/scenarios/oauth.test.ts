import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { autorun, parseAutorun, type ErrorsTarget } from '../autorun';
import { fetchCameras } from '../cameras';
import type { DeviceInfo } from '../device';
import { createI18n } from '../i18n';
import type { SpikeNative } from '../native';
import { detectBridge, type BridgePlugins } from '../oauthbridge';
import { PENDING_KEY, publish, ResultsQueue, type KeyValueStore, type ResultsTarget } from '../results';
import { finish, setup } from './fakes';
import { runScenario } from './index';
import { runOAuth, type OAuthOutcome } from './oauth';

// The shells' plugins, as the bridge imports them: each test points them at its fakes.
const shell = vi.hoisted(() => ({ plugins: undefined as BridgePlugins | undefined }));
const current = (): BridgePlugins => {
  if (!shell.plugins) throw new Error('the test set no plugins');
  return shell.plugins;
};
vi.mock('@capacitor/browser', () => ({ Browser: { open: (options: { url: string }) => current().browser.open(options) } }));
vi.mock('@capacitor/app', () => ({
  App: { addListener: (event: 'appUrlOpen', listener: (e: { url: string }) => void) => current().app.addListener(event, listener) },
}));
vi.mock('@tauri-apps/plugin-opener', () => ({ openUrl: (url: string) => current().openUrl(url) }));
vi.mock('@fabianlars/tauri-plugin-oauth', () => ({
  start: (config?: { response?: string }) => current().oauth.start(config),
  cancel: (port: number) => current().oauth.cancel(port),
  onUrl: (listener: (url: string) => void) => current().oauth.onUrl(listener),
}));

/** uploadspike on the PC, as the owner enters it on the phone (http://<hub device>:7091). */
const HUB = 'http://192.168.1.20:7091';

const ANDROID: DeviceInfo = {
  platform: 'android',
  ua: 'Mozilla/5.0 (Linux; Android 15; SM-A245F Build/AP3A; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0.0.0 Mobile Safari/537.36',
};
const WINDOWS: DeviceInfo = { platform: 'windows-tauri', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Edg/141.0.3537.71' };
const BROWSER: DeviceInfo = { platform: 'chrome', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/141.0.0.0 Safari/537.36' };

const GOOGLE_AUTH = 'https://accounts.google.com/o/oauth2/v2/auth?client_id=test-client&state=S';

/** A request the app made to the hub. */
interface Call {
  method: string;
  path: string;
  query: URLSearchParams;
  init: RequestInit | undefined;
}

interface HubOptions {
  platform?: 'android' | 'windows' | 'web';
  plan?: 'A' | 'B1';
  /** What begin answers; the default is the plan's answer for the platform. */
  begin?: Record<string, unknown>;
  /** The status of next's answer. */
  nextStatus?: number;
  completeStatus?: number;
}

/** uploadspike's handoff API (tools/spikes/upload/handoff) as the app calls it. */
function fakeHub(options: HubOptions = {}) {
  const platform = options.platform ?? 'android';
  const plan = options.plan ?? 'A';
  const calls: Call[] = [];
  /** False once the LAN listener closed: every request fails as with a closed port. */
  let open = true;
  const beginAnswer = (query: URLSearchParams): Record<string, unknown> => {
    if (options.begin) return options.begin;
    if (plan === 'B1') {
      return { state: 'S', plan: 'B1', verificationUrl: 'https://www.google.com/device', userCode: 'TEST-CODE', expiresAt: '2026-11-18T12:10:00Z' };
    }
    const redirectUri =
      platform === 'android'
        ? 'ro.ipcam.spike:/oauth2redirect'
        : platform === 'windows'
          ? `http://127.0.0.1:${query.get('port') ?? ''}/`
          : 'http://localhost:7091/oauth/callback';
    return { state: 'S', plan: 'A', authUrl: GOOGLE_AUTH, redirectUri, expiresAt: '2026-11-18T12:10:00Z' };
  };
  const fetch = vi.fn<typeof globalThis.fetch>((input, init) => {
    const url = new URL(String(input));
    calls.push({ method: init?.method ?? 'GET', path: url.pathname, query: url.searchParams, init });
    if (!open) return Promise.reject(new TypeError('Failed to fetch'));
    switch (url.pathname) {
      case '/spike/v1/cameras':
        return Promise.resolve(Response.json([]));
      case '/spike/v1/oauth/next':
        if (options.nextStatus !== undefined) {
          return Promise.resolve(Response.json({ error: 'no login is awaited on this platform' }, { status: options.nextStatus }));
        }
        return Promise.resolve(Response.json({ dest: `g-${platform}`, provider: 'GOOGLE_DRIVE', plan }));
      case '/spike/v1/oauth/begin':
        return Promise.resolve(Response.json(beginAnswer(url.searchParams)));
      case '/spike/v1/oauth/complete':
        return Promise.resolve(
          Response.json(
            { destId: `g-${platform}`, provider: 'GOOGLE_DRIVE', platform, plan: 'A', clientType: 'android' },
            { status: options.completeStatus ?? 200 },
          ),
        );
      case '/spike/v1/results':
        return Promise.resolve(Response.json({ file: 'x.json' }, { status: 201 }));
    }
    return Promise.resolve(new Response('404 page not found', { status: 404 }));
  });
  return {
    fetch,
    calls,
    /** The calls to one path. */
    to: (path: string) => calls.filter((call) => call.path === path),
    /** The LAN listener closes (uploadspike closes it once a login of another device completes). */
    close: () => {
      open = false;
    },
    reopen: () => {
      open = true;
    },
  };
}

/**
 * The shells' plugins: Browser.open answers with `redirect` (the provider's
 * redirect to the app), through appUrlOpen on Android and the loopback
 * listener on Windows; `undefined` never redirects.
 */
function fakeShell(redirect: ((authUrl: string) => string) | undefined, onOpen: () => void = () => undefined) {
  const events: string[] = [];
  const appListeners: ((event: { url: string }) => void)[] = [];
  const loopListeners: ((url: string) => void)[] = [];
  const answer = (url: string, to: (redirected: string) => void): void => {
    onOpen();
    if (redirect) {
      const redirected = redirect(url);
      setTimeout(() => {
        to(redirected);
      }, 20_000); // the person signs in
    }
  };
  const plugins: BridgePlugins = {
    browser: {
      open: ({ url }) => {
        events.push('custom tab');
        answer(url, (redirected) => {
          for (const listener of [...appListeners]) listener({ url: redirected });
        });
        return Promise.resolve();
      },
    },
    app: {
      addListener: (_event, listener) => {
        appListeners.push(listener);
        return Promise.resolve({
          remove: () => {
            appListeners.splice(appListeners.indexOf(listener), 1);
            return Promise.resolve();
          },
        });
      },
    },
    oauth: {
      start: () => {
        events.push('loopback');
        return Promise.resolve(53123);
      },
      cancel: (port) => {
        events.push(`loopback cancelled ${port}`);
        return Promise.resolve();
      },
      onUrl: (listener) => {
        loopListeners.push(listener);
        return Promise.resolve(() => {
          loopListeners.splice(loopListeners.indexOf(listener), 1);
        });
      },
    },
    openUrl: (url) => {
      events.push('default browser');
      answer(url, (redirected) => {
        for (const listener of [...loopListeners]) listener(redirected);
      });
      return Promise.resolve();
    },
    openTab: (url) => {
      events.push('new tab');
      answer(url, () => undefined);
      return true;
    },
  };
  shell.plugins = plugins;
  return { plugins, events };
}

const signsIn =
  (code = 'C') =>
  (authUrl: string): string =>
    `ro.ipcam.spike:/oauth2redirect?state=${new URL(authUrl).searchParams.get('state') ?? ''}&code=${code}&scope=drive.file`;

/** runOAuth on `device`, its fake timers advanced until it ends. */
async function login(device: DeviceInfo, hub: ReturnType<typeof fakeHub>): Promise<OAuthOutcome> {
  return finish(runOAuth({ hub: HUB, bridge: detectBridge(device), fetch: hub.fetch, now: () => Date.now() }));
}

const memory = (): KeyValueStore => {
  const map = new Map<string, string>();
  return { getItem: (key) => map.get(key) ?? null, setItem: (key, value) => map.set(key, value) };
};

beforeEach(() => {
  vi.useFakeTimers();
  // The web bridge's new tab (window.open).
  vi.stubGlobal('open', (url: string) => (current().openTab(url) ? { opener: {} } : null));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  shell.plugins = undefined;
});

describe('runOAuth', () => {
  it('oauthScenario_android_opensCustomTabAndPostsCodeAndState', async () => {
    const hub = fakeHub();
    const app = fakeShell(signsIn());

    const outcome = await login(ANDROID, hub);

    expect(outcome).toEqual({ kind: 'completed' });
    expect(app.events).toEqual(['custom tab']);
    expect(hub.to('/spike/v1/oauth/next')[0]?.query.get('platform')).toBe('android');
    const begin = hub.to('/spike/v1/oauth/begin')[0]?.query;
    expect(begin && Object.fromEntries(begin)).toEqual({ dest: 'g-android', provider: 'GOOGLE_DRIVE', platform: 'android' });
    const [complete] = hub.to('/spike/v1/oauth/complete');
    expect(complete?.method).toBe('POST');
    expect(JSON.parse(String(complete?.init?.body))).toEqual({ state: 'S', code: 'C' });
  });

  it('oauthScenario_windows_beginCarriesLoopbackPort', async () => {
    const hub = fakeHub({ platform: 'windows' });
    const app = fakeShell((authUrl) => `http://127.0.0.1:53123/?code=C&state=${new URL(authUrl).searchParams.get('state') ?? ''}`);

    const outcome = await login(WINDOWS, hub);

    expect(outcome).toEqual({ kind: 'completed' });
    expect(app.events).toEqual(['loopback', 'default browser']);
    expect(hub.to('/spike/v1/oauth/next')[0]?.query.get('platform')).toBe('windows');
    expect(hub.to('/spike/v1/oauth/begin')[0]?.query.get('port')).toBe('53123');
    expect(JSON.parse(String(hub.to('/spike/v1/oauth/complete')[0]?.init?.body))).toEqual({ state: 'S', code: 'C' });
  });

  it('oauthScenario_b1_showsUserCodeAndPostsNothing', async () => {
    const hub = fakeHub({ plan: 'B1' });
    const app = fakeShell(signsIn());

    const outcome = await login(ANDROID, hub);

    expect(outcome).toEqual({ kind: 'device-code', verificationUrl: 'https://www.google.com/device', userCode: 'TEST-CODE' });
    // The hub polls the provider itself: the app opens nothing, starts no loopback and posts nothing.
    expect(app.events).toEqual([]);
    expect(hub.calls.map((call) => `${call.method} ${call.path}`)).toEqual(['GET /spike/v1/oauth/next', 'GET /spike/v1/oauth/begin']);
    expect(hub.to('/spike/v1/oauth/begin')[0]?.query.has('port')).toBe(false);
  });

  it('oauthScenario_payloadIsExactlyCodeAndState', async () => {
    // The redirect carries more (scope, authuser); the hub takes exactly code and state.
    const hub = fakeHub();
    fakeShell((authUrl) => `ro.ipcam.spike:/oauth2redirect?state=S&code=C&scope=drive.file&authuser=0&prompt=consent&x=${authUrl.length}`);

    await login(ANDROID, hub);

    const body = JSON.parse(String(hub.to('/spike/v1/oauth/complete')[0]?.init?.body)) as Record<string, unknown>;
    expect(Object.keys(body).sort()).toEqual(['code', 'state']);
  });

  it('oauthScenario_stateMismatch_postsNothing', async () => {
    const hub = fakeHub();
    fakeShell(() => 'ro.ipcam.spike:/oauth2redirect?state=X&code=C');

    expect(await login(ANDROID, hub)).toEqual({ kind: 'failed', reason: 'state-mismatch' });
    expect(hub.to('/spike/v1/oauth/complete')).toEqual([]);
  });

  it('oauthScenario_refusedSignIn_postsNothing', async () => {
    // The person pressed «Cancel», or the provider refused the client.
    const hub = fakeHub();
    fakeShell(() => 'ro.ipcam.spike:/oauth2redirect?state=S&error=access_denied');

    expect(await login(ANDROID, hub)).toEqual({ kind: 'failed', reason: 'denied' });
    expect(hub.to('/spike/v1/oauth/complete')).toEqual([]);
  });

  it('oauthScenario_noRedirectIn5Minutes_timesOut', async () => {
    // Google keeps custom schemes off by default and may show its error page
    // without redirecting (P52): the app stops waiting after 5 min.
    const hub = fakeHub({ platform: 'windows' });
    const app = fakeShell(undefined);
    const started = Date.now();

    expect(await login(WINDOWS, hub)).toEqual({ kind: 'failed', reason: 'timeout' });
    expect(Date.now() - started).toBeGreaterThanOrEqual(300_000);
    expect(Date.now() - started).toBeLessThan(302_000);
    expect(app.events).toEqual(['loopback', 'default browser', 'loopback cancelled 53123']);
    expect(hub.to('/spike/v1/oauth/complete')).toEqual([]);
  });

  it('oauthScenario_hubAnswersAnError_failsAndOpensNothing', async () => {
    // No login awaited on this platform (uploadspike answers 404), and a hub that refuses the code.
    const idle = fakeHub({ nextStatus: 404 });
    const app = fakeShell(signsIn());
    expect(await login(ANDROID, idle)).toEqual({ kind: 'failed', reason: 'hub' });
    expect(app.events).toEqual([]);

    const refusing = fakeHub({ completeStatus: 400 });
    fakeShell(signsIn());
    expect(await login(ANDROID, refusing)).toEqual({ kind: 'failed', reason: 'hub' });
  });

  it('oauthScenario_browserDoesNotOpen_fails', async () => {
    // tauri-plugin-opener refuses a URL outside the capability's scope.
    const hub = fakeHub({ platform: 'windows' });
    const app = fakeShell(signsIn());
    shell.plugins = { ...app.plugins, openUrl: () => Promise.reject(new Error('Not allowed to open url')) };

    expect(await login(WINDOWS, hub)).toEqual({ kind: 'failed', reason: 'browser' });
    expect(hub.to('/spike/v1/oauth/complete')).toEqual([]);
  });

  it('oauthScenario_webPlanA_opensATabAndLeavesTheRedirectToTheHub', async () => {
    // OneDrive's web leg (only if A5 passed): the redirect goes to uploadspike's
    // own /oauth/callback, which completes the login; the app only opens the page.
    const hub = fakeHub({ platform: 'web' });
    const app = fakeShell(signsIn());

    expect(await login(BROWSER, hub)).toEqual({ kind: 'opened' });
    expect(app.events).toEqual(['new tab']);
    expect(hub.to('/spike/v1/oauth/begin')[0]?.query.has('port')).toBe(false);
    expect(hub.to('/spike/v1/oauth/complete')).toEqual([]);
  });

  it('oauthScenario_requestsNeedNoPreflight', async () => {
    // Simple requests only: GETs without a header of their own, and a POST of
    // text/plain, so that neither WebView sends a CORS preflight.
    const hub = fakeHub();
    fakeShell(signsIn());

    await login(ANDROID, hub);

    for (const path of ['/spike/v1/oauth/next', '/spike/v1/oauth/begin']) {
      const [call] = hub.to(path);
      expect(call?.method, path).toBe('GET');
      expect([...new Headers(call?.init?.headers).keys()], path).toEqual([]);
    }
    const [complete] = hub.to('/spike/v1/oauth/complete');
    expect(complete?.method).toBe('POST');
    expect([...new Headers(complete?.init?.headers).entries()]).toEqual([['content-type', 'text/plain']]);
    expect(complete?.init?.credentials ?? 'same-origin').not.toBe('include');
  });
});

describe('the OAUTH scenario', () => {
  /**
   * The environment of an OAUTH run on `device` as the app builds it:
   * environment() lists the hub's cameras first (uploadspike answers []).
   */
  async function prepare(device: DeviceInfo, hub: ReturnType<typeof fakeHub>, more: Parameters<typeof setup>[0] = {}) {
    vi.stubGlobal('fetch', hub.fetch);
    const cameras = await fetchCameras(HUB);
    const t = setup({ ...more, overrides: { hubUrl: HUB, cameras, device, ...more.overrides } });
    return { t, cameras };
  }

  it('oauthScenario_runsThroughRunScenario', async () => {
    const hub = fakeHub();
    const { t, cameras } = await prepare(ANDROID, hub);
    // The Custom Tab hides the app's page while the person signs in.
    fakeShell(signsIn(), () => {
      t.doc.hide();
      setTimeout(() => {
        t.doc.show();
      }, 25_000);
    });

    const result = await finish(runScenario('OAUTH', t.env));

    expect(cameras).toEqual([]);
    expect(result.scenario).toBe('OAUTH');
    expect(result.interrupted).toBe(false);
    expect(result.notes).toEqual(['outcome: completed']);
    expect(t.stage.statuses).toEqual([`OAUTH: ${t.env.t('statusRunningShort')}`]);
    expect(t.stage.said).toEqual([t.env.t('oauthOpening'), t.env.t('oauthDone')]);
    expect(hub.to('/spike/v1/oauth/complete')).toHaveLength(1);
  });

  it('oauthScenario_deviceCode_staysOnTheStageUntilThePersonAnswers', async () => {
    const hub = fakeHub({ plan: 'B1' });
    fakeShell(undefined);
    let answer: (yes: boolean) => void = () => undefined;
    const asked: string[] = [];
    const { t } = await prepare(ANDROID, hub, {
      ask: (question) => {
        asked.push(question);
        return new Promise((resolve) => {
          answer = resolve;
        });
      },
    });
    const running = runScenario('OAUTH', t.env);
    let ended = false;
    void running.finally(() => {
      ended = true;
    });

    // The person reads the code, opens the address on any device and signs in.
    await vi.advanceTimersByTimeAsync(10 * 60_000);
    const shown = t.env.t('oauthDeviceCode', { url: 'https://www.google.com/device', code: 'TEST-CODE' });
    expect(t.stage.said.at(-1)).toBe(shown);
    expect(asked).toEqual([t.env.t('oauthCodeEntered')]);
    expect(t.stage.active).toBe(true);
    expect(ended).toBe(false);

    answer(true);
    const result = await finish(running);

    expect(t.stage.active).toBe(false);
    expect(result.notes).toEqual(['outcome: device-code, answer: yes']);
    expect(hub.to('/spike/v1/oauth/complete')).toEqual([]);
  });

  it('oauthScenario_deviceCode_noAnswerIn15Minutes_endsTheRun', async () => {
    const hub = fakeHub({ plan: 'B1' });
    fakeShell(undefined);
    const { t } = await prepare(ANDROID, hub, { ask: () => new Promise<boolean>(() => undefined) });

    const result = await finish(runScenario('OAUTH', t.env));

    expect(result.notes).toEqual(['outcome: device-code, answer: timeout']);
    expect(result.durationS).toBeGreaterThanOrEqual(900);
    expect(result.durationS).toBeLessThan(902);
  });

  it('oauthScenario_deviceCode_unattended_endsAtOnce', async () => {
    // Autorun: nobody reads the code.
    const hub = fakeHub({ plan: 'B1' });
    fakeShell(undefined);
    const { t } = await prepare(ANDROID, hub);

    const result = await finish(runScenario('OAUTH', t.env));

    expect(result.notes).toEqual(['outcome: device-code, answer: unattended']);
    expect(result.durationS).toBeLessThan(2);
    expect(t.stage.said.at(-1)).toBe(t.env.t('oauthDeviceCode', { url: 'https://www.google.com/device', code: 'TEST-CODE' }));
  });

  it('oauthScenario_failedLogin_saysWhyAndOffersRepeat', async () => {
    const hub = fakeHub();
    fakeShell(() => 'ro.ipcam.spike:/oauth2redirect?state=S&error=access_denied');
    const { t } = await prepare(ANDROID, hub);

    const result = await finish(runScenario('OAUTH', t.env));

    expect(t.stage.said.at(-1)).toBe(t.env.t('oauthFailed', { reason: t.env.t('oauthReasonDenied') }));
    expect(t.stage.said.at(-1)).toBe('Conectarea nu a reușit: autentificarea a fost refuzată.');
    // A `failed: …` note marks the run as failed on the start page, which then offers «Repetă».
    expect(result.notes).toEqual(['outcome: failed (denied)', 'failed: login denied']);
  });

  it('oauthScenario_neverRecordsTheCodeTheStateOrTheAddresses', async () => {
    // PH0-22: the code, the state, the user code and the URLs reach no note
    // and no result; only the screen shows the code and its address.
    const secret = (name: string): string => 'SENT' + `INEL-${name}`;
    const userCode = secret('user-code');
    const verificationUrl = `https://www.google.com/device?${secret('verification')}`;
    const planA = fakeHub({
      begin: { state: secret('state'), plan: 'A', authUrl: `${GOOGLE_AUTH}&${secret('auth')}`, redirectUri: 'ro.ipcam.spike:/oauth2redirect' },
    });
    fakeShell(() => `ro.ipcam.spike:/oauth2redirect?state=${secret('state')}&code=${secret('code')}`);
    const a = await prepare(ANDROID, planA);
    const resultA = await finish(runScenario('OAUTH', a.t.env));
    const b1 = fakeHub({ plan: 'B1', begin: { state: secret('state'), plan: 'B1', verificationUrl, userCode } });
    const b = await prepare(ANDROID, b1);
    const resultB = await finish(runScenario('OAUTH', b.t.env));

    expect(resultA.notes).toEqual(['outcome: completed']);
    expect(JSON.stringify([resultA, resultB])).not.toContain('SENT' + 'INEL');
    expect(b.t.stage.said.join('\n')).toContain(userCode);
  });

  it('oauthScenario_autorun_takesItsScenarioAtTheRunsReadBeforeTheBrowserOpens', async () => {
    // C1-R30: the Android shell hands the launch extras' scenario over at the
    // run's own read. OAUTH backgrounds the app on purpose (the Custom Tab), so
    // that read must come before the browser opens, as for every scenario.
    expect(parseAutorun(`?autorun=OAUTH&hub=${encodeURIComponent(HUB)}`, 'http://localhost')).toEqual({ scenario: 'OAUTH', hub: HUB });
    const order: string[] = [];
    const native: SpikeNative = {
      metrics: (read) => {
        order.push(read?.run === true ? 'run read' : 'read');
        return Promise.resolve({ model: 'samsung SM-A245F' });
      },
    };
    const hub = fakeHub();
    fakeShell(signsIn(), () => order.push('custom tab'));
    vi.stubGlobal('fetch', hub.fetch);
    const t = setup({ native, overrides: { hubUrl: HUB, cameras: [], device: ANDROID } });
    const target: ResultsTarget & ErrorsTarget = {};
    const queue = new ResultsQueue({ storage: memory(), fetch: hub.fetch, hubUrl: () => HUB });

    const result = await finish(autorun({ scenario: 'OAUTH', hub: HUB }, { connect: () => Promise.resolve(t.env), queue, target }));

    expect(order).toEqual(['run read', 'custom tab']);
    expect(result.notes).toEqual(['outcome: completed']);
    expect(target.__spikeResults).toEqual([result]);
    expect(target.__spikeErrors).toBeUndefined();
  });

  it('oauthResult_afterTheLanListenerClosed_isKeptAndSentAgain', async () => {
    // Task 9B's cannot-verify (b): uploadspike closes the LAN listener as soon
    // as the phone's login completes, so the phone's publish() right after the
    // run finds the port closed. The result stays in localStorage, the page
    // shows «Rezultate netrimise» with «Trimite acum», and every later flush
    // (the button, the next launch) sends it again.
    const hub = fakeHub();
    fakeShell(signsIn());
    const { t } = await prepare(ANDROID, hub);
    const result = await finish(runScenario('OAUTH', t.env));
    hub.close();
    const storage = memory();
    const queue = new ResultsQueue({ storage, fetch: hub.fetch, hubUrl: () => HUB });

    expect(await publish(result, queue, {})).toBe(1);
    expect(queue.pending()).toBe(1);
    expect(JSON.parse(storage.getItem(PENDING_KEY) ?? '[]')).toEqual([result]);
    const ro = createI18n({ lang: 'ro' });
    expect(ro.t('pendingResults', { n: queue.pending() })).toBe('Rezultate netrimise: 1. Se trimit din nou la fiecare pornire.');
    expect(await queue.flush()).toBe(1); // «Trimite acum» while the port is still closed: kept again

    // The listener is open again (the next login of another device, or the
    // owner points the app at a hub that listens): the next launch sends it.
    hub.reopen();
    const nextLaunch = new ResultsQueue({ storage, fetch: hub.fetch, hubUrl: () => HUB });
    expect(nextLaunch.pending()).toBe(1);
    expect(await nextLaunch.flush()).toBe(0);
    const posted = hub.to('/spike/v1/results').at(-1);
    expect(posted?.method).toBe('POST');
    expect(JSON.parse(String(posted?.init?.body))).toEqual(result);
  });
});
