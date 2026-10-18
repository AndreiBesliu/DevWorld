import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { NativeMetrics } from '../native';
import { finish, setup } from './fakes';
import { runScenario } from './index';

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
