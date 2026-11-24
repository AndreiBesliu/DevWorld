import { describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import {
  CMD_NOT_FOUND,
  desktopHub,
  HUB_COMMAND,
  HUB_FOLDER_NAME,
  installTauriNative,
  updateArgs,
  UPDATE_COMMAND,
  UPDATE_FIXED_ARGS,
  type HubEvents,
  type SpawnHub,
  type TauriInstallDeps,
  type TauriMetrics,
} from './native-tauri';
import { HubStartError, type SpikeNative } from './native';

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


/** The shell's capability for the hub (apps/desktop/src-tauri/capabilities/spike-hub.json). */
interface HubCapability {
  windows: string[];
  permissions: (string | { identifier: string; allow: { name: string; cmd: string; args: unknown[] }[] })[];
}

const hubCapability = JSON.parse(
  readFileSync(new URL('../../../../../apps/desktop/src-tauri/capabilities/spike-hub.json', import.meta.url), 'utf8'),
) as HubCapability;

/** A spawn double: what the hub's events would carry, and whether it was killed. */
function fakeSpawn(): { spawn: SpawnHub; events: () => HubEvents; folders: string[]; kills: () => number } {
  let events: HubEvents | undefined;
  const folders: string[] = [];
  let kills = 0;
  const spawn: SpawnHub = (folder, e) => {
    folders.push(folder);
    events = e;
    return Promise.resolve({
      kill: () => {
        kills++;
        return Promise.resolve();
      },
    });
  };
  return {
    spawn,
    events: () => {
      if (!events) throw new Error('nothing was spawned');
      return events;
    },
    folders,
    kills: () => kills,
  };
}

describe('the Windows shell starts the hub', () => {
  it('nativeTauri_hub_passesLinesAndTheEnd', async () => {
    const fake = fakeSpawn();
    const hub = desktopHub(fake.spawn, () => Promise.resolve('C:\\Users\\a\\Downloads'));
    const lines: string[] = [];
    const ends: (number | null | 'missing')[] = [];

    await hub.start('C:\\hub', { line: (l) => lines.push(l), exit: (e) => ends.push(e) });
    fake.events().line('spikehub spike-video-v0.1.2\r\n');
    fake.events().line('time=… msg="spikehub listens"\n');
    fake.events().close(1);
    fake.events().close(1); // the plugin's error and close can both come: one end only

    expect(fake.folders).toEqual(['C:\\hub']);
    expect(lines).toEqual(['spikehub spike-video-v0.1.2', 'time=… msg="spikehub listens"']);
    expect(ends).toEqual([1]);
  });

  it('nativeTauri_hub_cmdNotFound_isMissing', async () => {
    // cmd ends with 9009 when the folder has no .\spikehub.exe.
    const fake = fakeSpawn();
    const ends: (number | null | 'missing')[] = [];
    await desktopHub(fake.spawn, () => Promise.resolve('D')).start('C:\\empty', { line: vi.fn(), exit: (e) => ends.push(e) });
    fake.events().close(CMD_NOT_FOUND);
    expect(ends).toEqual(['missing']);
  });

  it('nativeTauri_hub_stopKillsOnce', async () => {
    const fake = fakeSpawn();
    const ends: (number | null | 'missing')[] = [];
    const running = await desktopHub(fake.spawn, () => Promise.resolve('D')).start('C:\\hub', {
      line: vi.fn(),
      exit: (e) => ends.push(e),
    });
    await running.stop();
    fake.events().close(1); // the killed job's code comes after: the end was already told
    await running.stop();
    expect(fake.kills()).toBe(1);
    expect(ends).toEqual([null]);
  });

  it('nativeTauri_hub_spawnFailure_isHubStartError', async () => {
    // A folder that does not exist: Windows refuses the working directory.
    const spawn: SpawnHub = () => Promise.reject(new Error('The directory name is invalid. (os error 267)'));
    const start = desktopHub(spawn, () => Promise.resolve('D')).start('C:\\nowhere', { line: vi.fn(), exit: vi.fn() });
    await expect(start).rejects.toBeInstanceOf(HubStartError);
    await expect(start).rejects.toMatchObject({ detail: 'The directory name is invalid. (os error 267)' });
  });

  it('nativeTauri_hub_defaultFolderIsInDownloads', async () => {
    const spawn = fakeSpawn().spawn;
    await expect(desktopHub(spawn, () => Promise.resolve('C:\\Users\\besli\\Downloads\\')).defaultFolder()).resolves.toBe(
      `C:\\Users\\besli\\Downloads\\${HUB_FOLDER_NAME}`,
    );
    await expect(desktopHub(spawn, () => Promise.reject(new Error('no path permission'))).defaultFolder()).resolves.toBeUndefined();
  });

  it('nativeTauri_hub_onlyWithSpawn', () => {
    const withHub: { spikeNative?: SpikeNative } = {};
    installTauriNative({ tauri: true, invoke: vi.fn(), spawnHub: fakeSpawn().spawn, downloads: vi.fn(), target: withHub });
    expect(withHub.spikeNative?.hub).toBeDefined();
    const without: { spikeNative?: SpikeNative } = {};
    installTauriNative({ tauri: true, invoke: vi.fn(), target: without });
    expect(without.spikeNative?.hub).toBeUndefined();
  });

  it('nativeTauri_hubCapability_fixesTheWholeCommandLine', () => {
    // The page chooses only the working directory: every argument is fixed, and nothing else may run.
    expect(hubCapability.windows).toEqual(['main']);
    const spawn = hubCapability.permissions.find((p) => typeof p !== 'string' && p.identifier === 'shell:allow-spawn');
    if (!spawn || typeof spawn === 'string') throw new Error('the capability does not allow shell:allow-spawn');
    expect(spawn.allow).toEqual([{ name: HUB_COMMAND, cmd: 'cmd', args: ['/d', '/c', '.\\spikehub.exe', '-browser=false'] }]);
    expect(hubCapability.permissions.filter((p) => typeof p === 'string').sort()).toEqual([
      'core:path:allow-resolve-directory',
      'shell:allow-kill',
    ]);
  });
});
