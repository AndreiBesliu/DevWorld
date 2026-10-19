import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup } from './fakes';
import { runScenario } from './index';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('runner', () => {
  it('runner_stageIsShownOverThePageDuringTheRunAndTakenAwayAfter', async () => {
    // The stage covers the page while a scenario runs, so its video, text and question are on screen.
    const t = setup();
    expect(t.stage.active).toBe(false);

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(5000);
    expect(t.stage.active).toBe(true);
    expect(t.stage.statuses).toEqual([`S1: ${t.env.t('statusRunning')}`]);
    await finish(running);

    expect(t.stage.active).toBe(false);
  });

  it('runner_scenariosThatLeaveThePage_showAShortStatus', async () => {
    // S5 asks the person to press Home: «nu închideți ecranul» would contradict it.
    const t = setup({ fakePlayer: true, rvfc: false });

    await finish(runScenario('S5', t.env));

    expect(t.stage.statuses).toEqual([`S5: ${t.env.t('statusRunningShort')}`]);
  });

  it('runner_sessionLost_endsTheRunAsInterruptedWithoutMeasuredValues', async () => {
    // R49: the hub's PC went to sleep or the Wi-Fi dropped.
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(12_000); // a few starts measured
    t.session().jitter(4);
    t.session().setState('failed');
    const result = await finish(running);

    expect(result.interrupted).toBe(true);
    expect(result.interruption).toEqual({ kind: 'session-lost', atS: 12, how: 'connection failed' });
    expect(result.notes).toContain('interrupted: the hub session was lost 12 s into the run (connection failed)');
    expect(result.samples).toEqual({});
    expect(result.hubJitterMs).toEqual([]);
    expect(result.notes.filter((note) => note.startsWith('failed'))).toEqual([]);
    expect(t.session().binds.length).toBeLessThan(20); // it stopped there
  });

  it('runner_rpcChannelClosed_isASessionLoss', async () => {
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(5000);
    t.session().setState('closed');
    const result = await finish(running);

    expect(result.interruption).toEqual({ kind: 'session-lost', atS: 5, how: 'connection closed' });
  });

  it('runner_disconnectedForUpTo3s_isNotALoss', async () => {
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(5000);
    t.session().setState('disconnected');
    await vi.advanceTimersByTimeAsync(2900);
    t.session().setState('connected');
    const result = await finish(running);

    expect(result.interrupted).toBe(false);
    expect(result.samples.firstFrameMs).toHaveLength(20);
  });

  it('runner_disconnectedForMoreThan3s_isALoss', async () => {
    const t = setup();

    const running = runScenario('S1', t.env);
    await vi.advanceTimersByTimeAsync(5000);
    t.session().setState('disconnected');
    await vi.advanceTimersByTimeAsync(3100);
    t.session().setState('connected'); // too late
    const result = await finish(running);

    expect(result.interruption).toEqual({ kind: 'session-lost', atS: 8, how: 'connection disconnected for more than 3 s' });
    expect(result.samples).toEqual({});
  });

  it('runner_theRunsOwnTeardown_isNotALoss', async () => {
    // The runner closes the sessions at the end: that close is not a lost hub.
    const t = setup();

    const result = await finish(runScenario('S10', t.env));

    expect(t.session().closed).toBe(true);
    expect(result.interrupted).toBe(false);
    expect(result.interruption).toBeUndefined();
  });
});
