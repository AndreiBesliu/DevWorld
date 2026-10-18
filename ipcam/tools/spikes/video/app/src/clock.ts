/**
 * One ping as the client sees it. `c0` is the client time when the ping left,
 * `c1` when the answer came back, and `h` the hub's time when it answered. All
 * three are Unix milliseconds on the same scale: the client's are
 * `performance.timeOrigin + performance.now()`, never `Date.now()`, and the
 * hub's is its wall clock.
 */
export interface PingSample {
  c0: number;
  c1: number;
  h: number;
}

export interface ClockEstimate {
  /** Add this to a client time to get the hub's time. */
  offsetMs: number;
  /** The round trip of the sample that the offset comes from, the quickest of the pings. */
  rttMs: number;
  /** False when the round trip is above 50 ms or the offset moved by more than 20 ms since the last estimate. */
  reliable: boolean;
}

/** Pings per estimate (Review Focus 3). */
const PINGS = 20;
const MAX_RELIABLE_RTT_MS = 50;
const MAX_RELIABLE_OFFSET_STEP_MS = 20;

/**
 * Estimates the offset between the client's clock and the hub's, so that the
 * glass-to-glass latency never depends on the device's wall clock (Review Focus
 * 3). Of 20 pings it keeps the quickest: a short round trip leaves little room
 * for the two directions to differ, which is the error of the method. The
 * caller re-estimates every 30 s; an estimate is `reliable` only if the quickest
 * ping took at most 50 ms and the offset is within 20 ms of the previous one.
 */
export class ClockSync {
  private readonly ping: () => Promise<PingSample>;
  private lastOffsetMs: number | undefined;

  constructor(ping: () => Promise<PingSample>) {
    this.ping = ping;
  }

  /**
   * Rejects when not one ping was answered. Pings that fail (a timeout, say)
   * are skipped. They run one after the other, so that they do not queue behind
   * each other on the channel.
   */
  async estimate(): Promise<ClockEstimate> {
    let best: { offsetMs: number; rttMs: number } | undefined;
    for (let i = 0; i < PINGS; i += 1) {
      let sample: PingSample;
      try {
        sample = await this.ping();
      } catch {
        continue;
      }
      const rttMs = sample.c1 - sample.c0;
      if (best === undefined || rttMs < best.rttMs) {
        best = { rttMs, offsetMs: sample.h - (sample.c0 + rttMs / 2) };
      }
    }
    if (best === undefined) throw new Error('no ping was answered');

    const moved = this.lastOffsetMs === undefined ? 0 : Math.abs(best.offsetMs - this.lastOffsetMs);
    this.lastOffsetMs = best.offsetMs;
    return { ...best, reliable: best.rttMs <= MAX_RELIABLE_RTT_MS && moved <= MAX_RELIABLE_OFFSET_STEP_MS };
  }
}

/**
 * The time from the hub sending a frame to the glass showing it. The display
 * time is on the performance clock (what requestVideoFrameCallback reports),
 * which `timeOrigin` puts on the same Unix scale as the pings. The device's
 * wall clock is not read: if it is wrong, `timeOrigin` and the offset are wrong
 * by the same amount and cancel.
 */
export function glassToGlassMs(displayPerfMs: number, sentHubMs: number, offsetMs: number, timeOrigin: number): number {
  return timeOrigin + offsetMs - sentHubMs + displayPerfMs;
}

/**
 * Whether a `sent` record is older than the bind that is being measured (ruling
 * R26). When a camera is bound, the hub starts the stream at the GOP it has
 * cached, and those frames keep the time at which they were first emitted, so a
 * latency taken on them would include the age of the cache (up to one GOP, 1 s)
 * instead of the transport. Only records the hub emitted at or after the bind
 * count as samples.
 *
 * `requestedPerfMs` is `BindResult.requestedPerfMs`, the performance-clock time
 * at which `bind` was called; `offsetMs` is the current `ClockSync` estimate.
 * Like `glassToGlassMs`, it puts the performance clock on the Unix scale with
 * `timeOrigin` and then on the hub's with the offset, so the device's wall clock
 * does not enter. A cached frame emitted within half a round trip before the
 * hub received the bind is kept: its cache age is that short.
 */
export function predatesBind(sentHubMs: number, requestedPerfMs: number, offsetMs: number, timeOrigin: number): boolean {
  return sentHubMs < timeOrigin + requestedPerfMs + offsetMs;
}
