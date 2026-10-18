import { afterEach, describe, expect, it, vi } from 'vitest';
import { detectDevice } from './device';

const CHROME_LINUX =
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';
const CHROME_WINDOWS =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';
const EDGE_WINDOWS = `${CHROME_WINDOWS} Edg/141.0.3537.57`;
const FIREFOX_WINDOWS = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:143.0) Gecko/20100101 Firefox/143.0';
const ANDROID_WEBVIEW =
  'Mozilla/5.0 (Linux; Android 14; K; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0.7390.43 Mobile Safari/537.36';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('detectDevice', () => {
  it('detectDevice_chromeOnLinux', () => {
    expect(detectDevice({ userAgent: CHROME_LINUX })).toEqual({
      platform: 'chrome',
      ua: CHROME_LINUX,
      os: 'Linux',
      webview: 'Chrome/141.0.0.0',
    });
  });

  it('detectDevice_edgeIsNotChrome', () => {
    expect(detectDevice({ userAgent: EDGE_WINDOWS })).toMatchObject({
      platform: 'edge',
      os: 'Windows',
      webview: 'Edg/141.0.3537.57',
    });
  });

  it('detectDevice_firefox', () => {
    expect(detectDevice({ userAgent: FIREFOX_WINDOWS })).toMatchObject({
      platform: 'firefox',
      os: 'Windows',
      webview: 'Firefox/143.0',
    });
  });

  it('detectDevice_capacitorOnAndroid_isAndroid', () => {
    expect(detectDevice({ userAgent: ANDROID_WEBVIEW, capacitorPlatform: 'android' })).toMatchObject({
      platform: 'android',
      os: 'Android',
      webview: 'Chrome/141.0.7390.43',
    });
  });

  it('detectDevice_androidWebViewMarkerWithoutCapacitor_isAndroid', () => {
    expect(detectDevice({ userAgent: ANDROID_WEBVIEW }).platform).toBe('android');
  });

  it('detectDevice_tauriOnWebView2_isWindowsTauriNotEdge', () => {
    // WebView2 identifies itself as Edge; the shell marker decides.
    expect(detectDevice({ userAgent: EDGE_WINDOWS, tauri: true })).toMatchObject({
      platform: 'windows-tauri',
      os: 'Windows',
      webview: 'Edg/141.0.3537.57',
    });
  });

  it('detectDevice_unknownBrowser_keepsTheUserAgentAndOmitsTheRest', () => {
    const ua = 'SomeBrowser/1.0';

    expect(detectDevice({ userAgent: ua })).toStrictEqual({ platform: 'chrome', ua });
  });

  it('detectDevice_neverReportsAnythingButTheUserAgentAndItsParts', () => {
    // PH0-22: no addresses, names or serial numbers in a result.
    expect(Object.keys(detectDevice({ userAgent: CHROME_LINUX })).sort()).toEqual(['os', 'platform', 'ua', 'webview']);
  });

  it('detectDevice_withoutArguments_readsTheNavigatorAndTheShellGlobals', () => {
    vi.stubGlobal('navigator', { userAgent: EDGE_WINDOWS });
    expect(detectDevice().platform).toBe('edge');

    vi.stubGlobal('__TAURI_INTERNALS__', {});
    expect(detectDevice().platform).toBe('windows-tauri');
    vi.unstubAllGlobals();

    vi.stubGlobal('navigator', { userAgent: CHROME_LINUX });
    vi.stubGlobal('Capacitor', { getPlatform: () => 'android' });
    expect(detectDevice().platform).toBe('android');
  });
});
