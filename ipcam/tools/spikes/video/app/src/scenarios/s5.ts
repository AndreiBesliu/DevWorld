// S5, MV9: back from the background. With the substream of sim-01 playing, the
// screen asks the person to press Home, wait 2 minutes and come back; the time
// runs from the page turning visible to the first live frame that playback
// goes on from (liveResume). Going to the background is the point, so a hidden
// page does not interrupt S5.
//
// MV9 needs requestVideoFrameCallback: liveResume follows the presented
// frames, and the only fallback, the player's first-frame, comes once per play,
// not at a return to a play that goes on. Every MV9 target has it (R31);
// without it S5 notes `resume: n/a (no requestVideoFrameCallback)` and records
// nothing, rather than a false "no frame".
//
// Samples: `resumeMs` (visible to that frame), `backgroundS` (time hidden).
// Notes: `background shorter than 2 min`, `no frame within 30 s of the return`
// (no live frame), `no steady live frames within 30 s of the return` (live
// frames, but never 3 more within 1 s), `resume: <player error>`,
// `resume: n/a (no requestVideoFrameCallback)`,
// `the app did not go to the background within 10 min`, `start: ...`.
import type { Player } from '@ipcam/player';
import type { BandCode } from '../band';
import type { VisibilitySource } from '../wakelock';
import { describeFailure, onPresentedFrames, startStream, watchStart, type FirstFrame } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const WAIT_FOR_PERSON_MS = 10 * 60_000;
const BACKGROUND_MS = 2 * 60_000;
const RESUME_TIMEOUT_MS = 30_000;
const FIRST_FRAME_TIMEOUT_MS = 10_000;
/** A resumed frame counts once this many more frames follow it within STEADY_MS. */
const STEADY_FRAMES = 3;
const STEADY_MS = 1000;

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
    const session = await run.session(1);
    const player = run.player(video, session, 0);
    const started = await startStream(run, { session, player, video, slot: 0, cam: cam.id, profile: 'sub', timeoutMs: FIRST_FRAME_TIMEOUT_MS });
    if ('bindError' in started || started.frame === 'timeout' || 'failure' in started.frame) {
      run.note(`start: ${'bindError' in started ? `bind error ${started.bindError}` : 'no first frame'}`);
      return;
    }

    stage.say(t('s5Guide'));
    if ((await untilVisibility(run, doc, 'hidden', WAIT_FOR_PERSON_MS)) === 'timeout') {
      run.note('the app did not go to the background within 10 min');
      return;
    }
    const hiddenAt = run.now();
    const bandAtHide = run.env.readBand(video);
    if ((await untilVisibility(run, doc, 'visible', WAIT_FOR_PERSON_MS)) === 'timeout') {
      run.note('the app did not come back within 10 min');
      return;
    }
    const shownAt = run.now();
    stage.say('');
    const resumed = liveResume(run, player, video, shownAt, bandAtHide);
    // The WebView may have paused the element in the background; the app resumes it, as it would.
    if (video.paused) video.play().catch(() => undefined);
    run.sample('backgroundS', Math.round((shownAt - hiddenAt) / 1000));
    if (shownAt - hiddenAt < BACKGROUND_MS) run.note('background shorter than 2 min');
    const resume = await resumed;
    if (resume === 'timeout') run.note('no frame within 30 s of the return');
    else if (resume === 'unsteady') run.note('no steady live frames within 30 s of the return');
    else if ('failure' in resume) run.note(`resume: ${describeFailure(resume.failure)}`);
    else run.sample('resumeMs', resume.ms);
  },
};
