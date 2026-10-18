import Hls from 'hls.js';

/** The part of an hls.js instance the player uses. hls.js's own `Hls` satisfies it. */
export interface HlsLike {
  readonly media: HTMLMediaElement | null;
  readonly levels: readonly { readonly videoCodec?: string | undefined }[];
  readonly currentLevel: number;
  attachMedia(media: HTMLMediaElement): void;
  loadSource(url: string): void;
  destroy(): void;
}

export const defaultHlsFactory = (): HlsLike => new Hls({ lowLatencyMode: false });

/** Plays the playlist at `url` in the element. `hls.destroy()` undoes it. */
export function startHls(hls: HlsLike, video: HTMLVideoElement, url: string): void {
  hls.attachMedia(video);
  hls.loadSource(url);
}

/** The video codec of the level being played, once hls.js has chosen one. */
export function hlsCodec(hls: HlsLike): string {
  return hls.levels[hls.currentLevel]?.videoCodec ?? 'unknown';
}
