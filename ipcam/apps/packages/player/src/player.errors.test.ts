import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { stubMediaStream } from './fakes';
import { frames, LIVE, RECORDING, setup, stubAsyncThrows } from './harness';

beforeEach(() => {
  stubMediaStream();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** The unhandled promise rejections that Node reports while `body` runs and one macrotask after it. */
async function unhandledRejectionsDuring(body: () => Promise<void>): Promise<unknown[]> {
  const seen: unknown[] = [];
  const onRejection = (reason: unknown): void => {
    seen.push(reason);
  };
  process.on('unhandledRejection', onRejection);
  try {
    await body();
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
  } finally {
    process.off('unhandledRejection', onRejection);
  }
  return seen;
}

describe('the cause of a failed start', () => {
  it('play_refusedWithNotAllowedError_noFirstFrameCarriesTheName', async () => {
    const { player, video, clock, emitted } = setup();
    video.playResult = () => Promise.reject(new DOMException('autoplay is blocked', 'NotAllowedError'));

    const unhandled = await unhandledRejectionsDuring(async () => {
      await player.play(LIVE);
      clock.advance(3999);
      expect(emitted('error')).toEqual([]);
      clock.advance(1);
    });

    expect(emitted('error')).toStrictEqual([{ reason: 'NO_FIRST_FRAME', cause: 'NotAllowedError' }]);
    expect(unhandled).toEqual([]);
  });

  it('play_refused_stalledCarriesTheCauseToo', async () => {
    const { player, video, clock, emitted } = setup();
    video.playResult = () => Promise.reject(new DOMException('interrupted by a new load', 'AbortError'));
    await player.play(LIVE);
    frames(clock, video, 10);

    clock.advance(3500);

    expect(emitted('error')).toStrictEqual([{ reason: 'STALLED', cause: 'AbortError' }]);
  });

  it('play_refusedWithoutAName_causeIsUnknown', async () => {
    const { player, video, clock, emitted } = setup();
    video.playResult = () => Promise.reject('not an Error');
    await player.play(LIVE);

    clock.advance(4000);

    expect(emitted('error')).toStrictEqual([{ reason: 'NO_FIRST_FRAME', cause: 'unknown' }]);
  });

  it('recording_hlsFatalManifestLoadError_noFirstFrameCarriesTheDetails', async () => {
    const { player, hls, clock, emitted } = setup();
    await player.play(RECORDING);

    hls.emitError('manifestLoadError');
    expect(emitted('error')).toEqual([]); // the error event itself is still the timer's
    clock.advance(4000);

    expect(emitted('error')).toStrictEqual([{ reason: 'NO_FIRST_FRAME', cause: 'hls:manifestLoadError' }]);
  });

  it('recording_hlsFatalError_stalledCarriesTheDetails', async () => {
    const { player, video, hls, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 10);

    hls.emitError('bufferAppendError');
    clock.advance(3500);

    expect(emitted('error')).toStrictEqual([{ reason: 'STALLED', cause: 'hls:bufferAppendError' }]);
  });

  it('recording_hlsNonFatalError_isNoCause', async () => {
    const { player, hls, clock, emitted } = setup();
    await player.play(RECORDING);

    hls.emitError('fragLoadError', false);
    clock.advance(4000);

    expect(emitted('error')).toStrictEqual([{ reason: 'NO_FIRST_FRAME' }]);
  });

  it('cause_isTheLastRecordedReason', async () => {
    const { player, video, hls, clock, emitted } = setup();
    video.playResult = () => Promise.reject(new DOMException('autoplay is blocked', 'NotAllowedError'));
    await player.play(RECORDING);

    hls.emitError('levelLoadError');
    clock.advance(4000);

    expect(emitted('error')).toStrictEqual([{ reason: 'NO_FIRST_FRAME', cause: 'hls:levelLoadError' }]);
  });

  it('cause_belongsToOnePlay', async () => {
    const { player, instances, clock, emitted } = setup({ freshHls: true });
    await player.play(RECORDING);
    instances[0]?.emitError('manifestLoadError');
    clock.advance(4000);

    await player.play(RECORDING);
    clock.advance(4000);

    expect(emitted('error')).toStrictEqual([
      { reason: 'NO_FIRST_FRAME', cause: 'hls:manifestLoadError' },
      { reason: 'NO_FIRST_FRAME' },
    ]);
  });

  it('noFirstFrame_withNothingRecorded_hasNoCause', async () => {
    const { player, clock, emitted } = setup();
    await player.play(LIVE);

    clock.advance(4000);

    expect(emitted('error')).toStrictEqual([{ reason: 'NO_FIRST_FRAME' }]);
  });
});

describe('one policy for listener exceptions', () => {
  const boom = new Error('listener failed');

  it('firstFrame_firstListenerThrows_theSecondStillRunsAndTheExceptionSurfacesAsynchronously', async () => {
    const queued = stubAsyncThrows();
    const { player, video, clock } = setup();
    const second = vi.fn();
    player.on('first-frame', () => {
      throw boom;
    });
    player.on('first-frame', second);
    await player.play(LIVE);

    clock.advance(100);
    expect(() => video.renderFrame()).not.toThrow();

    expect(second).toHaveBeenCalledTimes(1);
    expect(queued).toHaveLength(1);
    expect(() => queued[0]?.()).toThrow(boom);
  });

  it('listenerThrows_reportErrorIsUsedWhenAvailable', async () => {
    const report = vi.fn();
    const queued = stubAsyncThrows(report);
    const { player, video, clock } = setup();
    player.on('first-frame', () => {
      throw boom;
    });
    await player.play(LIVE);

    clock.advance(100);
    video.renderFrame();

    expect(report).toHaveBeenCalledTimes(1);
    expect(report).toHaveBeenCalledWith(boom);
    expect(queued).toEqual([]);
  });

  it('error_listenerThrowsInsideATimer_nothingEscapesTheTimerAndTheNextListenerRuns', async () => {
    const queued = stubAsyncThrows();
    const { player, clock } = setup();
    const second = vi.fn();
    player.on('error', () => {
      throw boom;
    });
    player.on('error', second);
    await player.play(LIVE);

    expect(() => clock.advance(4000)).not.toThrow();

    expect(second).toHaveBeenCalledWith({ reason: 'NO_FIRST_FRAME' });
    expect(queued).toHaveLength(1);
    expect(() => queued[0]?.()).toThrow(boom);
  });

  it('transportChanged_listenerThrows_playStillSucceedsAndTheNextListenerRuns', async () => {
    const queued = stubAsyncThrows();
    const { player, video, link, clock } = setup();
    const second = vi.fn();
    player.on('transport-changed', () => {
      throw boom;
    });
    player.on('transport-changed', second);

    await expect(player.play(LIVE)).resolves.toBeUndefined();

    expect(second).toHaveBeenCalledWith({ transport: 'webrtc' });
    expect(video.streamTracks).toEqual([link.track]);
    expect(video.playCalls).toBe(1);
    expect(clock.pending).toBe(1); // still supervised
    expect(queued).toHaveLength(1);
    expect(() => queued[0]?.()).toThrow(boom);
  });

  it('listenerThrows_thePlayerKeepsWorking', async () => {
    stubAsyncThrows();
    const { player, video, clock, emitted } = setup();
    player.on('first-frame', () => {
      throw boom;
    });
    await player.play(LIVE);

    frames(clock, video, 50);
    expect(emitted('error')).toEqual([]);
    clock.advance(3500);

    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });
});
