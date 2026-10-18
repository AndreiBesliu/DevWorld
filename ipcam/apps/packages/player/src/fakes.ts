// Test doubles shared by the unit tests. Not exported from index.ts.
import { Events } from 'hls.js';
import { vi } from 'vitest';
import type { HlsErrorData, HlsLike } from './hls';
import type { MediaLink } from './player';

/** A fake clock: `now` and `setTimer` are injected into the player. */
export class FakeClock {
  private time = 0;
  private nextId = 1;
  private readonly timers = new Map<number, { at: number; fn: () => void }>();

  readonly now = (): number => this.time;

  readonly setTimer = (fn: () => void, ms: number): (() => void) => {
    const id = this.nextId++;
    this.timers.set(id, { at: this.time + ms, fn });
    return () => {
      this.timers.delete(id);
    };
  };

  /** Number of timers that are armed and not cancelled. */
  get pending(): number {
    return this.timers.size;
  }

  /** Moves time forward, firing due timers in order (timers armed by a timer included). */
  advance(ms: number): void {
    const target = this.time + ms;
    for (;;) {
      let nextId: number | undefined;
      let next: { at: number; fn: () => void } | undefined;
      for (const [id, timer] of this.timers) {
        if (timer.at <= target && (next === undefined || timer.at < next.at)) {
          nextId = id;
          next = timer;
        }
      }
      if (next === undefined || nextId === undefined) break;
      this.timers.delete(nextId);
      this.time = next.at;
      next.fn();
    }
    this.time = target;
  }
}

export class FakeMediaStream {
  readonly tracks: MediaStreamTrack[];
  constructor(tracks: MediaStreamTrack[] = []) {
    this.tracks = tracks;
  }
}

/** What the fake passes to a frame callback: the metadata fields a test sets. */
export type FrameMetadata = Partial<VideoFrameCallbackMetadata>;

type FrameCallback = (now: number, metadata: FrameMetadata) => void;

/**
 * Which playback quality counter the fake element offers:
 * - `quality`: getVideoPlaybackQuality().totalVideoFrames counts the frames.
 * - `zero`: getVideoPlaybackQuality() exists but totalVideoFrames stays 0, the
 *   unverified case of a MediaStream-backed element (ruling R31).
 * - `webkit`: no getVideoPlaybackQuality, only webkitDecodedFrameCount.
 */
export type QualityCounter = 'quality' | 'zero' | 'webkit';

/**
 * The part of HTMLVideoElement that the player touches. With `rvfc: false` the
 * element has no requestVideoFrameCallback, like an older browser.
 */
export class FakeVideo {
  srcObject: MediaProvider | null = null;
  currentTime = 0;
  playCalls = 0;
  paused = false;
  seeking = false;
  ended = false;
  /** The part of the owner document that the player reads: `hidden` is true while the app is in the background. */
  readonly ownerDocument = { hidden: false };
  /** What play() returns. Tests replace it to refuse playback, for example. */
  playResult: () => Promise<void> = () => Promise.resolve();
  getVideoPlaybackQuality?: () => VideoPlaybackQuality;
  webkitDecodedFrameCount?: number;
  requestVideoFrameCallback?: (callback: FrameCallback) => number;
  cancelVideoFrameCallback?: (handle: number) => void;
  /** Handles passed to cancelVideoFrameCallback. */
  readonly cancelledFrameCallbacks: number[] = [];

  private frames = 0;
  private nextHandle = 1;
  private readonly frameCallbacks = new Map<number, FrameCallback>();
  private readonly listeners = new Map<string, Set<() => void>>();

  constructor({ rvfc, counter }: { rvfc: boolean; counter: QualityCounter }) {
    if (counter === 'webkit') {
      this.webkitDecodedFrameCount = 0;
    } else {
      this.getVideoPlaybackQuality = () =>
        ({
          totalVideoFrames: counter === 'zero' ? 0 : this.frames,
          droppedVideoFrames: 0,
          corruptedVideoFrames: 0,
          creationTime: 0,
        }) as VideoPlaybackQuality;
    }
    if (rvfc) {
      this.requestVideoFrameCallback = (callback) => {
        const handle = this.nextHandle++;
        this.frameCallbacks.set(handle, callback);
        return handle;
      };
      this.cancelVideoFrameCallback = (handle) => {
        this.cancelledFrameCallbacks.push(handle);
        this.frameCallbacks.delete(handle);
      };
    }
  }

  /** The tracks of the stream in srcObject, or null when something else (or nothing) is there. */
  get streamTracks(): MediaStreamTrack[] | null {
    const source: unknown = this.srcObject;
    return source instanceof FakeMediaStream ? source.tracks : null;
  }

  get element(): HTMLVideoElement {
    return this as unknown as HTMLVideoElement;
  }

  /** Frame callbacks that are registered and not yet fired or cancelled. */
  get pendingFrameCallbacks(): number {
    return this.frameCallbacks.size;
  }

  listenerCount(type: string): number {
    return this.listeners.get(type)?.size ?? 0;
  }

  addEventListener(type: string, listener: () => void): void {
    const set = this.listeners.get(type) ?? new Set<() => void>();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: () => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  play(): Promise<void> {
    this.playCalls += 1;
    return this.playResult();
  }

  /**
   * The element presents one frame: the position moves and the browser fires
   * its events. `metadata` is what the frame callbacks get, `receiveTime` for
   * example.
   */
  renderFrame(metadata: FrameMetadata = {}): void {
    this.frames += 1;
    if (this.webkitDecodedFrameCount !== undefined) this.webkitDecodedFrameCount = this.frames;
    this.currentTime += 0.04;
    const due = [...this.frameCallbacks.values()];
    this.frameCallbacks.clear();
    for (const callback of due) callback(0, metadata);
    this.fireTimeUpdate();
  }

  fireTimeUpdate(): void {
    for (const listener of [...(this.listeners.get('timeupdate') ?? [])]) listener();
  }
}

export function fakeVideo(rvfc = true, counter: QualityCounter = 'quality'): FakeVideo {
  return new FakeVideo({ rvfc, counter });
}

/** The tests run in Node, which has no MediaStream. */
export function stubMediaStream(): void {
  vi.stubGlobal('MediaStream', FakeMediaStream);
}

export function fakeTrack(id = 'track-1'): MediaStreamTrack {
  return { id, kind: 'video' } as MediaStreamTrack;
}

export class FakeLink implements MediaLink {
  readonly webrtcTrack = vi.fn<MediaLink['webrtcTrack']>(() => Promise.resolve(this.track));
  readonly byteStream = vi.fn<MediaLink['byteStream']>(() =>
    Promise.reject(new Error('byteStream is not used by this plan')),
  );
  readonly track: MediaStreamTrack;
  constructor(track: MediaStreamTrack = fakeTrack()) {
    this.track = track;
  }
}

type HlsErrorListener = (event: Events.ERROR, data: HlsErrorData) => void;

export class FakeHls implements HlsLike {
  media: HTMLMediaElement | null = null;
  levels: { videoCodec?: string }[] = [];
  currentLevel = -1;
  loadedUrl: string | undefined;
  destroyed = false;
  private errorListeners: HlsErrorListener[] = [];

  on(_event: Events.ERROR, listener: HlsErrorListener): void {
    this.errorListeners.push(listener);
  }

  /** hls.js reports an error, fatal or not. `details` is its ErrorDetails value, e.g. `manifestLoadError`. */
  emitError(details: string, fatal = true): void {
    for (const listener of [...this.errorListeners]) listener(Events.ERROR, { fatal, details });
  }

  attachMedia(media: HTMLMediaElement): void {
    this.media = media;
  }

  loadSource(url: string): void {
    this.loadedUrl = url;
  }

  destroy(): void {
    this.destroyed = true;
    this.media = null;
    this.errorListeners = [];
  }
}
