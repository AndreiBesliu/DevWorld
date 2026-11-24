// The Windows shell's `window.spikeNative` (Tasks 11, 11b): native.ts's facade over
// the SPIKE-ONLY Tauri command `spike_metrics` of apps/desktop (cargo feature
// `spike-metrics`). In a browser and in the Android shell it installs nothing.
// A shell built without the feature has no such command: metrics() rejects,
// and the page records the shell's metrics as n/a.
import { invoke, isTauri } from '@tauri-apps/api/core';
import { downloadDir } from '@tauri-apps/api/path';
import { Command } from '@tauri-apps/plugin-shell';
import { HubStartError, type NativeMetrics, type ShellHub, type ShellUpdater, type SpikeNative } from './native';
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

/**
 * SPIKE-ONLY (owner's request of 2026-11-24, plan 2026-11-24-spike-hub-panel):
 * the shell plugin's command that starts spikehub. The shell's capability
 * (apps/desktop/src-tauri/capabilities/spike-hub.json) fixes the whole command
 * line, `cmd /d /c .\spikehub.exe -browser=false`; the page chooses only the
 * working directory, the folder that holds spikehub.exe, `media` and `results`.
 * The plugin starts it without a console window and, with `processGroup`, in a
 * job object, so that a kill ends cmd and spikehub together; it also kills it
 * when the app exits.
 */
export const HUB_COMMAND = 'spike-hub';
/** cmd's exit code when it finds no such program: the folder has no spikehub.exe. */
export const CMD_NOT_FOUND = 9009;
/** The folder that the release's spikehub-windows.zip extracts to, offered in Downloads. */
export const HUB_FOLDER_NAME = 'spikehub-windows';

/** What a started hub reports; the plugin's Command events, narrowed. */
export interface HubEvents {
  line(text: string): void;
  close(code: number | null): void;
  error(message: string): void;
}

/** Starts HUB_COMMAND in `folder`; rejects with the plugin's message when nothing starts. */
export type SpawnHub = (folder: string, events: HubEvents) => Promise<{ kill(): Promise<void> }>;

async function spawnHub(folder: string, events: HubEvents): Promise<{ kill(): Promise<void> }> {
  const command = Command.create(HUB_COMMAND, [], { cwd: folder, encoding: 'utf-8', processGroup: true });
  command.stdout.on('data', (line) => {
    events.line(line);
  });
  command.stderr.on('data', (line) => {
    events.line(line);
  });
  command.on('close', ({ code }) => {
    events.close(code);
  });
  command.on('error', (message) => {
    events.error(message);
  });
  return command.spawn();
}

/** The hub as native.ts's ShellHub, over `spawn`; `downloads` is the PC's Downloads folder. */
export function desktopHub(spawn: SpawnHub, downloads: () => Promise<string>): ShellHub {
  return {
    start: async (folder, out) => {
      let ended = false;
      const end = (value: number | null | 'missing'): void => {
        if (ended) return;
        ended = true;
        out.exit(value);
      };
      let child: { kill(): Promise<void> };
      try {
        child = await spawn(folder, {
          line: (text) => {
            out.line(text.replace(/\r?\n$/, ''));
          },
          close: (code) => {
            end(code === CMD_NOT_FOUND ? 'missing' : code);
          },
          // The plugin lost the child (a broken pipe, say): the hub may run on, but the page no longer hears it.
          error: (message) => {
            out.line(message);
            end(null);
          },
        });
      } catch (error) {
        throw new HubStartError(error instanceof Error ? error.message : String(error));
      }
      return {
        stop: async () => {
          if (ended) return;
          await child.kill();
          end(null);
        },
      };
    },
    defaultFolder: async () => {
      try {
        const folder = (await downloads()).replace(/[\\/]+$/, '');
        return folder ? `${folder}\\${HUB_FOLDER_NAME}` : undefined;
      } catch {
        return undefined;
      }
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
  /** Starts HUB_COMMAND; without it the shell offers no hub. */
  spawnHub?: SpawnHub;
  /** The PC's Downloads folder, where the hub's folder is looked for first. */
  downloads?(): Promise<string>;
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
  { tauri, invoke: call, runUpdate, spawnHub: spawn, downloads, target }: TauriInstallDeps = {
    tauri: isTauri(),
    invoke: (cmd) => invoke<TauriMetrics>(cmd),
    runUpdate: async (args) => {
      const { code, stderr } = await Command.create(UPDATE_COMMAND, args).execute();
      return { code, stderr };
    },
    spawnHub,
    downloads: downloadDir,
    target: globalThis as { spikeNative?: SpikeNative },
  },
): void {
  if (!tauri) return;
  target.spikeNative = {
    metrics: async () => toNativeMetrics(await call('spike_metrics')),
    ...(runUpdate ? { update: desktopUpdater(runUpdate) } : {}),
    ...(spawn ? { hub: desktopHub(spawn, downloads ?? (() => Promise.reject(new Error('no Downloads folder')))) } : {}),
  };
}
