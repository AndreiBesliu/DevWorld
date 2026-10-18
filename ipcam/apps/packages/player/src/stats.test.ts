import { describe, expect, it, vi } from 'vitest';
import { fakeVideo } from './fakes';
import { framesDecoded, watchFirstFrame } from './stats';

describe('framesDecoded', () => {
  it('framesDecoded_readsThePlaybackQualityFrameCount', () => {
    const video = fakeVideo();
    expect(framesDecoded(video.element)).toBe(0);
    video.renderFrame();
    video.renderFrame();
    expect(framesDecoded(video.element)).toBe(2);
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
