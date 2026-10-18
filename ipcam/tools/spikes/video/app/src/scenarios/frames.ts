// What several scenarios do with a player and its element: start a stream and
// time its first frame, follow the presented frames.
import type { Player, PlayerSource } from '@ipcam/player';
import type { BandCode } from '../band';
import { BindError, type BindResult, type Profile, type SpikeSession } from '../session';
import type { Run } from './runner';

/** Why the player gave up: its `error` reason, and its cause, which is only a hint. */
export interface PlayerFailure {
  reason: string;
  hint?: string;
}

/** The first frame `ms` after the start, the player's error, or nothing in time. */
export type FirstFrame = { ms: number } | { failure: PlayerFailure } | 'timeout';

/**
 * Reads the player's `error` payload. Its cause is the last thing that went
 * wrong in that play, which is not proof that it is why no frame came (Task 7b),
 * so it is kept as a hint.
 */
export function playerFailure(data: unknown): PlayerFailure {
  const { reason, cause } = (data ?? {}) as { reason?: unknown; cause?: unknown };
  return {
    reason: typeof reason === 'string' ? reason : 'unknown error',
    ...(typeof cause === 'string' ? { hint: cause } : {}),
  };
}

/** `REASON`, or `REASON (hint: cause)`. */
export function describeFailure({ reason, hint }: PlayerFailure): string {
  return hint === undefined ? reason : `${reason} (hint: ${hint})`;
}

/** Why a bind failed: the hub's code, or the error's message. */
export function bindFailure(error: unknown): string {
  if (error instanceof BindError) return error.code;
  return error instanceof Error ? error.message : String(error);
}

export const liveSource = (cameraId: string, profile: Profile): PlayerSource => ({ kind: 'live', cameraId, profile });

/**
 * Whether a presented frame belongs to the start made at `t0`, "the first frame
 * of the matching epoch". A slot's track is reused from bind to bind, so a
 * frame of the previous bind can still be presented after the next bind:
 * Chrome 141 did, 0.15 to 1 s late, while the hub's RTP timestamps stood still
 * across an empty slot (the session package fixed that in Task 9b; this check
 * stays as a guard). Where the browser says when a frame arrived (the
 * `receiveTime` of requestVideoFrameCallback, for WebRTC in Chromium), a frame
 * counts when it arrived at or after `t0`; elsewhere when its band differs from
 * `stale`, the band the element showed before the start.
 */
export function isFrameOfStart(
  metadata: { receiveTime?: number },
  t0: number,
  band: BandCode | null,
  stale: BandCode | null,
): boolean {
  if (typeof metadata.receiveTime === 'number') return metadata.receiveTime >= t0;
  return stale === null || band === null || band.camera !== stale.camera || band.frame !== stale.frame;
}

/**
 * Listens, from now on, for the first frame of a start made at `t0` and for
 * the player's error. With requestVideoFrameCallback (R8) the frame is the
 * element's first presented frame that isFrameOfStart accepts, timed at its
 * display; without it, the player's `first-frame` event. `wait` resolves with
 * the time since `t0`, the error, or `timeout` after `ms`; `stop` stops
 * listening.
 */
export function watchStart(
  run: Run,
  player: Player,
  video: HTMLVideoElement,
  t0: number,
  stale: BandCode | null = null,
): { wait(ms: number): Promise<FirstFrame>; stop(): void } {
  let settle: (outcome: { ms: number } | { failure: PlayerFailure }) => void = () => undefined;
  const outcome = new Promise<{ ms: number } | { failure: PlayerFailure }>((resolve) => {
    settle = resolve;
  });
  const offError = player.on('error', (data) => {
    settle({ failure: playerFailure(data) });
  });
  let offFrame: () => void;
  if (typeof video.requestVideoFrameCallback === 'function') {
    let handle = 0;
    let watching = true;
    const onFrame: VideoFrameRequestCallback = (now, metadata) => {
      if (!watching) return;
      if (isFrameOfStart(metadata, t0, run.env.readBand(video), stale)) {
        watching = false;
        settle({ ms: (metadata.expectedDisplayTime ?? now) - t0 });
        return;
      }
      handle = video.requestVideoFrameCallback(onFrame);
    };
    handle = video.requestVideoFrameCallback(onFrame);
    offFrame = () => {
      if (!watching) return;
      watching = false;
      video.cancelVideoFrameCallback(handle);
    };
  } else {
    offFrame = player.on('first-frame', () => {
      settle({ ms: run.now() - t0 });
    });
  }
  const stop = (): void => {
    offFrame();
    offError();
  };
  return {
    wait: (ms) => run.within(outcome, ms).finally(stop),
    stop,
  };
}

/**
 * How long a started stream plays before the scenario empties its slot again,
 * as a person looks at the tile they tapped.
 */
export const PLAY_BEFORE_STOP_MS = 1000;

export type Started = { bound: BindResult; frame: FirstFrame } | { bindError: string };

export interface StreamStart {
  session: SpikeSession;
  player: Player;
  video: HTMLVideoElement;
  slot: number;
  cam: string;
  profile: Profile;
  /** How long to wait for the bind and the first frame together. */
  timeoutMs: number;
  /** The band the element showed before the start, if any: see isFrameOfStart. */
  stale?: BandCode | null;
}

/**
 * Starts `cam`/`profile` on `slot` as the app does on a tap: the bind and the
 * player start at the same moment, and the first frame of this bind is timed
 * from it. A refused bind ends the wait at once.
 */
export async function startStream(run: Run, start: StreamStart): Promise<Started> {
  const { session, player, video, slot, cam, profile, timeoutMs } = start;
  const t0 = run.now();
  const watch = watchStart(run, player, video, t0, start.stale ?? null);
  const bind = session.bind(slot, cam, profile).then(
    (bound) => ({ bound }),
    (error: unknown) => ({ bindError: bindFailure(error) }),
  );
  try {
    // `prefer` stays undefined: the player's default plan (Task 7 notes).
    await player.play(liveSource(cam, profile));
  } catch (error) {
    watch.stop();
    throw error;
  }
  const answer = await run.within(bind, timeoutMs);
  if (answer === 'timeout') {
    watch.stop();
    return { bindError: 'no answer' };
  }
  if ('bindError' in answer) {
    watch.stop();
    return answer;
  }
  return { bound: answer.bound, frame: await watch.wait(Math.max(0, timeoutMs - (run.now() - t0))) };
}

/** `codec`, plus the profile-level-id when the hub sent the stream in another profile (R23). */
export function describeBound(bound: BindResult): string {
  return bound.fallback === undefined ? bound.codec : `${bound.codec}, fallback profile ${bound.fallback}`;
}

/**
 * Calls `cb` with the display time (performance clock) of every frame the
 * element presents: with requestVideoFrameCallback where the element has it
 * (R8, feature detection), else once per animation frame. Returns the function
 * that stops.
 */
export function onPresentedFrames(video: HTMLVideoElement, cb: (displayPerfMs: number) => void): () => void {
  let stopped = false;
  if (typeof video.requestVideoFrameCallback === 'function') {
    let handle = 0;
    const onFrame: VideoFrameRequestCallback = (now, metadata) => {
      if (stopped) return;
      cb(metadata.expectedDisplayTime ?? now);
      handle = video.requestVideoFrameCallback(onFrame);
    };
    handle = video.requestVideoFrameCallback(onFrame);
    return () => {
      stopped = true;
      video.cancelVideoFrameCallback(handle);
    };
  }
  if (typeof requestAnimationFrame !== 'function') return () => undefined;
  let handle = 0;
  const onAnimationFrame = (now: number): void => {
    if (stopped) return;
    cb(now);
    handle = requestAnimationFrame(onAnimationFrame);
  };
  handle = requestAnimationFrame(onAnimationFrame);
  return () => {
    stopped = true;
    cancelAnimationFrame(handle);
  };
}
