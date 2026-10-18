// What a scenario runs against. Everything that touches the hub, the screen,
// the clock or a browser API comes through ScenarioEnv, so that the tests run
// the scenarios on fakes and fake time (minutes of scenario in milliseconds).
import type { Player, PlayerOptions } from '@ipcam/player';
import type { BandCode } from '../band';
import type { HubCamera } from '../cameras';
import type { DeviceInfo } from '../device';
import type { Translate } from '../i18n';
import type { TileStats } from '../metrics';
import type { SpikeNative } from '../native';
import type { SpikeSession } from '../session';
import type { VisibilitySource, WakeLock } from '../wakelock';

export interface Clock {
  /** Milliseconds on the performance clock. */
  now(): number;
  /** Runs `fn` once after `ms` and returns the function that cancels it. */
  setTimer(fn: () => void, ms: number): () => void;
}

/** Where the video elements of a scenario are shown. Elements come muted and inline. */
export interface Stage {
  /** Shows `n` tiles in a grid (0 hides the grid) and returns their elements, the first ones reused. */
  grid(n: number): HTMLVideoElement[];
  /** Shows the fullscreen element over the grid and returns it. */
  fullscreen(): HTMLVideoElement;
  exitFullscreen(): void;
  /** A line of text over the video: guidance or a label. An empty text clears it. */
  say(text: string): void;
  /** Hides every element and the text. */
  reset(): void;
}

export interface ScenarioEnv {
  hubUrl: string;
  /** The hub's cameras, from GET /spike/v1/cameras. */
  cameras: HubCamera[];
  device: DeviceInfo;
  t: Translate;
  stage: Stage;
  /** Asks the person a yes/no question; absent when nobody can answer (autorun). */
  ask?: (question: string) => Promise<boolean>;
  native?: SpikeNative;
  doc: VisibilitySource;
  clock: Clock;
  openSession(hubUrl: string, slots: number): Promise<SpikeSession>;
  createPlayer(opts: PlayerOptions): Player;
  readBand(video: HTMLVideoElement): BandCode | null;
  sampleStats(pc: RTCPeerConnection): Promise<Map<string, TileStats>>;
  captureForDecoderInfo(): Promise<(() => void) | 'n/a'>;
  holdWakeLock(): Promise<WakeLock>;
  /** The JS heap in MB where the browser reports it (Chromium), else undefined. */
  jsHeapMb(): number | undefined;
  /** 0 (inclusive) to 1 (exclusive), like Math.random. */
  random(): number;
}
