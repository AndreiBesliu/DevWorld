/**
 * Frames the element has produced so far, from the playback quality counters
 * (`totalVideoFrames`). It only has to move while video flows: the stall check
 * compares successive readings.
 */
export function framesDecoded(video: HTMLVideoElement): number {
  return video.getVideoPlaybackQuality().totalVideoFrames;
}

/**
 * Calls `onFirstFrame` once, when the first frame has been presented. Returns a
 * function that stops watching.
 *
 * It uses requestVideoFrameCallback wherever the element has it (feature
 * detection, not browser sniffing). Without it, the first `timeupdate` with
 * `currentTime > 0` stands in for the first frame.
 */
export function watchFirstFrame(video: HTMLVideoElement, onFirstFrame: () => void): () => void {
  if (typeof video.requestVideoFrameCallback === 'function') {
    let waiting = true;
    const handle = video.requestVideoFrameCallback(() => {
      waiting = false;
      onFirstFrame();
    });
    return () => {
      if (!waiting) return;
      waiting = false;
      video.cancelVideoFrameCallback(handle);
    };
  }

  const onTimeUpdate = (): void => {
    if (video.currentTime > 0) {
      video.removeEventListener('timeupdate', onTimeUpdate);
      onFirstFrame();
    }
  };
  video.addEventListener('timeupdate', onTimeUpdate);
  return () => {
    video.removeEventListener('timeupdate', onTimeUpdate);
  };
}
