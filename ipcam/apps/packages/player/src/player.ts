import { defaultHlsFactory, hlsCodec, startHls, type HlsLike } from './hls';
import { framesDecoded, watchFirstFrame } from './stats';
import { defaultSetTimer, startWatchdog, type SetTimer, type Watchdog } from './timers';
import { attachTrack } from './webrtc';

export type Transport = 'webrtc' | 'mse' | 'hls' | 'snapshot';

export interface PlayerSource {
  kind: 'live' | 'recording';
  cameraId: string;
  profile: 'main' | 'sub';
  /**
   * Recordings: the wall-clock time of media position 0, the reference for
   * `seek`. Without it the reference is the Unix epoch, so `seek(new Date(ms))`
   * is an offset of `ms` milliseconds into the recording.
   */
  from?: Date;
  /** Recording: HLS playlist URL (spike); production uses a ReadRange loader (CTR-12). */
  url?: string;
}

/** Provided by @ipcam/core; the player does not know whether the hub is on the LAN or in the cloud. */
export interface MediaLink {
  webrtcTrack(slot: number): Promise<MediaStreamTrack>;
  byteStream(call: 'mse' | 'segment', req: Uint8Array): Promise<ReadableStream<Uint8Array>>;
}

export interface Player {
  /**
   * Binds the player to an element and a link. The player does not change the
   * element's settings: set `muted` and `playsInline` before `play`, because
   * browsers only autoplay muted video without a user gesture. Attaching
   * while something plays stops it.
   */
  attach(video: HTMLVideoElement, link: MediaLink): void;
  /** Resolves once playback is set up, not at the first frame: that is the `first-frame` event. */
  play(src: PlayerSource, prefer?: Transport[]): Promise<void>;
  /** Recordings only. Resolves once the seek is requested; the element's `seeked` event says when it is done. */
  seek(t: Date): Promise<void>;
  stop(): void;
  readonly stats: { transport: Transport; codec: string; framesDecoded: number; latencyMs?: number; hardware?: boolean };
  /** Returns the function that removes the callback. */
  on(e: 'first-frame' | 'transport-changed' | 'error', cb: (d: unknown) => void): () => void;
}

export interface PlayerOptions {
  /** Milliseconds on a monotonic clock. Default: `performance.now`. */
  now?: () => number;
  /** Default: `setTimeout`. */
  setTimer?: SetTimer;
  /** The PeerConnection slot that live video is read from. Default: 0. */
  slot?: number;
  /** Default: hls.js with `lowLatencyMode: false`. */
  hlsFactory?: () => HlsLike;
}

export type PlayerErrorCode = 'UNSUPPORTED_TRANSPORT' | 'NOT_ATTACHED' | 'MISSING_URL' | 'NOT_A_RECORDING';

/** Why a call was refused. The message is the code. */
export class PlayerError extends Error {
  readonly code: PlayerErrorCode;

  constructor(code: PlayerErrorCode) {
    super(code);
    this.name = 'PlayerError';
    this.code = code;
  }
}

type PlayerEvent = Parameters<Player['on']>[0];

/** What a call to `play` will do, once its arguments have been checked. */
type Plan =
  | { readonly transport: 'webrtc' }
  | { readonly transport: 'hls'; readonly url: string; readonly origin: Date };

/**
 * The player has one transport per kind of content for now: WebRTC for live and
 * HLS for recordings. MSE and snapshot come later, so a `prefer` list without
 * the one that fits is refused.
 */
function planFor(src: PlayerSource, prefer: Transport[] | undefined): Plan {
  const transport = src.kind === 'live' ? 'webrtc' : 'hls';
  if (prefer && !prefer.includes(transport)) throw new PlayerError('UNSUPPORTED_TRANSPORT');
  if (transport === 'webrtc') return { transport };
  if (!src.url) throw new PlayerError('MISSING_URL');
  return { transport, url: src.url, origin: src.from ?? new Date(0) };
}

/** One call to `play`, from its start until `stop` or the next `play`. */
interface Run {
  readonly transport: Transport;
  readonly startedAt: number;
  readonly watchdog: Watchdog;
  /** Recordings: the instance and the reference time of `seek`. */
  recording?: { readonly hls: HlsLike; readonly origin: Date };
  stopFirstFrame?: () => void;
  detach?: () => void;
}

export function createPlayer(opts: PlayerOptions = {}): Player {
  const now = opts.now ?? (() => performance.now());
  const setTimer = opts.setTimer ?? defaultSetTimer;
  const slot = opts.slot ?? 0;
  const hlsFactory = opts.hlsFactory ?? defaultHlsFactory;

  let video: HTMLVideoElement | undefined;
  let link: MediaLink | undefined;
  let run: Run | undefined;
  /** The transport of the latest play(); `transport-changed` fires when a play uses another one. */
  let lastTransport: Transport | undefined;

  const listeners: Record<PlayerEvent, Set<(data: unknown) => void>> = {
    'first-frame': new Set(),
    'transport-changed': new Set(),
    error: new Set(),
  };

  const emit = (event: PlayerEvent, data: unknown): void => {
    const registered = listeners[event];
    // A snapshot, so that a callback may unsubscribe itself or another one.
    for (const listener of [...registered]) {
      if (registered.has(listener)) listener(data);
    }
  };

  const endRun = (): void => {
    const ending = run;
    run = undefined;
    ending?.watchdog.cancel();
    ending?.stopFirstFrame?.();
    ending?.detach?.();
  };

  const onFirstFrame = (frame: Run): void => {
    if (run !== frame) return;
    frame.watchdog.firstFrame();
    emit('first-frame', { transport: frame.transport, elapsedMs: now() - frame.startedAt });
  };

  const play = async (src: PlayerSource, prefer?: Transport[]): Promise<void> => {
    if (!video || !link) throw new PlayerError('NOT_ATTACHED');
    const element = video;
    const mediaLink = link;
    const plan = planFor(src, prefer);

    endRun();
    // The deadline runs from here, so a track request that never answers is reported too.
    const current: Run = {
      transport: plan.transport,
      startedAt: now(),
      watchdog: startWatchdog({
        setTimer,
        now,
        framesDecoded: () => framesDecoded(element),
        onFailure: (reason) => {
          if (run === current) emit('error', { reason });
        },
      }),
    };
    run = current;

    try {
      if (plan.transport !== lastTransport) {
        lastTransport = plan.transport;
        emit('transport-changed', { transport: plan.transport });
        if (run !== current) return; // the callback stopped or restarted the player
      }
      if (plan.transport === 'webrtc') {
        const track = await mediaLink.webrtcTrack(slot);
        if (run !== current) return; // stopped or replaced while waiting for the track
        current.detach = attachTrack(element, track);
      } else {
        const hls = hlsFactory();
        current.recording = { hls, origin: plan.origin };
        current.detach = () => {
          hls.destroy();
        };
        startHls(hls, element, plan.url);
      }
      current.stopFirstFrame = watchFirstFrame(element, () => {
        onFirstFrame(current);
      });
      // A refused or interrupted play() needs no handling here: without a frame the deadline reports it.
      element.play().catch(() => undefined);
    } catch (error) {
      if (run === current) endRun();
      throw error;
    }
  };

  const seek = (t: Date): Promise<void> => {
    const recording = run?.recording;
    if (!recording) return Promise.reject(new PlayerError('NOT_A_RECORDING'));
    const element = recording.hls.media;
    if (!element) return Promise.reject(new PlayerError('NOT_ATTACHED'));
    element.currentTime = Math.max(0, (t.getTime() - recording.origin.getTime()) / 1000);
    return Promise.resolve();
  };

  return {
    attach(newVideo, newLink) {
      endRun();
      video = newVideo;
      link = newLink;
    },
    play,
    seek,
    stop: endRun,
    get stats() {
      return {
        transport: lastTransport ?? 'webrtc',
        codec: run?.recording ? hlsCodec(run.recording.hls) : 'unknown',
        framesDecoded: run && video ? framesDecoded(video) : 0,
      };
    },
    on(event, callback) {
      const listener = (data: unknown): void => {
        callback(data);
      };
      listeners[event].add(listener);
      return () => {
        listeners[event].delete(listener);
      };
    },
  };
}
