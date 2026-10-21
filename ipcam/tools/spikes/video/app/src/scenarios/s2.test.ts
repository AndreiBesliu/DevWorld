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

describe('S2', () => {
  it('S2_cachedGopRecords_areNotLatencySamples', async () => {
    // Every live frame reaches the glass 120 ms after the hub sent it. Right
    // after each bind the hub replays its cached GOP: frames emitted up to a
    // second before the bind, whose sent records carry that emit time (R24,
    // R26). Taken as samples they would read 1 s or more. The main bind comes
    // well after the sub bind, so its cached frames are newer than the sub
    // bind: only the main bind's own request time drops them.
    const t = setup({
      hub: {
        fps: (profile) => (profile === 'sub' ? 12 : 20),
        cachedGop: (profile) => (profile === 'sub' ? 12 : 20),
        latencyMs: 120,
        offsetMs: -3_600_000, // the hub's clock is an hour behind the phone's; the offset takes care of it
      },
    });

    const result = await finish(runScenario('S2', t.env));

    for (const key of ['subLatencyMs', 'mainLatencyMs']) {
      const samples = result.samples[key] ?? [];
      expect(samples, key).toHaveLength(100);
      for (const ms of samples) expect(ms, key).toBeCloseTo(120, 3);
    }
    expect(result.samples.subReliable).toEqual(Array<number>(100).fill(1));
    expect(result.samples.mainReliable).toEqual(Array<number>(100).fill(1));
    const binds = t.session().binds.map(({ slot, cam, profile }) => ({ slot, cam, profile }));
    expect(binds).toEqual([
      { slot: 0, cam: 'sim-01', profile: 'sub' },
      { slot: 0, cam: 'sim-01', profile: 'main' },
    ]);
    expect(result.interrupted).toBe(false);
  });

  it('S2_replayedFrameNumbers_matchTheMostRecentRecord', async () => {
    // A file that loops every 12 s: by the time a frame number comes round
    // again, the hub's record of its previous showing is still known. The frame
    // must wait for its own record, the most recent one, not take the old one.
    const t = setup({ hub: { fps: () => 12, latencyMs: 120, loopFrames: 144 } });

    const result = await finish(runScenario('S2', t.env));

    for (const key of ['subLatencyMs', 'mainLatencyMs']) {
      const samples = result.samples[key] ?? [];
      expect(samples, key).toHaveLength(100);
      for (const ms of samples) expect(ms, key).toBeCloseTo(120, 3);
    }
  });

  it('S2_playRejects_leavesNoUnhandledRejection', async () => {
    // play() fails, then the bind it raced with is refused: someone must have its answer.
    const t = setup({ fakePlayer: true, hub: { answer: () => ({ error: 'CODEC_UNSUPPORTED' }) } });
    t.env.createPlayer = (opts) => refusingPlayer(opts.slot);

    let notes: string[] = [];
    const unhandled = await unhandledRejections(async () => {
      notes = (await finish(runScenario('S2', t.env))).notes;
    });

    expect(unhandled).toEqual([]);
    expect(notes).toContain('failed: Error: no track for this slot');
  });

  it('S2_framesOfAnotherStream_areNotMatched', async () => {
    // Without a band of the bound stream (the wrong media file on screen), no sample is taken.
    const t = setup({ hub: { fps: () => 12, latencyMs: 120 } });
    t.env.readBand = () => ({ camera: 4, frame: 5, loop: 0 });

    const result = await finish(runScenario('S2', t.env));

    expect(result.samples.subLatencyMs ?? []).toEqual([]);
    expect(result.notes).toContain('sub: 0 of 100 samples within 60 s');
  });
});
