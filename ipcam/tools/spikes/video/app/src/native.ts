// What the Android and Windows shells add to the page (Tasks 10 and 11): they
// install `window.spikeNative`. In a browser it is undefined, and what depends
// on it is recorded as n/a.
import type { UpdateOffer } from './update';

/** Android's PowerManager thermal status, mildest first. */
export const THERMAL_LEVELS = ['NONE', 'LIGHT', 'MODERATE', 'SEVERE', 'CRITICAL', 'EMERGENCY', 'SHUTDOWN'] as const;

export type ThermalStatus = (typeof THERMAL_LEVELS)[number];

/** What a shell can measure that the page cannot. Each field is left out when the shell cannot tell. */
export interface NativeMetrics {
  thermal?: ThermalStatus;
  batteryPct?: number;
  /** Android: the phone is on the charger (plugged in), also when the battery level holds (R48). */
  charging?: boolean;
  memMb?: number;
  cpuPctTotal?: number;
  cpuPctApp?: number;
  /**
   * Windows: how many WebView2 processes cpuPctApp and memMb include. With 0 the
   * shell leaves those two out (n/a), since they would cover the app process alone (R52).
   */
  webview2Procs?: number;
  /** The device's physical memory in MiB, for the report's device facts. */
  ramMb?: number;
  model?: string;
  os?: string;
  webview?: string;
  gpu?: string;
}

export interface SpikeNative {
  metrics(): Promise<NativeMetrics>;
  /** Android: the Activity enters picture-in-picture. */
  enterPip?(): Promise<void>;
  /** Android: called on entering (true) and leaving (false) picture-in-picture. */
  onPip?(cb: (on: boolean) => void): void;
  /** Android: keeps the screen on (FLAG_KEEP_SCREEN_ON) while on is true. */
  keepAwake?(on: boolean): Promise<void>;
  /** Android: vibrates the phone briefly, to call the person (the automatic run's S5 and S6). */
  vibrate?(): Promise<void>;
  /** SPIKE-ONLY (update.ts): the shell installs its own newer release. */
  update?: ShellUpdater;
  /** SPIKE-ONLY (hubprocess.ts): Windows starts spikehub as its hidden child and passes its output on. */
  hub?: ShellHub;
}

/** Where a hub started by the shell sends what it writes, and its end. */
export interface HubOutput {
  /** One line of the hub's stdout or stderr, without its line break. */
  line(text: string): void;
  /**
   * The hub ended, by itself or stopped: its exit code, null when it is unknown,
   * or `missing` when the folder has no spikehub.exe.
   */
  exit(end: number | null | 'missing'): void;
}

/** A hub that the shell started. */
export interface RunningHub {
  /** Ends the hub and whatever it started; `exit` follows. */
  stop(): Promise<void>;
}

/**
 * SPIKE-ONLY (owner's request of 2026-11-24): spikehub as the shell's hidden
 * child, without a console window, so that the PC needs no terminal.
 */
export interface ShellHub {
  /**
   * Starts spikehub.exe in `folder` (which holds it, `media` and `results`).
   * Rejects with a HubStartError when nothing could start there, such as a
   * folder that does not exist.
   */
  start(folder: string, out: HubOutput): Promise<RunningHub>;
  /** The folder to offer before the person picks one, or undefined when the shell cannot tell. */
  defaultFolder(): Promise<string | undefined>;
}

/** The shell could not start the hub in the folder; `detail` is what the system said, for the screen. */
export class HubStartError extends Error {
  readonly detail: string;

  constructor(detail: string) {
    super(detail);
    this.name = 'HubStartError';
    this.detail = detail;
  }
}

/**
 * How a shell updates itself (update.ts): which release asset it installs, and
 * the install, which downloads the asset, checks its SHA-256 and starts the
 * installer. `installing`: the installer runs (Windows closes and restarts the
 * app; Android asks the person to confirm). `permission`: Android opened its
 * setting «Install unknown apps» for the app; the person allows it and taps again.
 */
export interface ShellUpdater {
  asset: string;
  install(offer: UpdateOffer): Promise<'installing' | 'permission'>;
}

/** The facade the shell installed, if any. */
export function spikeNative(): SpikeNative | undefined {
  return (globalThis as { spikeNative?: SpikeNative }).spikeNative;
}

/** How long the shell may take to answer metrics() before they count as n/a. */
export const NATIVE_METRICS_TIMEOUT_MS = 3000;

/**
 * The shell's metrics, or `n/a` when there is no shell or its facade throws,
 * rejects or does not answer within NATIVE_METRICS_TIMEOUT_MS: a broken bridge
 * must neither keep the page busy nor stop a run. `setTimer` arms the timeout
 * and returns its cancel function.
 */
export function readNativeMetrics(
  native: SpikeNative | undefined,
  setTimer: (fn: () => void, ms: number) => () => void,
): Promise<NativeMetrics | 'n/a'> {
  if (!native) return Promise.resolve('n/a');
  return new Promise((resolve) => {
    const cancel = setTimer(() => {
      resolve('n/a');
    }, NATIVE_METRICS_TIMEOUT_MS);
    const settle = (value: NativeMetrics | 'n/a'): void => {
      cancel();
      resolve(value);
    };
    try {
      native.metrics().then(settle, () => {
        settle('n/a');
      });
    } catch {
      settle('n/a');
    }
  });
}
