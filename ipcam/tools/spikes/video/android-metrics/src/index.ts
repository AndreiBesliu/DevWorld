// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): what the video
// spike reads from Android and production does not need. The Kotlin side is
// android/src/main/java/ro/ipcam/spike/MetricsPlugin.kt.
import { registerPlugin } from '@capacitor/core';

/** Each field is left out when the device cannot tell. No serial numbers or addresses (PH0-22). */
export interface AndroidMetrics {
  /** PowerManager.getCurrentThermalStatus as a name: NONE, LIGHT, MODERATE, SEVERE, CRITICAL, EMERGENCY or SHUTDOWN. */
  thermal?: string;
  batteryPct?: number;
  /** Proportional set size (PSS) of the app process in MiB. The WebView renderer is a process of its own and is not in it. */
  memMb?: number;
  /** Manufacturer and model, for example `samsung SM-A245F`. */
  model?: string;
  /** Android version and SDK level, for example `Android 15 (API 35)`. */
  os?: string;
  /** Package and version of the WebView provider. */
  webview?: string;
}

export interface SpikeMetricsPlugin {
  read(): Promise<AndroidMetrics>;
  /** Keeps the screen on (FLAG_KEEP_SCREEN_ON) while `on` is true. */
  keepAwake(options: { on: boolean }): Promise<void>;
}

export const SpikeMetrics = registerPlugin<SpikeMetricsPlugin>('SpikeMetrics');
