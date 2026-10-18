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
