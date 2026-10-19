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

  it('S4_android_pages4And9TilesEvery60sTogglesFullscreenEvery90sAndCountsUnprovokedErrors', async () => {
    let heap = 50;
    const t = setup({
      fakePlayer: true,
      native: { metrics: () => Promise.resolve({ memMb: 400 }) },
      overrides: { jsHeapMb: () => (heap += 0.5), device: { platform: 'android', ua: 'Mozilla/5.0 (Linux; Android 14; wv)' } },
    });

    const running = runScenario('S4', t.env);
    await vi.advanceTimersByTimeAsync(150_000); // a page of 4 tiles, after one of 9
    // A tile that is playing reports a stall: nothing in the scenario caused it.
    t.players[0]?.emit('error', { reason: 'STALLED' });
    // Tile 5 is off this page: what its player says is not about playback.
    t.players[5]?.emit('error', { reason: 'NO_FIRST_FRAME' });
    const result = await finish(running);

    expect(t.session().slots).toBe(9);
    expect(result.samples.tiles).toEqual([9]);
    // 30 min: a page change every 60 s (4 -> 9 -> 4 ...), fullscreen on or off every 90 s.
    expect(result.samples.pageSwitches).toEqual([29]);
    expect(result.samples.fullscreenToggles).toEqual([19]);
    // Memory every 30 s, from the start to the end.
    expect(result.samples.jsHeapMb).toHaveLength(61);
    expect(result.samples.memMb).toHaveLength(61);
    expect(result.samples.playerErrors).toEqual([1]);
    expect(result.notes).toContain('player error: STALLED');
    expect(result.notes).not.toContain('jsHeap: bucketed'); // it grows by 0.5 MB a reading
    const tilesBound = t.session().binds.filter((bind) => bind.profile === 'sub');
    const mainBound = t.session().binds.filter((bind) => bind.profile === 'main');
    // 15 pages of 4 and 15 of 9 tiles; fullscreen on the extra slot 10 times.
    expect(tilesBound).toHaveLength(15 * 4 + 15 * 9);
    expect(mainBound).toHaveLength(10);
    for (const bind of mainBound) expect(bind.slot).toBe(9);
    expect(result.interrupted).toBe(false);
  });

  it.each(['chrome', 'edge', 'firefox', 'windows-tauri'] as const)('S4_desktopPath_pages9And16Tiles (%s)', async (platform) => {
    // R45: G6 judges MV7 with 16 tiles on WIN-1; every desktop path pages the desktop grid.
    const t = setup({ fakePlayer: true, overrides: { device: { platform, ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' } } });

    const result = await finish(runScenario('S4', t.env));

    expect(t.session().slots).toBe(16);
    expect(result.samples.tiles).toEqual([16]);
    const tilesBound = t.session().binds.filter((bind) => bind.profile === 'sub');
    const mainBound = t.session().binds.filter((bind) => bind.profile === 'main');
    expect(tilesBound).toHaveLength(15 * 9 + 15 * 16);
    expect(Math.max(...tilesBound.map((bind) => bind.slot))).toBe(15);
    for (const bind of mainBound) expect(bind.slot).toBe(16);
  });

  it('S4_tauriShell_recordsTheWebView2ProcessesWithTheMemory', async () => {
    const t = setup({
      fakePlayer: true,
      native: { metrics: () => Promise.resolve({ memMb: 380, webview2Procs: 6 }) },
      overrides: { device: { platform: 'windows-tauri', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Edg/141.0.0.0' } },
    });

    const result = await finish(runScenario('S4', t.env));

    expect(result.samples.memMb).toHaveLength(61);
    expect(result.samples.webview2Procs).toHaveLength(61);
  });

  it('S4_bucketedJsHeap_onChromiumBucketEdges_isNoted', async () => {
    // R46: without a site-locked renderer (Android WebView), Chromium rounds the
    // heap to a bucket and refreshes it every 20 min.
    let reads = 0;
    const t = setup({
      fakePlayer: true,
      native: { metrics: () => Promise.resolve({ memMb: 210 }) },
      overrides: {
        jsHeapMb: () => ((reads++ < 40 ? 10_000_000 : 10_600_000) / 2 ** 20),
        device: { platform: 'android', ua: 'Mozilla/5.0 (Linux; Android 14; wv)' },
      },
    });

    const result = await finish(runScenario('S4', t.env));

    expect(result.notes).toContain('jsHeap: bucketed');
    expect(result.samples.memMb).toHaveLength(61); // MV7 is judged on the process memory then
  });

  it('S4_jsHeapThatNeverChanges_isNotedAsBucketed', async () => {
    const t = setup({ fakePlayer: true, overrides: { jsHeapMb: () => 23.4 } });

    const result = await finish(runScenario('S4', t.env));

    expect(result.notes).toContain('jsHeap: bucketed');
  });

  it('S4_lossCounters_naWhereTheBrowserLacksThem', async () => {
    // R50: Firefox, for example, may not report every counter.
    const t = setup({ fakePlayer: true, hub: { loss: { rtpLost: 7 } } });

    const result = await finish(runScenario('S4', t.env));

    expect(result.samples.rtpLost).toEqual([7]);
    expect(result.samples.nack).toBeUndefined();
    expect(result.notes).toEqual(expect.arrayContaining(['nack: n/a', 'pli: n/a', 'framesDropped: n/a']));
    expect(result.notes).not.toContain('rtpLost: n/a');
  });
});
