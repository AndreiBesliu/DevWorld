import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fakeVideo, stubMediaStream } from './fakes';
import { frames, LIVE, RECORDING, setup } from './harness';

// Ruling R31: the player counts presented frames, and the stall watchdog only
// runs while the element is playing.

beforeEach(() => {
  stubMediaStream();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('counting frames', () => {
  it('framesCounted_rvfcPresent_aStuckPlaybackQualityCounterRaisesNoStalled', async () => {
    // totalVideoFrames on a MediaStream-backed element is unverified outside desktop Chromium.
    const { player, video, clock, emitted } = setup({ rvfc: true, counter: 'zero' });
    await player.play(LIVE);

    frames(clock, video, 100); // 10 s of steady frames

    expect(video.element.getVideoPlaybackQuality().totalVideoFrames).toBe(0);
    expect(emitted('error')).toEqual([]);
    expect(player.stats.framesDecoded).toBe(100);
  });

  it('framesCounted_rvfcPresent_statsGrowWithEveryPresentedFrame', async () => {
    const { player, video, clock } = setup({ rvfc: true, counter: 'zero' });
    await player.play(LIVE);

    expect(player.stats.framesDecoded).toBe(0);
    frames(clock, video, 3);
    expect(player.stats.framesDecoded).toBe(3);
    frames(clock, video, 4);
    expect(player.stats.framesDecoded).toBe(7);
  });

  it('framesCounted_rvfcPresent_stalledStillRaisedWhenPresentedFramesStop', async () => {
    const { player, video, clock, emitted } = setup({ rvfc: true, counter: 'zero' });
    await player.play(LIVE);
    frames(clock, video, 20);

    clock.advance(3500);

    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('framesCounted_firstFrameListener_alreadySeesTheFrame', async () => {
    const { player, video, clock } = setup();
    const seen: number[] = [];
    player.on('first-frame', () => seen.push(player.stats.framesDecoded));
    await player.play(LIVE);

    clock.advance(50);
    video.renderFrame();

    expect(seen).toEqual([1]);
  });

  it('framesCounted_rvfcAbsent_usesTotalVideoFrames', async () => {
    const { player, video, clock, emitted } = setup({ rvfc: false, counter: 'quality' });
    await player.play(LIVE);

    frames(clock, video, 100);
    expect(emitted('error')).toEqual([]);
    expect(player.stats.framesDecoded).toBe(100);
    expect(video.element.getVideoPlaybackQuality().totalVideoFrames).toBe(100);

    clock.advance(3500);
    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('framesCounted_rvfcAbsent_aStuckTotalVideoFramesIsAStall', async () => {
    // Without rVFC the player has nothing better than the playback quality counter.
    const { player, video, clock, emitted } = setup({ rvfc: false, counter: 'zero' });
    await player.play(LIVE);

    frames(clock, video, 50);

    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('framesCounted_rvfcAndQualityAbsent_usesWebkitDecodedFrameCount', async () => {
    const { player, video, clock, emitted } = setup({ rvfc: false, counter: 'webkit' });
    await player.play(LIVE);

    frames(clock, video, 100);
    expect(emitted('error')).toEqual([]);
    expect(player.stats.framesDecoded).toBe(100);

    clock.advance(3500);
    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('rvfcLoop_isOneLoop_afterTheFirstFrame', async () => {
    const { player, video, clock } = setup();
    await player.play(LIVE);

    frames(clock, video, 5);

    expect(video.pendingFrameCallbacks).toBe(1);
  });

  it('rvfcLoop_stop_cancelsItAndFramesAreNoLongerCounted', async () => {
    const { player, video, clock } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);

    player.stop();

    expect(video.pendingFrameCallbacks).toBe(0);
    frames(clock, video, 5);
    expect(video.pendingFrameCallbacks).toBe(0);
    expect(player.stats.framesDecoded).toBe(0);
  });

  it('rvfcLoop_newPlay_replacesTheOldLoopAndCountsFromZero', async () => {
    const { player, video, clock } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);

    await player.play(LIVE);
    expect(player.stats.framesDecoded).toBe(0);
    frames(clock, video, 3);

    expect(video.pendingFrameCallbacks).toBe(1); // a leaked old loop would make it 2
    expect(player.stats.framesDecoded).toBe(3); // and count each frame twice
  });

  it('rvfcLoop_attachToAnotherVideo_cancelsTheLoopOnTheOldOne', async () => {
    const { player, video, link, clock } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);

    player.attach(fakeVideo().element, link);

    expect(video.pendingFrameCallbacks).toBe(0);
  });
});

describe('no stall while the element is not playing', () => {
  it('recording_paused_raisesNoStalledAfter10s', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 5);

    video.paused = true;
    clock.advance(10_000);

    expect(emitted('error')).toEqual([]);
  });

  it('recording_seeking_raisesNoStalledFor5s', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 5);

    video.seeking = true;
    clock.advance(5000);
    expect(emitted('error')).toEqual([]);

    video.seeking = false;
    frames(clock, video, 50); // the seek is done, frames flow again
    expect(emitted('error')).toEqual([]);
  });

  it('recording_ended_raisesNoStalled', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 5);

    video.ended = true;
    clock.advance(60_000);

    expect(emitted('error')).toEqual([]);
  });

  it('recording_afterPlaying_aRealStallOf3sRaisesStalled', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 5);
    clock.advance(1000); // the stall poll has seen the frames, and counts the quiet time from there
    video.paused = true;
    clock.advance(10_200);

    video.paused = false; // playback resumes, but no frame follows
    clock.advance(2999);
    expect(emitted('error')).toEqual([]);
    clock.advance(501);
    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('recording_afterPlaying_theStallWindowStartsWhenPlaybackResumes', async () => {
    // Frames stopped 11 s before the resume, but the 3 s window is a fresh one.
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 5);
    clock.advance(1000);
    video.paused = true;
    clock.advance(10_200);

    video.paused = false;
    clock.advance(1000);
    video.renderFrame();
    frames(clock, video, 20);

    expect(emitted('error')).toEqual([]);
  });

  it('recording_endedThenSeekedBack_stallsAreWatchedAgain', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    frames(clock, video, 5);
    video.ended = true;
    clock.advance(30_000);

    video.ended = false; // the user seeked back and the recording plays on
    clock.advance(3500);

    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('live_documentHidden_raisesNoStalledDuringTwoMinutesInTheBackground', async () => {
    // S5: the app goes to the background and requestVideoFrameCallback stops firing (ruling R32).
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);

    video.ownerDocument.hidden = true;
    clock.advance(120_000);

    expect(emitted('error')).toEqual([]);
  });

  it('live_returnFromTheBackground_theStallWindowStartsOnReturn', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);
    clock.advance(1000); // the stall poll has seen the frames
    video.ownerDocument.hidden = true;
    clock.advance(120_000);

    video.ownerDocument.hidden = false; // back in the foreground, the stream has not resumed yet
    clock.advance(2999);
    expect(emitted('error')).toEqual([]);
    clock.advance(501);
    expect(emitted('error')).toEqual([{ reason: 'STALLED' }]);
  });

  it('live_returnFromTheBackground_framesThatFlowAgainRaiseNoStalled', async () => {
    const { player, video, clock, emitted } = setup();
    await player.play(LIVE);
    frames(clock, video, 5);
    video.ownerDocument.hidden = true;
    clock.advance(120_000);

    video.ownerDocument.hidden = false;
    clock.advance(1000);
    frames(clock, video, 50);

    expect(emitted('error')).toEqual([]);
  });

  it('paused_beforeTheFirstFrame_stillRaisesNoFirstFrameAt4s', async () => {
    // Only the stall watchdog stands down: a play() that never starts is reported.
    const { player, video, clock, emitted } = setup();
    await player.play(RECORDING);
    video.paused = true;

    clock.advance(3999);
    expect(emitted('error')).toEqual([]);
    clock.advance(1);
    expect(emitted('error')).toEqual([{ reason: 'NO_FIRST_FRAME' }]);
  });
});
