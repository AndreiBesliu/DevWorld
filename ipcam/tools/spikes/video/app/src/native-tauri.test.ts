import { describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import {
  installTauriNative,
  updateArgs,
  UPDATE_COMMAND,
  UPDATE_FIXED_ARGS,
  type TauriInstallDeps,
  type TauriMetrics,
} from './native-tauri';
import type { SpikeNative } from './native';

/** What the shell's `spike_metrics` command answers on a Windows 11 PC. */
const WIN1: TauriMetrics = {
  cpu_pct_total: 12.5,
  cpu_pct_app: 4.25,
  rss_mb: 512.5,
  private_mb: 301.25,
  webview2_procs: 5,
  ram_mb: 16069,
  cpu_model: 'Intel(R) Core(TM) i5-12400',
  gpu: 'Intel(R) UHD Graphics 730',
  os: 'Windows 11 Pro',
  webview: '154.0.4258.62',
};

describe('installTauriNative', () => {
  it('nativeTauri_installsSpikeNative', async () => {
    const invoke = vi.fn<TauriInstallDeps['invoke']>(() => Promise.resolve(WIN1));
    const target: { spikeNative?: SpikeNative } = {};

    installTauriNative({ tauri: true, invoke, target });

    const native = target.spikeNative;
    if (!native) throw new Error('window.spikeNative was not installed');
    await expect(native.metrics()).resolves.toEqual({
      cpuPctTotal: 12.5,
      cpuPctApp: 4.25,
      // Private bytes, not the working sets (R37).
      memMb: 301.25,
      webview2Procs: 5,
      ramMb: 16069,
      model: 'Intel(R) Core(TM) i5-12400',
      gpu: 'Intel(R) UHD Graphics 730',
      os: 'Windows 11 Pro',
      webview: 'WebView2 154.0.4258.62',
    });
    expect(invoke.mock.calls).toEqual([['spike_metrics']]);
    // Windows has no picture-in-picture or keep-awake in Phase 0.
    expect(native.enterPip).toBeUndefined();
    expect(native.onPip).toBeUndefined();
    expect(native.keepAwake).toBeUndefined();
  });

  it('nativeTauri_unknownNamesAndRam_areLeftOut', async () => {
    // The shell sends empty names when it cannot tell (no display adapter, a CPU without a brand string,
    // no OS name or WebView2 Runtime version), and 0 MiB of RAM when it cannot read the total.
    const invoke = vi.fn<TauriInstallDeps['invoke']>(() =>
      Promise.resolve({ ...WIN1, cpu_model: '', gpu: '', os: '', webview: '', ram_mb: 0 }),
    );
    const target: { spikeNative?: SpikeNative } = {};

    installTauriNative({ tauri: true, invoke, target });

    await expect(target.spikeNative?.metrics()).resolves.toEqual({
      cpuPctTotal: 12.5,
      cpuPctApp: 4.25,
      memMb: 301.25,
      webview2Procs: 5,
    });
  });

  it('nativeTauri_noWebView2Process_appSumsAreNa', async () => {
    // R52: without a WebView2 process the shell's sums would cover the app process alone, so it sends null
    // (n/a) for them, and the page records no cpuPctApp or memMb, only that it counted 0 WebView2 processes.
    const invoke = vi.fn<TauriInstallDeps['invoke']>(() =>
      Promise.resolve({ ...WIN1, cpu_pct_app: null, rss_mb: null, private_mb: null, webview2_procs: 0 }),
    );
    const target: { spikeNative?: SpikeNative } = {};

    installTauriNative({ tauri: true, invoke, target });

    await expect(target.spikeNative?.metrics()).resolves.toEqual({
      cpuPctTotal: 12.5,
      webview2Procs: 0,
      ramMb: 16069,
      model: 'Intel(R) Core(TM) i5-12400',
      gpu: 'Intel(R) UHD Graphics 730',
      os: 'Windows 11 Pro',
      webview: 'WebView2 154.0.4258.62',
    });
  });

  it('nativeTauri_outsideTauri_installsNothing', () => {
    const invoke = vi.fn<TauriInstallDeps['invoke']>(() => Promise.resolve(WIN1));
    const target: { spikeNative?: SpikeNative } = {};

    installTauriNative({ tauri: false, invoke, target });

    expect(target.spikeNative).toBeUndefined();
    expect(invoke).not.toHaveBeenCalled();
  });
});

/** The shell's capability for the update command (apps/desktop/src-tauri/capabilities/spike-update.json). */
interface Capability {
  windows: string[];
  permissions: { identifier: string; allow: { name: string; cmd: string; args: (string | { validator: string })[] }[] }[];
}

const capability = JSON.parse(
  readFileSync(new URL('../../../../../apps/desktop/src-tauri/capabilities/spike-update.json', import.meta.url), 'utf8'),
) as Capability;

const OFFER = {
  version: 'spike-video-v0.1.2',
  url: 'https://github.com/AndreiBesliu/DevWorld/releases/download/spike-video-v0.1.2/ipcam-desktop-setup.exe',
  sha256: '4c733ad2ae4569924b165b0369cdd571113b6deaca7f2c43b6f3ca49d602d3e6',
};

describe('the Windows shell updates itself', () => {
  it('nativeTauri_update_runsTheCommandWithTheOffer', async () => {
    const runUpdate = vi.fn<NonNullable<TauriInstallDeps['runUpdate']>>(() => Promise.resolve({ code: 0, stderr: '' }));
    const target: { spikeNative?: SpikeNative } = {};
    installTauriNative({ tauri: true, invoke: vi.fn(), runUpdate, target });

    expect(target.spikeNative?.update?.asset).toBe('ipcam-desktop-setup.exe');
    await expect(target.spikeNative?.update?.install(OFFER)).resolves.toBe('installing');
    expect(runUpdate.mock.calls).toEqual([[updateArgs(OFFER)]]);
  });

  it('nativeTauri_update_aFailedCommandRejectsWithItsMessage', async () => {
    const runUpdate = vi.fn<NonNullable<TauriInstallDeps['runUpdate']>>(() =>
      Promise.resolve({ code: 1, stderr: 'SHA-256 mismatch\n' }),
    );
    const target: { spikeNative?: SpikeNative } = {};
    installTauriNative({ tauri: true, invoke: vi.fn(), runUpdate, target });

    await expect(target.spikeNative?.update?.install(OFFER)).rejects.toThrow('SHA-256 mismatch');
  });

  it('nativeTauri_update_theCapabilityChecksWhatThePageSends', () => {
    // The shell plugin takes the fixed arguments from the capability and checks the others with ^…$ validators.
    const [permission] = capability.permissions;
    expect(capability.windows).toEqual(['main']);
    expect(permission?.identifier).toBe('shell:allow-execute');
    const command = permission?.allow.find((c) => c.name === UPDATE_COMMAND);
    if (!command) throw new Error(`the capability does not allow ${UPDATE_COMMAND}`);
    expect(command.cmd).toBe('powershell');
    const args = command.args;
    expect(args.slice(0, UPDATE_FIXED_ARGS).every((a) => typeof a === 'string')).toBe(true);
    expect(args).toHaveLength(UPDATE_FIXED_ARGS + 2);
    const validators = args.slice(UPDATE_FIXED_ARGS).map((a) => {
      if (typeof a === 'string') throw new Error('the address and the SHA-256 must be checked');
      return new RegExp(`^${a.validator}$`);
    });
    const [url, sha] = validators;
    expect(url?.test(OFFER.url)).toBe(true);
    expect(sha?.test(OFFER.sha256)).toBe(true);
    for (const bad of [
      'https://github.com/AndreiBesliu/DevWorld/releases/download/spike-video-v0.1.2/other.exe',
      'https://example.com/AndreiBesliu/DevWorld/releases/download/spike-video-v0.1.2/ipcam-desktop-setup.exe',
      `${OFFER.url} ; calc`,
    ]) {
      expect(url?.test(bad)).toBe(false);
    }
    expect(sha?.test(`${OFFER.sha256};x`)).toBe(false);
    // The script is one PowerShell string without double quotes, which Windows would split differently.
    const script = args[UPDATE_FIXED_ARGS - 1];
    expect(typeof script === 'string' && !script.includes('"')).toBe(true);
    expect(script).toContain("Get-FileHash -Algorithm SHA256");
    expect(script).toContain("'/P','/R','/UPDATE'");
  });
});

