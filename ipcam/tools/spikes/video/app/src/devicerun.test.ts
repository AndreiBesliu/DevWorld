import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChargerState } from './charger';
import { DEVICE_PLANS, type DevicePlan } from './deviceplan';
import { askOnce, runQueue, type QueueEnv, type QueueEvent, type QueueOutcome } from './devicerun';
import { ConnectError } from './hubhttp';
import type { InterruptReason, ScenarioId, ScenarioResult } from './results';
import { FakeDocument, finish } from './scenarios/fakes';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

const plan = (id: string): DevicePlan => {
  const found = DEVICE_PLANS.find((candidate) => candidate.id === id);
  if (!found) throw new Error(`no plan ${id}`);
  return found;
};

const result = (scenario: ScenarioId, over: Partial<ScenarioResult> = {}): ScenarioResult => ({
  scenario,
  device: { platform: 'chrome', ua: 'test' },
  startedAt: '2026-11-18T09:00:00.000Z',
  durationS: 30,
  interrupted: false,
  hubJitterMs: [],
  samples: {},
  notes: [],
  ...over,
});

const interrupted = (scenario: ScenarioId, interruption: InterruptReason = { kind: 'hidden', atS: 3 }): ScenarioResult =>
  result(scenario, { interrupted: true, interruption, notes: ['interrupted: the page was hidden 3 s into the run'] });

/** A step of the fake run: what the scenario returns, after how long. */
type Outcome = ScenarioResult | Error | ((doc: FakeDocument) => ScenarioResult | Error);

interface Harness {
  env: QueueEnv;
  doc: FakeDocument;
  /** Scenario starts, in order, with the fake time they started at. */
  starts: { id: ScenarioId; at: number }[];
  events: QueueEvent[];
  /** What happened, in order: scenario starts and the queue's calls. */
  log: string[];
  wakeLock: { held: number; released: number };
  confirm: { resolve: () => void };
  pip: { end: () => void; inPip: boolean };
}

function harness(
  planId: string,
  options: {
    complete?: ScenarioId[];
    rerunAll?: boolean;
    outcomes?: Partial<Record<ScenarioId, Outcome[]>>;
    runMs?: number;
    preflight?: boolean;
    pip?: boolean;
    /** With `pip`: whether S6 ends in picture-in-picture (true unless false). */
    pipEntered?: boolean;
    /** What the app knows of the charger at fake time `now` (Android); absent: it cannot read it. */
    charger?: (now: number) => ChargerState | undefined;
  } = {},
): Harness {
  const doc = new FakeDocument();
  const starts: Harness['starts'] = [];
  const events: QueueEvent[] = [];
  const log: string[] = [];
  const wakeLock = { held: 0, released: 0 };
  const queues = new Map<ScenarioId, Outcome[]>(Object.entries(options.outcomes ?? {}) as [ScenarioId, Outcome[]][]);
  let confirmResolve: () => void = () => undefined;
  const pipState = { inPip: false, waiters: [] as (() => void)[] };
  const h: Harness = {
    doc,
    starts,
    events,
    log,
    wakeLock,
    confirm: { resolve: () => confirmResolve() },
    pip: {
      end: () => {
        pipState.inPip = false;
        for (const waiter of pipState.waiters.splice(0)) waiter();
      },
      get inPip() {
        return pipState.inPip;
      },
    },
    env: {
      plan: plan(planId),
      complete: new Set(options.complete ?? []),
      rerunAll: options.rerunAll ?? false,
      doc,
      clock: {
        now: () => performance.now(),
        setTimer: (fn, ms) => {
          const id = setTimeout(fn, ms);
          return () => {
            clearTimeout(id);
          };
        },
      },
      runScenario: async (id) => {
        starts.push({ id, at: performance.now() });
        log.push(id);
        await new Promise((resolve) => setTimeout(resolve, options.runMs ?? 30_000));
        const next = queues.get(id)?.shift() ?? result(id);
        const outcome = typeof next === 'function' ? next(doc) : next;
        if (outcome instanceof Error) throw outcome;
        if (id === 'S6' && options.pip && options.pipEntered !== false) pipState.inPip = true;
        return outcome;
      },
      requestCamera: () => {
        log.push('camera');
        return Promise.resolve();
      },
      ...(options.charger === undefined
        ? {}
        : {
            charger: () => {
              log.push('charger?');
              return Promise.resolve(options.charger?.(performance.now()));
            },
          }),
      ...(options.preflight === undefined
        ? {}
        : {
            preflight: () => {
              log.push('preflight');
              return Promise.resolve(options.preflight === true);
            },
          }),
      holdWakeLock: () => {
        wakeLock.held += 1;
        log.push('wake lock');
        return Promise.resolve({
          kind: 'screen' as const,
          release: () => {
            wakeLock.released += 1;
            log.push('wake lock released');
            return Promise.resolve();
          },
        });
      },
      alert: () => {
        log.push('alert');
      },
      confirm: () => {
        log.push('confirm?');
        return new Promise<void>((resolve) => {
          confirmResolve = () => {
            log.push('confirmed');
            resolve();
          };
        });
      },
      ...(options.pip
        ? {
            pip: {
              active: () => pipState.inPip,
              ended: () => {
                log.push('pip?');
                return pipState.inPip ? new Promise<void>((resolve) => pipState.waiters.push(resolve)) : Promise.resolve();
              },
            },
          }
        : {}),
      show: (event) => {
        events.push(event);
      },
    },
  };
  return h;
}

const ran = (h: Harness): ScenarioId[] => h.starts.map((start) => start.id);

describe('the automatic run', () => {
  it('queue_runsTheDevicesListInOrder', async () => {
    const h = harness('edge');

    const outcome = await finish(runQueue(h.env));

    expect(ran(h)).toEqual(['S10', 'S1', 'S2', 'S3-9', 'S9']);
    expect(outcome).toEqual({ kind: 'done', ran: ['S10', 'S1', 'S2', 'S3-9', 'S9'], skipped: [], failed: [] });
    const scenarioEvents = h.events.filter((event) => event.kind === 'scenario');
    expect(scenarioEvents.map((event) => [event.id, event.index, event.total])).toEqual([
      ['S10', 1, 5],
      ['S1', 2, 5],
      ['S2', 3, 5],
      ['S3-9', 4, 5],
      ['S9', 5, 5],
    ]);
    expect(h.events.filter((event) => event.kind === 'result')).toHaveLength(5);
  });

  it('skip_completeScenarios_areSkipped', async () => {
    // Chrome ran everything by hand on 2026-11-17 but S7 and S9.
    const h = harness('chrome', { complete: ['S10', 'S1', 'S2', 'S3-9', 'S3-16', 'S4'] });

    const outcome = await finish(runQueue(h.env));

    expect(ran(h)).toEqual(['S7', 'S9']);
    expect(outcome).toEqual({ kind: 'done', ran: ['S7', 'S9'], skipped: ['S10', 'S1', 'S2', 'S3-9', 'S3-16', 'S4'], failed: [] });
  });

  it('skip_rerunAll_runsEverything', async () => {
    const h = harness('edge', { complete: ['S10', 'S1', 'S2', 'S3-9', 'S9'], rerunAll: true });

    await finish(runQueue(h.env));

    expect(ran(h)).toEqual(['S10', 'S1', 'S2', 'S3-9', 'S9']);
  });

  it('skip_scenarioOutsideTheTable_isNeitherRunNorSkipped', async () => {
    // AGE (the recorder spike's MI4) is run from the list only: a complete AGE
    // changes nothing, and no run of the table starts it, «Rulează din nou tot» included.
    const h = harness('edge', { complete: ['AGE', 'S10'] });

    expect(await finish(runQueue(h.env))).toEqual({ kind: 'done', ran: ['S1', 'S2', 'S3-9', 'S9'], skipped: ['S10'], failed: [] });
    expect(ran(h)).toEqual(['S1', 'S2', 'S3-9', 'S9']);

    const again = harness('chrome', { complete: ['AGE'], rerunAll: true });
    const outcome = await finish(runQueue(again.env));

    expect(ran(again)).toEqual(['S10', 'S1', 'S2', 'S3-9', 'S3-16', 'S4', 'S7', 'S9']);
    expect(outcome).toMatchObject({ kind: 'done', skipped: [] });
    expect(again.events.flatMap((event) => (event.kind === 'scenario' ? [event.total] : []))).toEqual(Array<number>(8).fill(8));
  });

  it('skip_OAUTH_isNeitherRunNorSkipped', async () => {
    // OAUTH (the upload spike's MI6 login) is started from the list too, never by the device's run.
    const desktop = harness('ipcam-desktop', { complete: ['OAUTH', 'S10'] });

    const outcome = await finish(runQueue(desktop.env));

    expect(ran(desktop)).toEqual(['S1', 'S2', 'S3-16', 'S4', 'S7', 'S9']);
    expect(outcome).toEqual({ kind: 'done', ran: ['S1', 'S2', 'S3-16', 'S4', 'S7', 'S9'], skipped: ['S10'], failed: [] });
    expect(desktop.events.flatMap((event) => (event.kind === 'scenario' ? [event.total] : []))).toEqual(Array<number>(6).fill(6));
  });

  it('skip_nothingLeft_startsNothing', async () => {
    const h = harness('edge', { complete: ['S10', 'S1', 'S2', 'S3-9', 'S9'], preflight: true });

    const outcome = await finish(runQueue(h.env));

    expect(outcome).toEqual({ kind: 'done', ran: [], skipped: ['S10', 'S1', 'S2', 'S3-9', 'S9'], failed: [] });
    expect(h.log).toEqual([]); // no preflight, no wake lock, no camera
  });

  it('wakeLock_heldForTheWholeRun', async () => {
    const h = harness('edge');

    await finish(runQueue(h.env));

    expect(h.wakeLock).toEqual({ held: 1, released: 1 });
    expect(h.log[0]).toBe('wake lock');
    expect(h.log.at(-1)).toBe('wake lock released');
  });

  it('queue_hubUnreachable_stopsAndReleasesTheWakeLock', async () => {
    // A ConnectError (or NotStarted: S3 on the charger) ends the run; the page says why.
    const h = harness('edge', { outcomes: { S1: [new ConnectError('refused', 'http://192.168.1.20:7080', 'Failed to fetch')] } });

    const failure = await finish(runQueue(h.env).catch((error: unknown) => error));

    expect(failure).toBeInstanceOf(ConnectError);
    expect(ran(h)).toEqual(['S10', 'S1']);
    expect(h.wakeLock).toEqual({ held: 1, released: 1 });
  });
});

describe('the questions come first', () => {
  it('camera_askedOnceAfterS10_beforeTheUnattendedPart', async () => {
    // S10 measures the connection before any camera access; the camera prompt follows at once.
    const h = harness('edge');

    await finish(runQueue(h.env));

    expect(h.log).toEqual(['wake lock', 'S10', 'camera', 'S1', 'S2', 'S3-9', 'S9', 'wake lock released']);
  });

  it('camera_s10Done_askedAtTheStart', async () => {
    const h = harness('firefox', { complete: ['S10'] });

    await finish(runQueue(h.env));

    expect(h.log.slice(0, 3)).toEqual(['wake lock', 'camera', 'S1']);
  });

  it('camera_noS3Left_notAsked', async () => {
    const h = harness('chrome', { complete: ['S3-9', 'S3-16'] });

    await finish(runQueue(h.env));

    expect(h.log).not.toContain('camera');
  });

  it('askOnce_asksUntilAnsweredThenKeepsTheAnswer', async () => {
    // S10's question is asked once: a restarted S10 does not ask again, an unanswered one does.
    const answers: ((value: boolean) => void)[] = [];
    const ask = vi.fn<(question: string) => Promise<boolean>>(() => new Promise<boolean>((resolve) => answers.push(resolve)));
    const once = askOnce(ask);

    void once('q'); // left unanswered: the run was interrupted
    const second = once('q');
    answers[1]?.(true);

    expect(await second).toBe(true);
    expect(await once('q')).toBe(true);
    expect(ask).toHaveBeenCalledTimes(2);
  });
});

describe('interruptions resume', () => {
  it('resume_hiddenPage_pausesTheQueueAndRestartsTheScenarioWhenVisible', async () => {
    const h = harness('edge', {
      outcomes: {
        S2: [
          (doc) => {
            doc.hide(); // the window was minimised: runDefinition ended the run as interrupted
            return interrupted('S2');
          },
        ],
      },
    });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(3 * 30_000 + 60_000);
    // Paused: nothing runs while the page is hidden, however long.
    await vi.advanceTimersByTimeAsync(3 * 3_600_000);
    expect(ran(h)).toEqual(['S10', 'S1', 'S2']);
    expect(h.events.at(-1)).toEqual({ kind: 'interrupted', id: 'S2', count: 1, reason: { kind: 'hidden', atS: 3 } });

    const shownAt = performance.now();
    h.doc.show();
    const outcome = await finish(running);

    expect(ran(h)).toEqual(['S10', 'S1', 'S2', 'S2', 'S3-9', 'S9']);
    expect(h.starts[3]?.at).toBeGreaterThanOrEqual(shownAt);
    expect(h.events).toContainEqual({ kind: 'resumed', id: 'S2', count: 1, reason: { kind: 'hidden', atS: 3 } });
    expect(outcome).toMatchObject({ kind: 'done', failed: [] });
  });

  it('resume_threeInterruptions_stopTheRun', async () => {
    const h = harness('edge', { outcomes: { S1: [interrupted('S1'), interrupted('S1'), interrupted('S1'), result('S1')] } });

    const outcome = await finish(runQueue(h.env));

    expect(outcome).toEqual<QueueOutcome>({ kind: 'interrupted-too-often', id: 'S1', reason: { kind: 'hidden', atS: 3 } });
    expect(ran(h)).toEqual(['S10', 'S1', 'S1', 'S1']);
    expect(h.wakeLock.released).toBe(1);
  });

  it('resume_eventsSayWhyTheScenarioWasInterrupted', async () => {
    // A Wi-Fi drop while the window is visible: the banner must name the hub
    // connection, not a hidden window, so the events carry the reason.
    const lost: InterruptReason = { kind: 'session-lost', atS: 9, how: 'connection failed' };
    const h = harness('edge', { outcomes: { S9: [interrupted('S9', lost)] } });

    await finish(runQueue(h.env));

    expect(h.events).toContainEqual({ kind: 'interrupted', id: 'S9', count: 1, reason: lost });
    expect(h.events).toContainEqual({ kind: 'resumed', id: 'S9', count: 1, reason: lost });
  });

  it('resume_threeInterruptions_theOutcomeSaysWhyTheLastOneHappened', async () => {
    const early: InterruptReason = { kind: 'early-return', backgroundS: 40 };
    const h = harness('phone', {
      complete: ['S10', 'S1', 'S2', 'S3-9', 'S3-4', 'S4'],
      outcomes: { S5: [interrupted('S5'), interrupted('S5', { kind: 'session-lost', atS: 5, how: 'connection closed' }), interrupted('S5', early)] },
    });

    const outcome = await finish(runQueue(h.env));

    expect(outcome).toEqual<QueueOutcome>({ kind: 'interrupted-too-often', id: 'S5', reason: early });
  });

  it('resume_resultWithoutAReason_readsAsHidden', async () => {
    // results.ts: a result without `interruption` reads as hidden.
    const h = harness('edge', { outcomes: { S1: [result('S1', { interrupted: true, durationS: 12 })] } });

    await finish(runQueue(h.env));

    expect(h.events).toContainEqual({ kind: 'resumed', id: 'S1', count: 1, reason: { kind: 'hidden', atS: 12 } });
  });

  it('resume_otherInterruptions_restartToo', async () => {
    // The hub session was lost (R49): the scenario runs again, within the same limit.
    const h = harness('edge', { outcomes: { S9: [interrupted('S9', { kind: 'session-lost', atS: 9, how: 'connection failed' })] } });

    await finish(runQueue(h.env));

    expect(ran(h)).toEqual(['S10', 'S1', 'S2', 'S3-9', 'S9', 'S9']);
  });

  it('resume_hiddenBeforeAScenario_waitsForTheWindow', async () => {
    const h = harness('edge', { complete: ['S10'] });
    h.doc.hide();

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(3_600_000);
    expect(ran(h)).toEqual([]);
    h.doc.show();
    await finish(running);

    expect(ran(h)).toEqual(['S1', 'S2', 'S3-9', 'S9']);
  });

  it('failed_repeatedOnceThenTheRunMovesOn', async () => {
    // The guide's rule for «eșuat: …»: repeat once, then go on to the next scenario.
    const failed = result('S2', { notes: ['failed: Error: no track for this slot'] });
    const h = harness('edge', { outcomes: { S2: [failed, failed] } });

    const outcome = await finish(runQueue(h.env));

    expect(ran(h)).toEqual(['S10', 'S1', 'S2', 'S2', 'S3-9', 'S9']);
    expect(h.events).toContainEqual({ kind: 'retry', id: 'S2' });
    expect(outcome).toEqual({ kind: 'done', ran: ['S10', 'S1', 'S2', 'S3-9', 'S9'], skipped: [], failed: ['S2'] });
  });
});

describe('the phone', () => {
  it('pause_tenMinutesAfterS39_countedDownOnScreen', async () => {
    const h = harness('phone', { pip: true });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(4 * 30_000 + 5000);
    const pauses = h.events.filter((event) => event.kind === 'pause');
    expect(pauses[0]).toEqual({ kind: 'pause', leftMs: 600_000 });
    expect(pauses.at(-1)).toEqual({ kind: 'pause', leftMs: 595_000 });
    expect(ran(h).at(-1)).toBe('S3-9');
    await vi.advanceTimersByTimeAsync(600_000);
    expect(ran(h).at(-1)).toBe('S3-4');
    const s39 = h.starts.find((start) => start.id === 'S3-9')?.at ?? 0;
    const s34 = h.starts.find((start) => start.id === 'S3-4')?.at ?? 0;
    expect(s34 - (s39 + 30_000)).toBeGreaterThanOrEqual(600_000);

    // S5 and S6 wait for the person; let them through.
    await vi.advanceTimersByTimeAsync(10 * 30_000);
    h.confirm.resolve();
    await vi.advanceTimersByTimeAsync(31_000);
    h.pip.end();
    await finish(running);
    expect(h.events.filter((event) => event.kind === 'pause').at(-1)).toEqual({ kind: 'pause', leftMs: 0 });
  });

  it('pause_s39DoneBefore_noPause', async () => {
    const h = harness('phone', { complete: ['S10', 'S1', 'S2', 'S3-9', 'S5', 'S6'] });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(29_000);

    expect(ran(h)).toEqual(['S3-4']);
    expect(h.events.some((event) => event.kind === 'pause')).toBe(false);
    await finish(running);
  });

  it('attention_s5_alertsShowsItsInstructionAndWaitsWithoutTimeout', async () => {
    // S5 needs the person: a sound and a vibration, again every minute, until the app goes to the background.
    const h = harness('phone', { complete: ['S10', 'S1', 'S2', 'S3-9', 'S3-4', 'S4', 'S6', 'S7', 'S8', 'S9'], runMs: 3 * 3_600_000 });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(1000);
    expect(h.log).toEqual(['wake lock', 'alert', 'S5']);
    expect(h.events).toContainEqual({ kind: 'attention', id: 'S5' });
    await vi.advanceTimersByTimeAsync(5 * 60_000);
    expect(h.log.filter((entry) => entry === 'alert')).toHaveLength(6);
    h.doc.hide(); // Home was pressed
    await vi.advanceTimersByTimeAsync(10 * 60_000);
    expect(h.log.filter((entry) => entry === 'alert')).toHaveLength(6);
    h.doc.show();
    await finish(running);
  });

  it('attention_s6_waitsForContinueThenForTheReturnFromPip', async () => {
    const h = harness('phone', { complete: ['S10', 'S1', 'S2', 'S3-9', 'S3-4', 'S4', 'S5', 'S8', 'S9'], pip: true });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(3 * 3_600_000); // nobody there: it waits, without a timeout
    expect(ran(h)).toEqual([]);
    expect(h.events).toContainEqual({ kind: 'confirm', id: 'S6' });
    h.confirm.resolve();
    await vi.advanceTimersByTimeAsync(31_000);
    expect(ran(h)).toEqual(['S6']);
    // S6 ended in picture-in-picture: S7 waits until the person brings the app back.
    await vi.advanceTimersByTimeAsync(3_600_000);
    expect(ran(h)).toEqual(['S6']);
    expect(h.events).toContainEqual({ kind: 'pip' });
    const alerts = h.log.filter((entry) => entry === 'alert').length;
    expect(alerts).toBeGreaterThan(2);
    h.pip.end();
    await finish(running);

    expect(ran(h)).toEqual(['S6', 'S7']);
    expect(h.log.indexOf('confirmed')).toBeLessThan(h.log.indexOf('S6'));
  });
});

describe('the phone after S6', () => {
  it('attention_s6_notInPip_doesNotCallThePersonAgain', async () => {
    // S6 ended with the app already out of picture-in-picture: there is
    // nothing to wait for, so no sound, no vibration and no «S6 s-a terminat».
    const h = harness('phone', {
      complete: ['S10', 'S1', 'S2', 'S3-9', 'S3-4', 'S4', 'S5', 'S8', 'S9'],
      pip: true,
      pipEntered: false,
    });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(1000);
    h.confirm.resolve();
    await finish(running);

    expect(ran(h)).toEqual(['S6', 'S7']);
    expect(h.events).not.toContainEqual({ kind: 'pip' });
    expect(h.log.slice(h.log.indexOf('S6'))).not.toContain('alert');
  });
});

describe('the phone off the charger (S3 starts cold)', () => {
  const offSince = (since: number) => (now: number) => ({ kind: 'off' as const, forMs: now - since });
  const cold = (h: Harness) => h.events.filter((event) => event.kind === 'cold');
  // Everything of the phone but S3-9, so that S3-9 is the first scenario that runs.
  const allButS39: ScenarioId[] = ['S10', 'S1', 'S2', 'S3-4', 'S4', 'S5', 'S6', 'S7', 'S8', 'S9'];

  it('cold_s3SoonAfterTheCharger_waitsOnScreenUntilTenMinutesOff', async () => {
    // The phone came off the charger when the run started: S3-9 waits until it
    // has been off for 10 minutes, counted down on the screen, so that MV5
    // (9 tiles, thermal) starts from a cold phone.
    const t0 = performance.now();
    const h = harness('phone', { complete: allButS39, charger: offSince(t0) });

    const running = runQueue(h.env);
    await vi.advanceTimersByTimeAsync(5000);
    expect(ran(h)).toEqual([]);
    expect(cold(h)[0]).toEqual({ kind: 'cold', id: 'S3-9', leftMs: 600_000 });
    expect(cold(h).at(-1)).toEqual({ kind: 'cold', id: 'S3-9', leftMs: 595_000 });
    await finish(running);

    expect(ran(h)).toEqual(['S3-9']);
    expect((h.starts[0]?.at ?? 0) - t0).toBeGreaterThanOrEqual(600_000);
    expect(cold(h).at(-1)).toEqual({ kind: 'cold', id: 'S3-9', leftMs: 0 });
    expect(h.log.slice(0, 3)).toEqual(['wake lock', 'camera', 'charger?']);
  });

  it('cold_offTheChargerTenMinutesAlready_startsAtOnce', async () => {
    const t0 = performance.now();
    const h = harness('phone', { complete: allButS39, charger: offSince(t0 - 11 * 60_000) });

    await finish(runQueue(h.env));

    expect((h.starts[0]?.at ?? 0) - t0).toBeLessThan(1000);
    expect(cold(h)).toEqual([]);
  });

  it('cold_pluggedInDuringTheWait_waitsAgain', async () => {
    // 2 min off at the start; plugged in and out again 6 min 40 s later: the
    // 10 minutes count again from then.
    const t0 = performance.now();
    const h = harness('phone', {
      complete: allButS39,
      charger: (now) => (now < t0 + 400_000 ? offSince(t0 - 120_000)(now) : offSince(t0 + 400_000)(now)),
    });

    await finish(runQueue(h.env));

    expect((h.starts[0]?.at ?? 0) - t0).toBeGreaterThanOrEqual(1_000_000);
  });

  it('cold_onTheChargerOrUnknown_noWait', async () => {
    // On the charger, S3 refuses by itself and says so («Scoateți telefonul din
    // încărcător înainte de S3.»); without a reading there is nothing to count.
    for (const state of [{ kind: 'on' as const }, undefined]) {
      const t0 = performance.now();
      const h = harness('phone', { complete: allButS39, charger: () => state });

      await finish(runQueue(h.env));

      expect((h.starts[0]?.at ?? 0) - t0).toBeLessThan(1000);
      expect(cold(h)).toEqual([]);
    }
  });

  it('cold_s34First_waitsToo', async () => {
    // S3-9 was done in an earlier session: S3-4 is the first S3, so it waits.
    const t0 = performance.now();
    const h = harness('phone', {
      complete: ['S10', 'S1', 'S2', 'S3-9', 'S4', 'S5', 'S6', 'S7', 'S8', 'S9'],
      charger: offSince(t0),
    });

    await finish(runQueue(h.env));

    expect(ran(h)).toEqual(['S3-4']);
    expect((h.starts[0]?.at ?? 0) - t0).toBeGreaterThanOrEqual(600_000);
  });

  it('cold_pcPlans_haveNoChargerAndNeverWait', async () => {
    const h = harness('edge');

    await finish(runQueue(h.env));

    expect(cold(h)).toEqual([]);
    expect(h.log).not.toContain('charger?');
  });
});

describe('Firefox', () => {
  it('preflight_noH264_doesNotStart', async () => {
    const h = harness('firefox', { preflight: false });

    const outcome = await finish(runQueue(h.env));

    expect(outcome).toEqual({ kind: 'no-h264' });
    expect(h.log).toEqual(['preflight']);
  });

  it('preflight_ok_runs', async () => {
    const h = harness('firefox', { preflight: true });

    await finish(runQueue(h.env));

    expect(h.log.slice(0, 3)).toEqual(['preflight', 'wake lock', 'S10']);
  });
});
