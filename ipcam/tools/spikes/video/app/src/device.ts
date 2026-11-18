/** The shells and browsers that Phase 0 measures. */
export type Platform = 'android' | 'windows-tauri' | 'chrome' | 'edge' | 'firefox';

/** What a result says about the device, as far as the page can tell. No addresses, names or serial numbers (PH0-22). */
export interface DeviceInfo {
  platform: Platform;
  ua: string;
  /** Operating system family. Its version is not here: the user agent freezes it, the native shell reports it. */
  os?: string;
  /** The engine or browser token of the user agent, for example `Chrome/141.0.0.0`. */
  webview?: string;
}

export interface DeviceEnvironment {
  userAgent: string;
  /** The page runs in the Tauri shell. */
  tauri?: boolean;
  /** `Capacitor.getPlatform()` when the page runs in the Capacitor shell. */
  capacitorPlatform?: string;
}

function readEnvironment(): DeviceEnvironment {
  const g = globalThis as { __TAURI_INTERNALS__?: unknown; __TAURI__?: unknown; Capacitor?: { getPlatform?: () => string } };
  const capacitorPlatform = g.Capacitor?.getPlatform?.();
  return {
    userAgent: navigator.userAgent,
    tauri: g.__TAURI_INTERNALS__ !== undefined || g.__TAURI__ !== undefined,
    ...(capacitorPlatform === undefined ? {} : { capacitorPlatform }),
  };
}

// Edge contains the Chrome token too, so the order matters.
const ENGINE_TOKENS = [/\bEdg\/[\d.]+/, /\bFirefox\/[\d.]+/, /\bChrome\/[\d.]+/];

const OS_FAMILIES: [RegExp, string][] = [
  [/Android/, 'Android'],
  [/Windows/, 'Windows'],
  [/Mac OS X|Macintosh/, 'macOS'],
  [/Linux|X11/, 'Linux'],
];

/**
 * Names the platform of a result. The shells are told apart by what they inject
 * into the page: WebView2 in Tauri calls itself Edge, and the Android WebView
 * calls itself Chrome with a `wv` mark. Anything else counts as Chrome unless it
 * says Edge or Firefox, which suits the Chromium and Firefox browsers of Phase
 * 0; the user agent is kept as it is, so a result from another engine shows.
 */
export function detectDevice(env: DeviceEnvironment = readEnvironment()): DeviceInfo {
  const ua = env.userAgent;
  const engine = ENGINE_TOKENS.map((token) => token.exec(ua)?.[0]).find((match) => match !== undefined);
  const os = OS_FAMILIES.find(([pattern]) => pattern.test(ua))?.[1];

  let platform: Platform = 'chrome';
  if (env.tauri === true) platform = 'windows-tauri';
  else if (env.capacitorPlatform === 'android' || /; wv\)/.test(ua)) platform = 'android';
  else if (/\bFirefox\//.test(ua)) platform = 'firefox';
  else if (/\bEdg\//.test(ua)) platform = 'edge';

  return {
    platform,
    ua,
    ...(os === undefined ? {} : { os }),
    ...(engine === undefined ? {} : { webview: engine }),
  };
}
