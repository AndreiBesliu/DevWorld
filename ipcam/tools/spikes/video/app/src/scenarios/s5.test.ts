import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup, type FakeVideo } from './fakes';
import { runScenario } from './index';

/** `count` frames of a live stream on `video`, `everyMs` apart, the first one now. */
async function liveFrames(video: FakeVideo, count: number, everyMs = 83): Promise<void> {
  for (let i = 0; i < count; i += 1) {
    if (i > 0) await vi.advanceTimersByTimeAsync(everyMs);
    video.present({ camera: 1, frame: 500 + i, loop: 0 });
  }
}

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
    await liveFrames(video, 4);
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
    await liveFrames(t.stage.all[0] as FakeVideo, 4);
    const result = await finish(running);

    expect(result.samples.resumeMs).toEqual([100]);
    expect(result.notes).toContain('background shorter than 2 min');
  });

  it.each([true, false])('S5_staleFrameAfterTheReturn_resumeIsTheFirstLiveFrame (receiveTime %s)', async (receiveTime) => {
    const t = setup({ fakePlayer: true, replaysLastFrame: true, receiveTime });

    const running = runScenario('S5', t.env);
    await vi.advanceTimersByTimeAsync(2000);
    const video = t.stage.all[0] as FakeVideo;
    t.doc.hide();
    video.pause();
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show(); // the scenario resumes the element, which shows its last frame again after 16 ms
    await vi.advanceTimersByTimeAsync(400);
    await liveFrames(video, 4);
    const result = await finish(running);

    expect(video.presented).toBe(6); // the first frame, its replay and the 4 live frames
    expect(result.samples.resumeMs).toEqual([400]);
  });

  it('S5_aLiveFrameThenAFreeze_resumeIsWhereFramesKeepComing', async () => {
    const t = setup({ fakePlayer: true });

    const running = runScenario('S5', t.env);
    await vi.advanceTimersByTimeAsync(2000);
    const video = t.stage.all[0] as FakeVideo;
    t.doc.hide();
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show();
    await vi.advanceTimersByTimeAsync(100);
    await liveFrames(video, 3, 400); // 3 frames, then nothing: only 2 follow the first within 1 s
    await vi.advanceTimersByTimeAsync(2000);
    await liveFrames(video, 4); // at 2900 ms, and 3 more within 1 s
    const result = await finish(running);

    expect(result.samples.resumeMs).toEqual([2900]);
  });

  it('S5_framesDoNotKeepComing_noResumeAndANote', async () => {
    const t = setup({ fakePlayer: true });

    const running = runScenario('S5', t.env);
    await vi.advanceTimersByTimeAsync(2000);
    t.doc.hide();
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show();
    await vi.advanceTimersByTimeAsync(100);
    await liveFrames(t.stage.all[0] as FakeVideo, 2, 400);
    const result = await finish(running);

    expect(result.samples.resumeMs).toBeUndefined();
    expect(result.notes).toContain('no steady live frames within 30 s of the return');
  });
});
