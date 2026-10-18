import { describe, expect, it, vi } from 'vitest';
import { installTauriNative, type TauriInstallDeps, type TauriMetrics } from './native-tauri';
import type { SpikeNative } from './native';

/** What the shell's `spike_metrics` command answers on a Windows 11 PC. */
const WIN1: TauriMetrics = {
  cpu_pct_total: 12.5,
  cpu_pct_app: 4.25,
  rss_mb: 512.5,
  webview2_procs: 5,
  cpu_model: 'Intel(R) Core(TM) i5-12400',
  gpu: 'Intel(R) UHD Graphics 730',
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
      memMb: 512.5,
      model: 'Intel(R) Core(TM) i5-12400',
      gpu: 'Intel(R) UHD Graphics 730',
    });
    expect(invoke.mock.calls).toEqual([['spike_metrics']]);
    // Windows has no picture-in-picture or keep-awake in Phase 0.
    expect(native.enterPip).toBeUndefined();
    expect(native.onPip).toBeUndefined();
    expect(native.keepAwake).toBeUndefined();
  });

  it('nativeTauri_unknownCpuAndGpu_areLeftOut', async () => {
    // The shell sends empty names when it cannot tell (no display adapter, a CPU without a brand string).
    const invoke = vi.fn<TauriInstallDeps['invoke']>(() => Promise.resolve({ ...WIN1, cpu_model: '', gpu: '' }));
    const target: { spikeNative?: SpikeNative } = {};

    installTauriNative({ tauri: true, invoke, target });

    await expect(target.spikeNative?.metrics()).resolves.toEqual({ cpuPctTotal: 12.5, cpuPctApp: 4.25, memMb: 512.5 });
  });

  it('nativeTauri_outsideTauri_installsNothing', () => {
    const invoke = vi.fn<TauriInstallDeps['invoke']>(() => Promise.resolve(WIN1));
    const target: { spikeNative?: SpikeNative } = {};

    installTauriNative({ tauri: false, invoke, target });

    expect(target.spikeNative).toBeUndefined();
    expect(invoke).not.toHaveBeenCalled();
  });
});
