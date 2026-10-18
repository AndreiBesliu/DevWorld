import Hls, { Events } from 'hls.js';

/** The part of hls.js's `ErrorData` that the player reads. `details` is an `ErrorDetails` value, e.g. `manifestLoadError`. */
export interface HlsErrorData {
  readonly fatal: boolean;
  readonly details: string;
}

/** The part of an hls.js instance the player uses. hls.js's own `Hls` satisfies it. */
export interface HlsLike {
  readonly media: HTMLMediaElement | null;
  readonly levels: readonly { readonly videoCodec?: string | undefined }[];
  readonly currentLevel: number;
  /** Subscribes to hls.js errors. `destroy()` drops the subscription. */
  on(event: Events.ERROR, listener: (event: Events.ERROR, data: HlsErrorData) => void): void;
  attachMedia(media: HTMLMediaElement): void;
  loadSource(url: string): void;
  destroy(): void;
}

export const defaultHlsFactory = (): HlsLike => new Hls({ lowLatencyMode: false });

/**
 * Plays the playlist at `url` in the element. `hls.destroy()` undoes it.
 * `onFatalError` gets the `details` of every fatal hls.js error (a playlist 404,
 * a fatal media error); hls.js's non-fatal errors are recoveries under way.
 */
export function startHls(
  hls: HlsLike,
  video: HTMLVideoElement,
  url: string,
  onFatalError: (details: string) => void,
): void {
  hls.on(Events.ERROR, (_event, data) => {
    if (data.fatal) onFatalError(data.details);
  });
  hls.attachMedia(video);
  hls.loadSource(url);
}

/** The video codec of the level being played, once hls.js has chosen one. */
export function hlsCodec(hls: HlsLike): string {
  return hls.levels[hls.currentLevel]?.videoCodec ?? 'unknown';
}
