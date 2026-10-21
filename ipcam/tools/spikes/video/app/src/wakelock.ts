// Keeps the screen on while a scenario runs (Review Focus 2): a phone whose
// screen turns off in the middle of a 10 to 30 min scenario interrupts it.
import type { SpikeNative } from './native';

/** The part of navigator.wakeLock that the app uses. */
export interface ScreenWakeLock {
  request(type: 'screen'): Promise<{ readonly released: boolean; release(): Promise<void> }>;
}

/** The part of document that the app uses. */
export interface VisibilitySource {
  readonly visibilityState: DocumentVisibilityState;
  addEventListener(type: 'visibilitychange', listener: () => void): void;
  removeEventListener(type: 'visibilitychange', listener: () => void): void;
}

export interface WakeLock {
  /** `screen`: the Screen Wake Lock API; `native`: the shell's keepAwake; `n/a`: neither. */
  readonly kind: 'screen' | 'native' | 'n/a';
  release(): Promise<void>;
}

export interface WakeLockDeps {
  wakeLock?: ScreenWakeLock;
  native?: SpikeNative;
  doc?: VisibilitySource;
}

/**
 * Holds the screen on: with navigator.wakeLock where the page gets it, else
 * with the shell's keepAwake, else not at all (`n/a`). The browser drops a
 * screen wake lock while the page is hidden, so it is asked for again when the
 * page is visible again.
 */
export async function holdWakeLock({ wakeLock, native, doc }: WakeLockDeps): Promise<WakeLock> {
  if (wakeLock) {
    try {
      let sentinel = await wakeLock.request('screen');
      let held = true;
      const onVisibility = (): void => {
        if (!held || doc?.visibilityState !== 'visible' || !sentinel.released) return;
        wakeLock.request('screen').then(
          (next) => {
            if (held) sentinel = next;
            else void next.release();
          },
          () => undefined, // refused this time; the run goes on, maybe with the screen dimming
        );
      };
      doc?.addEventListener('visibilitychange', onVisibility);
      return {
        kind: 'screen',
        release: async () => {
          held = false;
          doc?.removeEventListener('visibilitychange', onVisibility);
          await sentinel.release().catch(() => undefined);
        },
      };
    } catch {
      // Refused, as in an Android WebView: try the shell.
    }
  }
  const keepAwake = native?.keepAwake?.bind(native);
  if (keepAwake) {
    try {
      await keepAwake(true);
      return { kind: 'native', release: () => keepAwake(false).catch(() => undefined) };
    } catch {
      // Neither works: the run goes on without.
    }
  }
  return { kind: 'n/a', release: () => Promise.resolve() };
}
