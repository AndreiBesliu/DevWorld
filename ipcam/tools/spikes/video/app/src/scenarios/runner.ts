// Runs one scenario and turns what it measured into a ScenarioResult. The
// runner owns what every scenario shares: the wake lock, the visibility rule
// (Review Focus 2), the hub's jitter reports (R28), the clean-up of sessions and
// players, and keeping addresses out of the notes (PH0-22).
import type { MediaLink, Player } from '@ipcam/player';
import { simCameras, type HubCamera } from '../cameras';
import { ConnectError } from '../hubhttp';
import { scrubText, type ScenarioId, type ScenarioResult } from '../results';
import type { SpikeSession } from '../session';
import type { ScenarioEnv } from './context';

/** A scenario stopped because the page was hidden. */
export class Interrupted extends Error {
  constructor() {
    super('interrupted');
    this.name = 'Interrupted';
  }
}

/** What a scenario measures with. Waits reject with Interrupted once the run is interrupted. */
export interface Run {
  readonly env: ScenarioEnv;
  now(): number;
  sleep(ms: number): Promise<void>;
  /** The value of `promise`, or `timeout` when it has not settled within `ms`. */
  within<T>(promise: Promise<T>, ms: number): Promise<T | 'timeout'>;
  /** Calls `fn` every `ms` until the returned function or the end of the run stops it. */
  every(ms: number, fn: () => void): () => void;
  sample(key: string, value: number): void;
  /** A note for the result. URLs and IP literals are removed (PH0-22). */
  note(text: string): void;
  /** Opens a session with the hub, closed at the end of the run; its jitter reports go into the result. */
  session(slots: number): Promise<SpikeSession>;
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
  /** S5 and S6 send the app to the background or into PiP on purpose: a hidden page does not interrupt them. */
  readonly backgroundsOnPurpose?: boolean;
  run(run: Run): Promise<void>;
}

const NATIVE_METRICS_TIMEOUT_MS = 2000;

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
 * Runs `definition` and returns its result. A page that becomes hidden
 * interrupts the run (unless the scenario backgrounds on purpose): the run
 * stops, keeps what it measured and is marked `interrupted`. A ConnectError
 * (the hub cannot be reached) is thrown, for the start page to explain; any
 * other failure ends the run with a note.
 */
export async function runDefinition(id: ScenarioId, definition: ScenarioDefinition, env: ScenarioEnv): Promise<ScenarioResult> {
  const startedAt = new Date().toISOString();
  const start = env.clock.now();
  const samples: Record<string, number[]> = {};
  const notes: string[] = [];
  const hubJitterMs: number[] = [];
  const endings: (() => void)[] = [];
  const sessions: SpikeSession[] = [];
  let interrupted = false;
  let interrupt: () => void = () => undefined;
  const interruption = new Promise<never>((_resolve, reject) => {
    interrupt = () => {
      reject(new Interrupted());
    };
  });
  interruption.catch(() => undefined); // read by the waits; nobody may be waiting

  const onVisibility = (): void => {
    if (interrupted || env.doc.visibilityState !== 'hidden') return;
    interrupted = true;
    notes.push(`interrupted: the page was hidden ${Math.round((env.clock.now() - start) / 1000)} s into the run`);
    interrupt();
  };
  if (!definition.backgroundsOnPurpose) env.doc.addEventListener('visibilitychange', onVisibility);

  const guard = <T>(promise: Promise<T>): Promise<T> => Promise.race([promise, interruption]);
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
      const stop = (): void => {
        stopped = true;
        cancel();
      };
      tick();
      endings.push(stop);
      return stop;
    },
    sample(key, value) {
      (samples[key] ??= []).push(value);
    },
    note(text) {
      notes.push(scrubText(text));
    },
    async session(slots) {
      const session = await guard(env.openSession(env.hubUrl, slots));
      sessions.push(session);
      session.onJitter((ms) => hubJitterMs.push(ms));
      return session;
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
    const metrics = env.native ? await run.within(env.native.metrics(), NATIVE_METRICS_TIMEOUT_MS).catch(() => undefined) : undefined;
    if (metrics !== undefined && metrics !== 'timeout') {
      device = {
        ...device,
        ...(metrics.model ? { model: metrics.model } : {}),
        ...(metrics.os ? { os: metrics.os } : {}),
        ...(metrics.webview ? { webview: metrics.webview } : {}),
      };
    }
    await definition.run(run);
  } catch (error) {
    if (!interrupted) {
      if (error instanceof ConnectError) failure = error;
      else run.note(`failed: ${describe(error)}`);
    }
  } finally {
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

  return {
    scenario: id,
    device,
    startedAt,
    durationS: Math.round(env.clock.now() - start) / 1000,
    interrupted,
    hubJitterMs,
    samples,
    notes,
  };
}
