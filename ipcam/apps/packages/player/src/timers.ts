/** Arms `fn` once after `ms` and returns a function that cancels it. Injected so tests control time. */
export type SetTimer = (fn: () => void, ms: number) => () => void;

/** No first frame this long after play() starts: NO_FIRST_FRAME (spec 6.2). */
export const NO_FIRST_FRAME_MS = 4000;
/** The frame count does not move for this long: STALLED (spec 6.2). */
export const STALL_MS = 3000;
/**
 * How often the frame count is read once frames flow. A stall is reported 3.0 to
 * 3.5 s after the last frame, or after playback resumed (see `playbackActive`).
 */
export const STALL_POLL_MS = 500;

export const defaultSetTimer: SetTimer = (fn, ms) => {
  const id = setTimeout(fn, ms);
  return () => {
    clearTimeout(id);
  };
};

export type FailureReason = 'NO_FIRST_FRAME' | 'STALLED';

export interface Watchdog {
  /** The first frame arrived: drop the deadline and start watching for a stall. */
  firstFrame(): void;
  /** Disarm everything. Safe to call at any time, any number of times. */
  cancel(): void;
}

export interface WatchdogOptions {
  setTimer: SetTimer;
  now: () => number;
  framesDecoded: () => number;
  /**
   * False while no frames are expected (paused, seeking, ended, document
   * hidden). The stall check stands down then, and when this turns true again
   * it starts a fresh STALL_MS window: the time spent not playing never counts
   * as a stall. It is read at every poll, so a pause shorter than
   * STALL_POLL_MS may go unseen.
   */
  playbackActive: () => boolean;
  /** Called at most once. The watchdog has already disarmed itself when it runs. */
  onFailure: (reason: FailureReason) => void;
}

/**
 * Supervises one play: the first frame must arrive within NO_FIRST_FRAME_MS,
 * after that the frame count must not stand still for STALL_MS while playback
 * is active. The first-frame deadline does not look at `playbackActive`: a play
 * that never starts is reported even when the element is paused.
 */
export function startWatchdog({
  setTimer,
  now,
  framesDecoded,
  playbackActive,
  onFailure,
}: WatchdogOptions): Watchdog {
  let cancelTimer: (() => void) | undefined;
  let finished = false;
  let watchingStall = false;

  const cancel = (): void => {
    finished = true;
    cancelTimer?.();
    cancelTimer = undefined;
  };

  const fail = (reason: FailureReason): void => {
    cancel();
    onFailure(reason);
  };

  const watchStall = (): void => {
    let lastFrames = framesDecoded();
    let lastProgressAt = now();
    let idle = false;
    const poll = (): void => {
      if (!playbackActive()) {
        idle = true;
      } else {
        const frames = framesDecoded();
        if (idle || frames !== lastFrames) {
          // Frames moved, or playback just resumed: either way the window starts now.
          idle = false;
          lastFrames = frames;
          lastProgressAt = now();
        } else if (now() - lastProgressAt >= STALL_MS) {
          fail('STALLED');
          return;
        }
      }
      cancelTimer = setTimer(poll, STALL_POLL_MS);
    };
    cancelTimer = setTimer(poll, STALL_POLL_MS);
  };

  cancelTimer = setTimer(() => fail('NO_FIRST_FRAME'), NO_FIRST_FRAME_MS);

  return {
    firstFrame() {
      if (finished || watchingStall) return;
      watchingStall = true;
      cancelTimer?.();
      watchStall();
    },
    cancel,
  };
}
