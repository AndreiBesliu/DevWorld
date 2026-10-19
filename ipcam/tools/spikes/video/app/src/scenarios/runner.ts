// Runs one scenario and turns what it measured into a ScenarioResult. The
// runner owns what every scenario shares: the wake lock, the stage over the
// page, the visibility rule (Review Focus 2), the watch on the hub session
// (R49), the hub's jitter reports (R28), the clean-up of sessions and players,
// and keeping addresses out of the notes (PH0-22).
import type { MediaLink, Player } from '@ipcam/player';
import { simCameras, type HubCamera } from '../cameras';
import { ConnectError } from '../hubhttp';
import { readNativeMetrics } from '../native';
import { scrubText, type InterruptReason, type ScenarioId, type ScenarioResult } from '../results';
import type { SpikeSession } from '../session';
import type { ScenarioEnv } from './context';

/** How long a session may stay `disconnected` before the run counts it as lost (R49). */
export const DISCONNECTED_GRACE_MS = 3000;

/** A scenario stopped before it measured what it should; the result says why (InterruptReason). */
export class Interrupted extends Error {
  constructor() {
    super('interrupted');
    this.name = 'Interrupted';
  }
}

/** The `interrupted: …` note of a reason, which the report reads. */
export function interruptionNote(reason: InterruptReason): string {
  switch (reason.kind) {
    case 'hidden':
      return `interrupted: the page was hidden ${reason.atS} s into the run`;
    case 'session-lost':
      return `interrupted: the hub session was lost ${reason.atS} s into the run (${reason.how})`;
    case 'charging':
      return `interrupted: the phone was on the charger ${reason.atS} s into the run`;
    case 'early-return':
      return `interrupted: back from the background after ${reason.backgroundS} s, at least 120 s are needed`;
    case 'pip-left':
      return `interrupted: picture-in-picture ended after ${reason.pipS} s, 300 s are needed`;
  }
}

/** What a scenario measures with. Waits reject with Interrupted once the run is interrupted. */
export interface Run {
  readonly env: ScenarioEnv;
  now(): number;
  /** Seconds since the run started, rounded. */
  elapsedS(): number;
  sleep(ms: number): Promise<void>;
  /** The value of `promise`, or `timeout` when it has not settled within `ms`. */
  within<T>(promise: Promise<T>, ms: number): Promise<T | 'timeout'>;
  /** Calls `fn` every `ms` until the returned function or the end of the run stops it. */
  every(ms: number, fn: () => void): () => void;
  sample(key: string, value: number): void;
  /** A note for the result. URLs and IP literals are removed (PH0-22). */
  note(text: string): void;
  /**
   * Opens a session with the hub, closed at the end of the run; its jitter
   * reports go into the result. The run watches it: when the session is lost
   * (its rpc channel or PeerConnection closes, the PeerConnection fails, or it
   * stays disconnected for over DISCONNECTED_GRACE_MS) the run ends as
   * interrupted, without measured values (R49).
   */
  session(slots: number): Promise<SpikeSession>;
  /**
   * Stops watching `session` (S5's background phase, R44) and returns the
   * function that watches it again, which checks its state at once.
   */
  unwatch(session: SpikeSession): () => void;
  /**
   * Ends the run as interrupted for `reason`: the result is left out of the
   * verdicts and the page offers «Repetă». Notes the reason and throws, so
   * the scenario stops there.
   */
  interrupt(reason: InterruptReason): never;
  /**
   * A player on `video`, muted and inline, reading `slot` of `session` (live)
   * or nothing (recordings), stopped at the end of the run.
   */
  player(video: HTMLVideoElement, session?: SpikeSession, slot?: number): Player;
  /** Runs `fn` at the end of the run, before the sessions close. */
  atEnd(fn: () => void): void;
  /** The hub's simulated cameras. Throws when it has none. */
  sims(): HubCamera[];
}

export interface ScenarioDefinition {
  /**
   * S5 and S6 send the app to the background or into PiP on purpose: a hidden
   * page does not interrupt them, and the stage's status line does not ask to
   * keep the screen on.
   */
  readonly backgroundsOnPurpose?: boolean;
  run(run: Run): Promise<void>;
}

function describe(error: unknown): string {
  return error instanceof Error ? `${error.name}: ${error.message}` : String(error);
}

function linkFor(session: SpikeSession | undefined): MediaLink {
  return {
    webrtcTrack: (slot) =>
      session ? Promise.resolve(session.track(slot)) : Promise.reject(new Error('this player has no WebRTC session')),
    byteStream: () => Promise.reject(new Error('the spike plays recordings over HLS')),
  };
}

/**
 * Runs `definition` and returns its result. The stage covers the page while it
 * runs. A page that becomes hidden interrupts the run (unless the scenario
 * backgrounds on purpose): the run stops, keeps what it measured and is marked
 * `interrupted`. A lost hub session interrupts it too, and then nothing it
 * measured is kept (R49). A ConnectError (the hub cannot be reached) is
 * thrown, for the start page to explain; any other failure ends the run with
 * a `failed: …` note.
 */
export async function runDefinition(id: ScenarioId, definition: ScenarioDefinition, env: ScenarioEnv): Promise<ScenarioResult> {
  const startedAt = new Date().toISOString();
  const start = env.clock.now();
  const samples: Record<string, number[]> = {};
  const notes: string[] = [];
  const hubJitterMs: number[] = [];
  const endings: (() => void)[] = [];
  const sessions: SpikeSession[] = [];
  const watches = new Map<SpikeSession, () => void>();
  let interruption: InterruptReason | undefined;
  let interrupt: () => void = () => undefined;
  const interrupted = new Promise<never>((_resolve, reject) => {
    interrupt = () => {
      reject(new Interrupted());
    };
  });
  interrupted.catch(() => undefined); // read by the waits; nobody may be waiting
  const elapsedS = (): number => Math.round((env.clock.now() - start) / 1000);

  /** Ends the run as interrupted, once: the first reason is the one recorded. */
  const stop = (reason: InterruptReason): void => {
    if (interruption) return;
    interruption = reason;
    notes.push(interruptionNote(reason));
    interrupt();
  };

  const onVisibility = (): void => {
    if (env.doc.visibilityState === 'hidden') stop({ kind: 'hidden', atS: elapsedS() });
  };
  if (!definition.backgroundsOnPurpose) {
    env.doc.addEventListener('visibilitychange', onVisibility);
    onVisibility(); // a run started while the page is hidden: an autorun sent to a phone whose screen is off
  }

  const watch = (session: SpikeSession): void => {
    if (watches.has(session)) return;
    let cancelGrace: (() => void) | undefined;
    const lose = (how: string): void => {
      stop({ kind: 'session-lost', atS: elapsedS(), how });
    };
    const check = (state: RTCPeerConnectionState): void => {
      if (state === 'failed' || state === 'closed') {
        lose(`connection ${state}`);
      } else if (state === 'disconnected') {
        cancelGrace ??= env.clock.setTimer(() => {
          lose(`connection disconnected for more than ${DISCONNECTED_GRACE_MS / 1000} s`);
        }, DISCONNECTED_GRACE_MS);
      } else {
        cancelGrace?.();
        cancelGrace = undefined;
      }
    };
    const off = session.onConnectionState(check);
    watches.set(session, () => {
      off();
      cancelGrace?.();
    });
    check(session.connectionState());
  };
  const unwatch = (session: SpikeSession): void => {
    watches.get(session)?.();
    watches.delete(session);
  };

  const guard = <T>(promise: Promise<T>): Promise<T> => Promise.race([promise, interrupted]);
  const delay = (ms: number): { promise: Promise<void>; cancel: () => void } => {
    let cancel: () => void = () => undefined;
    const promise = new Promise<void>((resolve) => {
      cancel = env.clock.setTimer(resolve, ms);
    });
    return { promise, cancel };
  };

  const run: Run = {
    env,
    now: () => env.clock.now(),
    elapsedS,
    sleep(ms) {
      const { promise, cancel } = delay(ms);
      return guard(promise).finally(cancel);
    },
    within<T>(promise: Promise<T>, ms: number) {
      const { promise: timeout, cancel } = delay(ms);
      return guard(Promise.race([promise, timeout.then(() => 'timeout' as const)])).finally(cancel);
    },
    every(ms, fn) {
      let cancel: () => void = () => undefined;
      let stopped = false;
      const tick = (): void => {
        if (stopped) return;
        cancel = env.clock.setTimer(() => {
          fn();
          tick();
        }, ms);
      };
      const stopEvery = (): void => {
        stopped = true;
        cancel();
      };
      tick();
      endings.push(stopEvery);
      return stopEvery;
    },
    sample(key, value) {
      (samples[key] ??= []).push(value);
    },
    note(text) {
      notes.push(scrubText(text));
    },
    async session(slots) {
      const opening = env.openSession(env.hubUrl, slots);
      const session = await guard(opening).catch((error: unknown) => {
        // Interrupted while it opened: the session that comes later is closed at once.
        opening.then(
          (late) => {
            late.close();
          },
          () => undefined,
        );
        throw error;
      });
      sessions.push(session);
      session.onJitter((ms) => hubJitterMs.push(ms));
      watch(session);
      return session;
    },
    unwatch(session) {
      unwatch(session);
      return () => {
        watch(session);
      };
    },
    interrupt(reason) {
      stop(reason);
      throw new Interrupted();
    },
    player(video, session, slot = 0) {
      // Browsers autoplay only muted inline video without a gesture; the player leaves both to its host.
      video.muted = true;
      video.playsInline = true;
      const player = env.createPlayer({ slot });
      player.attach(video, linkFor(session));
      endings.push(() => {
        player.stop();
      });
      return player;
    },
    atEnd(fn) {
      endings.push(fn);
    },
    sims() {
      const sims = simCameras(env.cameras);
      if (sims.length === 0) throw new Error('the hub has no simulated cameras (spikehub -sims 0)');
      return sims;
    },
  };

  const wakeLock = await env.holdWakeLock();
  if (wakeLock.kind === 'n/a') run.note('wake lock: n/a');
  let failure: unknown;
  let device: ScenarioResult['device'] = env.device;
  try {
    if (env.native) {
      const metrics = await guard(readNativeMetrics(env.native, env.clock.setTimer));
      if (metrics === 'n/a') {
        run.note('native metrics: n/a');
      } else {
        device = {
          ...device,
          ...(metrics.model ? { model: metrics.model } : {}),
          ...(metrics.os ? { os: metrics.os } : {}),
          ...(metrics.webview ? { webview: metrics.webview } : {}),
        };
      }
    }
    env.stage.open(`${id}: ${env.t(definition.backgroundsOnPurpose ? 'statusRunningShort' : 'statusRunning')}`);
    await definition.run(run);
  } catch (error) {
    if (interruption === undefined) {
      if (error instanceof ConnectError) failure = error;
      else run.note(`failed: ${describe(error)}`);
    }
  } finally {
    for (const stopWatching of watches.values()) stopWatching();
    watches.clear();
    env.doc.removeEventListener('visibilitychange', onVisibility);
    for (const end of endings.reverse()) {
      try {
        end();
      } catch {
        // Teardown goes on: a player or a channel that is already gone.
      }
    }
    for (const session of sessions) session.close();
    env.stage.reset();
    await wakeLock.release();
  }
  if (failure !== undefined) throw failure;

  // A lost session leaves frozen or empty counters behind: nothing measured counts (R49).
  const lost = interruption?.kind === 'session-lost';
  return {
    scenario: id,
    device,
    startedAt,
    durationS: Math.round(env.clock.now() - start) / 1000,
    interrupted: interruption !== undefined,
    ...(interruption ? { interruption } : {}),
    hubJitterMs: lost ? [] : hubJitterMs,
    samples: lost ? {} : samples,
    notes,
  };
}
