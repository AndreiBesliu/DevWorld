// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md
// The shells' part of MI6's login (phase0c2 plan, Task 9a): the app opens the
// provider's sign-in page in the system browser and takes the redirect that
// carries the code and the state back, for the hub to redeem (T11, T34). The
// PKCE verifier never leaves the hub; the page sees only the code and the
// state, and keeps neither (PH0-22).
//
// - Android: a Custom Tab (@capacitor/browser), and the redirect to the
//   custom scheme ro.ipcam.spike, which the spike flavour's intent filter
//   gives the Activity and @capacitor/app's appUrlOpen gives the page.
// - Windows: the default browser (@tauri-apps/plugin-opener, scoped to the
//   two authorization hosts in capabilities/spike-oauth.json), and
//   tauri-plugin-oauth's listener on 127.0.0.1, whose port goes into the
//   redirect URI.
// - Web: a new tab. No redirect reaches a web page here: OneDrive's web leg
//   redirects to uploadspike's own /oauth/callback, and Google on the web
//   always uses the hub's device flow (B1).
import { App } from '@capacitor/app';
import { Browser } from '@capacitor/browser';
import { cancel, onUrl, start } from '@fabianlars/tauri-plugin-oauth';
import { openUrl } from '@tauri-apps/plugin-opener';
import { detectDevice, type DeviceInfo } from './device';

export type OAuthPlatform = 'android' | 'windows' | 'web';

export interface OAuthBridge {
  readonly platform: OAuthPlatform;
  /**
   * Windows: starts tauri-plugin-oauth's loopback listener and returns its
   * port; android and web: null. On Android it starts listening for the
   * redirect (appUrlOpen), so call it before openSystemBrowser on every
   * platform.
   */
  startLoopback(): Promise<number | null>;
  /** Opens url in the system browser: Custom Tabs (@capacitor/browser) on Android, the default browser (plugin-opener) on Windows. */
  openSystemBrowser(url: string): Promise<void>;
  /**
   * The first redirect URL starting with prefix that arrives after
   * openSystemBrowser: appUrlOpen (@capacitor/app) on Android, the loopback
   * request on Windows. Rejects after timeoutMs, and at once on the web.
   */
  waitForRedirect(prefix: string, timeoutMs: number): Promise<string>;
}

/** The shells' plugins as the bridges use them; the tests replace them. */
export interface BridgePlugins {
  browser: { open(options: { url: string }): Promise<void> };
  app: {
    addListener(event: 'appUrlOpen', listener: (event: { url: string }) => void): Promise<{ remove(): Promise<void> }>;
  };
  oauth: {
    start(config?: { response?: string }): Promise<number>;
    cancel(port: number): Promise<void>;
    onUrl(listener: (url: string) => void): Promise<() => void>;
  };
  /** Windows: the default browser. */
  openUrl(url: string): Promise<void>;
  /** The web: a new tab; false when the browser blocked it. */
  openTab(url: string): boolean;
}

export interface BridgeOptions {
  /**
   * The page that the browser shows once the loopback listener has the
   * redirect (Windows): a line in the person's language. The plugin's own
   * page is English.
   */
  browserPage?: string;
  plugins?: BridgePlugins;
}

/** How long a loopback listener waits at most: the hub keeps a login's state 10 min (T11). */
const LOOPBACK_LIFE_MS = 10 * 60_000;

const defaultPlugins = (): BridgePlugins => ({
  browser: Browser,
  app: App,
  oauth: { start, cancel, onUrl },
  openUrl: (url) => openUrl(url),
  openTab: (url) => {
    const tab = globalThis.open(url, '_blank');
    if (tab === null) return false;
    tab.opener = null;
    return true;
  },
});

/** Text as HTML. */
const escapeHtml = (text: string): string =>
  text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** The loopback's page: the plugin puts its script after <head> and sends it without a charset. */
const loopbackPage = (text: string): string =>
  `<!doctype html><html><head><meta charset="utf-8"><title>ipcam</title></head><body><p>${escapeHtml(text)}</p></body></html>`;

/**
 * The redirects that arrive while a login waits: those before the browser
 * opened (a URL that Android kept from an earlier login, until a listener
 * took it) are dropped; the first one that starts with the awaited prefix
 * settles the wait.
 */
class Redirects {
  private opened = false;
  private readonly arrived: string[] = [];
  private waiter: ((url: string) => void) | undefined;
  private prefix = '';

  reset(): void {
    this.opened = false;
    this.arrived.length = 0;
    this.waiter = undefined;
  }

  markOpened(): void {
    this.opened = true;
  }

  push(url: string): void {
    if (!this.opened) return;
    if (this.waiter && url.startsWith(this.prefix)) this.waiter(url);
    else this.arrived.push(url);
  }

  /** The first redirect that starts with prefix, or the error after timeoutMs; `done` runs either way. */
  wait(prefix: string, timeoutMs: number, done: () => void): Promise<string> {
    return new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiter = undefined;
        done();
        reject(new Error(`no redirect within ${Math.round(timeoutMs / 1000)} s`));
      }, timeoutMs);
      const settle = (url: string): void => {
        clearTimeout(timer);
        this.waiter = undefined;
        done();
        resolve(url);
      };
      const early = this.arrived.find((url) => url.startsWith(prefix));
      if (early !== undefined) {
        settle(early);
        return;
      }
      this.prefix = prefix;
      this.waiter = settle;
    });
  }
}

function androidBridge(plugins: BridgePlugins): OAuthBridge {
  const redirects = new Redirects();
  let listening: Promise<{ remove(): Promise<void> }> | undefined;
  const stop = (): void => {
    const handle = listening;
    listening = undefined;
    void handle?.then((h) => h.remove()).catch(() => undefined);
  };
  return {
    platform: 'android',
    async startLoopback() {
      stop();
      redirects.reset();
      listening = plugins.app.addListener('appUrlOpen', ({ url }) => {
        redirects.push(url);
      });
      await listening;
      return null;
    },
    async openSystemBrowser(url) {
      redirects.markOpened();
      await plugins.browser.open({ url });
    },
    waitForRedirect(prefix, timeoutMs) {
      return redirects.wait(prefix, timeoutMs, stop);
    },
  };
}

function windowsBridge(plugins: BridgePlugins, page: string | undefined): OAuthBridge {
  const redirects = new Redirects();
  let unlisten: (() => void) | undefined;
  let port: number | undefined;
  let life: ReturnType<typeof setTimeout> | undefined;
  /** Stops listening, and stops the plugin's listener unless the redirect already ended it. */
  const stop = (listenerEnded: boolean): void => {
    clearTimeout(life);
    unlisten?.();
    unlisten = undefined;
    if (port !== undefined && !listenerEnded) void plugins.oauth.cancel(port).catch(() => undefined);
    port = undefined;
  };
  return {
    platform: 'windows',
    async startLoopback() {
      stop(false);
      redirects.reset();
      // The listener is registered before the loopback starts: no redirect is missed.
      unlisten = await plugins.oauth.onUrl((url) => {
        redirects.push(url);
      });
      port = await plugins.oauth.start(page === undefined ? undefined : { response: loopbackPage(page) });
      life = setTimeout(() => {
        stop(false);
      }, LOOPBACK_LIFE_MS);
      return port;
    },
    async openSystemBrowser(url) {
      redirects.markOpened();
      try {
        await plugins.openUrl(url);
      } catch (error) {
        stop(false);
        throw error;
      }
    },
    async waitForRedirect(prefix, timeoutMs) {
      let received = false;
      try {
        const url = await redirects.wait(prefix, timeoutMs, () => undefined);
        received = true;
        return url;
      } finally {
        // tauri-plugin-oauth's listener ends after the request that it handed over.
        stop(received);
      }
    },
  };
}

function webBridge(plugins: BridgePlugins): OAuthBridge {
  return {
    platform: 'web',
    startLoopback: () => Promise.resolve(null),
    openSystemBrowser(url) {
      return plugins.openTab(url) ? Promise.resolve() : Promise.reject(new Error('the browser blocked the new tab'));
    },
    waitForRedirect: () => Promise.reject(new Error('no redirect reaches a web page')),
  };
}

/**
 * The bridge of the shell that the page runs in, by device.ts's platform:
 * android → android, windows-tauri → windows, any browser → web. It reads
 * no globals of its own.
 */
export function detectBridge(device: DeviceInfo = detectDevice(), options: BridgeOptions = {}): OAuthBridge {
  const plugins = options.plugins ?? defaultPlugins();
  switch (device.platform) {
    case 'android':
      return androidBridge(plugins);
    case 'windows-tauri':
      return windowsBridge(plugins, options.browserPage);
    default:
      return webBridge(plugins);
  }
}
