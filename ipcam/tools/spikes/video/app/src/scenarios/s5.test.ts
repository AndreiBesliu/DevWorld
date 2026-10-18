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

describe('S5', () => {
  it('S5_timesFromVisibleToTheNextFrame_andIsNotInterruptedByGoingHidden', async () => {
    const t = setup({ fakePlayer: true });

    const running = runScenario('S5', t.env);
    await vi.advanceTimersByTimeAsync(2000);
    expect(t.stage.said).toContain(t.env.t('s5Guide'));
    const video = t.stage.all[0] as FakeVideo;
    t.doc.hide(); // Home
    video.pause(); // the WebView pauses the element in the background
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show(); // back after 2 min 5 s
    await vi.advanceTimersByTimeAsync(800);
    expect(video.paused).toBe(false); // the scenario resumed the element
    video.present();
    const result = await finish(running);

    expect(result.interrupted).toBe(false); // S5 goes to the background on purpose
    expect(result.samples.resumeMs).toEqual([800]);
    expect(result.samples.backgroundS).toEqual([125]);
  });

  it('S5_backInLessThan2Minutes_saysSo', async () => {
    const t = setup({ fakePlayer: true });

    const running = runScenario('S5', t.env);
    await vi.advanceTimersByTimeAsync(2000);
    t.doc.hide();
    await vi.advanceTimersByTimeAsync(30_000);
    t.doc.show();
    await vi.advanceTimersByTimeAsync(100);
    (t.stage.all[0] as FakeVideo).present();
    const result = await finish(running);

    expect(result.samples.resumeMs).toEqual([100]);
    expect(result.notes).toContain('background shorter than 2 min');
  });
});
