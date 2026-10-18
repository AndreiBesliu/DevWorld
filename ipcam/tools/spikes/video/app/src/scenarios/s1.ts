// S1, MV1: the time from a tap to the first frame. The substream of sim-01 is
// bound to slot 0 twenty times, each time from an empty slot after a 2 s
// pause; the time runs from the bind to the display of the first frame of that
// bind (frames.ts, isFrameOfStart). Each start plays for 1 s before the slot is
// emptied (PLAY_BEFORE_STOP_MS).
//
// Samples: `firstFrameMs` (one per start that showed a frame).
// Notes: `sub: <codec>[, fallback profile <id>]` for the first bound start;
// `start <n>: bind error <code>` or `start <n>: <player error | no first frame>`.
import type { BandCode } from '../band';
import { describeBound, describeFailure, PLAY_BEFORE_STOP_MS, startStream } from './frames';
import type { ScenarioDefinition } from './runner';

const STARTS = 20;
const PAUSE_MS = 2000;
/** Long enough to see the player's own 4 s NO_FIRST_FRAME. */
const FIRST_FRAME_TIMEOUT_MS = 10_000;

export const S1: ScenarioDefinition = {
  async run(run) {
    const [cam] = run.sims();
    const session = await run.session(1);
    const [video] = run.env.stage.grid(1);
    if (!cam || !video) return;
    const player = run.player(video, session, 0);
    let codecNoted = false;
    let stale: BandCode | null = null;
    for (let i = 1; i <= STARTS; i += 1) {
      if (i > 1) {
        await run.sleep(PLAY_BEFORE_STOP_MS);
        stale = run.env.readBand(video);
        player.stop();
        session.unbind(0);
        await run.sleep(PAUSE_MS);
      }
      const started = await startStream(run, {
        session,
        player,
        video,
        slot: 0,
        cam: cam.id,
        profile: 'sub',
        timeoutMs: FIRST_FRAME_TIMEOUT_MS,
        stale,
      });
      if ('bindError' in started) {
        run.note(`start ${i}: bind error ${started.bindError}`);
        continue;
      }
      if (!codecNoted) {
        run.note(`sub: ${describeBound(started.bound)}`);
        codecNoted = true;
      }
      if (started.frame === 'timeout') run.note(`start ${i}: no first frame within ${FIRST_FRAME_TIMEOUT_MS / 1000} s`);
      else if ('failure' in started.frame) run.note(`start ${i}: ${describeFailure(started.frame.failure)}`);
      else run.sample('firstFrameMs', started.frame.ms);
    }
  },
};
