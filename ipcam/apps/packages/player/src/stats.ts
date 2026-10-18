/** A running count of the frames an element presents. */
export interface FrameCount {
  count(): number;
  /** Stops counting. Safe to call any number of times. */
  stop(): void;
}

/** `webkitDecodedFrameCount` is non-standard, so lib.dom does not declare it. */
type WebkitCounter = { readonly webkitDecodedFrameCount?: number };

/**
 * The browser's own frame counters, for elements without
 * requestVideoFrameCallback: `getVideoPlaybackQuality().totalVideoFrames`, then
 * `webkitDecodedFrameCount`. 0 when the element offers neither.
 */
function browserFrameCount(video: HTMLVideoElement): number {
  if (typeof video.getVideoPlaybackQuality === 'function') {
    return video.getVideoPlaybackQuality().totalVideoFrames;
  }
  return (video as WebkitCounter).webkitDecodedFrameCount ?? 0;
}

/**
 * Counts the frames the user sees (ruling R31). Where the element has
 * requestVideoFrameCallback (feature detection, not browser sniffing) it counts
 * the callbacks of a loop that registers itself again on every presented frame:
 * `totalVideoFrames` is unverified on a MediaStream-backed element, and a
 * counter stuck at 0 would read as a stall. Elsewhere it reads the browser's
 * own counter on demand. The stall check compares successive readings, so only
 * their movement matters.
 */
export function countFrames(video: HTMLVideoElement): FrameCount {
  if (typeof video.requestVideoFrameCallback !== 'function') {
    return { count: () => browserFrameCount(video), stop: () => undefined };
  }

  let presented = 0;
  let pending: number | undefined;
  const onFrame = (): void => {
    presented += 1;
    pending = video.requestVideoFrameCallback(onFrame);
  };
  pending = video.requestVideoFrameCallback(onFrame);
  return {
    count: () => presented,
    stop() {
      if (pending === undefined) return;
      video.cancelVideoFrameCallback(pending);
      pending = undefined;
    },
  };
}

/** False while the element is paused, seeking or ended: no frames are expected then (ruling R31). */
export function isPlaybackActive(video: HTMLVideoElement): boolean {
  return !video.paused && !video.seeking && !video.ended;
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
