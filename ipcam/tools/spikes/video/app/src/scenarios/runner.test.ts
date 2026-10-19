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
});
