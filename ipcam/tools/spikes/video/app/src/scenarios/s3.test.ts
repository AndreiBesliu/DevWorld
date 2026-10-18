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
      hub: { stats: { fps: 12, decoder: 'ExternalDecoder (MediaCodecVideoDecoder)', powerEfficient: true } },
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
    // The camera is held for MV4 and released at the end; the screen said why first.
    expect(released).toBe(1);
    expect(t.stage.said).toContain(t.env.t('mv4Explain'));
  });

  it('S3_mainPhase_mv4ExplanationStaysOverTheFullscreenVideo', async () => {
    // The camera is still held while the main stream fills the screen, so the
    // reason for its light must stay readable over the fullscreen video.
    const t = setup({ fakePlayer: true, hub: { stats: { fps: 12 } }, overrides: { captureForDecoderInfo: () => Promise.resolve(() => undefined) } });

    const running = runScenario('S3-4', t.env);
    await vi.advanceTimersByTimeAsync(11 * 60_000); // into the 2 min of main
    expect(t.stage.big).toBeDefined();
    expect(t.stage.said.at(-1)).toBe(t.env.t('mv4Explain'));
    await finish(running);

    const styles = pageStyles();
    const overlay = styles.get('.stage .full');
    const messageOverOverlay = styles.get('.stage:has(> .full:not([hidden])) .message');
    expect(overlay?.get('position')).toBe('fixed');
    expect(messageOverOverlay?.get('position')).toBe('fixed');
    expect(Number(messageOverOverlay?.get('z-index'))).toBeGreaterThan(Number(overlay?.get('z-index') ?? 0));
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
    const t = setup({ fakePlayer: true, hub: { stats: { fps: 12 } } });

    const result = await finish(runScenario('S3-9', t.env));

    expect(t.session().slots).toBe(9);
    expect(result.samples.tileFps).toHaveLength(9);
    expect(result.samples.tileHardware).toBeUndefined();
    expect(result.samples.thermal).toBeUndefined();
    expect(result.notes).toEqual(expect.arrayContaining(['native metrics: n/a', 'decoder info: n/a']));
  });
});
