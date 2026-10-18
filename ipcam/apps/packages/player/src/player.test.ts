import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  FakeClock,
  FakeHls,
  FakeLink,
  fakeTrack,
  fakeVideo,
  stubMediaStream,
  type FakeVideo,
} from './fakes';
import { createPlayer, PlayerError, type Player, type PlayerSource } from './index';

const LIVE: PlayerSource = { kind: 'live', cameraId: 'sim-01', profile: 'sub' };
const PLAYLIST = 'http://hub.test/media/hls/index.m3u8';
const RECORDING: PlayerSource = {
  kind: 'recording',
  cameraId: 'sim-01',
  profile: 'main',
  from: new Date('2026-10-18T10:00:00Z'),
  url: PLAYLIST,
};

/** Collects what a player emits, in order, per event name. */
function recordEvents(player: Player) {
  const log: { event: string; data: unknown }[] = [];
  for (const event of ['first-frame', 'transport-changed', 'error'] as const) {
    player.on(event, (data) => log.push({ event, data }));
  }
  return (event: string): unknown[] => log.filter((entry) => entry.event === event).map((entry) => entry.data);
}

function setup(options: { rvfc?: boolean; slot?: number; hls?: FakeHls } = {}) {
  const clock = new FakeClock();
  const video = fakeVideo(options.rvfc ?? true);
  const link = new FakeLink();
  const hls = options.hls ?? new FakeHls();
  const hlsFactory = vi.fn(() => hls);
  const player = createPlayer({
    now: clock.now,
    setTimer: clock.setTimer,
    hlsFactory,
    ...(options.slot === undefined ? {} : { slot: options.slot }),
  });
  player.attach(video.element, link);
  return { clock, video, link, hls, hlsFactory, player, emitted: recordEvents(player) };
}

/** `count` frames, one every `everyMs`. */
function frames(clock: FakeClock, video: FakeVideo, count: number, everyMs = 100): void {
  for (let i = 0; i < count; i += 1) {
    clock.advance(everyMs);
    video.renderFrame();
  }
}

beforeEach(() => {
  stubMediaStream();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('live over WebRTC', () => {
  it('play_webrtc_attachesTrackAndEmitsFirstFrame', async () => {
    const { player, video, link, clock, emitted } = setup();

    await player.play(LIVE);

    expect(link.webrtcTrack).toHaveBeenCalledTimes(1);
    expect(link.webrtcTrack).toHaveBeenCalledWith(0);
    expect(video.streamTracks).toEqual([link.track]);
    expect(video.playCalls).toBe(1);
    expect(emitted('transport-changed')).toEqual([{ transport: 'webrtc' }]);
    expect(emitted('first-frame')).toEqual([]);

    clock.advance(120);
    video.renderFrame();
    expect(emitted('first-frame')).toEqual([{ transport: 'webrtc', elapsedMs: 120 }]);

    video.renderFrame();
    expect(emitted('first-frame')).toHaveLength(1);
    expect(player.stats).toEqual({ transport: 'webrtc', codec: 'unknown', framesDecoded: 2 });
  });

  it('play_webrtc_usesTheConfiguredSlot', async () => {
    const { player, link } = setup({ slot: 3 });
    await player.play(LIVE);
    expect(link.webrtcTrack).toHaveBeenCalledWith(3);
  });

  it('play_live_neverBuildsAnHlsInstance', async () => {
    const { player, hlsFactory } = setup();
    await player.play(LIVE);
    expect(hlsFactory).not.toHaveBeenCalled();
  });

  it('noFirstFrameIn4s_emitsErrorNoFirstFrame', async () => {
    const { player, clock, emitted } = setup();
    await player.play(LIVE);

    clock.advance(3999);
    expect(emitted('error')).toEqual([]);
    clock.advance(1);
    expect(emitted('error')).toEqual([{ reason: 'NO_FIRST_FRAME' }]);

    // One error per play: neither a repeat nor a STALLED follows, and nothing stays armed.
    clock.advance(60_000);
    expect(emitted('error')).toHaveLength(1);
    expect(clock.pending).toBe(0);
  });

  it('firstFrameWithin4s_cancelsTheDeadline', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);

    clock.advance(3999);
    video.renderFrame();
    frames(clock, video, 100); // 10 s of steady frames
    expect(emitted('error')).toEqual([]);
  });

  it('framesStalled3s_emitsErrorStalled', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    frames(clock, video, 20); // 2 s of steady frames, last one at t = 2000
    expect(emitted('error')).toEqual([]);

    // Frames stop. Not before 3 s without a frame, and within 3.5 s (one poll later).
    clock.advance(2999);
    expect(emitted('error')).toEqual([]);
    clock.advance(501);
    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);

    clock.advance(60_000);
    expect(emitted('error')).toHaveLength(1);
    expect(clock.pending).toBe(0);
  });

  it('framesResumeBefore3s_noStalledError', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);

    for (let round = 0; round < 10; round += 1) {
      clock.advance(2900); // a gap just under the limit, then frames again
      video.renderFrame();
    }
    expect(emitted('error')).toEqual([]);
  });

  it('play_preferListWithWebrtc_playsWebrtc', async () => {
    const { player, link } = setup();
    await player.play(LIVE, ['mse', 'webrtc', 'snapshot']);
    expect(link.webrtcTrack).toHaveBeenCalledTimes(1);
  });

  it('play_preferOnlyMseOrSnapshot_rejectsUnsupportedTransport', async () => {
    const { player, link, video, clock } = setup();

    const rejection = player.play(LIVE, ['mse', 'snapshot']);
    await expect(rejection).rejects.toBeInstanceOf(PlayerError);
    await expect(rejection).rejects.toMatchObject({ code: 'UNSUPPORTED_TRANSPORT', message: 'UNSUPPORTED_TRANSPORT' });
    await expect(player.play(LIVE, [])).rejects.toMatchObject({ code: 'UNSUPPORTED_TRANSPORT' });

    expect(link.webrtcTrack).not.toHaveBeenCalled();
    expect(video.srcObject).toBeNull();
    expect(clock.pending).toBe(0);
  });

  it('play_hlsForLiveOrWebrtcForRecording_rejectsUnsupportedTransport', async () => {
    const { player } = setup();
    await expect(player.play(LIVE, ['hls'])).rejects.toMatchObject({ code: 'UNSUPPORTED_TRANSPORT' });
    await expect(player.play(RECORDING, ['webrtc'])).rejects.toMatchObject({ code: 'UNSUPPORTED_TRANSPORT' });
  });

  it('play_rejectedRequest_keepsThePreviousPlayback', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    video.renderFrame();

    await expect(player.play(LIVE, ['mse'])).rejects.toMatchObject({ code: 'UNSUPPORTED_TRANSPORT' });
    await expect(
      player.play({ kind: 'recording', cameraId: 'sim-01', profile: 'main' }),
    ).rejects.toMatchObject({ code: 'MISSING_URL' });

    expect(video.streamTracks).not.toBeNull();
    expect(clock.pending).toBe(1); // still watching for a stall
    expect(emitted('transport-changed')).toEqual([{ transport: 'webrtc' }]);
  });

  it('play_beforeAttach_rejectsNotAttached', async () => {
    const player = createPlayer();
    await expect(player.play(LIVE)).rejects.toMatchObject({ code: 'NOT_ATTACHED' });
  });

  it('play_trackRequestFails_rejectsAndDisarmsTheTimers', async () => {
    const { player, link, clock, emitted } = setup();
    link.webrtcTrack.mockRejectedValueOnce(new Error('slot not bound'));

    await expect(player.play(LIVE)).rejects.toThrow('slot not bound');

    expect(clock.pending).toBe(0);
    clock.advance(60_000);
    expect(emitted('error')).toEqual([]);
  });

  it('play_stoppedWhileTheTrackIsPending_attachesNothing', async () => {
    const { player, link, video, clock, emitted } = setup();
    let release: (track: MediaStreamTrack) => void = () => undefined;
    link.webrtcTrack.mockReturnValueOnce(new Promise((resolve) => (release = resolve)));

    const pending = player.play(LIVE);
    player.stop();
    release(fakeTrack('late'));
    await pending;

    expect(video.srcObject).toBeNull();
    expect(video.playCalls).toBe(0);
    expect(clock.pending).toBe(0);
    clock.advance(60_000);
    expect(emitted('error')).toEqual([]);
  });

  it('play_again_dropsTheTimersOfThePreviousPlay', async () => {
    const { player, clock, emitted } = setup();
    await player.play(LIVE); // deadline at t = 4000
    clock.advance(2000);
    await player.play(LIVE); // deadline at t = 6000

    clock.advance(2500); // t = 4500: the first deadline is gone
    expect(emitted('error')).toEqual([]);
    clock.advance(1500); // t = 6000
    expect(emitted('error')).toEqual([{ reason: 'NO_FIRST_FRAME' }]);
  });

  it('error_listenerRestartsPlayback_armsFreshTimers', async () => {
    const { player, clock } = setup();
    const reasons: unknown[] = [];
    player.on('error', (data) => {
      reasons.push(data);
      if (reasons.length === 1) void player.play(LIVE);
    });

    await player.play(LIVE);
    clock.advance(4000);
    expect(reasons).toHaveLength(1);
    await Promise.resolve();
    expect(clock.pending).toBe(1); // the restarted play has its own deadline

    clock.advance(4000);
    expect(reasons).toEqual([{ reason: 'NO_FIRST_FRAME' }, { reason: 'NO_FIRST_FRAME' }]);
  });

  it('transportChanged_callbackStopsThePlayer_attachesNothing', async () => {
    const { player, link, video, clock } = setup();
    player.on('transport-changed', () => player.stop());

    await player.play(LIVE);

    expect(link.webrtcTrack).not.toHaveBeenCalled();
    expect(video.srcObject).toBeNull();
    expect(clock.pending).toBe(0);
  });

  it('transportChanged_callbackThrows_rejectsAndDisarmsTheTimers', async () => {
    const { player, link, clock } = setup();
    player.on('transport-changed', () => {
      throw new Error('listener failed');
    });

    await expect(player.play(LIVE)).rejects.toThrow('listener failed');

    expect(link.webrtcTrack).not.toHaveBeenCalled();
    expect(clock.pending).toBe(0);
  });

  it('attach_whilePlaying_releasesThePreviousVideo', async () => {
    const { player, video, link, clock, emitted } = setup();
    await player.play(LIVE);
    const other = fakeVideo();

    player.attach(other.element, link);

    expect(video.srcObject).toBeNull();
    expect(clock.pending).toBe(0);
    clock.advance(60_000);
    expect(emitted('error')).toEqual([]);
  });

  it('transportChanged_firesWhenAPlayChangesTheTransport', async () => {
    const { player, emitted } = setup();
    await player.play(LIVE);
    await player.play(LIVE);
    await player.play(RECORDING);
    await player.play(RECORDING);
    await player.play(LIVE);
    expect(emitted('transport-changed')).toEqual([
      { transport: 'webrtc' },
      { transport: 'hls' },
      { transport: 'webrtc' },
    ]);
  });
});

describe('recordings over HLS', () => {
  it('recording_usesHlsFactoryAndSeeks', async () => {
    const { player, video, link, hls, hlsFactory, emitted } = setup();

    await player.play(RECORDING);

    expect(hlsFactory).toHaveBeenCalledTimes(1);
    expect(hls.media).toBe(video.element);
    expect(hls.loadedUrl).toBe(PLAYLIST);
    expect(video.playCalls).toBe(1);
    expect(link.webrtcTrack).not.toHaveBeenCalled();
    expect(emitted('transport-changed')).toEqual([{ transport: 'hls' }]);
    expect(player.stats.transport).toBe('hls');

    // `from` is the wall-clock time of media position 0.
    await player.seek(new Date('2026-10-18T10:02:30Z'));
    expect(hls.media?.currentTime).toBe(150);
    await player.seek(new Date('2026-10-18T10:00:04.500Z'));
    expect(hls.media?.currentTime).toBe(4.5);
  });

  it('recording_seekBeforeTheOrigin_clampsToZero', async () => {
    const { player, hls } = setup();
    await player.play(RECORDING);
    await player.seek(new Date('2026-10-18T09:59:00Z'));
    expect(hls.media?.currentTime).toBe(0);
  });

  it('recording_withoutFrom_seeksRelativeToTheEpoch', async () => {
    const { player, hls } = setup();
    await player.play({ kind: 'recording', cameraId: 'sim-01', profile: 'main', url: PLAYLIST });
    await player.seek(new Date(90_000));
    expect(hls.media?.currentTime).toBe(90);
  });

  it('recording_withoutUrl_rejectsMissingUrl', async () => {
    const { player, hlsFactory } = setup();
    await expect(
      player.play({ kind: 'recording', cameraId: 'sim-01', profile: 'main' }),
    ).rejects.toMatchObject({ code: 'MISSING_URL' });
    expect(hlsFactory).not.toHaveBeenCalled();
  });

  it('recording_stats_reportTheCodecOfTheCurrentLevel', async () => {
    const { player, video, hls, clock } = setup();
    await player.play(RECORDING);
    expect(player.stats.codec).toBe('unknown');

    hls.levels = [{ videoCodec: 'avc1.4d4028' }];
    hls.currentLevel = 0;
    clock.advance(10);
    video.renderFrame();
    expect(player.stats).toEqual({ transport: 'hls', codec: 'avc1.4d4028', framesDecoded: 1 });
  });

  it('recording_noFirstFrameIn4s_emitsErrorNoFirstFrame', async () => {
    const { player, clock, emitted } = setup();
    await player.play(RECORDING);
    clock.advance(4000);
    expect(emitted('error')).toEqual([{ reason: 'NO_FIRST_FRAME' }]);
  });

  it('recording_framesStalled_emitsErrorStalled', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 10);
    clock.advance(3500);
    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('seek_whenHlsHasLostTheElement_rejectsNotAttached', async () => {
    const { player, hls } = setup();
    await player.play(RECORDING);
    hls.media = null; // hls.js detached itself, for example after a fatal error

    await expect(player.seek(new Date(1000))).rejects.toMatchObject({ code: 'NOT_ATTACHED' });
  });

  it('seek_withoutARecording_rejectsNotARecording', async () => {
    const { player } = setup();
    await expect(player.seek(new Date())).rejects.toMatchObject({ code: 'NOT_A_RECORDING' });

    await player.play(LIVE);
    await expect(player.seek(new Date())).rejects.toMatchObject({ code: 'NOT_A_RECORDING' });
  });

  it('stop_destroysTheHlsInstance', async () => {
    const { player, hls, clock, emitted } = setup();
    await player.play(RECORDING);

    player.stop();

    expect(hls.destroyed).toBe(true);
    expect(clock.pending).toBe(0);
    await expect(player.seek(new Date())).rejects.toMatchObject({ code: 'NOT_A_RECORDING' });
    clock.advance(60_000);
    expect(emitted('error')).toEqual([]);
  });

  it('play_hlsFactoryThrows_rejectsAndDisarmsTheTimers', async () => {
    const clock = new FakeClock();
    const player = createPlayer({
      now: clock.now,
      setTimer: clock.setTimer,
      hlsFactory: () => {
        throw new Error('MSE is unavailable');
      },
    });
    player.attach(fakeVideo().element, new FakeLink());

    await expect(player.play(RECORDING)).rejects.toThrow('MSE is unavailable');
    expect(clock.pending).toBe(0);
  });
});

describe('stop, events and defaults', () => {
  it('stop_detachesAndClearsTimers', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    expect(clock.pending).toBe(1);
    expect(video.pendingFrameCallbacks).toBe(1);

    player.stop();

    expect(video.srcObject).toBeNull();
    expect(clock.pending).toBe(0);
    expect(video.pendingFrameCallbacks).toBe(0);
    expect(video.cancelledFrameCallbacks).toHaveLength(1);
    clock.advance(60_000);
    video.renderFrame();
    expect(emitted('error')).toEqual([]);
    expect(emitted('first-frame')).toEqual([]);
    expect(player.stats.framesDecoded).toBe(0);
  });

  it('stop_afterTheFirstFrame_clearsStallPolling', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    frames(clock, video, 3);
    expect(clock.pending).toBe(1); // the stall poll

    player.stop();

    expect(clock.pending).toBe(0);
    clock.advance(60_000);
    expect(emitted('error')).toEqual([]);
  });

  it('stop_withoutAPlay_isHarmless', () => {
    const { player } = setup();
    expect(() => player.stop()).not.toThrow();
  });

  it('stop_withoutRvfc_removesTheTimeupdateListener', async () => {
    const { player, video } = setup({ rvfc: false });
    await player.play(LIVE);
    expect(video.listenerCount('timeupdate')).toBe(1);

    player.stop();

    expect(video.listenerCount('timeupdate')).toBe(0);
  });

  it('firstFrame_withoutRvfc_comesFromTimeupdateWithPositiveTime', async () => {
    const { player, video, clock, emitted } = setup({ rvfc: false });
    await player.play(LIVE);

    video.fireTimeUpdate(); // currentTime is still 0
    expect(emitted('first-frame')).toEqual([]);

    clock.advance(200);
    video.renderFrame(); // currentTime moves past 0
    expect(emitted('first-frame')).toEqual([{ transport: 'webrtc', elapsedMs: 200 }]);
    expect(video.listenerCount('timeupdate')).toBe(0);
  });

  it('onUnsubscribe_stopsCallbacks', async () => {
    const { player, video, clock } = setup();
    const onError = vi.fn();
    const onFirstFrame = vi.fn();
    const onTransport = vi.fn();
    const offError = player.on('error', onError);
    const offFirstFrame = player.on('first-frame', onFirstFrame);
    const offTransport = player.on('transport-changed', onTransport);

    await player.play(LIVE);
    expect(onTransport).toHaveBeenCalledTimes(1);
    clock.advance(4000);
    expect(onError).toHaveBeenCalledTimes(1);

    offError();
    offFirstFrame();
    offTransport();

    await player.play(RECORDING);
    clock.advance(4000);
    video.renderFrame();
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onFirstFrame).not.toHaveBeenCalled();
    expect(onTransport).toHaveBeenCalledTimes(1);
  });

  it('onUnsubscribe_withTheSameCallbackTwice_removesOnlyThatRegistration', async () => {
    const { player, clock } = setup();
    const callback = vi.fn();
    const first = player.on('error', callback);
    player.on('error', callback);

    first();
    await player.play(LIVE);
    clock.advance(4000);

    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('onUnsubscribe_insideAnotherCallback_skipsTheRemovedOne', async () => {
    const { player, clock } = setup();
    const second = vi.fn();
    let offSecond: () => void = () => undefined;
    player.on('error', () => offSecond());
    offSecond = player.on('error', second);

    await player.play(LIVE);
    clock.advance(4000);

    expect(second).not.toHaveBeenCalled();
  });

  it('stats_beforeAnyPlay_areEmpty', () => {
    const player = createPlayer();
    expect(player.stats).toEqual({ transport: 'webrtc', codec: 'unknown', framesDecoded: 0 });
  });

  it('defaults_useTheRealClockAndTimers', async () => {
    vi.useFakeTimers();
    const video = fakeVideo();
    const player = createPlayer();
    player.attach(video.element, new FakeLink());
    const errors: unknown[] = [];
    player.on('error', (data) => errors.push(data));

    await player.play(LIVE);
    vi.advanceTimersByTime(3999);
    expect(errors).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(errors).toEqual([{ reason: 'NO_FIRST_FRAME' }]);

    player.stop();
    expect(vi.getTimerCount()).toBe(0);
  });
});
