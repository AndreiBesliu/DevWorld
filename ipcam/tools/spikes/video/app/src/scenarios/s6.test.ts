import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup, type FakeVideo } from './fakes';
import { runScenario } from './index';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S6', () => {
  it('S6_withoutThePipFacade_isNa', async () => {
    const t = setup({ fakePlayer: true });

    const result = await finish(runScenario('S6', t.env));

    expect(result.samples).toEqual({});
    expect(result.notes).toContain('n/a: picture-in-picture needs the Android shell');
    expect(t.sessions).toHaveLength(0);
  });

  it('S6_countsFramesInPipAndTheLongestGap_andIsNotInterrupted', async () => {
    let onPip: ((on: boolean) => void) | undefined;
    let entered = 0;
    const t = setup({
      fakePlayer: true,
      native: {
        metrics: () => Promise.resolve({}),
        enterPip: () => {
          entered += 1;
          onPip?.(true);
          return Promise.resolve();
        },
        onPip: (cb) => {
          onPip = cb;
        },
      },
    });

    const running = runScenario('S6', t.env);
    await vi.advanceTimersByTimeAsync(1000);
    expect(entered).toBe(1);
    t.doc.hide(); // whatever the WebView reports in PiP, S6 is not interrupted by it
    const video = t.stage.all[0] as FakeVideo;
    // 5 min of frames every 100 ms, with one gap of 2.5 s after the first minute.
    for (let ms = 0; ms < 300_000; ms += 100) {
      await vi.advanceTimersByTimeAsync(100);
      if (ms <= 60_000 || ms >= 62_500) video.present();
    }
    const result = await finish(running);

    expect(result.interrupted).toBe(false);
    expect(result.samples.pipMaxGapMs).toEqual([2500]);
    expect(result.samples.pipFrames?.[0]).toBeGreaterThan(2900);
    expect(result.samples.pipS).toEqual([300]);
  });

  it('S6_leavingPip_endsTheMeasurementAndCountsTheSilenceBeforeIt', async () => {
    let onPip: ((on: boolean) => void) | undefined;
    const t = setup({
      fakePlayer: true,
      native: {
        metrics: () => Promise.resolve({}),
        enterPip: () => {
          onPip?.(true);
          return Promise.resolve();
        },
        onPip: (cb) => {
          onPip = cb;
        },
      },
    });

    const running = runScenario('S6', t.env);
    await vi.advanceTimersByTimeAsync(1000);
    const video = t.stage.all[0] as FakeVideo;
    for (let ms = 0; ms < 50_000; ms += 100) {
      await vi.advanceTimersByTimeAsync(100);
      video.present();
    }
    await vi.advanceTimersByTimeAsync(10_000); // the video froze
    onPip?.(false); // and the person closed the PiP window
    const result = await finish(running);

    expect(result.samples.pipMaxGapMs?.[0]).toBeGreaterThanOrEqual(10_000);
    expect(result.samples.pipS?.[0]).toBeGreaterThanOrEqual(60);
    expect(result.samples.pipS?.[0]).toBeLessThan(62);
    expect(result.notes.some((note) => note.startsWith('left picture-in-picture after'))).toBe(true);
  });

  it('S6_withoutRequestVideoFrameCallback_isNa', async () => {
    const t = setup({
      fakePlayer: true,
      rvfc: false,
      native: { metrics: () => Promise.resolve({}), enterPip: () => Promise.resolve() },
    });

    const result = await finish(runScenario('S6', t.env));

    expect(result.samples).toEqual({});
    expect(result.notes).toContain('n/a: requestVideoFrameCallback is not available');
  });
});
