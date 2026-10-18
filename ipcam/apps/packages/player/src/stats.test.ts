import { describe, expect, it, vi } from 'vitest';
import { fakeVideo } from './fakes';
import { countFrames, isPlaybackActive, watchFirstFrame } from './stats';

describe('countFrames', () => {
  it('countFrames_rvfcPresent_countsOnePerPresentedFrame', () => {
    const video = fakeVideo(true);
    const frames = countFrames(video.element);
    expect(frames.count()).toBe(0);

    video.renderFrame();
    video.renderFrame();
    video.renderFrame();

    expect(frames.count()).toBe(3);
    expect(video.pendingFrameCallbacks).toBe(1); // the loop keeps itself registered
  });

  it('countFrames_rvfcPresent_ignoresAPlaybackQualityCounterStuckAtZero', () => {
    const video = fakeVideo(true, 'zero');
    const frames = countFrames(video.element);

    for (let i = 0; i < 25; i += 1) video.renderFrame();

    expect(video.element.getVideoPlaybackQuality().totalVideoFrames).toBe(0);
    expect(frames.count()).toBe(25);
  });

  it('countFrames_stop_cancelsTheLoop', () => {
    const video = fakeVideo(true);
    const frames = countFrames(video.element);
    video.renderFrame();

    frames.stop();
    video.renderFrame();
    video.renderFrame();

    expect(frames.count()).toBe(1);
    expect(video.pendingFrameCallbacks).toBe(0);
    expect(video.cancelledFrameCallbacks).toHaveLength(1);
  });

  it('countFrames_stopTwice_cancelsOnce', () => {
    const video = fakeVideo(true);
    const frames = countFrames(video.element);

    frames.stop();
    frames.stop();

    expect(video.cancelledFrameCallbacks).toHaveLength(1);
  });

  it('countFrames_rvfcAbsent_readsTotalVideoFrames', () => {
    const video = fakeVideo(false);
    const frames = countFrames(video.element);
    expect(frames.count()).toBe(0);

    video.renderFrame();
    video.renderFrame();

    expect(frames.count()).toBe(2);
    expect(() => frames.stop()).not.toThrow();
  });

  it('countFrames_rvfcAndQualityAbsent_readsWebkitDecodedFrameCount', () => {
    const video = fakeVideo(false, 'webkit');
    const frames = countFrames(video.element);

    video.renderFrame();
    video.renderFrame();
    video.renderFrame();

    expect(frames.count()).toBe(3);
  });

  it('countFrames_noCounterAtAll_isZero', () => {
    const video = fakeVideo(false, 'webkit');
    delete video.webkitDecodedFrameCount;

    expect(countFrames(video.element).count()).toBe(0);
  });
});

describe('isPlaybackActive', () => {
  it('isPlaybackActive_pausedSeekingOrEnded_isFalse', () => {
    const video = fakeVideo();
    expect(isPlaybackActive(video.element)).toBe(true);

    video.paused = true;
    expect(isPlaybackActive(video.element)).toBe(false);
    video.paused = false;

    video.seeking = true;
    expect(isPlaybackActive(video.element)).toBe(false);
    video.seeking = false;

    video.ended = true;
    expect(isPlaybackActive(video.element)).toBe(false);
    video.ended = false;

    expect(isPlaybackActive(video.element)).toBe(true);
  });
});

describe('watchFirstFrame', () => {
  it('rvfcPresent_firesOnTheFirstPresentedFrameOnly', () => {
    const video = fakeVideo(true);
    const onFirstFrame = vi.fn();

    watchFirstFrame(video.element, onFirstFrame);
    expect(onFirstFrame).not.toHaveBeenCalled();
    video.renderFrame();
    video.renderFrame();

    expect(onFirstFrame).toHaveBeenCalledTimes(1);
    expect(video.pendingFrameCallbacks).toBe(0);
  });

  it('rvfcPresent_ignoresTimeupdate', () => {
    const video = fakeVideo(true);
    const onFirstFrame = vi.fn();
    watchFirstFrame(video.element, onFirstFrame);

    video.currentTime = 5;
    video.fireTimeUpdate();

    expect(onFirstFrame).not.toHaveBeenCalled();
    expect(video.listenerCount('timeupdate')).toBe(0);
  });

  it('rvfcAbsent_firesOnTheFirstTimeupdateWithPositiveTime', () => {
    const video = fakeVideo(false);
    const onFirstFrame = vi.fn();

    watchFirstFrame(video.element, onFirstFrame);
    video.fireTimeUpdate();
    expect(onFirstFrame).not.toHaveBeenCalled();

    video.renderFrame();
    video.renderFrame();
    expect(onFirstFrame).toHaveBeenCalledTimes(1);
    expect(video.listenerCount('timeupdate')).toBe(0);
  });

  it('cancel_withRvfc_cancelsTheCallback', () => {
    const video = fakeVideo(true);
    const onFirstFrame = vi.fn();

    const cancel = watchFirstFrame(video.element, onFirstFrame);
    cancel();
    video.renderFrame();

    expect(onFirstFrame).not.toHaveBeenCalled();
    expect(video.cancelledFrameCallbacks).toHaveLength(1);
  });

  it('cancel_withoutRvfc_removesTheListener', () => {
    const video = fakeVideo(false);
    const onFirstFrame = vi.fn();

    const cancel = watchFirstFrame(video.element, onFirstFrame);
    expect(video.listenerCount('timeupdate')).toBe(1);
    cancel();
    video.renderFrame();

    expect(onFirstFrame).not.toHaveBeenCalled();
    expect(video.listenerCount('timeupdate')).toBe(0);
  });

  it('cancel_afterTheFirstFrame_cancelsNothing', () => {
    const video = fakeVideo(true);
    const cancel = watchFirstFrame(video.element, () => undefined);
    video.renderFrame();

    cancel();

    expect(video.cancelledFrameCallbacks).toEqual([]);
  });
});
