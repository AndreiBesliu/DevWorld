// The Windows shell's `window.spikeNative` (Tasks 11, 11b): native.ts's facade over
// the SPIKE-ONLY Tauri command `spike_metrics` of apps/desktop (cargo feature
// `spike-metrics`). In a browser and in the Android shell it installs nothing.
// A shell built without the feature has no such command: metrics() rejects,
// and the page records the shell's metrics as n/a.
import { invoke, isTauri } from '@tauri-apps/api/core';
import { Command } from '@tauri-apps/plugin-shell';
import type { NativeMetrics, ShellUpdater, SpikeNative } from './native';
import type { UpdateOffer } from './update';

/**
 * SPIKE-ONLY (update.ts): the shell plugin's command that downloads the
 * installer, checks its SHA-256 and starts it in passive mode (/P /R /UPDATE:
 * it closes the app, installs over it and starts it again). Its PowerShell
 * script and the checks of its two arguments are in the shell's capability
 * (apps/desktop/src-tauri/capabilities/spike-update.json); the plugin takes
 * the fixed arguments from there and ignores what the page sends in their place.
 */
export const UPDATE_COMMAND = 'spike-update';
/** How many fixed arguments come before the address and the SHA-256. */
export const UPDATE_FIXED_ARGS = 4;
/** The Windows shell's release asset. */
export const DESKTOP_ASSET = 'ipcam-desktop-setup.exe';

/** What the command ended with. */
export interface CommandOutcome {
  code: number | null;
  stderr: string;
}

/** The update command's arguments: placeholders for the fixed ones, then the offer's address and SHA-256. */
export function updateArgs(offer: UpdateOffer): string[] {
  return [...Array<string>(UPDATE_FIXED_ARGS).fill(''), offer.url, offer.sha256];
}

function desktopUpdater(run: (args: string[]) => Promise<CommandOutcome>): ShellUpdater {
  return {
    asset: DESKTOP_ASSET,
    install: async (offer) => {
      const { code, stderr } = await run(updateArgs(offer));
      if (code !== 0) throw new Error(stderr.trim() || `the update command ended with ${String(code)}`);
      return 'installing';
    },
  };
}

/** What `spike_metrics` answers (apps/desktop/src-tauri/src/spike_metrics.rs). */
export interface TauriMetrics {
  /** The whole machine, 0–100. */
  cpu_pct_total: number;
  /**
   * The app process and its WebView2 processes, as a share of the whole machine, 0–100. This and the
   * other two sums are null when the shell finds no WebView2 process under the app (R52).
   */
  cpu_pct_app: number | null;
  /** Their resident memory (working sets), MiB, in which a page that several processes share counts in each. */
  rss_mb: number | null;
  /** Their private bytes (commit charge), MiB: the page's memMb (R37). */
  private_mb: number | null;
  /** How many WebView2 processes the sums include: the page's webview2Procs. */
  webview2_procs: number;
  /** The PC's physical memory in MiB; 0 when the shell cannot tell. */
  ram_mb: number;
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
  /** Runs UPDATE_COMMAND with these arguments; without it the shell offers no updates. */
  runUpdate?(args: string[]): Promise<CommandOutcome>;
  target: { spikeNative?: SpikeNative };
}

/**
 * The command's answer as NativeMetrics: memory is the private bytes, the CPU
 * brand is the model of the PC, the runtime version reads "WebView2 <version>",
 * a null sum (no WebView2 process, R52), an empty name and 0 MiB of RAM are
 * left out (n/a).
 */
function toNativeMetrics(answer: TauriMetrics): NativeMetrics {
  const { cpu_pct_total, cpu_pct_app, private_mb, webview2_procs, ram_mb, cpu_model, gpu, os, webview } = answer;
  return {
    cpuPctTotal: cpu_pct_total,
    ...(cpu_pct_app === null ? {} : { cpuPctApp: cpu_pct_app }),
    ...(private_mb === null ? {} : { memMb: private_mb }),
    webview2Procs: webview2_procs,
    ...(ram_mb > 0 ? { ramMb: ram_mb } : {}),
    ...(cpu_model ? { model: cpu_model } : {}),
    ...(gpu ? { gpu } : {}),
    ...(os ? { os } : {}),
    ...(webview ? { webview: `WebView2 ${webview}` } : {}),
  };
}

/** Installs `window.spikeNative` when the page runs in the Windows shell. */
export function installTauriNative(
  { tauri, invoke: call, runUpdate, target }: TauriInstallDeps = {
    tauri: isTauri(),
    invoke: (cmd) => invoke<TauriMetrics>(cmd),
    runUpdate: async (args) => {
      const { code, stderr } = await Command.create(UPDATE_COMMAND, args).execute();
      return { code, stderr };
    },
    target: globalThis as { spikeNative?: SpikeNative },
  },
): void {
  if (!tauri) return;
  target.spikeNative = {
    metrics: async () => toNativeMetrics(await call('spike_metrics')),
    ...(runUpdate ? { update: desktopUpdater(runUpdate) } : {}),
  };
}
