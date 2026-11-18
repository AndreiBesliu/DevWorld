import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { NativeMetrics } from '../native';
import { finish, setup } from './fakes';
import { runScenario } from './index';

/** The declarations of each rule in the page's style sheet, by selector. */
function pageStyles(): Map<string, Map<string, string>> {
  const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
  const css = /<style>([\s\S]*?)<\/style>/.exec(html)?.[1] ?? '';
  const rules = new Map<string, Map<string, string>>();
  for (const [, selector = '', body = ''] of css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const declarations = new Map<string, string>();
    for (const declaration of body.split(';')) {
      const [property = '', ...value] = declaration.split(':');
      if (property.trim() !== '') declarations.set(property.trim(), value.join(':').trim());
    }
    rules.set(selector.trim(), declarations);
  }
  return rules;
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S3', () => {
  it('S3_gridOfFour_statsEvery2sNativeEvery10sThenMainFullscreen', async () => {
    let released = 0;
    const metrics: NativeMetrics = { thermal: 'LIGHT', batteryPct: 81, memMb: 412, cpuPctTotal: 31.5 };
    const t = setup({
      fakePlayer: true,
      hub: {
        stats: { fps: 12, decoder: 'ExternalDecoder (MediaCodecVideoDecoder)', powerEfficient: true },
        loss: { rtpLost: 12, nack: 30, pli: 2, framesDropped: 5 },
      },
      native: { metrics: () => Promise.resolve(metrics) },
      overrides: {
        captureForDecoderInfo: () =>
          Promise.resolve(() => {
            released += 1;
          }),
      },
    });

    const result = await finish(runScenario('S3-4', t.env));

    expect(result.scenario).toBe('S3-4');
    const binds = t.session().binds.map(({ slot, cam, profile }) => `${slot}:${cam}/${profile}`);
    expect(binds).toEqual(['0:sim-01/sub', '1:sim-02/sub', '2:sim-03/sub', '3:sim-04/sub', '4:sim-01/main']);
    expect(t.session().slots).toBe(4); // slot 4 is the extra one, for the fullscreen main stream
    // 10 min of grid, a sample every 2 s; 2 min of main.
    expect(result.samples.gridFps).toHaveLength(300);
    expect(result.samples.mainFps).toHaveLength(60);
    for (const fps of result.samples.gridFps ?? []) expect(fps).toBeCloseTo(12, 0);
    expect(result.samples.tileFps).toHaveLength(4);
    expect(result.samples.tileDroppedPct).toEqual([0, 0, 0, 0]);
    expect(result.samples.tileHardware).toEqual([1, 1, 1, 1]);
    expect(result.samples.mainHardware).toEqual([1]);
    // Native metrics every 10 s: 60 in the grid, 12 with the main stream.
    expect(result.samples.thermal).toHaveLength(60);
    expect(result.samples.thermal?.[0]).toBe(1); // LIGHT
    expect(result.samples.batteryPct).toHaveLength(60);
    expect(result.samples.mainCpuPctTotal).toHaveLength(12);
    expect(result.samples.cpuPctApp).toBeUndefined(); // not reported, so not invented
    expect(result.notes).toContain('decoders: ExternalDecoder (MediaCodecVideoDecoder) ×4');
    expect(result.notes).toContain('main decoder: ExternalDecoder (MediaCodecVideoDecoder)');
    // The camera is held twice for MV4 and released each time; the screen said why first.
    expect(released).toBe(2);
    expect(t.stage.said).toContain(t.env.t('mv4Explain'));
    // R50: the run's loss counters, summed over its streams.
    expect(result.samples.rtpLost).toEqual([12]);
    expect(result.samples.nack).toEqual([30]);
    expect(result.samples.pli).toEqual([2]);
    expect(result.samples.framesDropped).toEqual([5]);
    expect(result.interrupted).toBe(false);
  });

  it.each([
    ['android', 'mv4ExplainAndroid'],
    ['windows-tauri', 'mv4Explain'],
    ['chrome', 'mv4Explain'],
  ] as const)('S3_mv4Explain_onlyThePhoneHearsAboutThePhonesVerdict (%s)', async (platform, key) => {
    const t = setup({
      fakePlayer: true,
      hub: { stats: { fps: 12, decoder: 'ExternalDecoder', powerEfficient: true } },
      native: { metrics: () => Promise.resolve({ thermal: 'NONE', batteryPct: 80, charging: false }) },
      overrides: {
        device: { platform, ua: 'Mozilla/5.0' },
        captureForDecoderInfo: () => Promise.resolve(() => undefined),
      },
    });

    await finish(runScenario('S3-4', t.env));

    expect(t.stage.said).toContain(t.env.t(key));
    expect(t.stage.said.filter((text) => /verdictul pentru telefon/.test(text))).toEqual(platform === 'android' ? [t.env.t(key), t.env.t(key)] : []);
  });

  it('S3_mv4Capture_onlyInTwoShortWindowsOutsideTheMV5AndMV6Windows', async () => {
    // R47: the camera runs at most 10 s before the grid's measurement and at
    // most 10 s after the fullscreen one; battery, heat, CPU and fps are
    // measured without it.
    const windows: { from: number; to?: number; fullscreen: boolean; message?: string }[] = [];
    const live = (): boolean => windows.some((window) => window.to === undefined);
    const nativeWhileLive: number[] = [];
    const measuredStats: number[] = [];
    const t = setup({
      fakePlayer: true,
      hub: { stats: { fps: 12, decoder: 'ExternalDecoder', powerEfficient: true } },
      native: {
        metrics: () => {
          if (live()) nativeWhileLive.push(performance.now());
          return Promise.resolve({ thermal: 'NONE', batteryPct: 80, cpuPctTotal: 12 });
        },
      },
    });
    t.env.captureForDecoderInfo = () => {
      const message = t.stage.said.at(-1);
      const window: (typeof windows)[number] = {
        from: performance.now(),
        fullscreen: t.stage.big !== undefined,
        ...(message === undefined ? {} : { message }),
      };
      windows.push(window);
      return Promise.resolve(() => {
        window.to = performance.now();
      });
    };
    const sampleStats = t.env.sampleStats;
    // Chrome reports the decoder only while a capture is held.
    t.env.sampleStats = async (pc) => {
      const stats = await sampleStats(pc);
      if (live()) return stats;
      measuredStats.push(performance.now());
      return new Map(
        [...stats].map(([track, tile]) => [track, { framesDecoded: tile.framesDecoded, framesDropped: tile.framesDropped, fps: tile.fps }]),
      );
    };

    const result = await finish(runScenario('S3-4', t.env));

    expect(windows).toHaveLength(2);
    const [atGrid, afterFullscreen] = windows;
    for (const window of windows) expect((window.to ?? Number.POSITIVE_INFINITY) - window.from).toBeLessThanOrEqual(10_000);
    // No capture inside the MV5/MV6 windows: every stats read of the measurements comes after the first window and before the second.
    expect(measuredStats.length).toBeGreaterThan(300);
    expect(atGrid?.to).toBeLessThanOrEqual(Math.min(...measuredStats));
    expect(afterFullscreen?.from).toBeGreaterThanOrEqual(Math.max(...measuredStats));
    expect(nativeWhileLive).toEqual([]);
    // The decoders seen in each window are recorded.
    expect(result.samples.tileHardware).toEqual([1, 1, 1, 1]);
    expect(result.samples.mainHardware).toEqual([1]);
    expect(result.notes).toEqual(expect.arrayContaining(['decoders: ExternalDecoder ×4', 'main decoder: ExternalDecoder']));
    // Each time the screen says why the camera light turns on; the second time over the fullscreen video.
    expect(atGrid?.message).toBe(t.env.t('mv4Explain'));
    expect(afterFullscreen?.message).toBe(t.env.t('mv4Explain'));
    expect(atGrid?.fullscreen).toBe(false);
    expect(afterFullscreen?.fullscreen).toBe(true);
    expect(t.stage.said.at(-1)).toBe(''); // the text goes once the camera is released
    const styles = pageStyles();
    const overlay = styles.get('.stage .full');
    const messageOverOverlay = styles.get('.stage:has(> .full:not([hidden])) .message');
    expect(overlay?.get('position')).toBe('fixed');
    expect(messageOverOverlay?.get('position')).toBe('fixed');
    expect(Number(messageOverOverlay?.get('z-index'))).toBeGreaterThan(Number(overlay?.get('z-index') ?? 0));
  });

  it('S3_sessionLostInTheGridWindow_isInterruptedWithoutFps', async () => {
    // R49: the hub's PC slept or the Wi-Fi dropped; frozen counters must not become a measured MV3/MV5.
    const t = setup({ fakePlayer: true, hub: { stats: { fps: 12 } } });

    const running = runScenario('S3-9', t.env);
    await vi.advanceTimersByTimeAsync(5 * 60_000);
    t.session().setState('closed');
    const result = await finish(running);

    expect(result.interrupted).toBe(true);
    expect(result.interruption?.kind).toBe('session-lost');
    expect(result.samples.gridFps).toBeUndefined();
    expect(result.samples.tileFps).toBeUndefined();
    expect(result.samples.mainFps).toBeUndefined();
    expect(t.session().binds.filter((bind) => bind.profile === 'main')).toEqual([]);
  });

  it('S3_playerStallInAMeasuredWindow_isNotedWithItsTime', async () => {
    const t = setup({ fakePlayer: true, hub: { stats: { fps: 12 } } });

    const running = runScenario('S3-4', t.env);
    await vi.advanceTimersByTimeAsync(180_000);
    t.players[2]?.emit('error', { reason: 'STALLED', cause: 'no frame for 3 s' });
    await vi.advanceTimersByTimeAsync(9 * 60_000); // into the fullscreen window
    t.players[4]?.emit('error', { reason: 'STALLED' });
    const result = await finish(running);

    const stalls = result.notes.filter((note) => note.includes('STALLED'));
    expect(stalls).toHaveLength(2);
    expect(stalls[0]).toMatch(/^tile 2: STALLED \(hint: no frame for 3 s\) 1(79|80) s into the grid window$/);
    expect(stalls[1]).toMatch(/^main: STALLED \d+ s into the fullscreen window$/);
    expect(result.interrupted).toBe(false);
  });

  it('S3_androidOnTheCharger_doesNotStart', async () => {
    // R48: a phone on the charger measures neither battery nor heat.
    const t = setup({
      fakePlayer: true,
      native: { metrics: () => Promise.resolve({ batteryPct: 100, charging: true }) },
      overrides: { device: { platform: 'android', ua: 'Mozilla/5.0 (Linux; Android 14; wv)' } },
    });

    // Settled at once into a value, so that the rejection is never left unhandled while the fake clock runs.
    const outcome = await finish(runScenario('S3-9', t.env).then(() => 'started', (error: unknown) => error));

    expect(outcome).toMatchObject({ name: 'NotStarted', key: 's3Charging' });

    expect(t.sessions).toHaveLength(0);
    expect(t.stage.active).toBe(false);
    expect(t.wakeLock.released).toBe(t.wakeLock.held);
    expect(t.env.t('s3Charging')).toBe('Scoateți telefonul din încărcător înainte de S3.');
  });

  it('S3_chargerConnectedDuringTheRun_endsItAsInterrupted', async () => {
    let charging = false;
    const t = setup({
      fakePlayer: true,
      hub: { stats: { fps: 12 } },
      native: { metrics: () => Promise.resolve({ batteryPct: 70, charging }) },
      overrides: { device: { platform: 'android', ua: 'Mozilla/5.0 (Linux; Android 14; wv)' } },
    });

    const running = runScenario('S3-9', t.env);
    await vi.advanceTimersByTimeAsync(4 * 60_000);
    charging = true;
    const result = await finish(running);

    expect(result.interrupted).toBe(true);
    expect(result.interruption?.kind).toBe('charging');
    expect(result.notes.some((note) => /^interrupted: the phone was on the charger \d+ s into the run$/.test(note))).toBe(true);
    expect(result.samples.charging?.at(0)).toBe(0);
    expect(result.samples.charging?.at(-1)).toBe(1);
    expect(result.samples.mainFps).toBeUndefined(); // it stopped in the grid
  });

  it('S3_tauriShell_recordsTheWebView2ProcessesOnItsSamples', async () => {
    // R52: the report needs webview2Procs ≥ 1 on the samples MV6 reads.
    const t = setup({
      fakePlayer: true,
      hub: { stats: { fps: 12 } },
      native: { metrics: () => Promise.resolve({ cpuPctTotal: 9, cpuPctApp: 4, memMb: 380, webview2Procs: 6 }) },
      overrides: { device: { platform: 'windows-tauri', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Edg/141.0.0.0' } },
    });

    const result = await finish(runScenario('S3-16', t.env));

    expect(result.samples.webview2Procs).toHaveLength(60);
    expect(result.samples.mainWebview2Procs).toHaveLength(12);
    expect(result.samples.webview2Procs?.[0]).toBe(6);
    expect(result.samples.charging).toBeUndefined();
  });

  it('S3_nativeMetricsThrow_theGridIsMeasuredAllTheSame', async () => {
    // A broken shell bridge costs the shell's numbers, not the run.
    const t = setup({
      fakePlayer: true,
      hub: { stats: { fps: 12 } },
      native: {
        metrics: () => {
          throw new Error('the bridge is gone');
        },
      },
    });

    const result = await finish(runScenario('S3-4', t.env));

    expect(result.samples.gridFps).toHaveLength(300);
    expect(result.samples.mainFps).toHaveLength(60);
    expect(result.samples.thermal).toBeUndefined();
    expect(result.notes).toContain('native metrics: n/a');
    expect(result.notes.filter((note) => note.startsWith('failed'))).toEqual([]);
  });

  it('S3_withoutNativeOrDecoderInfo_marksThemNa', async () => {
    let asked = 0;
    const t = setup({
      fakePlayer: true,
      hub: { stats: { fps: 12 } },
      overrides: {
        captureForDecoderInfo: () => {
          asked += 1;
          return Promise.resolve('n/a');
        },
      },
    });

    const result = await finish(runScenario('S3-9', t.env));

    expect(t.session().slots).toBe(9);
    expect(result.samples.tileFps).toHaveLength(9);
    expect(result.samples.tileHardware).toBeUndefined();
    expect(result.samples.thermal).toBeUndefined();
    expect(result.notes).toEqual(expect.arrayContaining(['native metrics: n/a', 'decoder info: n/a']));
    expect(result.notes.filter((note) => note === 'decoder info: n/a')).toHaveLength(1);
    expect(asked).toBe(1); // a refused or unanswered camera is not asked for a second time
  });
});
