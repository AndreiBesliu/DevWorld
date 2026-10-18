// The Android shell's `window.spikeNative` (Task 10): native.ts's facade over
// the shell's two Capacitor plugins, the production `pip` plugin
// (apps/mobile/plugins/pip) and the SPIKE-ONLY metrics plugin
// (tools/spikes/video/android-metrics). In a browser it installs nothing.
import { Capacitor } from '@capacitor/core';
import { Pip, type PipPlugin } from '@ipcam/capacitor-pip';
import { SpikeMetrics, type AndroidMetrics, type SpikeMetricsPlugin } from 'spike-android-metrics';
import { THERMAL_LEVELS, type NativeMetrics, type SpikeNative, type ThermalStatus } from './native';

export interface AndroidPlugins {
  pip: Pick<PipPlugin, 'enter' | 'addListener'>;
  metrics: SpikeMetricsPlugin;
}

export interface InstallDeps {
  /** `Capacitor.getPlatform()`: `android` in the shell, `web` in a browser. */
  platform: string;
  plugins: AndroidPlugins;
  target: { spikeNative?: SpikeNative };
}

const isThermal = (value: string | undefined): value is ThermalStatus =>
  (THERMAL_LEVELS as readonly (string | undefined)[]).includes(value);

/** The plugin's answer as NativeMetrics; a thermal status the page does not know is left out. */
function toNativeMetrics({ thermal, ...rest }: AndroidMetrics): NativeMetrics {
  return isThermal(thermal) ? { thermal, ...rest } : rest;
}

/** Installs `window.spikeNative` when the page runs in the Android shell. */
export function installAndroidNative(
  { platform, plugins: { pip, metrics }, target }: InstallDeps = {
    platform: Capacitor.getPlatform(),
    plugins: { pip: Pip, metrics: SpikeMetrics },
    target: globalThis as { spikeNative?: SpikeNative },
  },
): void {
  if (platform !== 'android') return;
  target.spikeNative = {
    metrics: async () => toNativeMetrics(await metrics.read()),
    enterPip: () => pip.enter(),
    onPip: (cb) => {
      void pip.addListener('pipChanged', ({ on }) => {
        cb(on);
      });
    },
    keepAwake: (on) => metrics.keepAwake({ on }),
  };
}
