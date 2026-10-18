import { describe, expect, it, vi } from 'vitest';
import { installAndroidNative, type AndroidPlugins } from './native-android';
import type { SpikeNative } from './native';

type PipListener = (event: { on: boolean }) => void;

/** The two plugins as the shell's bridge answers them. */
function fakePlugins(read: Awaited<ReturnType<AndroidPlugins['metrics']['read']>>) {
  const pipListeners: PipListener[] = [];
  const plugins = {
    pip: {
      enter: vi.fn(() => Promise.resolve()),
      addListener: vi.fn((_event: 'pipChanged', listener: PipListener) => {
        pipListeners.push(listener);
        return Promise.resolve({ remove: () => Promise.resolve() });
      }),
    },
    metrics: {
      read: vi.fn(() => Promise.resolve(read)),
      keepAwake: vi.fn<AndroidPlugins['metrics']['keepAwake']>(() => Promise.resolve()),
    },
  } satisfies AndroidPlugins;
  const emitPip = (on: boolean): void => {
    for (const listener of pipListeners) listener({ on });
  };
  return { plugins, emitPip };
}

const A24 = {
  thermal: 'MODERATE',
  batteryPct: 81,
  memMb: 212.5,
  model: 'samsung SM-A245F',
  os: 'Android 15 (API 35)',
  webview: 'com.google.android.webview 141.0.7390.122',
};

describe('installAndroidNative', () => {
  it('nativeAndroid_installsSpikeNative', async () => {
    const { plugins, emitPip } = fakePlugins(A24);
    const target: { spikeNative?: SpikeNative } = {};

    installAndroidNative({ platform: 'android', plugins, target });

    const native = target.spikeNative;
    if (!native) throw new Error('window.spikeNative was not installed');
    await expect(native.metrics()).resolves.toEqual(A24);

    await native.enterPip?.();
    expect(plugins.pip.enter).toHaveBeenCalledTimes(1);

    const seen: boolean[] = [];
    native.onPip?.((on) => seen.push(on));
    emitPip(true);
    emitPip(false);
    expect(plugins.pip.addListener).toHaveBeenCalledWith('pipChanged', expect.any(Function));
    expect(seen).toEqual([true, false]);

    await native.keepAwake?.(true);
    await native.keepAwake?.(false);
    expect(plugins.metrics.keepAwake.mock.calls).toEqual([[{ on: true }], [{ on: false }]]);
  });

  it('nativeAndroid_unknownThermalStatus_isLeftOut', async () => {
    // A status the page does not know must not reach the MV5 comparison.
    const { plugins } = fakePlugins({ thermal: 'HOT', model: 'x', os: 'Android 16 (API 36)' });
    const target: { spikeNative?: SpikeNative } = {};

    installAndroidNative({ platform: 'android', plugins, target });

    await expect(target.spikeNative?.metrics()).resolves.toEqual({ model: 'x', os: 'Android 16 (API 36)' });
  });

  it('nativeAndroid_inABrowser_installsNothing', () => {
    const { plugins } = fakePlugins(A24);
    const target: { spikeNative?: SpikeNative } = {};

    installAndroidNative({ platform: 'web', plugins, target });

    expect(target.spikeNative).toBeUndefined();
    expect(plugins.pip.addListener).not.toHaveBeenCalled();
  });
});
