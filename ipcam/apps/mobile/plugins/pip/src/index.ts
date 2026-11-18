// The JavaScript side of the `pip` plugin (spec §6.6): picture-in-picture of
// the whole Android Activity, at the 16:9 of a camera picture. The Kotlin side
// is android/src/main/java/ro/ipcam/pip/PipPlugin.kt.
import { registerPlugin, type PluginListenerHandle } from '@capacitor/core';

export interface PipChange {
  /** True on entering picture-in-picture, false on leaving it. */
  on: boolean;
}

export interface PipPlugin {
  /** Puts the Activity into picture-in-picture. Rejects where the device or the Activity cannot. */
  enter(): Promise<void>;
  addListener(eventName: 'pipChanged', listener: (change: PipChange) => void): Promise<PluginListenerHandle>;
}

export const Pip = registerPlugin<PipPlugin>('Pip');
