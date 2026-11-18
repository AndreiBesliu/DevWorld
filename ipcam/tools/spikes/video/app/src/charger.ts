// The phone's charger as the app has seen it (ruling B-R5, item 2). S3 measures
// heat (MV5), so the phone must start it cold: the guide asks to unplug it at
// least 10 minutes before the button is pressed, and the automatic run waits
// before an S3 until the app has seen the phone off the charger for that long
// (devicerun.ts). The shell tells only whether the phone is on the charger now
// (NativeMetrics.charging), never since when, so the count starts at the
// first reading off the charger after the last reading on it: the app's own
// start at the earliest.

/** What the app knows of the charger: on it now, or off it for `forMs` at least. */
export type ChargerState = { kind: 'on' } | { kind: 'off'; forMs: number };

export interface ChargerWatch {
  /** A reading of the shell: on the charger, off it, or undefined when the shell could not tell. */
  see(charging: boolean | undefined): void;
  /** The state as seen so far; undefined before the first reading that told. */
  state(): ChargerState | undefined;
}

export function watchCharger(now: () => number): ChargerWatch {
  let on: boolean | undefined;
  let offSince = 0;
  return {
    see(charging) {
      if (charging === undefined) return;
      if (!charging && on !== false) offSince = now();
      on = charging;
    },
    state() {
      if (on === undefined) return undefined;
      return on ? { kind: 'on' } : { kind: 'off', forMs: now() - offSince };
    },
  };
}
