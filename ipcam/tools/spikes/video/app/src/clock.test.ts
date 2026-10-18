import { describe, expect, it } from 'vitest';
import { ClockSync, glassToGlassMs, predatesBind, type PingSample } from './clock';

/** The hub's clock is the client's plus this many ms. */
const TRUE_OFFSET_MS = 1000;

/**
 * One ping as the client sees it: the request takes `forwardMs`, the answer
 * `backMs`. The offset a sample implies is the true one plus (forward - back) / 2,
 * so only a symmetric round trip gives it exactly.
 */
function pingSample(startMs: number, forwardMs: number, backMs: number, offsetMs = TRUE_OFFSET_MS): PingSample {
  return { c0: startMs, c1: startMs + forwardMs + backMs, h: startMs + forwardMs + offsetMs };
}

/** A ping function that answers with the given samples, one per call. */
function pingsFrom(samples: PingSample[]): () => Promise<PingSample> {
  const queue = [...samples];
  return () => {
    const next = queue.shift();
    return next ? Promise.resolve(next) : Promise.reject(new Error('no more samples'));
  };
}

/** Where in the series the quickest ping is: neither the first nor the last, so that picking either would fail. */
const QUICKEST = 13;

/**
 * 20 samples with round trips of 5 to 80 ms in a shuffled order (7 is coprime
 * with 20, so `(7i + 9) % 20` visits every step once). Only the quickest one is
 * symmetric; the slower ones are all delayed on the way out, which biases them.
 */
function jitterySamples(): PingSample[] {
  const samples: PingSample[] = [];
  for (let i = 0; i < 20; i += 1) {
    const rtt = 5 + ((7 * i + 9) % 20) * (75 / 19);
    // The way back always takes 2.5 ms; the request is what a busy Wi-Fi delays.
    samples.push(pingSample(10_000 * (i + 1), rtt - 2.5, 2.5));
  }
  return samples;
}

describe('ClockSync', () => {
  it('clockOffset_jitterySamples_minRttWins', async () => {
    const samples = jitterySamples();
    expect(samples).toHaveLength(20);
    const rtts = samples.map((s) => s.c1 - s.c0);
    expect(Math.min(...rtts)).toBeCloseTo(5, 6);
    expect(Math.max(...rtts)).toBeCloseTo(80, 6);
    expect(rtts.indexOf(Math.min(...rtts))).toBe(QUICKEST);

    const estimate = await new ClockSync(pingsFrom(samples)).estimate();

    expect(estimate.offsetMs).toBeCloseTo(TRUE_OFFSET_MS, 6);
    expect(estimate.rttMs).toBeCloseTo(5, 6);
    expect(estimate.reliable).toBe(true);
  });

  it('clockOffset_everyOtherSampleAloneWouldBeOff_soTheTestProvesTheSelection', () => {
    // Guard for the test above: every slower ping is biased, so the first, the
    // last, any other sample or the mean of all the offsets misses the true one.
    const offsets = jitterySamples().map((s) => s.h - (s.c0 + s.c1) / 2);
    const others = offsets.filter((_, i) => i !== QUICKEST);

    expect(offsets[QUICKEST]).toBeCloseTo(TRUE_OFFSET_MS, 6);
    for (const offset of others) expect(Math.abs(offset - TRUE_OFFSET_MS)).toBeGreaterThan(1);
    const mean = offsets.reduce((sum, offset) => sum + offset, 0) / offsets.length;
    expect(Math.abs(mean - TRUE_OFFSET_MS)).toBeGreaterThan(1);
  });

  it('clockOffset_asksForTwentyPings', async () => {
    let calls = 0;
    const clock = new ClockSync(() => {
      calls += 1;
      return Promise.resolve(pingSample(calls * 1000, 5, 5));
    });

    await clock.estimate();

    expect(calls).toBe(20);
  });

  it('clockOffset_bestRttAbove50ms_isUnreliable', async () => {
    const samples = Array.from({ length: 20 }, (_, i) => pingSample(1000 * (i + 1), 30 + (i % 3), 30 + (i % 4)));

    const estimate = await new ClockSync(pingsFrom(samples)).estimate();

    expect(estimate.rttMs).toBe(60);
    expect(estimate.reliable).toBe(false);
  });

  it('clockOffset_bestRttExactly50ms_isStillReliable', async () => {
    const samples = Array.from({ length: 20 }, (_, i) => pingSample(1000 * (i + 1), 25, 25));

    const estimate = await new ClockSync(pingsFrom(samples)).estimate();

    expect(estimate.rttMs).toBe(50);
    expect(estimate.reliable).toBe(true);
  });

  it('clockOffset_movesMoreThan20msBetweenEstimates_isUnreliable', async () => {
    const steady = (offset: number): PingSample[] =>
      Array.from({ length: 20 }, (_, i) => pingSample(1000 * (i + 1), 3, 3, offset));
    const clock = new ClockSync(pingsFrom([...steady(1000), ...steady(1020), ...steady(1041.5), ...steady(1041.5)]));

    const first = await clock.estimate();
    const exactly20 = await clock.estimate();
    const moreThan20 = await clock.estimate();
    const settled = await clock.estimate();

    expect(first.reliable).toBe(true); // nothing to compare with yet
    expect(exactly20.offsetMs).toBeCloseTo(1020, 6);
    expect(exactly20.reliable).toBe(true);
    expect(moreThan20.offsetMs).toBeCloseTo(1041.5, 6);
    expect(moreThan20.reliable).toBe(false);
    expect(settled.reliable).toBe(true); // the next estimate is measured against the last one
  });

  it('clockOffset_movesDownByMoreThan20ms_isUnreliable', async () => {
    const steady = (offset: number): PingSample[] =>
      Array.from({ length: 20 }, (_, i) => pingSample(1000 * (i + 1), 3, 3, offset));
    const clock = new ClockSync(pingsFrom([...steady(1000), ...steady(970)]));

    await clock.estimate();
    const second = await clock.estimate();

    expect(second.reliable).toBe(false);
  });

  it('clockOffset_failedPings_areSkipped', async () => {
    let call = 0;
    const clock = new ClockSync(() => {
      call += 1;
      return call % 2 === 0 ? Promise.reject(new Error('ping timed out')) : Promise.resolve(pingSample(call * 1000, 4, 4));
    });

    const estimate = await clock.estimate();

    expect(call).toBe(20);
    expect(estimate.rttMs).toBe(8);
    expect(estimate.offsetMs).toBeCloseTo(TRUE_OFFSET_MS, 6);
  });

  it('clockOffset_noPingAnswered_rejects', async () => {
    const clock = new ClockSync(() => Promise.reject(new Error('rpc channel closed')));

    await expect(clock.estimate()).rejects.toThrow('no ping was answered');
  });
});

describe('glassToGlassMs', () => {
  it('glassToGlass_knownValues', () => {
    // The frame is on the glass at client time origin + 12345.5 ms, which is
    // origin + 12595.5 ms on the hub's clock; the hub sent it at ...500.
    const timeOrigin = 1_729_260_000_000;

    expect(glassToGlassMs(12_345.5, timeOrigin + 12_500, 250, timeOrigin)).toBeCloseTo(95.5, 6);
    expect(glassToGlassMs(5_000, timeOrigin + 4_920, 0, timeOrigin)).toBeCloseTo(80, 6);
  });

  it('glassToGlass_deviceClockOneHourOff_sameLatency', () => {
    // The offset estimate absorbs whatever the device's wall clock says, because
    // the pings and the display time use the same origin.
    const hubOrigin = 1_729_260_000_000;
    const deviceOrigin = hubOrigin + 3_600_000; // the device thinks it is an hour later
    const offsetMs = -3_600_000; // what the pings measure

    expect(glassToGlassMs(7_000, hubOrigin + 6_940, offsetMs, deviceOrigin)).toBeCloseTo(60, 6);
    expect(glassToGlassMs(7_000, hubOrigin + 6_940, 0, hubOrigin)).toBeCloseTo(60, 6);
  });
});

describe('predatesBind', () => {
  const timeOrigin = 1_729_260_000_000;
  // The bind left at origin + 12000.4 ms on the client's clock, which reads
  // origin + 12250.4 ms on the hub's.
  const requestedPerfMs = 12_000.4;
  const offsetMs = 250;

  it('predatesBind_cachedGopRecord_isTrue', () => {
    // The hub cached this frame a second before the bind and replays it with its
    // original emit time, so a latency measured on it would include the cache's age.
    expect(predatesBind(timeOrigin + 11_250, requestedPerfMs, offsetMs, timeOrigin)).toBe(true);
    expect(predatesBind(timeOrigin + 12_250, requestedPerfMs, offsetMs, timeOrigin)).toBe(true);
  });

  it('predatesBind_liveRecord_isFalse', () => {
    expect(predatesBind(timeOrigin + 12_251, requestedPerfMs, offsetMs, timeOrigin)).toBe(false);
    expect(predatesBind(timeOrigin + 13_000, requestedPerfMs, offsetMs, timeOrigin)).toBe(false);
  });

  it('predatesBind_recordAtTheBindInstant_isKept', () => {
    // Only a record that is strictly earlier than the bind is discarded.
    const bindOnTheHub = timeOrigin + 12_000 + offsetMs;

    expect(predatesBind(bindOnTheHub, 12_000, offsetMs, timeOrigin)).toBe(false);
    expect(predatesBind(bindOnTheHub - 1, 12_000, offsetMs, timeOrigin)).toBe(true);
  });

  it('predatesBind_usesTheOffset', () => {
    // The same record is live on a hub that runs 250 ms ahead and cached on one
    // that runs 250 ms behind: the offset moves the bind onto the hub's clock.
    const sentHubMs = timeOrigin + 12_100;

    expect(predatesBind(sentHubMs, 12_000, -250, timeOrigin)).toBe(false);
    expect(predatesBind(sentHubMs, 12_000, 250, timeOrigin)).toBe(true);
  });

  it('predatesBind_deviceClockOneHourOff_sameVerdicts', () => {
    // As for glassToGlassMs: the device's wall clock is an hour ahead, so its
    // timeOrigin is an hour ahead and the pings measure an offset an hour
    // lower. Both cancel, and the verdicts equal those of a correct clock.
    const hubOrigin = 1_729_260_000_000;
    const deviceOrigin = hubOrigin + 3_600_000;
    const slowOffset = -3_600_000 + 250;
    const records = [hubOrigin + 11_250, hubOrigin + 12_249, hubOrigin + 12_251, hubOrigin + 14_000];

    const onDevice = records.map((r) => predatesBind(r, 12_000, slowOffset, deviceOrigin));
    const onHub = records.map((r) => predatesBind(r, 12_000, 250, hubOrigin));

    expect(onDevice).toStrictEqual([true, true, false, false]);
    expect(onHub).toStrictEqual(onDevice);
  });

  it('predatesBind_cachedGopThenLiveFrames_onlyTheLiveOnesRemain', async () => {
    // A 12 fps source with a 12-frame GOP, cached up to the moment of the bind:
    // 12 replayed records (the last ones are the newest of the cache), then live
    // ones. The offset comes from ClockSync, the way Task 9 gets it.
    const samples = Array.from({ length: 20 }, (_, i) => pingSample(1_000 * (i + 1), 2, 2, 250));
    const { offsetMs: estimated } = await new ClockSync(pingsFrom(samples)).estimate();
    const bindOnTheHub = timeOrigin + 30_000 + estimated;
    const frameMs = 1000 / 12;
    const cached = Array.from({ length: 12 }, (_, i): [number, number, number] => [i, 0, bindOnTheHub - (12 - i) * frameMs]);
    const live = Array.from({ length: 5 }, (_, i): [number, number, number] => [12 + i, 0, bindOnTheHub + (i + 1) * frameMs]);

    const kept = [...cached, ...live].filter(([, , hubMs]) => !predatesBind(hubMs, 30_000, estimated, timeOrigin));

    expect(kept.map(([frame]) => frame)).toStrictEqual([12, 13, 14, 15, 16]);
  });
});
