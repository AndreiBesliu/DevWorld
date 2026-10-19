// The Windows shell's `window.spikeNative` (Tasks 11, 11b): native.ts's facade over
// the SPIKE-ONLY Tauri command `spike_metrics` of apps/desktop (cargo feature
// `spike-metrics`). In a browser and in the Android shell it installs nothing.
// A shell built without the feature has no such command: metrics() rejects,
// and the page records the shell's metrics as n/a.
import { invoke, isTauri } from '@tauri-apps/api/core';
import type { NativeMetrics, SpikeNative } from './native';

/** What `spike_metrics` answers (apps/desktop/src-tauri/src/spike_metrics.rs). */
export interface TauriMetrics {
  /** The whole machine, 0–100. */
  cpu_pct_total: number;
  /** The app process and its WebView2 processes, as a share of the whole machine, 0–100. */
  cpu_pct_app: number;
  /** Their resident memory (working sets), MiB, in which a page that several processes share counts in each. */
  rss_mb: number;
  /** Their private bytes (commit charge), MiB: the page's memMb (R37). */
  private_mb: number;
  /** How many WebView2 processes the sums include. The page has no field for it. */
  webview2_procs: number;
  /** The CPU's brand string; empty when the shell cannot tell. */
  cpu_model: string;
  /**
   * The display adapters, without those DXGI flags as software; "Microsoft Basic
   * Render Driver" on a PC without a GPU driver. Empty when the shell cannot tell.
   */
  gpu: string;
  /** The long OS version, such as "Windows 11 Pro"; empty when the shell cannot tell. */
  os: string;
  /** The WebView2 Runtime version, such as "154.0.4258.62"; empty when the shell cannot tell. */
  webview: string;
}

export interface TauriInstallDeps {
  /** `isTauri()`: true when the page runs in the Tauri shell. */
  tauri: boolean;
  invoke(cmd: 'spike_metrics'): Promise<TauriMetrics>;
  target: { spikeNative?: SpikeNative };
}

/**
 * The command's answer as NativeMetrics: memory is the private bytes, the CPU
 * brand is the model of the PC, the runtime version reads "WebView2 <version>",
 * and an empty name is left out.
 */
function toNativeMetrics(answer: TauriMetrics): NativeMetrics {
  const { cpu_pct_total, cpu_pct_app, private_mb, cpu_model, gpu, os, webview } = answer;
  return {
    cpuPctTotal: cpu_pct_total,
    cpuPctApp: cpu_pct_app,
    memMb: private_mb,
    ...(cpu_model ? { model: cpu_model } : {}),
    ...(gpu ? { gpu } : {}),
    ...(os ? { os } : {}),
    ...(webview ? { webview: `WebView2 ${webview}` } : {}),
  };
}

/** Installs `window.spikeNative` when the page runs in the Windows shell. */
export function installTauriNative(
  { tauri, invoke: call, target }: TauriInstallDeps = {
    tauri: isTauri(),
    invoke: (cmd) => invoke<TauriMetrics>(cmd),
    target: globalThis as { spikeNative?: SpikeNative },
  },
): void {
  if (!tauri) return;
  target.spikeNative = {
    metrics: async () => toNativeMetrics(await call('spike_metrics')),
  };
}
