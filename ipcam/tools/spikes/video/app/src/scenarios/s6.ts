// S6, MV8 (Android): picture-in-picture. The substream of sim-01 plays in the
// stage's fullscreen element, which is fixed over the whole viewport, so that
// the PiP window, a small viewport of the same page, shows the video (critic-2).
// The shell puts the Activity into PiP, and the presented frames are counted
// for 5 min with requestVideoFrameCallback (R8: where the element has it).
// Every second S6 also reads whether the element is inside the viewport (an
// IntersectionObserver), so the report can tell "visible and advancing" from
// "advancing". MV8 passes when no gap between frames exceeds 2 s; the report
// decides that from `pipMaxGapMs`. PiP that ends before 5 min ends the run as
// interrupted (integration-4). Without the shell's PiP, S6 is n/a. PiP is the
// point, so a hidden page does not interrupt S6.
//
// Samples: `pipFrames`, `pipMaxGapMs` (from entering PiP to its end, the edges
// included), `pipS` (how long it was measured).
// Notes: `n/a: ...`, `left picture-in-picture after <s> s`, `start: ...`,
// `pipVisible: <x>/<y> s` (seconds of the measurement in which the video
// intersected the viewport, of those sampled), `pipVisible: n/a`.
import { onPresentedFrames, startStream } from './frames';
import type { ScenarioDefinition } from './runner';

const PIP_MS = 5 * 60_000;
const CHECK_EVERY_MS = 1000;
const FIRST_FRAME_TIMEOUT_MS = 10_000;

export const S6: ScenarioDefinition = {
  backgroundsOnPurpose: true,
  async run(run) {
    const native = run.env.native;
    if (!native?.enterPip) {
      run.note('n/a: picture-in-picture needs the Android shell');
      return;
    }
    const video = run.env.stage.fullscreen();
    if (typeof video.requestVideoFrameCallback !== 'function') {
      run.note('n/a: requestVideoFrameCallback is not available');
      return;
    }
    const [cam] = run.sims();
    if (!cam) return;
    const session = await run.session(1);
    const player = run.player(video, session, 0);
    const started = await startStream(run, { session, player, video, slot: 0, cam: cam.id, profile: 'sub', timeoutMs: FIRST_FRAME_TIMEOUT_MS });
    if ('bindError' in started || started.frame === 'timeout' || 'failure' in started.frame) {
      run.note(`start: ${'bindError' in started ? `bind error ${started.bindError}` : 'no first frame'}`);
      return;
    }

    let inPip = false;
    let leftAt: number | undefined;
    native.onPip?.((on) => {
      if (on) inPip = true;
      else if (inPip) leftAt ??= run.now();
    });
    const intersection = run.env.watchIntersection(video);
    await native.enterPip();
    const start = run.now();
    let last = start;
    let maxGap = 0;
    let frames = 0;
    let visibleS = 0;
    let sampledS = 0;
    const stop = onPresentedFrames(video, (at) => {
      if (leftAt !== undefined || at - start > PIP_MS) return;
      frames += 1;
      maxGap = Math.max(maxGap, at - last);
      last = at;
    });
    try {
      while (leftAt === undefined && run.now() - start < PIP_MS) {
        await run.sleep(CHECK_EVERY_MS);
        if (intersection === 'n/a') continue;
        sampledS += 1;
        if (intersection.ratio() > 0) visibleS += 1;
      }
    } finally {
      stop();
      if (intersection !== 'n/a') intersection.stop();
    }
    const end = Math.min(leftAt ?? run.now(), start + PIP_MS);
    maxGap = Math.max(maxGap, end - last);
    const pipS = Math.round((end - start) / 1000);
    run.sample('pipFrames', frames);
    run.sample('pipMaxGapMs', maxGap);
    run.sample('pipS', pipS);
    run.note(intersection === 'n/a' ? 'pipVisible: n/a' : `pipVisible: ${visibleS}/${sampledS} s`);
    if (leftAt !== undefined) {
      run.note(`left picture-in-picture after ${Math.round((leftAt - start) / 1000)} s`);
      run.interrupt({ kind: 'pip-left', pipS });
    }
  },
};
