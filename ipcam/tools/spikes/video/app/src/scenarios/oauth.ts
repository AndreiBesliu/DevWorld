// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md
// OAUTH, MI6's login on this device (G15, phase0c2 plan Task 9a), against
// uploadspike's handoff API on port 7091 (tools/spikes/upload/handoff): the
// app asks which login uploadspike awaits on this platform (next), begins it
// on the hub (begin), and then either opens the provider's sign-in page and
// hands the redirect's code and state to the hub (plan A), or shows the hub's
// device-flow code (B1), which the hub polls for itself. The PKCE verifier and
// the state are the hub's; the app keeps nothing (T11, T34).
//
// The requests are simple ones, so that neither WebView sends a CORS
// preflight: GETs without a header of their own, and complete as a POST of
// text/plain JSON, as results.ts posts its results.
//
// OAUTH is started from the list, like AGE: the device table's automatic run
// never counts it (deviceplan.ts TABLE_SCENARIOS). It backgrounds on purpose,
// like S5 and S6: the system browser hides the app's page.
//
// Notes: `outcome: completed`, `outcome: opened`, `outcome: failed (<reason>)`
// with a `failed: login <reason>` note, which marks the run as failed on the
// start page (it offers «Repetă»), or `outcome: device-code, answer: yes|no|
// timeout|unattended`. Never the code, the state, the user code or an address
// (PH0-22): the screen alone shows the device-flow code and its address.
import type { MessageKey } from '../i18n';
import { detectBridge, type OAuthBridge } from '../oauthbridge';
import type { ScenarioDefinition } from './runner';

export interface OAuthRunInput {
  hub: string;
  bridge: OAuthBridge;
  fetch: typeof fetch;
  now: () => number;
}

/**
 * Why a login failed:
 * - `state-mismatch`: the redirect's state is not the one the hub began with;
 * - `timeout`: no redirect came within 5 min (Google may show its error page
 *   for a custom scheme without redirecting, P52);
 * - `denied`: the redirect carries an error, or no code (the person refused);
 * - `hub`: uploadspike awaits no login here, or it, or the hub, refused;
 * - `browser`: the sign-in page could not be opened (the shell's opener
 *   refused it, the browser blocked the tab) or the loopback did not start.
 */
export type OAuthFailure = 'state-mismatch' | 'timeout' | 'denied' | 'hub' | 'browser';

/**
 * - `completed`: the hub has the code and redeemed it;
 * - `device-code`: B1, the person enters `userCode` at `verificationUrl`;
 * - `opened`: the web's plan A (OneDrive's web leg, only if A5 passed): the
 *   sign-in page is open in a new tab, whose redirect goes to uploadspike's
 *   own /oauth/callback, which completes the login and says so;
 * - `failed`.
 */
export type OAuthOutcome =
  | { kind: 'completed' }
  | { kind: 'device-code'; verificationUrl: string; userCode: string }
  | { kind: 'opened' }
  | { kind: 'failed'; reason: OAuthFailure };

/** How long the app waits for the provider's redirect. */
export const REDIRECT_TIMEOUT_MS = 5 * 60_000;

/** How long a call to the hub may take: uploadspike gives the hub's own call 40 s. */
const HUB_TIMEOUT_MS = 60_000;

/** How long the person has to enter the device-flow code and say so. */
export const CODE_ANSWER_TIMEOUT_MS = 15 * 60_000;

/** The login that uploadspike awaits on this platform (handoff.Next). */
interface Next {
  dest: string;
  provider: string;
  plan: string;
}

/** What the hub's begin answers (hubproc.BeginResult). */
interface Begun {
  state: string;
  plan: string;
  authUrl?: string;
  redirectUri?: string;
  verificationUrl?: string;
  userCode?: string;
}

const failed = (reason: OAuthFailure): OAuthOutcome => ({ kind: 'failed', reason });

const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null;
const text = (value: unknown): string | undefined => (typeof value === 'string' && value !== '' ? value : undefined);

function readNext(body: unknown): Next {
  const dest = isObject(body) ? text(body.dest) : undefined;
  const provider = isObject(body) ? text(body.provider) : undefined;
  const plan = isObject(body) ? text(body.plan) : undefined;
  if (dest === undefined || provider === undefined || plan === undefined) throw new Error('next: not a login');
  return { dest, provider, plan };
}

function readBegun(body: unknown): Begun {
  if (!isObject(body)) throw new Error('begin: not an object');
  const state = text(body.state);
  const plan = text(body.plan);
  if (state === undefined || plan === undefined) throw new Error('begin: no state or plan');
  const optional = (key: keyof Begun): Partial<Begun> => {
    const value = text(body[key]);
    return value === undefined ? {} : { [key]: value };
  };
  return {
    state,
    plan,
    ...optional('authUrl'),
    ...optional('redirectUri'),
    ...optional('verificationUrl'),
    ...optional('userCode'),
  };
}

/**
 * Calls the hub with `init` and reads its JSON answer; rejects on a network
 * failure, an HTTP error or after HUB_TIMEOUT_MS. The error says nothing of
 * the request (its URL holds the hub's address).
 */
async function callHub(request: typeof fetch, url: string, init: RequestInit): Promise<unknown> {
  const controller = new AbortController();
  const timer = setTimeout(() => {
    controller.abort();
  }, HUB_TIMEOUT_MS);
  try {
    const response = await request(url, { ...init, signal: controller.signal });
    if (!response.ok) throw new Error(`the hub answered ${response.status}`);
    return (await response.json()) as unknown;
  } finally {
    clearTimeout(timer);
  }
}

/** The redirect's query: its state, code and error. */
function redirectParams(url: string): URLSearchParams | undefined {
  try {
    return new URL(url).searchParams;
  } catch {
    return undefined;
  }
}

/**
 * Runs MI6's login on this device against uploadspike at `hub` and says how it
 * ended. It never throws, and nothing it returns or rejects with holds the
 * code or the state.
 */
export async function runOAuth(input: OAuthRunInput): Promise<OAuthOutcome> {
  const { fetch: request, bridge, now } = input;
  const base = input.hub.replace(/\/+$/, '');
  const platform = bridge.platform;

  let next: Next;
  try {
    next = readNext(await callHub(request, `${base}/spike/v1/oauth/next?${new URLSearchParams({ platform }).toString()}`, { method: 'GET' }));
  } catch {
    return failed('hub');
  }

  let port: number | null = null;
  if (next.plan !== 'B1') {
    try {
      port = await bridge.startLoopback();
    } catch {
      return failed('browser');
    }
  }

  const query = new URLSearchParams({ dest: next.dest, provider: next.provider, platform });
  if (port !== null) query.set('port', String(port));
  let begun: Begun;
  try {
    begun = readBegun(await callHub(request, `${base}/spike/v1/oauth/begin?${query.toString()}`, { method: 'GET' }));
  } catch {
    return failed('hub');
  }

  if (begun.plan === 'B1') {
    if (begun.verificationUrl === undefined || begun.userCode === undefined) return failed('hub');
    return { kind: 'device-code', verificationUrl: begun.verificationUrl, userCode: begun.userCode };
  }
  if (begun.authUrl === undefined || begun.redirectUri === undefined) return failed('hub');

  // The 5 min count from the hub's answer, the browser's opening included.
  const deadline = now() + REDIRECT_TIMEOUT_MS;
  try {
    await bridge.openSystemBrowser(begun.authUrl);
  } catch {
    return failed('browser');
  }
  if (platform === 'web') return { kind: 'opened' };

  let redirect: string;
  try {
    redirect = await bridge.waitForRedirect(begun.redirectUri, Math.max(0, deadline - now()));
  } catch {
    return failed('timeout');
  }
  const params = redirectParams(redirect);
  if (params === undefined || params.get('state') !== begun.state) return failed('state-mismatch');
  const code = params.get('code');
  if (params.has('error') || code === null || code === '') return failed('denied');

  try {
    await callHub(request, `${base}/spike/v1/oauth/complete`, {
      method: 'POST',
      headers: { 'Content-Type': 'text/plain' },
      body: JSON.stringify({ state: begun.state, code }),
    });
  } catch {
    return failed('hub');
  }
  return { kind: 'completed' };
}

/** The person's words for a failure. */
const REASON_KEYS: Record<OAuthFailure, MessageKey> = {
  'state-mismatch': 'oauthReasonStateMismatch',
  timeout: 'oauthReasonTimeout',
  denied: 'oauthReasonDenied',
  hub: 'oauthReasonHub',
  browser: 'oauthReasonBrowser',
};

export const OAUTH: ScenarioDefinition = {
  backgroundsOnPurpose: true,
  async run(run) {
    const { stage, t } = run.env;
    stage.say(t('oauthOpening'));
    const outcome = await runOAuth({
      hub: run.env.hubUrl,
      bridge: detectBridge(run.env.device, { browserPage: t('oauthBrowserPage') }),
      fetch: (resource, init) => fetch(resource, init),
      now: () => run.now(),
    });
    switch (outcome.kind) {
      case 'completed':
        stage.say(t('oauthDone'));
        run.note('outcome: completed');
        return;
      case 'opened':
        stage.say(t('oauthOpened'));
        run.note('outcome: opened');
        return;
      case 'failed':
        stage.say(t('oauthFailed', { reason: t(REASON_KEYS[outcome.reason]) }));
        run.note(`outcome: failed (${outcome.reason})`);
        run.note(`failed: login ${outcome.reason}`);
        return;
      case 'device-code': {
        // The stage, and so the code, stays until the person says they signed in.
        stage.say(t('oauthDeviceCode', { url: outcome.verificationUrl, code: outcome.userCode }));
        const ask = run.env.ask;
        const answer = ask ? await run.within(ask(t('oauthCodeEntered')), CODE_ANSWER_TIMEOUT_MS) : 'unattended';
        run.note(`outcome: device-code, answer: ${typeof answer === 'boolean' ? (answer ? 'yes' : 'no') : answer}`);
        return;
      }
    }
  },
};
