// S5, MV9: back from the background. With the substream of sim-01 playing, the
// screen asks the person to press Home, wait 2 minutes and come back; the time
// runs from the page turning visible to the next presented frame. Going to the
// background is the point, so a hidden page does not interrupt S5.
//
// Samples: `resumeMs` (visible to the next frame), `backgroundS` (time hidden).
// Notes: `background shorter than 2 min`, `no frame within 30 s of the return`,
// `the app did not go to the background within 10 min`, `start: ...`.
import type { VisibilitySource } from '../wakelock';
import { nextFrame, startStream } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const WAIT_FOR_PERSON_MS = 10 * 60_000;
const BACKGROUND_MS = 2 * 60_000;
const RESUME_TIMEOUT_MS = 30_000;
const FIRST_FRAME_TIMEOUT_MS = 10_000;

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
    const session = await run.session(1);
    const [video] = stage.grid(1);
    if (!cam || !video) return;
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
    if ((await untilVisibility(run, doc, 'visible', WAIT_FOR_PERSON_MS)) === 'timeout') {
      run.note('the app did not come back within 10 min');
      return;
    }
    const shownAt = run.now();
    stage.say('');
    const next = nextFrame(video);
    // The WebView may have paused the element in the background; the app resumes it, as it would.
    if (video.paused) video.play().catch(() => undefined);
    run.sample('backgroundS', Math.round((shownAt - hiddenAt) / 1000));
    if (shownAt - hiddenAt < BACKGROUND_MS) run.note('background shorter than 2 min');
    const frameAt = await run.within(next.frame, RESUME_TIMEOUT_MS).finally(next.stop);
    if (frameAt === 'timeout') run.note('no frame within 30 s of the return');
    else run.sample('resumeMs', frameAt - shownAt);
  },
};
