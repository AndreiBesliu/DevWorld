import { describe, expect, it } from 'vitest';
import { detectDevice } from './device';
import { APP_VERSION, DEVICE_PLANS, deviceKey, planFor, scenariosOf, type Step } from './deviceplan';

const CHROME_WINDOWS = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';
const EDGE_WINDOWS = `${CHROME_WINDOWS} Edg/141.0.3537.71`;
const FIREFOX_WINDOWS = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0';
const WEBVIEW2 = EDGE_WINDOWS;
const ANDROID_WEBVIEW = 'Mozilla/5.0 (Linux; Android 15; SM-A245F Build/AP3A; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0.0.0 Mobile Safari/537.36';
const CHROME_LINUX = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';
const CHROME_ANDROID = 'Mozilla/5.0 (Linux; Android 15; SM-A245F) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36';
const SAFARI_MAC = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 15_1) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15';
const FIREFOX_LINUX = 'Mozilla/5.0 (X11; Linux x86_64; rv:144.0) Gecko/20100101 Firefox/144.0';

const ids = (steps: readonly Step[]): string[] => steps.map((step) => (step.kind === 'pause' ? `pause ${step.ms / 60_000} min` : step.id));

describe('the device table (deviceplan.json)', () => {
  it('deviceTable_isTheOwnersTable', () => {
    // Post-release task A, requirement 2.
    expect(DEVICE_PLANS.map((plan) => [plan.id, plan.label, plan.platform, plan.pc, ids(plan.steps)])).toEqual([
      ['chrome', 'Chrome', 'chrome', true, ['S10', 'S1', 'S2', 'S3-9', 'S3-16', 'S4', 'S7', 'S9']],
      ['edge', 'Edge', 'edge', true, ['S10', 'S1', 'S2', 'S3-9', 'S9']],
      ['firefox', 'Firefox', 'firefox', true, ['S10', 'S1', 'S2', 'S3-9', 'S9']],
      ['ipcam-desktop', 'ipcam-desktop', 'windows-tauri', true, ['S10', 'S1', 'S2', 'S3-16', 'S4', 'S7', 'S9']],
      [
        'phone',
        'Galaxy A24',
        'android',
        false,
        ['S10', 'S1', 'S2', 'S3-9', 'pause 10 min', 'S3-4', 'S4', 'S5', 'S6', 'S7', 'S8', 'S9'],
      ],
    ]);
    expect(APP_VERSION).toBe('spike-video-v0.1.3');
  });

  it('scenariosOf_leavesOutThePause', () => {
    const phone = DEVICE_PLANS.find((plan) => plan.id === 'phone');
    expect(phone && scenariosOf(phone)).toEqual(['S10', 'S1', 'S2', 'S3-9', 'S3-4', 'S4', 'S5', 'S6', 'S7', 'S8', 'S9']);
  });
});

describe('the device the page runs on', () => {
  it('planFor_picksTheListOfTheDetectedPlatform', () => {
    const plan = (userAgent: string, more: { tauri?: boolean; capacitorPlatform?: string } = {}): string =>
      planFor(detectDevice({ userAgent, ...more }))?.id ?? 'none';

    expect(plan(CHROME_WINDOWS)).toBe('chrome');
    expect(plan(EDGE_WINDOWS)).toBe('edge');
    expect(plan(FIREFOX_WINDOWS)).toBe('firefox');
    expect(plan(WEBVIEW2, { tauri: true })).toBe('ipcam-desktop');
    expect(plan(ANDROID_WEBVIEW, { capacitorPlatform: 'android' })).toBe('phone');
  });

  it('planFor_unknownPlatform_hasNone', () => {
    // The guide runs the browsers on the owner's Windows PC; elsewhere they are other devices (hub-4).
    for (const userAgent of [CHROME_LINUX, CHROME_ANDROID, SAFARI_MAC, FIREFOX_LINUX]) {
      expect(planFor(detectDevice({ userAgent })), userAgent).toBeUndefined();
    }
  });

  it('deviceKey_isSpikehubsAndTheReportsKey', () => {
    // spikehub and the report key the results with resultrules.Key: the same cases as its TestKey.
    expect(deviceKey('chrome', 'Windows')).toBe('chrome');
    expect(deviceKey('chrome', 'Linux')).toBe('chrome-linux');
    expect(deviceKey('chrome', 'macOS')).toBe('chrome-macos');
    expect(deviceKey('firefox', undefined)).toBe('firefox');
    expect(deviceKey('edge', 'Android')).toBe('edge-android');
    expect(deviceKey('android', 'Android 15 (API 35)')).toBe('android');
    expect(deviceKey('windows-tauri', 'Windows 11')).toBe('windows-tauri');
    expect(deviceKey('chrome', '  Windows 11')).toBe('chrome');
    expect(deviceKey('edge', 'Chrome_OS 141')).toBe('edge-chromeos');
    expect(deviceKey('chrome', 'averyveryverylongosname 1')).toBe('chrome-averyveryverylon');
    expect(deviceKey('firefox', '--- 1')).toBe('firefox');
  });
});
