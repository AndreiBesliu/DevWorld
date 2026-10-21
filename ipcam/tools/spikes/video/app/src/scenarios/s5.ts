// S5, MV9: back from the background, the way the product's SUSPENDED state
// does it (spec §6.3 state table, R44). With the substream of sim-01 playing,
// the screen asks the person to press Home and to come back after a time on
// the clock, at least 2 minutes later. On hide the app releases the slot at
// once (unbind) and, if it is still hidden after 60 s, closes the session. On
// return it keeps the session when it is alive, or opens a new one when it is
// closed or dead (its rpc channel closed, its PeerConnection failed or closed,
// or disconnected for over 1 s), binds the slot again and times MV9 from the
// page turning visible to the first live frame that playback goes on from
// (liveResume): a reconnect is part of it. A return before 2 min ends the run
// as interrupted, after that measurement. Going to the background is the
// point, so a hidden page does not interrupt S5, and the run does not watch the
// session while the app is away (R49).
//
// MV9 needs requestVideoFrameCallback: liveResume follows the presented
// frames, and the only fallback, the player's first-frame, does not say which
// frame is live. Every MV9 target has it (R31); without it S5 notes
// `resume: n/a (no requestVideoFrameCallback)` and records nothing, rather
// than a false "no frame".
//
// Samples: `resumeMs` (visible to that frame), `backgroundS` (time hidden).
// Notes: `session: kept` or `session: reopened`; `connection at return: <state>`
// (the session's connectionState when the page turned visible, `closed` once
// the app closed it); `the app closed the session after 60 s in the background
// (connection <state> then)`, or `the app closed the session at the return,
// after <s> s in the background (connection <state> then)` when no timer ran
// in the background (a frozen app); `background shorter than 2 min`,
// `no frame within 30 s of the return` (no live frame), `no steady live frames
// within 30 s of the return` (live frames, but never 3 more within 1 s),
// `resume: <player error>`, `resume: bind error <code>`,
// `resume: n/a (no requestVideoFrameCallback)`,
// `the app did not go to the background within 10 min`,
// `the app did not come back within 10 min`, `start: ...`.
import type { Player } from '@ipcam/player';
import type { BandCode } from '../band';
import type { SpikeSession } from '../session';
import type { VisibilitySource } from '../wakelock';
import { bindFailure, describeFailure, liveSource, onPresentedFrames, startStream, watchStart, type FirstFrame } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const WAIT_FOR_PERSON_MS = 10 * 60_000;
const BACKGROUND_MS = 2 * 60_000;
/** The product closes a session after this long in SUSPENDED (spec §6.3 state table). */
const SUSPENDED_CLOSE_MS = 60_000;
/** A session still disconnected this long after it became so is dead at the return. */
const DEAD_AFTER_DISCONNECTED_MS = 1000;
/** Added to the 2 minutes before the time on screen is rounded up: the person reads it, then presses Home. */
const RETURN_MARGIN_MS = 30_000;
const GUIDE_EVERY_MS = 1000;
const RESUME_TIMEOUT_MS = 30_000;
const FIRST_FRAME_TIMEOUT_MS = 10_000;
/** A resumed frame counts once this many more frames follow it within STEADY_MS. */
const STEADY_FRAMES = 3;
const STEADY_MS = 1000;

/**
 * The time on the device's clock, `HH:MM`, after which a return is at least
 * 2 minutes after `nowMs`: now plus 2 min and a margin, rounded up to the
 * minute, so that a clock that shows only minutes is enough (integration-4).
 */
export function earliestReturn(nowMs: number): string {
  const at = new Date(Math.ceil((nowMs + BACKGROUND_MS + RETURN_MARGIN_MS) / 60_000) * 60_000);
  return `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
}

/**
 * When live playback resumed after the return at `shownAt`: the first frame
 * that watchStart accepts as live (it arrived after the return, or shows
 * another band than `bandAtHide`) and that STEADY_FRAMES more frames follow
 * within STEADY_MS. A frame from before the background shown again, or a live
 * frame followed by a freeze, does not count. `unsteady`: live frames came,
 * but never steadily, within RESUME_TIMEOUT_MS.
 */
async function liveResume(
  run: Run,
  player: Player,
  video: HTMLVideoElement,
  shownAt: number,
  bandAtHide: BandCode | null,
): Promise<FirstFrame | 'unsteady'> {
  const first = await watchStart(run, player, video, shownAt, bandAtHide).wait(RESUME_TIMEOUT_MS);
  if (first === 'timeout' || 'failure' in first) return first;
  // Frames come in order, so every frame after a live one is live too.
  const live = [shownAt + first.ms];
  let resumedAt: (displayPerfMs: number) => void = () => undefined;
  const steady = new Promise<number>((resolve) => {
    resumedAt = resolve;
  });
  const stop = onPresentedFrames(video, (displayPerfMs) => {
    live.push(displayPerfMs);
    // The earliest frame that STEADY_FRAMES more follow within STEADY_MS: an
    // earlier one was ruled out when its own STEADY_FRAMES-th follower came.
    const from = live[live.length - 1 - STEADY_FRAMES];
    if (from !== undefined && displayPerfMs - from <= STEADY_MS) resumedAt(from);
  });
  const outcome = await run.within(steady, Math.max(0, shownAt + RESUME_TIMEOUT_MS - run.now())).finally(stop);
  return outcome === 'timeout' ? 'unsteady' : { ms: outcome - shownAt };
}

/** Resolves once the document is in `state`. */
function untilVisibility(run: Run, doc: VisibilitySource, state: DocumentVisibilityState, ms: number): Promise<void | 'timeout'> {
  let listener: () => void = () => undefined;
  const reached = new Promise<void>((resolve) => {
    listener = () => {
      if (doc.visibilityState === state) resolve();
    };
    doc.addEventListener('visibilitychange', listener);
    listener();
  });
  return run.within(reached, ms).finally(() => {
    doc.removeEventListener('visibilitychange', listener);
  });
}

/** When `session` became disconnected, followed from now until stop(). */
function followDisconnected(run: Run, session: SpikeSession): { since(): number | undefined; stop(): void } {
  let since = session.connectionState() === 'disconnected' ? run.now() : undefined;
  const stop = session.onConnectionState((state) => {
    if (state === 'disconnected') since ??= run.now();
    else since = undefined;
  });
  return { since: () => since, stop };
}

/**
 * Whether `session` can carry the stream again at the return: not closed, not
 * failed, and not disconnected for over DEAD_AFTER_DISCONNECTED_MS. A session
 * that became disconnected less than that ago gets the rest of the time to
 * recover.
 */
async function aliveAtReturn(run: Run, session: SpikeSession, disconnectedSince: number | undefined): Promise<boolean> {
  if (session.connectionState() === 'disconnected') {
    const left = (disconnectedSince ?? run.now()) + DEAD_AFTER_DISCONNECTED_MS - run.now();
    if (left > 0) {
      let stop: () => void = () => undefined;
      const recovered = new Promise<void>((resolve) => {
        stop = session.onConnectionState((state) => {
          if (state !== 'disconnected') resolve();
        });
      });
      await run.within(recovered, left).finally(stop);
    }
  }
  const state = session.connectionState();
  return state !== 'closed' && state !== 'failed' && state !== 'disconnected';
}

export const S5: ScenarioDefinition = {
  backgroundsOnPurpose: true,
  async run(run) {
    const { stage, t, doc } = run.env;
    const [cam] = run.sims();
    const [video] = stage.grid(1);
    if (!cam || !video) return;
    if (typeof video.requestVideoFrameCallback !== 'function') {
      run.note('resume: n/a (no requestVideoFrameCallback)');
      return;
    }
    let session = await run.session(1);
    let player = run.player(video, session, 0);
    const started = await startStream(run, { session, player, video, slot: 0, cam: cam.id, profile: 'sub', timeoutMs: FIRST_FRAME_TIMEOUT_MS });
    if ('bindError' in started || started.frame === 'timeout' || 'failure' in started.frame) {
      run.note(`start: ${'bindError' in started ? `bind error ${started.bindError}` : 'no first frame'}`);
      return;
    }

    // The time to come back moves on with the clock until the app goes to the background.
    const guide = (): void => {
      stage.say(t('s5Guide', { time: earliestReturn(Date.now()) }));
    };
    guide();
    const stopGuide = run.every(GUIDE_EVERY_MS, guide);
    const hidden = await untilVisibility(run, doc, 'hidden', WAIT_FOR_PERSON_MS);
    stopGuide();
    if (hidden === 'timeout') {
      run.note('the app did not go to the background within 10 min');
      return;
    }

    // SUSPENDED: the slot is released at once, and the session closed after 60 s.
    const hiddenAt = run.now();
    const bandAtHide = run.env.readBand(video);
    const watchAgain = run.unwatch(session);
    const disconnected = followDisconnected(run, session);
    player.stop();
    session.unbind(0);
    let closedByApp: string | undefined;
    const suspended = session;
    const close = (when: string): void => {
      closedByApp = `the app closed the session ${when} (connection ${suspended.connectionState()} then)`;
      suspended.close();
    };
    const cancelClose = run.env.clock.setTimer(() => {
      close(`after ${SUSPENDED_CLOSE_MS / 1000} s in the background`);
    }, SUSPENDED_CLOSE_MS);
    const shown = await untilVisibility(run, doc, 'visible', WAIT_FOR_PERSON_MS).finally(cancelClose);
    if (shown === 'timeout') {
      disconnected.stop();
      run.note('the app did not come back within 10 min');
      return;
    }
    const shownAt = run.now();
    stage.say('');
    const backgroundMs = shownAt - hiddenAt;
    const backgroundS = Math.round(backgroundMs / 1000);
    run.sample('backgroundS', backgroundS);
    // A frozen app ran no timer in the background; the product's 60 s apply all the same.
    if (closedByApp === undefined && backgroundMs >= SUSPENDED_CLOSE_MS) close(`at the return, after ${backgroundS} s in the background`);
    run.note(`connection at return: ${session.connectionState()}`);
    if (closedByApp !== undefined) run.note(closedByApp);

    const kept = closedByApp === undefined && (await aliveAtReturn(run, session, disconnected.since()));
    disconnected.stop();
    if (kept) {
      watchAgain();
    } else {
      session.close();
      session = await run.session(1);
      player = run.player(video, session, 0);
    }
    run.note(`session: ${kept ? 'kept' : 'reopened'}`);

    // Bound again from the return on, as the app would; the first live frame from then on is the resume.
    const resumed = liveResume(run, player, video, shownAt, bandAtHide);
    resumed.catch(() => undefined); // left unread when the bind is refused
    const bind = session.bind(0, cam.id, 'sub').then(
      () => undefined,
      (error: unknown) => bindFailure(error),
    );
    await player.play(liveSource(cam.id, 'sub'));
    const refused = await bind;
    if (refused !== undefined) {
      run.note(`resume: bind error ${refused}`);
    } else {
      const resume = await resumed;
      if (resume === 'timeout') run.note('no frame within 30 s of the return');
      else if (resume === 'unsteady') run.note('no steady live frames within 30 s of the return');
      else if ('failure' in resume) run.note(`resume: ${describeFailure(resume.failure)}`);
      else run.sample('resumeMs', resume.ms);
    }
    if (backgroundMs < BACKGROUND_MS) {
      run.note('background shorter than 2 min');
      run.interrupt({ kind: 'early-return', backgroundS });
    }
  },
};
