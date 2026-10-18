// A ready-made player on test doubles, shared by the player tests. Not exported from index.ts.
import { vi } from 'vitest';
import { FakeClock, FakeHls, FakeLink, fakeVideo, type FakeVideo, type QualityCounter } from './fakes';
import { createPlayer, type Player, type PlayerSource } from './index';

export const LIVE: PlayerSource = { kind: 'live', cameraId: 'sim-01', profile: 'sub' };
export const PLAYLIST = 'http://hub.test/media/hls/index.m3u8';
export const RECORDING: PlayerSource = {
  kind: 'recording',
  cameraId: 'sim-01',
  profile: 'main',
  from: new Date('2026-10-18T10:00:00Z'),
  url: PLAYLIST,
};

/** Collects what a player emits, in order, per event name. */
export function recordEvents(player: Player) {
  const log: { event: string; data: unknown }[] = [];
  for (const event of ['first-frame', 'transport-changed', 'error'] as const) {
    player.on(event, (data) => log.push({ event, data }));
  }
  return (event: string): unknown[] => log.filter((entry) => entry.event === event).map((entry) => entry.data);
}

export interface SetupOptions {
  /** Whether the element has requestVideoFrameCallback. Default: true. */
  rvfc?: boolean;
  /** Which playback quality counter the element offers. Default: `quality`. */
  counter?: QualityCounter;
  slot?: number;
  /** The one hls.js instance every `hlsFactory()` call returns. */
  hls?: FakeHls;
  /** Every `hlsFactory()` call returns a new instance, collected in `instances`. */
  freshHls?: boolean;
}

export function setup(options: SetupOptions = {}) {
  const clock = new FakeClock();
  const video = fakeVideo(options.rvfc ?? true, options.counter);
  const link = new FakeLink();
  const hls = options.hls ?? new FakeHls();
  const instances: FakeHls[] = [];
  const hlsFactory = vi.fn(() => {
    if (!options.freshHls) return hls;
    const fresh = new FakeHls();
    instances.push(fresh);
    return fresh;
  });
  const player = createPlayer({
    now: clock.now,
    setTimer: clock.setTimer,
    hlsFactory,
    ...(options.slot === undefined ? {} : { slot: options.slot }),
  });
  player.attach(video.element, link);
  return { clock, video, link, hls, instances, hlsFactory, player, emitted: recordEvents(player) };
}

/** `count` frames, one every `everyMs`. */
export function frames(clock: FakeClock, video: FakeVideo, count: number, everyMs = 100): void {
  for (let i = 0; i < count; i += 1) {
    clock.advance(everyMs);
    video.renderFrame();
  }
}

/**
 * Replaces the two ways the player can surface a listener's exception without
 * throwing it into the caller: `reportError` (absent unless `report` is given)
 * and `queueMicrotask` (the tasks are collected, not run). Undone by
 * `vi.unstubAllGlobals()`.
 */
export function stubAsyncThrows(report?: (error: unknown) => void): (() => void)[] {
  const queued: (() => void)[] = [];
  vi.stubGlobal('reportError', report);
  vi.stubGlobal('queueMicrotask', (task: () => void) => {
    queued.push(task);
  });
  return queued;
}
