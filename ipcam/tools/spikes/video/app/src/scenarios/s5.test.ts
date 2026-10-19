import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ScenarioResult } from '../results';
import { finish, setup, type FakeHubOptions, type FakeVideo, type TestSetup } from './fakes';
import { runScenario } from './index';

/** `count` frames of a live stream on `video`, `everyMs` apart, the first one now. */
async function liveFrames(video: FakeVideo, count: number, everyMs = 83): Promise<void> {
  for (let i = 0; i < count; i += 1) {
    if (i > 0) await vi.advanceTimersByTimeAsync(everyMs);
    video.present({ camera: 1, frame: 500 + i, loop: 0 });
  }
}

/**
 * A setup whose hub shows the first frame of the first bind and nothing for
 * any later bind, so that the test presents the frames after the return itself.
 */
function quietAfterTheFirstBind(options: FakeHubOptions = {}): TestSetup {
  let binds = 0;
  return setup({ fakePlayer: true, hub: { ...options, firstFrameMs: () => (binds++ === 0 ? 300 : undefined) } });
}

/**
 * Starts S5, lets the stream start, and sends the app to the background.
 * Returns the run in an object: an async function would wait for it.
 */
async function startAndHide(t: TestSetup): Promise<{ running: Promise<ScenarioResult> }> {
  const running = runScenario('S5', t.env);
  await vi.advanceTimersByTimeAsync(2000);
  t.doc.hide(); // Home
  return { running };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S5', () => {
  it('S5_guideShowsTheEarliestReturnTimeOnTheClock', async () => {
    // integration-4: a clock that shows only minutes cannot time 2 minutes; the app says when to come back.
    vi.setSystemTime(new Date(2026, 9, 19, 14, 30, 20));
    const t = setup({ fakePlayer: true });

    const running = runScenario('S5', t.env);
    await vi.advanceTimersByTimeAsync(2000);

    expect(t.stage.said).toContain(t.env.t('s5Guide', { time: '14:33' }));
    expect(t.env.t('s5Guide', { time: '14:33' })).toBe('Apăsați butonul Acasă și reveniți după ora 14:33 (cel puțin 2 minute).');
    t.doc.hide();
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show();
    await finish(running);
  });

  it('S5_hiddenFor60s_releasesTheSlotAtOnceClosesTheSessionAndReopensItAtTheReturn', async () => {
    // R44, the spec's SUSPENDED state: slots freed within 1 s, the session closed after 60 s.
    const t = setup({ fakePlayer: true, hub: { fps: () => 12 } });

    const { running } = await startAndHide(t);
    const first = t.session();
    await vi.advanceTimersByTimeAsync(1000);
    expect(first.unbinds).toEqual([0]);
    expect(t.players[0]?.stops).toBeGreaterThan(0);
    expect(first.closed).toBe(false);
    first.setState('failed'); // the hub gave up on the frozen app: not a lost run, S5 is in the background
    await vi.advanceTimersByTimeAsync(60_000);
    expect(first.closed).toBe(true);
    expect(t.sessions).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(64_000);
    t.doc.show(); // back after 2 min 5 s
    const result = await finish(running);

    expect(t.sessions).toHaveLength(2);
    expect(t.sessions[1]?.binds.map(({ slot, cam, profile }) => `${slot}:${cam}/${profile}`)).toEqual(['0:sim-01/sub']);
    expect(result.notes).toEqual(
      expect.arrayContaining([
        'session: reopened',
        'connection at return: closed',
        'the app closed the session after 60 s in the background (connection failed then)',
      ]),
    );
    expect(result.samples.backgroundS).toEqual([125]);
    // From the return to the first live frame, the reconnect included: bind at once, frame at 300 + 120 ms.
    expect(result.samples.resumeMs).toEqual([420]);
    expect(result.interrupted).toBe(false);
    expect(result.interruption).toBeUndefined();
  });

  it('S5_backBefore60s_keepsTheSessionButIsInterruptedAsAnEarlyReturn', async () => {
    const t = setup({ fakePlayer: true, hub: { fps: () => 12 } });

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(40_000);
    t.doc.show();
    const result = await finish(running);

    expect(t.sessions).toHaveLength(1);
    expect(t.session().binds.map(({ slot }) => slot)).toEqual([0, 0]); // bound again on the same session
    expect(result.notes).toEqual(expect.arrayContaining(['session: kept', 'connection at return: connected']));
    expect(result.samples.resumeMs).toEqual([420]);
    expect(result.interrupted).toBe(true);
    expect(result.interruption).toEqual({ kind: 'early-return', backgroundS: 40 });
  });

  it.each(['failed', 'disconnected'] as const)('S5_connection_%s_atTheReturn_reopensTheSession', async (state) => {
    // integration-3: a session that died in the background is the expected path, not a resume failure.
    const t = setup({ fakePlayer: true, hub: { fps: () => 12 } });

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(20_000);
    t.session().setState(state);
    await vi.advanceTimersByTimeAsync(25_000);
    t.doc.show();
    const result = await finish(running);

    expect(t.sessions).toHaveLength(2);
    expect(t.sessions[0]?.closed).toBe(true);
    expect(result.notes).toEqual(expect.arrayContaining(['session: reopened', `connection at return: ${state}`]));
    expect(result.samples.resumeMs).toHaveLength(1);
    expect(result.interruption).toEqual({ kind: 'early-return', backgroundS: 45 });
  });

  it('S5_disconnectedForLessThan1sAtTheReturn_keepsTheSessionWhenItRecovers', async () => {
    const t = setup({ fakePlayer: true, hub: { fps: () => 12 } });

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(30_000);
    t.session().setState('disconnected');
    await vi.advanceTimersByTimeAsync(200);
    t.doc.show();
    await vi.advanceTimersByTimeAsync(300);
    t.session().setState('connected');
    const result = await finish(running);

    expect(t.sessions).toHaveLength(1);
    expect(result.notes).toEqual(expect.arrayContaining(['session: kept', 'connection at return: disconnected']));
  });

  it('S5_backgroundTimerNeverRan_theSessionIsClosedAtTheReturn', async () => {
    // A frozen app runs no timer in the background; the product's 60 s still apply.
    const t = setup({ fakePlayer: true, hub: { fps: () => 12 } });
    const { setTimer } = t.env.clock;
    t.env.clock = { ...t.env.clock, setTimer: (fn, ms) => (ms === 60_000 ? () => undefined : setTimer(fn, ms)) };

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(125_000);
    expect(t.sessions[0]?.closed).toBe(false);
    t.doc.show();
    const result = await finish(running);

    expect(t.sessions).toHaveLength(2);
    expect(result.notes).toEqual(
      expect.arrayContaining([
        'session: reopened',
        'the app closed the session at the return, after 125 s in the background (connection connected then)',
      ]),
    );
    expect(result.interrupted).toBe(false);
  });

  it('S5_backInLessThan2Minutes_isInterruptedWithTheTimeAway', async () => {
    // integration-4: an early return is repeated at once, not discovered in the report.
    const t = setup({ fakePlayer: true, hub: { fps: () => 12 } });

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(100_000);
    t.doc.show();
    const result = await finish(running);

    expect(result.interrupted).toBe(true);
    expect(result.interruption).toEqual({ kind: 'early-return', backgroundS: 100 });
    expect(result.notes).toEqual(
      expect.arrayContaining(['background shorter than 2 min', 'interrupted: back from the background after 100 s, at least 120 s are needed']),
    );
  });

  it('S5_aLiveFrameThenAFreeze_resumeIsWhereFramesKeepComing', async () => {
    const t = quietAfterTheFirstBind();

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show();
    await vi.advanceTimersByTimeAsync(100);
    const video = t.stage.all[0] as FakeVideo;
    await liveFrames(video, 3, 400); // 3 frames, then nothing: only 2 follow the first within 1 s
    await vi.advanceTimersByTimeAsync(2000);
    await liveFrames(video, 4); // at 2900 ms, and 3 more within 1 s
    const result = await finish(running);

    expect(result.samples.resumeMs).toEqual([2900]);
  });

  it.each([true, false])('S5_staleFrameAfterAKeptReturn_resumeIsTheFirstLiveFrame (receiveTime %s)', async (receiveTime) => {
    // The element shows the last frame from before the background again when the stream is attached again.
    let binds = 0;
    const t = setup({
      fakePlayer: true,
      replaysLastFrame: true,
      receiveTime,
      hub: { firstFrameMs: () => (binds++ === 0 ? 300 : undefined) },
    });

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(30_000);
    t.doc.show(); // the slot is bound again; the element shows its last frame again after 16 ms
    await vi.advanceTimersByTimeAsync(400);
    const video = t.stage.all[0] as FakeVideo;
    await liveFrames(video, 4);
    const result = await finish(running);

    expect(video.presented).toBe(6); // the first frame, its replay and the 4 live frames
    expect(result.samples.resumeMs).toEqual([400]);
  });

  it('S5_withoutRequestVideoFrameCallback_isNaInsteadOfNoFrame', async () => {
    const t = setup({ fakePlayer: true, rvfc: false });

    const result = await finish(runScenario('S5', t.env));

    // first-frame fires once per play, so it cannot time the return.
    expect(result.notes).toContain('resume: n/a (no requestVideoFrameCallback)');
    expect(result.notes).not.toContain('no frame within 30 s of the return');
    expect(result.samples).toEqual({});
    expect(t.sessions).toHaveLength(0);
  });

  it('S5_framesDoNotKeepComing_noResumeAndANote', async () => {
    const t = quietAfterTheFirstBind();

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show();
    await vi.advanceTimersByTimeAsync(100);
    await liveFrames(t.stage.all[0] as FakeVideo, 2, 400);
    const result = await finish(running);

    expect(result.samples.resumeMs).toBeUndefined();
    expect(result.notes).toContain('no steady live frames within 30 s of the return');
  });

  it('S5_noFrameAfterTheReturn_saysSo', async () => {
    const t = quietAfterTheFirstBind();

    const { running } = await startAndHide(t);
    await vi.advanceTimersByTimeAsync(125_000);
    t.doc.show();
    const result = await finish(running);

    expect(result.samples.resumeMs).toBeUndefined();
    expect(result.notes).toContain('no frame within 30 s of the return');
    expect(result.interrupted).toBe(false);
  });
});
