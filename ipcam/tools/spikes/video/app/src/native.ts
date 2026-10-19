// What the Android and Windows shells add to the page (Tasks 10 and 11): they
// install `window.spikeNative`. In a browser it is undefined, and what depends
// on it is recorded as n/a.

/** Android's PowerManager thermal status, mildest first. */
export const THERMAL_LEVELS = ['NONE', 'LIGHT', 'MODERATE', 'SEVERE', 'CRITICAL', 'EMERGENCY', 'SHUTDOWN'] as const;

export type ThermalStatus = (typeof THERMAL_LEVELS)[number];

/** What a shell can measure that the page cannot. Each field is left out when the shell cannot tell. */
export interface NativeMetrics {
  thermal?: ThermalStatus;
  batteryPct?: number;
  memMb?: number;
  cpuPctTotal?: number;
  cpuPctApp?: number;
  /**
   * Windows: how many WebView2 processes cpuPctApp and memMb include. With 0 the
   * shell leaves those two out (n/a), since they would cover the app process alone (R52).
   */
  webview2Procs?: number;
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
