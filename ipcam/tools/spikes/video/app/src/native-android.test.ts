import { describe, expect, it, vi } from 'vitest';
import { installAndroidNative, type AndroidPlugins } from './native-android';
import type { SpikeNative } from './native';
import { unhandledRejections } from './scenarios/fakes';

type PipListener = (event: { on: boolean }) => void;

/**
 * The two plugins as the shell's bridge answers them. The first `rejectPipListener`
 * calls of `pip.addListener` reject, like a bridge that is gone.
 */
function fakePlugins(read: Awaited<ReturnType<AndroidPlugins['metrics']['read']>>, { rejectPipListener = 0 } = {}) {
  const pipListeners: PipListener[] = [];
  /** The event name of every addListener call, the rejected ones included. */
  const pipSubscriptions: string[] = [];
  let rejections = rejectPipListener;
  const plugins = {
    pip: {
      enter: vi.fn(() => Promise.resolve()),
      // A plain function: a vi.fn spy handles the promise it returns, which would hide an unhandled rejection.
      addListener: (eventName: 'pipChanged', listener: PipListener) => {
        pipSubscriptions.push(eventName);
        if (rejections > 0) {
          rejections -= 1;
          return Promise.reject(new Error('the bridge is gone'));
        }
        pipListeners.push(listener);
        return Promise.resolve({ remove: () => Promise.resolve() });
      },
    },
    metrics: {
      read: vi.fn(() => Promise.resolve(read)),
      keepAwake: vi.fn<AndroidPlugins['metrics']['keepAwake']>(() => Promise.resolve()),
    },
  } satisfies AndroidPlugins;
  const emitPip = (on: boolean): void => {
    for (const listener of pipListeners) listener({ on });
  };
  return { plugins, emitPip, pipSubscriptions };
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
    const { plugins, emitPip, pipSubscriptions } = fakePlugins(A24);
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
    expect(pipSubscriptions).toEqual(['pipChanged']);
    expect(seen).toEqual([true, false]);

    await native.keepAwake?.(true);
    await native.keepAwake?.(false);
    expect(plugins.metrics.keepAwake.mock.calls).toEqual([[{ on: true }], [{ on: false }]]);
  });

  it('nativeAndroid_twoPipCallbacks_shareOneNativeListener', () => {
    const { plugins, emitPip, pipSubscriptions } = fakePlugins(A24);
    const target: { spikeNative?: SpikeNative } = {};
    installAndroidNative({ platform: 'android', plugins, target });

    const first: boolean[] = [];
    const second: boolean[] = [];
    target.spikeNative?.onPip?.((on) => first.push(on));
    target.spikeNative?.onPip?.((on) => second.push(on));
    emitPip(true);
    emitPip(false);

    expect(pipSubscriptions).toEqual(['pipChanged']);
    expect(first).toEqual([true, false]);
    expect(second).toEqual([true, false]);
  });

  it('nativeAndroid_pipListenerRejected_isNoUnhandledRejection', async () => {
    const { plugins } = fakePlugins(A24, { rejectPipListener: 1 });
    const target: { spikeNative?: SpikeNative } = {};
    installAndroidNative({ platform: 'android', plugins, target });

    const unhandled = await unhandledRejections(() => {
      target.spikeNative?.onPip?.(() => undefined);
      return Promise.resolve();
    });

    expect(unhandled).toEqual([]);
  });

  it('nativeAndroid_pipListenerRejected_theNextOnPipRegistersAgain', async () => {
    const { plugins, emitPip, pipSubscriptions } = fakePlugins(A24, { rejectPipListener: 1 });
    const target: { spikeNative?: SpikeNative } = {};
    installAndroidNative({ platform: 'android', plugins, target });
    const seen: boolean[] = [];

    await unhandledRejections(() => {
      target.spikeNative?.onPip?.(() => undefined);
      return Promise.resolve();
    });
    target.spikeNative?.onPip?.((on) => seen.push(on));
    emitPip(true);

    expect(pipSubscriptions).toEqual(['pipChanged', 'pipChanged']);
    expect(seen).toEqual([true]);
  });

  it('nativeAndroid_unknownThermalStatus_isLeftOut', async () => {
    // A status the page does not know must not reach the MV5 comparison.
    const { plugins } = fakePlugins({ thermal: 'HOT', model: 'x', os: 'Android 16 (API 36)' });
    const target: { spikeNative?: SpikeNative } = {};

    installAndroidNative({ platform: 'android', plugins, target });

    await expect(target.spikeNative?.metrics()).resolves.toEqual({ model: 'x', os: 'Android 16 (API 36)' });
  });

  it('nativeAndroid_inABrowser_installsNothing', () => {
    const { plugins, pipSubscriptions } = fakePlugins(A24);
    const target: { spikeNative?: SpikeNative } = {};

    installAndroidNative({ platform: 'web', plugins, target });

    expect(target.spikeNative).toBeUndefined();
    expect(pipSubscriptions).toEqual([]);
  });
});
