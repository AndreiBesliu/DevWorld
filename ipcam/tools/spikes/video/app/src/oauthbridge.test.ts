import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { detectDevice } from './device';
import { detectBridge, type BridgePlugins } from './oauthbridge';

const ANDROID_WEBVIEW = 'Mozilla/5.0 (Linux; Android 15; SM-A245F Build/AP3A; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0.0.0 Mobile Safari/537.36';
const WEBVIEW2 = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.3537.71';
const CHROME_WINDOWS = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';
const FIREFOX_WINDOWS = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0';

/** The Windows shell's files, read from the repository (apps/desktop/src-tauri). */
const SRC_TAURI = join(import.meta.dirname, '../../../../../apps/desktop/src-tauri');

/** Fake plugins: what each bridge called, and the listeners it registered. */
function fakePlugins() {
  const calls: string[] = [];
  const appListeners: ((event: { url: string }) => void)[] = [];
  const oauthListeners: ((url: string) => void)[] = [];
  const startConfigs: unknown[] = [];
  const plugins: BridgePlugins = {
    browser: {
      open: (options) => {
        calls.push(`browser.open ${options.url}`);
        return Promise.resolve();
      },
    },
    app: {
      addListener: (event, listener) => {
        calls.push(`app.addListener ${event}`);
        appListeners.push(listener);
        return Promise.resolve({
          remove: () => {
            calls.push('app.remove');
            appListeners.splice(appListeners.indexOf(listener), 1);
            return Promise.resolve();
          },
        });
      },
    },
    oauth: {
      start: (config) => {
        calls.push('oauth.start');
        startConfigs.push(config);
        return Promise.resolve(53123);
      },
      cancel: (port) => {
        calls.push(`oauth.cancel ${port}`);
        return Promise.resolve();
      },
      onUrl: (listener) => {
        calls.push('oauth.onUrl');
        oauthListeners.push(listener);
        return Promise.resolve(() => {
          calls.push('oauth.unlisten');
          oauthListeners.splice(oauthListeners.indexOf(listener), 1);
        });
      },
    },
    openUrl: (url) => {
      calls.push(`openUrl ${url}`);
      return Promise.resolve();
    },
    openTab: (url) => {
      calls.push(`openTab ${url}`);
      return true;
    },
  };
  return {
    plugins,
    calls,
    startConfigs,
    /** An appUrlOpen of the Android shell. */
    appUrlOpen: (url: string) => {
      for (const listener of [...appListeners]) listener({ url });
    },
    /** A request that reached tauri-plugin-oauth's loopback listener. */
    loopbackUrl: (url: string) => {
      for (const listener of [...oauthListeners]) listener(url);
    },
    listening: () => appListeners.length + oauthListeners.length,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('the OAuth bridge', () => {
  it('oauthBridge_detectsPlatform', () => {
    // device.ts's DeviceEnvironment seam: the shells by what they inject, a desktop browser as web.
    const platform = (userAgent: string, more: { tauri?: boolean; capacitorPlatform?: string } = {}): string =>
      detectBridge(detectDevice({ userAgent, ...more })).platform;

    expect(platform(ANDROID_WEBVIEW, { capacitorPlatform: 'android' })).toBe('android');
    expect(platform(WEBVIEW2, { tauri: true })).toBe('windows');
    expect(platform(CHROME_WINDOWS)).toBe('web');
    expect(platform(FIREFOX_WINDOWS)).toBe('web');
  });

  it('oauthBridge_android_opensACustomTabAndTakesTheAppUrlOpenAfterIt', async () => {
    const fake = fakePlugins();
    const bridge = detectBridge({ platform: 'android', ua: ANDROID_WEBVIEW }, { plugins: fake.plugins });

    expect(await bridge.startLoopback()).toBeNull();
    // A URL the system held from an earlier login (Capacitor keeps an
    // appUrlOpen until a listener takes it) is not this login's redirect.
    fake.appUrlOpen('ro.ipcam.spike:/oauth2redirect?state=old&code=old');
    await bridge.openSystemBrowser('https://accounts.google.com/o/oauth2/v2/auth?state=S');
    const redirect = bridge.waitForRedirect('ro.ipcam.spike:/oauth2redirect', 300_000);
    fake.appUrlOpen('https://example.org/other');
    fake.appUrlOpen('ro.ipcam.spike:/oauth2redirect?state=S&code=C');

    expect(await redirect).toBe('ro.ipcam.spike:/oauth2redirect?state=S&code=C');
    expect(fake.calls).toEqual([
      'app.addListener appUrlOpen',
      'browser.open https://accounts.google.com/o/oauth2/v2/auth?state=S',
      'app.remove',
    ]);
  });

  it('oauthBridge_windows_startsTheLoopbackWithTheOwnersPageAndOpensTheDefaultBrowser', async () => {
    const fake = fakePlugins();
    const bridge = detectBridge(
      { platform: 'windows-tauri', ua: WEBVIEW2 },
      { plugins: fake.plugins, browserPage: 'Răspunsul a ajuns la aplicație. <ok>' },
    );

    expect(await bridge.startLoopback()).toBe(53123);
    await bridge.openSystemBrowser('https://accounts.google.com/o/oauth2/v2/auth?state=S');
    const redirect = bridge.waitForRedirect('http://127.0.0.1:53123/', 300_000);
    fake.loopbackUrl('http://127.0.0.1:53123/?code=C&state=S');

    expect(await redirect).toBe('http://127.0.0.1:53123/?code=C&state=S');
    // The listener is registered before the loopback starts, so no request is missed.
    expect(fake.calls).toEqual(['oauth.onUrl', 'oauth.start', 'openUrl https://accounts.google.com/o/oauth2/v2/auth?state=S', 'oauth.unlisten']);
    // The page that the browser shows after the redirect: the owner's language,
    // escaped, in UTF-8 (the plugin's answer names no charset).
    const [config] = fake.startConfigs as [{ response: string }];
    expect(config.response).toContain('<meta charset="utf-8">');
    expect(config.response).toContain('Răspunsul a ajuns la aplicație. &lt;ok&gt;');
    expect(config.response).toMatch(/<head>/);
  });

  it('oauthBridge_windows_noRedirectInTime_rejectsAndStopsTheLoopback', async () => {
    const fake = fakePlugins();
    const bridge = detectBridge({ platform: 'windows-tauri', ua: WEBVIEW2 }, { plugins: fake.plugins });
    await bridge.startLoopback();

    const redirect = bridge.waitForRedirect('http://127.0.0.1:53123/', 300_000).catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(300_000);

    expect(await redirect).toBeInstanceOf(Error);
    expect(fake.calls).toContain('oauth.cancel 53123');
    expect(fake.listening()).toBe(0);
  });

  it('oauthBridge_windows_aSecondLoginStopsTheFirstLoopback', async () => {
    const fake = fakePlugins();
    const bridge = detectBridge({ platform: 'windows-tauri', ua: WEBVIEW2 }, { plugins: fake.plugins });

    await bridge.startLoopback();
    await bridge.startLoopback();

    expect(fake.calls).toEqual(['oauth.onUrl', 'oauth.start', 'oauth.unlisten', 'oauth.cancel 53123', 'oauth.onUrl', 'oauth.start']);
    expect(fake.listening()).toBe(1);
  });

  it('oauthBridge_failedOpen_rejects', async () => {
    // tauri-plugin-opener refuses a URL outside the capability's scope; a blocked tab on the web.
    const fake = fakePlugins();
    const windows = detectBridge(
      { platform: 'windows-tauri', ua: WEBVIEW2 },
      { plugins: { ...fake.plugins, openUrl: () => Promise.reject(new Error('Not allowed to open url')) } },
    );
    const web = detectBridge({ platform: 'chrome', ua: CHROME_WINDOWS }, { plugins: { ...fake.plugins, openTab: () => false } });

    await expect(windows.openSystemBrowser('https://accounts.google.com/x')).rejects.toThrow();
    await expect(web.openSystemBrowser('https://login.microsoftonline.com/x')).rejects.toThrow();
  });

  it('oauthBridge_web_opensATabAndHasNoLoopback', async () => {
    const fake = fakePlugins();
    const bridge = detectBridge({ platform: 'edge', ua: WEBVIEW2 }, { plugins: fake.plugins });

    expect(await bridge.startLoopback()).toBeNull();
    await bridge.openSystemBrowser('https://login.microsoftonline.com/x');
    // OneDrive's web redirect goes to uploadspike's own page, never to this one.
    await expect(bridge.waitForRedirect('http://localhost:7091/oauth/callback', 300_000)).rejects.toThrow();
    expect(fake.calls).toEqual(['openTab https://login.microsoftonline.com/x']);
  });
});

describe("the Windows shell's settings for the handoff", () => {
  it('tauriCsp_allowsHandoffPort', () => {
    const conf = JSON.parse(readFileSync(join(SRC_TAURI, 'tauri.conf.json'), 'utf8')) as { app: { security: { csp: string } } };
    const directives = conf.app.security.csp.split(';').map((directive) => directive.trim().split(/\s+/));
    const connect = directives.find(([name]) => name === 'connect-src') ?? [];

    // uploadspike on this PC (localhost, 127.0.0.1) and on another device of the LAN (the Pi).
    expect(connect).toEqual(expect.arrayContaining(['http://localhost:7091', 'http://127.0.0.1:7091', 'http://*:7091']));
    // spikehub's port stays.
    expect(connect).toEqual(expect.arrayContaining(['http://localhost:7080', 'http://127.0.0.1:7080', 'http://*:7080']));
  });

  it('tauriCapability_scopesOpenUrlToAuthHosts', () => {
    type Permission = string | { identifier: string; allow?: { url?: string }[]; deny?: unknown[] };
    const capability = JSON.parse(readFileSync(join(SRC_TAURI, 'capabilities/spike-oauth.json'), 'utf8')) as {
      windows: string[];
      permissions: Permission[];
    };
    const id = (permission: Permission): string => (typeof permission === 'string' ? permission : permission.identifier);

    expect(capability.windows).toEqual(['main']);
    expect(capability.permissions.map(id).sort()).toEqual(['core:default', 'oauth:allow-cancel', 'oauth:allow-start', 'opener:allow-open-url']);
    // The opener's only grant is open-url, scoped to the two authorization
    // hosts: without a scope it would refuse every URL, and opener:default
    // would allow any http(s), mailto and tel URL.
    const opener = capability.permissions.filter((permission) => id(permission).startsWith('opener:'));
    expect(opener).toEqual([
      {
        identifier: 'opener:allow-open-url',
        allow: [{ url: 'https://accounts.google.com/*' }, { url: 'https://login.microsoftonline.com/*' }],
      },
    ]);
  });
});
