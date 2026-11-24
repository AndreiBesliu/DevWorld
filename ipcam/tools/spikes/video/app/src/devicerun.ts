// The automatic run (post-release task A): «Rulează tot pentru acest
// dispozitiv» runs the device's scenarios in the order of the guide's table
// (deviceplan.ts), by itself.
//
// - It skips what spikehub already holds complete results of, unless «Rulează
//   din nou tot» asked for everything.
// - The questions come first: S10's (asked once, askOnce) and the camera's, one
//   getUserMedia prompt right after S10, which must measure the connection
//   before any camera access. Only S5 and S6 need the person later: the run
//   calls them with a sound and a vibration, again every minute, shows the
//   instruction and waits without a timeout.
// - A page that goes to the background ends the scenario as interrupted, as a
//   run from the start page does (runner.ts); the run then waits until the page
//   is visible and starts that scenario again from its beginning. Any other
//   interruption (the hub session lost, S5 back too early, S6's window closed)
//   starts it again too. The events carry the reason, so that the banner says
//   which. The third interruption of one scenario stops the run.
// - A scenario that fails (a `failed:` note) is repeated once, then the run
//   goes on, as the guide says for «eșuat: …».
// - An S3 starts from a cold phone (MV5): on the phone, the run waits before it
//   until the app has seen the phone off the charger for COLD_START_MS,
//   counted down on the screen (charger.ts; ruling B-R5, item 2).
// - The person may skip that wait and the cooling pause («Sari peste
//   așteptare», owner's request of 2026-11-24, plan
//   2026-11-24-spike-skip-wait-find-hub): a phone that is still warm can only
//   measure worse on heat, fps and CPU, never better. The next scenario's
//   result notes the skipped wait (`wait skipped: …`), on each of its attempts.
// - The screen stays on for the whole run (a wake lock), pauses included.
import type { ChargerState } from './charger';
import type { DevicePlan } from './deviceplan';
import type { InterruptReason, ScenarioId, ScenarioResult } from './results';
import type { Clock } from './scenarios/context';
import type { VisibilitySource, WakeLock } from './wakelock';

/** The interruption of one scenario that stops the run instead of starting it again. */
export const MAX_INTERRUPTIONS = 3;
/** How many times a failing scenario runs before the run goes on without it. */
export const MAX_FAILED_RUNS = 2;
/** How often the run calls the person again while it waits for them. */
export const CALL_AGAIN_MS = 60_000;
/** How long the phone stays off the charger before an S3, so that it starts cold (the guide's 10 minutes). */
export const COLD_START_MS = 10 * 60_000;
const PAUSE_TICK_MS = 1000;

/** The scenarios that need the person: S5 (Home, then back after 2 min) and S6 (picture-in-picture). */
const NEEDS_PERSON: readonly ScenarioId[] = ['S5', 'S6'];

/** What the run is doing, for the page. */
export type QueueEvent =
  | { kind: 'preflight' }
  | { kind: 'camera' }
  /** Scenario `index` of `total` (1-based) starts; `attempt` counts its runs, 1 for the first. */
  | { kind: 'scenario'; id: ScenarioId; index: number; total: number; attempt: number }
  | { kind: 'result'; id: ScenarioId; result: ScenarioResult }
  /** The scenario was interrupted for the `count`-th time, for `reason`; the run waits until the page is visible. */
  | { kind: 'interrupted'; id: ScenarioId; count: number; reason: InterruptReason }
  /** The page is visible: the scenario starts again from its beginning. */
  | { kind: 'resumed'; id: ScenarioId; count: number; reason: InterruptReason }
  /** The scenario failed once and runs again. */
  | { kind: 'retry'; id: ScenarioId }
  /** The cooling pause, with the time left. */
  | { kind: 'pause'; leftMs: number }
  /** Before S3 `id`: the phone has been off the charger for less than COLD_START_MS; the time left. */
  | { kind: 'cold'; id: ScenarioId; leftMs: number }
  /** S5 is next: the person is called, and S5 shows its instruction. */
  | { kind: 'attention'; id: ScenarioId }
  /** S6 is next: the person is called and the run waits for «Continuă». */
  | { kind: 'confirm'; id: ScenarioId }
  /** S6 ended in picture-in-picture: the run waits until the person brings the app back. */
  | { kind: 'pip' };

export type QueueOutcome =
  /** Every scenario ran (`ran`) or was complete already (`skipped`); `failed` failed twice. */
  | { kind: 'done'; ran: ScenarioId[]; skipped: ScenarioId[]; failed: ScenarioId[] }
  /** `id` was interrupted MAX_INTERRUPTIONS times, the last time for `reason`: the run stopped there. */
  | { kind: 'interrupted-too-often'; id: ScenarioId; reason: InterruptReason }
  /** The browser does not decode H.264 (Firefox without OpenH264): nothing ran. */
  | { kind: 'no-h264' };

export interface QueueEnv {
  plan: DevicePlan;
  /** The scenarios that spikehub holds complete results of, for this device. */
  complete: ReadonlySet<ScenarioId>;
  /** «Rulează din nou tot»: run every scenario, complete or not. */
  rerunAll: boolean;
  doc: VisibilitySource;
  clock: Clock;
  /**
   * Runs a scenario and stores its result, as the start page does, with
   * `notes` added to the result's notes (the waits the person skipped before
   * it). Rejects as runScenario does: a ConnectError (the hub cannot be
   * reached) or NotStarted (S3 on the charger) ends the run.
   */
  runScenario(id: ScenarioId, notes: readonly string[]): Promise<ScenarioResult>;
  /** The one camera prompt (getUserMedia) for the S3 scenarios, before the unattended part. */
  requestCamera(): Promise<void>;
  /** Whether the browser decodes H.264 (Firefox); absent where nothing is checked. */
  preflight?(): Promise<boolean>;
  holdWakeLock(): Promise<WakeLock>;
  /** A sound and a vibration, to call the person. */
  alert(): void;
  /** Resolves when the person presses «Continuă» (S6). */
  confirm(): Promise<void>;
  /** Android: picture-in-picture, in which S6 ends; absent elsewhere. */
  pip?: {
    /** Whether the app is in picture-in-picture now. */
    active(): boolean;
    /** Resolves once the app is not in picture-in-picture. */
    ended(): Promise<void>;
  };
  /**
   * Android: the charger as the app has seen it (charger.ts), read again for
   * each call; undefined when the shell cannot tell. Absent where the app
   * cannot read it (the PC), where an S3 does not wait.
   */
  charger?(): Promise<ChargerState | undefined>;
  /**
   * Offers «Sari peste așteptare» for a wait that has begun (before an S3, the
   * cooling pause): `pressed` resolves when the person presses it, and
   * `withdraw` takes the button away once the wait has ended, either way.
   * Absent: the waits run out.
   */
  offerSkip?(): SkipOffer;
  show(event: QueueEvent): void;
}

/** «Sari peste așteptare» while a wait runs (QueueEnv.offerSkip). */
export interface SkipOffer {
  pressed: Promise<void>;
  withdraw(): void;
}

/**
 * `ask`, asked once: the first answer is kept for the questions after it. A
 * question left unanswered (its run was interrupted) is asked again. S10 asks
 * whether a permission prompt appeared, and a restarted S10 does not ask again.
 */
export function askOnce(ask: (question: string) => Promise<boolean>): (question: string) => Promise<boolean> {
  let answer: boolean | undefined;
  return async (question) => {
    if (answer !== undefined) return answer;
    const given = await ask(question);
    answer = given;
    return given;
  };
}

/** Resolves once the page is visible: at once when it is. */
function untilVisible(doc: VisibilitySource): Promise<void> {
  if (doc.visibilityState === 'visible') return Promise.resolve();
  return new Promise((resolve) => {
    const listener = (): void => {
      if (doc.visibilityState !== 'visible') return;
      doc.removeEventListener('visibilitychange', listener);
      resolve();
    };
    doc.addEventListener('visibilitychange', listener);
  });
}

/**
 * Calls the person now and every CALL_AGAIN_MS until the returned function
 * stops it; with `untilHidden`, also once the page goes to the background (S5:
 * Home was pressed).
 */
function callPerson(env: QueueEnv, untilHidden: boolean): () => void {
  let stopped = false;
  let cancel: () => void = () => undefined;
  const ring = (): void => {
    if (stopped) return;
    env.alert();
    cancel = env.clock.setTimer(ring, CALL_AGAIN_MS);
  };
  const onVisibility = (): void => {
    if (env.doc.visibilityState === 'hidden') stop();
  };
  const stop = (): void => {
    stopped = true;
    cancel();
    env.doc.removeEventListener('visibilitychange', onVisibility);
  };
  if (untilHidden) env.doc.addEventListener('visibilitychange', onVisibility);
  ring();
  return stop;
}

/** How a wait ended: it ran out, or the person skipped it after `waitedMs`. */
type WaitEnd = { kind: 'done' } | { kind: 'skipped'; waitedMs: number };

/**
 * A wait of `ms`, counted down on the page every second with the event that
 * `event` makes of the time left, which the person may skip where the page
 * offers it (offerSkip).
 */
function countDown(env: QueueEnv, ms: number, event: (leftMs: number) => QueueEvent): Promise<WaitEnd> {
  const start = env.clock.now();
  const elapsed = (): number => Math.round(env.clock.now() - start);
  return new Promise((resolve) => {
    let ended = false;
    let cancel: () => void = () => undefined;
    const offer = ms > 0 ? env.offerSkip?.() : undefined;
    const end = (how: WaitEnd): void => {
      if (ended) return;
      ended = true;
      cancel();
      offer?.withdraw();
      resolve(how);
    };
    const tick = (): void => {
      const leftMs = Math.max(0, ms - elapsed());
      env.show(event(leftMs));
      if (leftMs === 0) end({ kind: 'done' });
      else cancel = env.clock.setTimer(tick, Math.min(PAUSE_TICK_MS, leftMs));
    };
    void offer?.pressed.then(() => {
      end({ kind: 'skipped', waitedMs: Math.min(ms, elapsed()) });
    });
    tick();
  });
}

const seconds = (ms: number): number => Math.round(ms / 1000);

/**
 * Before S3 `id`: waits until the app has seen the phone off the charger for
 * COLD_START_MS, and reads the charger again after each wait, since the phone
 * may have been plugged in meanwhile. On the charger, or without a reading,
 * nothing to wait for: S3 itself refuses the charger and says so. Answers the
 * note of a wait that the person skipped, undefined otherwise.
 */
async function coldStart(env: QueueEnv, id: ScenarioId): Promise<string | undefined> {
  if (!env.charger) return undefined;
  for (;;) {
    const state = await env.charger();
    if (state?.kind !== 'off' || state.forMs >= COLD_START_MS) return undefined;
    const wait = await countDown(env, COLD_START_MS - state.forMs, (leftMs) => ({ kind: 'cold', id, leftMs }));
    if (wait.kind === 'skipped') {
      return `wait skipped: off the charger for ${seconds(state.forMs + wait.waitedMs)} s of ${seconds(COLD_START_MS)} before ${id}`;
    }
  }
}

const failed = (result: ScenarioResult): boolean => result.notes.some((note) => note.startsWith('failed:'));

/**
 * Why an interrupted result was interrupted. A result without the reason (written
 * before the field existed) reads as hidden, as results.ts says, at its end.
 */
export const reasonOf = (result: ScenarioResult): InterruptReason => result.interruption ?? { kind: 'hidden', atS: Math.round(result.durationS) };

/** What the run does: the scenarios that are not complete, and a pause only between two that run. */
function stepsToRun(env: QueueEnv): { steps: DevicePlan['steps']; skipped: ScenarioId[] } {
  const runs = (step: DevicePlan['steps'][number] | undefined): boolean =>
    step?.kind === 'scenario' && (env.rerunAll || !env.complete.has(step.id));
  const skipped: ScenarioId[] = [];
  const steps = env.plan.steps.filter((step, index, all) => {
    if (step.kind === 'pause') return runs(all[index - 1]) && all.slice(index + 1).some(runs);
    if (runs(step)) return true;
    skipped.push(step.id);
    return false;
  });
  return { steps, skipped };
}

/**
 * Runs the device's scenarios. Resolves with how the run ended; rejects with
 * what runScenario rejects with (the hub out of reach, S3 on the charger),
 * which ends the run. The wake lock is held from the first scenario to the end.
 */
export async function runQueue(env: QueueEnv): Promise<QueueOutcome> {
  const { steps, skipped } = stepsToRun(env);
  const scenarios = steps.flatMap((step) => (step.kind === 'scenario' ? [step.id] : []));
  if (scenarios.length === 0) return { kind: 'done', ran: [], skipped, failed: [] };
  if (env.preflight) {
    env.show({ kind: 'preflight' });
    if (!(await env.preflight())) return { kind: 'no-h264' };
  }
  const needsCamera = scenarios.some((id) => id.startsWith('S3-'));
  let cameraAsked = false;
  const failures: ScenarioId[] = [];
  const wakeLock = await env.holdWakeLock();
  try {
    let index = 0;
    // The note of a cooling pause that the person skipped, for the scenario after it.
    let skipped: string[] = [];
    for (const step of steps) {
      if (step.kind === 'pause') {
        const wait = await countDown(env, step.ms, (leftMs) => ({ kind: 'pause', leftMs }));
        if (wait.kind === 'skipped') skipped = [`wait skipped: cooling pause after ${seconds(wait.waitedMs)} s of ${seconds(step.ms)}`];
        continue;
      }
      if (needsCamera && !cameraAsked && step.id !== 'S10') {
        cameraAsked = true;
        env.show({ kind: 'camera' });
        await env.requestCamera();
      }
      index += 1;
      const outcome = await runOne(env, step.id, index, scenarios.length, skipped);
      skipped = [];
      if (typeof outcome === 'object') return { kind: 'interrupted-too-often', id: step.id, reason: outcome.tooOften };
      if (outcome === 'failed') failures.push(step.id);
    }
  } finally {
    await wakeLock.release();
  }
  return { kind: 'done', ran: scenarios, skipped, failed: failures };
}

/**
 * Runs one scenario until it completes, fails twice or is interrupted too
 * often; for the last, with the reason of the last interruption. `skipped`
 * holds the note of the cooling pause skipped before it; each attempt's result
 * carries it, with the notes of the cold starts skipped so far.
 */
async function runOne(
  env: QueueEnv,
  id: ScenarioId,
  index: number,
  total: number,
  skipped: readonly string[],
): Promise<'done' | 'failed' | { tooOften: InterruptReason }> {
  let interruptions = 0;
  let failedRuns = 0;
  const notes = [...skipped];
  for (let attempt = 1; ; attempt += 1) {
    if (id.startsWith('S3-')) {
      const note = await coldStart(env, id);
      if (note !== undefined) notes.push(note);
    }
    await untilVisible(env.doc);
    let stopCalling: () => void = () => undefined;
    if (id === 'S6') {
      env.show({ kind: 'confirm', id });
      stopCalling = callPerson(env, false);
      await env.confirm();
      stopCalling();
    } else if (NEEDS_PERSON.includes(id)) {
      env.show({ kind: 'attention', id });
      stopCalling = callPerson(env, true);
    }
    env.show({ kind: 'scenario', id, index, total, attempt });
    let result: ScenarioResult;
    try {
      result = await env.runScenario(id, [...notes]);
    } finally {
      stopCalling();
    }
    env.show({ kind: 'result', id, result });
    if (result.interrupted) {
      interruptions += 1;
      const reason = reasonOf(result);
      if (interruptions >= MAX_INTERRUPTIONS) return { tooOften: reason };
      env.show({ kind: 'interrupted', id, count: interruptions, reason });
      await untilVisible(env.doc);
      env.show({ kind: 'resumed', id, count: interruptions, reason });
      continue;
    }
    if (failed(result)) {
      failedRuns += 1;
      if (failedRuns >= MAX_FAILED_RUNS) return 'failed';
      env.show({ kind: 'retry', id });
      continue;
    }
    // S6 ended in picture-in-picture: the person brings the app back. Out of it already, nobody is called.
    if (id === 'S6' && env.pip?.active()) {
      env.show({ kind: 'pip' });
      const stop = callPerson(env, false);
      await env.pip.ended().finally(stop);
    }
    return 'done';
  }
}
