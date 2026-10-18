import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, refusingPlayer, setup, unhandledRejections } from './fakes';
import { runScenario } from './index';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S4', () => {
  it('S4_nativeMetricsThrow_runsTheFull30Minutes', async () => {
    const t = setup({
      fakePlayer: true,
      native: {
        metrics: () => {
          throw new Error('the bridge is gone');
        },
      },
    });

    const result = await finish(runScenario('S4', t.env));

    expect(result.samples.pageSwitches).toEqual([29]);
    expect(result.samples.memMb).toBeUndefined();
    expect(result.notes.filter((note) => note.startsWith('failed'))).toEqual([]);
  });

  it('S4_playRejects_leavesNoUnhandledRejection', async () => {
    const t = setup({ fakePlayer: true, hub: { answer: () => ({ error: 'CODEC_UNSUPPORTED' }) } });
    t.env.createPlayer = (opts) => refusingPlayer(opts.slot);

    let notes: string[] = [];
    const unhandled = await unhandledRejections(async () => {
      notes = (await finish(runScenario('S4', t.env))).notes;
    });

    expect(unhandled).toEqual([]);
    expect(notes).toContain('failed: Error: no track for this slot');
  });

  it('S4_pagesEvery60sTogglesFullscreenEvery90sAndCountsUnprovokedErrors', async () => {
    let heap = 50;
    const t = setup({
      fakePlayer: true,
      native: { metrics: () => Promise.resolve({ memMb: 400 }) },
      overrides: { jsHeapMb: () => (heap += 0.5) },
    });

    const running = runScenario('S4', t.env);
    await vi.advanceTimersByTimeAsync(150_000); // a page of 4 tiles, after one of 9
    // A tile that is playing reports a stall: nothing in the scenario caused it.
    t.players[0]?.emit('error', { reason: 'STALLED' });
    // Tile 5 is off this page: what its player says is not about playback.
    t.players[5]?.emit('error', { reason: 'NO_FIRST_FRAME' });
    const result = await finish(running);

    expect(t.session().slots).toBe(9);
    // 30 min: a page change every 60 s (4 -> 9 -> 4 ...), fullscreen on or off every 90 s.
    expect(result.samples.pageSwitches).toEqual([29]);
    expect(result.samples.fullscreenToggles).toEqual([19]);
    // Memory every 30 s, from the start to the end.
    expect(result.samples.jsHeapMb).toHaveLength(61);
    expect(result.samples.memMb).toHaveLength(61);
    expect(result.samples.playerErrors).toEqual([1]);
    expect(result.notes).toContain('player error: STALLED');
    const tilesBound = t.session().binds.filter((bind) => bind.profile === 'sub');
    const mainBound = t.session().binds.filter((bind) => bind.profile === 'main');
    // 15 pages of 4 and 15 of 9 tiles; fullscreen on the extra slot 10 times.
    expect(tilesBound).toHaveLength(15 * 4 + 15 * 9);
    expect(mainBound).toHaveLength(10);
    for (const bind of mainBound) expect(bind.slot).toBe(9);
    expect(result.interrupted).toBe(false);
  });
});
