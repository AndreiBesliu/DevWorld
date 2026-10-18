// Test doubles for the scenario tests: a fake hub behind the SpikeSession
// interface, video elements, the stage, the document and a player. Not part of
// the shipped app.
import { setImmediate as realSetImmediate } from 'node:timers';
import { createPlayer, type MediaLink, type Player, type PlayerOptions, type PlayerSource } from '@ipcam/player';
import { vi } from 'vitest';
import type { BandCode } from '../band';
import type { HubCamera } from '../cameras';
import { createI18n } from '../i18n';
import type { TileStats } from '../metrics';
import type { SpikeNative } from '../native';
import { BindError, type BindResult, type Pair, type Profile, type SpikeSession } from '../session';
import type { WakeLock } from '../wakelock';
import type { ScenarioEnv, Stage } from './context';

/** The cameras of a hub started with `-sims 4`, as GET /spike/v1/cameras lists them. */
export const TEST_CAMERAS: HubCamera[] = [
  ...[1, 2, 3, 4].map((n) => ({ id: `sim-0${n}`, camera: n, codec: 'H264', profiles: ['sub', 'main'] })),
  { id: 'mx-h265', camera: 17, codec: 'H265', profiles: ['main', 'sub'] },
  { id: 'mx-high51', camera: 18, codec: 'H264', profileLevel: '640033', profiles: ['main', 'sub'] },
];

export const TEST_HUB = 'http://192.168.1.20:7080';

/** `document` as far as the scenarios and the player read it. */
export class FakeDocument extends EventTarget {
  visibilityState: DocumentVisibilityState = 'visible';

  get hidden(): boolean {
    return this.visibilityState === 'hidden';
  }

  hide(): void {
    this.visibilityState = 'hidden';
    this.dispatchEvent(new Event('visibilitychange'));
  }

  show(): void {
    this.visibilityState = 'visible';
    this.dispatchEvent(new Event('visibilitychange'));
  }
}

export class FakeTrack {
  readonly kind = 'video';
  /** The last frame the track received, which an element with `replaysLastFrame` presents again when it attaches the track. */
  last: { band: BandCode | null; receivedAt: number } | undefined;
  constructor(readonly id: string) {}
}

/** The global MediaStream that the player's attachTrack creates. */
export class FakeMediaStream {
  readonly tracks: unknown[];
  constructor(tracks: unknown[] = []) {
    this.tracks = tracks;
  }
}

type FrameCallback = (now: number, metadata: { expectedDisplayTime: number; receiveTime?: number }) => void;

/** The part of HTMLVideoElement that the player and the scenarios use. */
export class FakeVideo extends EventTarget {
  muted = false;
  playsInline = false;
  paused = true;
  seeking = false;
  ended = false;
  videoWidth = 640;
  videoHeight = 360;
  /** The band of the frame on screen, which the scenarios' band reader returns. */
  band: BandCode | null = null;
  /** muted and playsInline at each play(): browsers only autoplay muted inline video. */
  readonly playCalls: { muted: boolean; playsInline: boolean }[] = [];
  presented = 0;
  /** Absent when the test makes an element without requestVideoFrameCallback (R8). */
  requestVideoFrameCallback?: (cb: FrameCallback) => number;
  cancelVideoFrameCallback?: (handle: number) => void;
  readonly ownerDocument: FakeDocument;
  private time = 0;
  private nextHandle = 1;
  private readonly callbacks = new Map<number, FrameCallback>();

  /** False for a browser whose frame callbacks do not say when a frame arrived (no `receiveTime`). */
  reportsReceiveTime = true;

  constructor(doc: FakeDocument, rvfc = true) {
    super();
    this.ownerDocument = doc;
    if (rvfc) {
      this.requestVideoFrameCallback = (cb) => {
        const handle = this.nextHandle++;
        this.callbacks.set(handle, cb);
        return handle;
      };
      this.cancelVideoFrameCallback = (handle) => {
        this.callbacks.delete(handle);
      };
    }
  }

  get element(): HTMLVideoElement {
    return this as unknown as HTMLVideoElement;
  }

  private source: unknown = null;

  get srcObject(): unknown {
    return this.source;
  }

  /**
   * A frame of the previous bind presented after the track is attached again.
   * Chrome 141 did this in the Task 9 smoke run, 0.15 to 1 s late, while the
   * hub's RTP timestamps stood still across an empty slot (fixed in Task 9b).
   * The element also presents its track's last frame again when play()
   * resumes it after a pause, as after the background (S5).
   */
  replaysLastFrame = false;

  set srcObject(value: unknown) {
    this.source = value;
    const last = value instanceof FakeMediaStream ? value.tracks.find((t) => t instanceof FakeTrack)?.last : undefined;
    if (!last || !this.replaysLastFrame) return;
    setTimeout(() => {
      if (this.source === value) this.present(last.band, last.receivedAt);
    }, 16);
  }

  /** The tracks the element shows. */
  get tracks(): unknown[] {
    return this.srcObject instanceof FakeMediaStream ? this.srcObject.tracks : [];
  }

  get currentTime(): number {
    return this.time;
  }

  /** A seek: `seeked` follows after `seekMs`, with a frame. */
  set currentTime(value: number) {
    this.time = value;
    this.seeking = true;
    setTimeout(() => {
      this.seeking = false;
      this.dispatchEvent(new Event('seeked'));
      this.present();
    }, this.seekMs);
  }

  seekMs = 150;

  play(): Promise<void> {
    this.playCalls.push({ muted: this.muted, playsInline: this.playsInline });
    if (this.paused && this.replaysLastFrame) {
      const source = this.source;
      const last = source instanceof FakeMediaStream ? source.tracks.find((t) => t instanceof FakeTrack)?.last : undefined;
      if (last) {
        setTimeout(() => {
          if (this.source === source) this.present(last.band, last.receivedAt);
        }, 16);
      }
    }
    this.paused = false;
    return Promise.resolve();
  }

  pause(): void {
    this.paused = true;
  }

  /** A frame is shown now: every pending frame callback runs once. `receivedAt`: when the frame arrived, default now. */
  present(band: BandCode | null = this.band, receivedAt?: number): void {
    this.band = band;
    this.presented += 1;
    const now = performance.now();
    const due = [...this.callbacks.values()];
    this.callbacks.clear();
    const metadata = this.reportsReceiveTime ? { expectedDisplayTime: now, receiveTime: receivedAt ?? now } : { expectedDisplayTime: now };
    for (const cb of due) cb(now, metadata);
  }
}

/** The stage on which the scenarios lay out their video elements. */
export class FakeStage implements Stage {
  /** The tiles shown now; the elements are reused across layouts, like the app's stage. */
  tiles: FakeVideo[] = [];
  private readonly pool: FakeVideo[] = [];
  big: FakeVideo | undefined;
  readonly said: string[] = [];
  /** Every element the stage made, for the fake hub to present frames on. */
  readonly all: FakeVideo[] = [];
  rvfc = true;

  constructor(readonly doc: FakeDocument) {}

  reportsReceiveTime = true;
  replaysLastFrame = false;

  private make(): FakeVideo {
    const video = new FakeVideo(this.doc, this.rvfc);
    video.reportsReceiveTime = this.reportsReceiveTime;
    video.replaysLastFrame = this.replaysLastFrame;
    this.all.push(video);
    return video;
  }

  grid(n: number): HTMLVideoElement[] {
    while (this.pool.length < n) this.pool.push(this.make());
    this.tiles = this.pool.slice(0, n);
    return this.tiles.map((video) => video.element);
  }

  fullscreen(): HTMLVideoElement {
    this.big ??= this.make();
    return this.big.element;
  }

  exitFullscreen(): void {
    this.big = undefined;
  }

  say(text: string): void {
    this.said.push(text);
  }

  reset(): void {
    this.tiles = [];
    this.big = undefined;
  }
}

/** What the fake hub answers to a bind. */
export type BindAnswer = { codec: string; fallback?: string } | { error: string };

export interface FakeHubOptions {
  /** Default: every bind is bound, H.264 (H.265 for mx-h265 main). */
  answer?: (slot: number, cam: string, profile: Profile) => BindAnswer;
  /** Ms from the bind to its first frame on screen; undefined: no frame comes. Default 300. */
  firstFrameMs?: (bindIndex: number, slot: number, cam: string, profile: Profile) => number | undefined;
  /** Frames per second after the first, with band and sent records; 0 (default) for the first frame only. */
  fps?: (profile: Profile) => number;
  /** Glass-to-glass latency of the live frames. Default 120. */
  latencyMs?: number;
  /** The hub's clock minus the client's. Default 250. */
  offsetMs?: number;
  /** Frames of the cached GOP shown right after a bind, emitted before it (R24, R26). Default 0. */
  cachedGop?: (profile: Profile) => number;
  /** Frames in the media file: frame numbers start again after it, as the hub loops the file (R18). Default 720. */
  loopFrames?: number;
  /** What the stats say per bound slot. */
  stats?: Partial<TileStats> & { fps?: number };
  pair?: Pair;
}

const fileId = (cam: string, profile: Profile): number => {
  if (profile === 'sub') return 1;
  if (cam === 'mx-high51') return 3;
  if (cam === 'mx-h265') return 4;
  return 2;
};

const cameraNumber = (cam: string): number => TEST_CAMERAS.find((c) => c.id === cam)?.camera ?? 99;

interface Feed {
  cam: string;
  profile: Profile;
  boundAt: number;
  timers: ReturnType<typeof setTimeout>[];
}

/**
 * A hub and its session in one: binds are answered after 20 ms, frames are
 * presented on the stage's elements that show a slot's track, sent records are
 * reported every 500 ms like the hub's fan-out, pings are answered at once.
 */
export class FakeHubSession implements SpikeSession {
  readonly binds: { slot: number; cam: string; profile: Profile; at: number }[] = [];
  readonly unbinds: number[] = [];
  closed = false;
  readonly pc: RTCPeerConnection;
  private nextEpoch = 1;
  private readonly feeds = new Map<number, Feed>();
  private readonly sentListeners = new Set<(cam: number, profile: Profile, recs: [number, number, number][]) => void>();
  private readonly jitterListeners = new Set<(ms: number) => void>();
  private readonly outbox = new Map<string, [number, number, number][]>();
  private readonly flushTimer: ReturnType<typeof setInterval>;

  constructor(
    readonly slots: number,
    private readonly stage: FakeStage,
    private readonly options: FakeHubOptions = {},
  ) {
    this.pc = { getStats: () => Promise.resolve(new Map()), getTransceivers: () => [] } as unknown as RTCPeerConnection;
    this.flushTimer = setInterval(() => {
      this.flushSent();
    }, 500);
  }

  private get offsetMs(): number {
    return this.options.offsetMs ?? 250;
  }

  /** The hub's Unix time at the client's performance time `perfMs`. */
  hubTime(perfMs: number): number {
    return performance.timeOrigin + perfMs + this.offsetMs;
  }

  bind(slot: number, cam: string, profile: Profile): Promise<BindResult> {
    if (this.closed) return Promise.reject(new Error('SESSION_CLOSED'));
    const requestedPerfMs = performance.now();
    const index = this.binds.length;
    this.binds.push({ slot, cam, profile, at: requestedPerfMs });
    const answer =
      this.options.answer?.(slot, cam, profile) ?? { codec: cam === 'mx-h265' && profile === 'main' ? 'H265' : 'H264' };
    const epoch = this.nextEpoch++;
    return new Promise((resolve, reject) => {
      setTimeout(() => {
        if ('error' in answer) {
          reject(new BindError(answer.error));
          return;
        }
        this.startFeed(index, slot, cam, profile, requestedPerfMs);
        resolve({ epoch, codec: answer.codec, requestedPerfMs, ...(answer.fallback ? { fallback: answer.fallback } : {}) });
      }, 20);
    });
  }

  unbind(slot: number): void {
    this.unbinds.push(slot);
    this.stopFeed(slot);
  }

  track(slot: number): MediaStreamTrack {
    if (slot < 0 || slot > this.slots) throw new RangeError(`slot ${slot} does not exist`);
    return this.trackOf(slot) as unknown as MediaStreamTrack;
  }

  private readonly trackObjects = new Map<number, FakeTrack>();

  private trackOf(slot: number): FakeTrack {
    let track = this.trackObjects.get(slot);
    if (!track) {
      track = new FakeTrack(`track-${slot}`);
      this.trackObjects.set(slot, track);
    }
    return track;
  }

  onSent(cb: (cam: number, profile: Profile, recs: [number, number, number][]) => void): () => void {
    this.sentListeners.add(cb);
    return () => this.sentListeners.delete(cb);
  }

  onJitter(cb: (ms: number) => void): () => void {
    this.jitterListeners.add(cb);
    return () => this.jitterListeners.delete(cb);
  }

  /** The hub reports its pacing jitter. */
  jitter(ms: number): void {
    for (const cb of this.jitterListeners) cb(ms);
  }

  pair(): Promise<Pair | 'n/a'> {
    return Promise.resolve(this.options.pair ?? 'n/a');
  }

  ping(): Promise<{ c0: number; c1: number; h: number }> {
    const c0 = performance.timeOrigin + performance.now();
    return Promise.resolve({ c0, c1: c0, h: c0 + this.offsetMs });
  }

  close(): void {
    this.closed = true;
    clearInterval(this.flushTimer);
    for (const slot of [...this.feeds.keys()]) this.stopFeed(slot);
  }

  /** The stats of every bound slot, keyed by its track, as sampleStats returns them. */
  stats(): Map<string, TileStats> {
    const out = new Map<string, TileStats>();
    const { fps = 12, ...rest } = this.options.stats ?? {};
    for (const [slot, feed] of this.feeds) {
      const seconds = (performance.now() - feed.boundAt) / 1000;
      out.set(this.trackOf(slot).id, { framesDecoded: Math.floor(seconds * fps), framesDropped: 0, fps, ...rest });
    }
    return out;
  }

  private present(slot: number, band: BandCode | null): void {
    const track = this.trackOf(slot);
    track.last = { band, receivedAt: performance.now() };
    for (const video of this.stage.all) if (video.tracks.includes(track)) video.present(band);
  }

  private record(cam: string, profile: Profile, frame: number, hubMs: number): void {
    const key = `${cameraNumber(cam)}/${profile}`;
    const list = this.outbox.get(key) ?? [];
    list.push([frame, 3, hubMs]); // the hub's replay counter; the band's loop is always 0 (R18)
    this.outbox.set(key, list);
  }

  private flushSent(): void {
    for (const [key, recs] of this.outbox) {
      const [cam = '0', profile = 'sub'] = key.split('/');
      for (const cb of [...this.sentListeners]) cb(Number(cam), profile as Profile, recs);
    }
    this.outbox.clear();
  }

  private startFeed(index: number, slot: number, cam: string, profile: Profile, bindAt: number): void {
    this.stopFeed(slot);
    const feed: Feed = { cam, profile, boundAt: performance.now(), timers: [] };
    this.feeds.set(slot, feed);
    const first = this.options.firstFrameMs ? this.options.firstFrameMs(index, slot, cam, profile) : 300;
    if (first === undefined) return;
    const after = (ms: number, fn: () => void): void => {
      feed.timers.push(setTimeout(fn, Math.max(0, bindAt + ms - performance.now())));
    };
    const fps = this.options.fps?.(profile) ?? 0;
    if (fps === 0) {
      // One frame: the IDR that starts the cached GOP, a different one for each bind.
      after(first, () => {
        this.present(slot, { camera: fileId(cam, profile), frame: (index * 12) % 720, loop: 0 });
      });
      return;
    }
    const period = 1000 / fps;
    const latency = this.options.latencyMs ?? 120;
    const band = (frame: number): BandCode => ({ camera: fileId(cam, profile), frame, loop: 0 });
    // The cached GOP: emitted up to a second before the bind, shown 1 ms apart right after it (R24).
    const cached = this.options.cachedGop?.(profile) ?? 0;
    for (let i = 0; i < cached; i += 1) {
      this.record(cam, profile, i, this.hubTime(bindAt - 1000 + i * period));
      after(first + i, () => {
        this.present(slot, band(i));
      });
    }
    // Then live frames, each shown `latency` ms after the hub sent it.
    const live = (k: number): void => {
      const frame = (cached + k) % (this.options.loopFrames ?? 720);
      this.record(cam, profile, frame, this.hubTime(performance.now()));
      feed.timers.push(
        setTimeout(() => {
          this.present(slot, band(frame));
        }, latency),
      );
      feed.timers.push(
        setTimeout(() => {
          live(k + 1);
        }, period),
      );
    };
    after(first + cached, () => {
      live(0);
    });
  }

  private stopFeed(slot: number): void {
    const feed = this.feeds.get(slot);
    if (!feed) return;
    for (const timer of feed.timers) clearTimeout(timer);
    this.feeds.delete(slot);
  }
}

/** A player that shows its slot's track on play(), like @ipcam/player, and fails on demand. */
export class FakePlayer implements Player {
  readonly plays: PlayerSource[] = [];
  stops = 0;
  private video: HTMLVideoElement | undefined;
  private link: MediaLink | undefined;
  private readonly listeners = new Map<string, Set<(d: unknown) => void>>();

  constructor(private readonly slot: number) {}

  attach(video: HTMLVideoElement, link: MediaLink): void {
    this.video = video;
    this.link = link;
  }

  async play(src: PlayerSource): Promise<void> {
    this.plays.push(src);
    const track = await this.link?.webrtcTrack(this.slot);
    if (this.video) this.video.srcObject = new FakeMediaStream([track]) as unknown as MediaStream;
  }

  seek(): Promise<void> {
    return Promise.resolve();
  }

  stop(): void {
    this.stops += 1;
    if (this.video) this.video.srcObject = null;
  }

  readonly stats = { transport: 'webrtc' as const, codec: 'unknown', framesDecoded: 0 };

  on(event: string, cb: (d: unknown) => void): () => void {
    const set = this.listeners.get(event) ?? new Set();
    set.add(cb);
    this.listeners.set(event, set);
    return () => set.delete(cb);
  }

  emit(event: string, data: unknown): void {
    for (const cb of [...(this.listeners.get(event) ?? [])]) cb(data);
  }
}

/** A player whose play() rejects, as @ipcam/player's does when the track cannot be had. */
export function refusingPlayer(slot = 0): FakePlayer {
  const player = new FakePlayer(slot);
  player.play = () => Promise.reject(new Error('no track for this slot'));
  return player;
}

/** What the wake lock fake records. */
export class FakeWakeLock {
  held = 0;
  released = 0;

  hold(): Promise<WakeLock> {
    this.held += 1;
    return Promise.resolve({
      kind: 'screen',
      release: () => {
        this.released += 1;
        return Promise.resolve();
      },
    });
  }
}

export interface TestSetup {
  env: ScenarioEnv;
  doc: FakeDocument;
  stage: FakeStage;
  sessions: FakeHubSession[];
  /** The session the scenario opened last. */
  session(): FakeHubSession;
  players: FakePlayer[];
  wakeLock: FakeWakeLock;
}

export interface SetupOptions {
  hub?: FakeHubOptions;
  /** Use FakePlayer instead of @ipcam/player. */
  fakePlayer?: boolean;
  native?: SpikeNative;
  ask?: (question: string) => Promise<boolean>;
  rvfc?: boolean;
  /** False: frame callbacks without `receiveTime`, as in a browser that does not report it. */
  receiveTime?: boolean;
  /** True: an element presents its track's previous frame again when it attaches it (FakeVideo.replaysLastFrame). */
  replaysLastFrame?: boolean;
  /** Extra options for the real player (an hls.js stand-in, for example). */
  playerOptions?: PlayerOptions;
  overrides?: Partial<ScenarioEnv>;
}

/**
 * A scenario environment over fakes. Call with fake timers on: the scenarios'
 * clock and the player's timers are the global ones. Stubs MediaStream, which
 * vi.unstubAllGlobals() undoes.
 */
export function setup(options: SetupOptions = {}): TestSetup {
  vi.stubGlobal('MediaStream', FakeMediaStream);
  const doc = new FakeDocument();
  const stage = new FakeStage(doc);
  stage.rvfc = options.rvfc ?? true;
  stage.reportsReceiveTime = options.receiveTime ?? true;
  stage.replaysLastFrame = options.replaysLastFrame ?? false;
  const sessions: FakeHubSession[] = [];
  const players: FakePlayer[] = [];
  const wakeLock = new FakeWakeLock();
  const env: ScenarioEnv = {
    hubUrl: TEST_HUB,
    cameras: TEST_CAMERAS,
    device: { platform: 'chrome', ua: 'Mozilla/5.0 (X11; Linux x86_64) TestBrowser' },
    t: createI18n().t,
    stage,
    ...(options.ask ? { ask: options.ask } : {}),
    ...(options.native ? { native: options.native } : {}),
    doc,
    clock: {
      now: () => performance.now(),
      setTimer: (fn, ms) => {
        const id = setTimeout(fn, ms);
        return () => {
          clearTimeout(id);
        };
      },
    },
    openSession: (_hub, slots) => {
      const session = new FakeHubSession(slots, stage, options.hub);
      sessions.push(session);
      return Promise.resolve(session);
    },
    createPlayer: (opts) => {
      if (options.fakePlayer) {
        const player = new FakePlayer(opts.slot ?? 0);
        players.push(player);
        return player;
      }
      return createPlayer({ ...opts, ...options.playerOptions });
    },
    readBand: (video) => (video as unknown as FakeVideo).band,
    sampleStats: () => Promise.resolve(sessions.at(-1)?.stats() ?? new Map()),
    captureForDecoderInfo: () => Promise.resolve('n/a'),
    holdWakeLock: () => wakeLock.hold(),
    jsHeapMb: () => undefined,
    random: () => 0.5,
    ...options.overrides,
  };
  return {
    env,
    doc,
    stage,
    sessions,
    session: () => {
      const last = sessions.at(-1);
      if (!last) throw new Error('the scenario has not opened a session');
      return last;
    },
    players,
    wakeLock,
  };
}

/**
 * Runs `fn` and returns the promise rejections that nothing handled meanwhile
 * (Node's `unhandledRejection`), after giving Node a real turn to report them
 * (node:timers is not faked).
 */
export async function unhandledRejections(fn: () => Promise<unknown>): Promise<unknown[]> {
  const seen: unknown[] = [];
  const listener = (reason: unknown): void => {
    seen.push(reason);
  };
  process.on('unhandledRejection', listener);
  try {
    await fn();
    await new Promise((resolve) => {
      realSetImmediate(resolve);
    });
    return seen;
  } finally {
    process.off('unhandledRejection', listener);
  }
}

/**
 * Advances the fake clock until `promise` settles, a second at a time, and
 * returns its value. Fails after `maxMs` of fake time.
 */
export async function finish<T>(promise: Promise<T>, maxMs = 4 * 3_600_000): Promise<T> {
  let settled = false;
  const done = promise.finally(() => {
    settled = true;
  });
  for (let elapsed = 0; !settled; elapsed += 1000) {
    if (elapsed > maxMs) throw new Error(`still running after ${maxMs} ms of fake time`);
    await vi.advanceTimersByTimeAsync(1000);
  }
  return done;
}
