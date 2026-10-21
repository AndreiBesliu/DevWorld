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

describe('S1', () => {
  it('S1_collects20FirstFrameSamples', async () => {
    // The hub shows the first frame of the i-th bind 300 + 10 i ms after it.
    const t = setup({ hub: { firstFrameMs: (i) => 300 + 10 * i } });

    const result = await finish(runScenario('S1', t.env));

    expect(result.scenario).toBe('S1');
    expect(result.interrupted).toBe(false);
    expect(result.samples.firstFrameMs).toEqual(Array.from({ length: 20 }, (_, i) => 300 + 10 * i));
    const session = t.session();
    expect(session.binds).toHaveLength(20);
    for (const bind of session.binds) expect(bind).toMatchObject({ slot: 0, cam: 'sim-01', profile: 'sub' });
    // Each start begins from an empty slot, 2 s after the previous camera left it.
    expect(session.unbinds.length).toBeGreaterThanOrEqual(19);
    for (let i = 1; i < 20; i += 1) {
      expect((session.binds[i]?.at ?? 0) - (session.binds[i - 1]?.at ?? 0)).toBeGreaterThanOrEqual(2000 + 300);
    }
    // Browsers autoplay only muted inline video: set before every play() (Task 7 notes).
    const video = t.stage.all[0] as FakeVideo;
    expect(video.playCalls.length).toBeGreaterThanOrEqual(20);
    for (const call of video.playCalls) expect(call).toEqual({ muted: true, playsInline: true });
    expect(session.closed).toBe(true);
  });

  it('S1_aFrameOfThePreviousBind_isNotTheFirstFrame', async () => {
    // A frame of the previous bind can still be presented after the slot is
    // bound again (Chrome did, when the previous stream was cut in the middle of
    // its cached-GOP burst). It is not the first frame of this bind: it arrived
    // before the bind, or, without receiveTime, it shows the previous band.
    for (const receiveTime of [true, false]) {
      const t = setup({ hub: { firstFrameMs: (i) => 300 + 10 * i }, receiveTime, replaysLastFrame: true });

      const result = await finish(runScenario('S1', t.env));

      expect(result.samples.firstFrameMs, `receiveTime ${receiveTime}`).toEqual(Array.from({ length: 20 }, (_, i) => 300 + 10 * i));
      vi.unstubAllGlobals();
    }
  });

  it('S1_bindRefused_recordsTheCodeAndNoSample', async () => {
    let n = 0;
    const t = setup({ hub: { answer: () => (n++ === 3 ? { error: 'UNKNOWN_CAMERA' } : { codec: 'H264' }) } });

    const result = await finish(runScenario('S1', t.env));

    expect(result.samples.firstFrameMs).toHaveLength(19);
    expect(result.notes).toContain('start 4: bind error UNKNOWN_CAMERA');
  });

  it('S1_recordsTheCodecOfTheBoundAndItsFallback', async () => {
    // A browser without Main profile in its offer gets the substream in its closest profile (R23).
    const t = setup({ hub: { answer: () => ({ codec: 'H264', fallback: '42e01f' }) } });

    const result = await finish(runScenario('S1', t.env));

    expect(result.notes).toContain('sub: H264, fallback profile 42e01f');
    expect(result.samples.firstFrameMs).toHaveLength(20); // a fallback is no failure
  });

  it('S1_recordsTheLossCountersOfTheRun', async () => {
    // R50: the report shows them next to MV1, so a slow start on a lossy Wi-Fi can be told apart.
    const t = setup({ hub: { loss: { rtpLost: 3, nack: 3, pli: 1, framesDropped: 0 } } });

    const result = await finish(runScenario('S1', t.env));

    expect(result.samples.rtpLost).toEqual([3]);
    expect(result.samples.nack).toEqual([3]);
    expect(result.samples.pli).toEqual([1]);
    expect(result.samples.framesDropped).toEqual([0]);
  });
});

describe('the run of a scenario', () => {
  it('scenario_visibilityHidden_marksInterrupted', async () => {
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(7000); // a few starts in
    t.doc.hide(); // the screen turned off
    const result = await finish(running);

    expect(result.interrupted).toBe(true);
    expect(result.samples.firstFrameMs?.length ?? 0).toBeLessThan(20);
    expect(result.notes.some((note) => note.startsWith('interrupted'))).toBe(true);
    // The run stops there: the app offers to repeat it instead of measuring a background tab.
    expect(t.session().binds.length).toBeLessThan(20);
    expect(t.session().closed).toBe(true);
  });

  it('runner_startedWhileHidden_isInterrupted', async () => {
    // An autorun intent sent while the phone's screen is off (Task 10).
    const t = setup();
    t.doc.hide();

    const result = await finish(runScenario('S1', t.env));

    expect(result.interrupted).toBe(true);
    expect(result.notes).toContain('interrupted: the page was hidden 0 s into the run');
    expect(result.samples.firstFrameMs).toBeUndefined();
  });

  it('runner_nativeMetricsThrow_naAndTheScenarioRuns', async () => {
    const t = setup({
      native: {
        metrics: () => {
          throw new Error('the bridge is gone');
        },
      },
    });

    const result = await finish(runScenario('S10', t.env));

    expect(result.notes).toEqual(['native metrics: n/a', 'pair: n/a', 'prompt: n/a']);
    expect(result.device).toEqual(t.env.device);
  });

  it('runner_nativeMetricsNeverAnswer_naAfter3s', async () => {
    const t = setup({ native: { metrics: () => new Promise(() => undefined) } });

    const running = runScenario('S10', t.env);
    await vi.advanceTimersByTimeAsync(2999);
    expect(t.sessions).toHaveLength(0); // still waiting for the shell
    await vi.advanceTimersByTimeAsync(1);
    expect(t.sessions).toHaveLength(1);
    const result = await finish(running);

    expect(result.notes).toEqual(['native metrics: n/a', 'pair: n/a', 'prompt: n/a']);
  });

  it('runner_collectsTheHubJitterOfTheRun', async () => {
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(10_000);
    t.session().jitter(3.5);
    await vi.advanceTimersByTimeAsync(10_000);
    t.session().jitter(24.25);
    const result = await finish(running);

    expect(result.hubJitterMs).toEqual([3.5, 24.25]);
  });

  it('runner_holdsAWakeLockWhileRunning', async () => {
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(1000);
    expect(t.wakeLock.held).toBe(1);
    expect(t.wakeLock.released).toBe(0);
    const result = await finish(running);

    expect(t.wakeLock.released).toBe(1);
    expect(result.device).toEqual(t.env.device);
    expect(Date.parse(result.startedAt)).not.toBeNaN();
    expect(result.durationS).toBeGreaterThan(40); // 20 starts with 2 s pauses
  });

  it('runner_nativeMetricsNameTheModel', async () => {
    const t = setup({
      native: { metrics: () => Promise.resolve({ model: 'SM-A245F', os: 'Android 14 (SDK 34)', webview: '141.0.7390.122' }) },
    });

    const result = await finish(runScenario('S10', t.env));

    expect(result.device).toEqual({
      ...t.env.device,
      model: 'SM-A245F',
      os: 'Android 14 (SDK 34)',
      webview: '141.0.7390.122',
    });
  });
});
