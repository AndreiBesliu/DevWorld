// S9, G9: the codec matrix. The main streams of mx-h265 (H.265) and mx-high51
// (H.264 High 5.1) are each bound to slot 0; a path is `ok` when the hub binds
// it and its first frame shows within 4 s (APP-05). Otherwise the app falls
// back to the camera's substream, as the product does, and shows the label of
// the case: H.265 → `fallback` ("calitate redusă: H.265 nesuportat"), a main
// profile the browser does not offer or decode → `unsupported-profile`
// ("profil video nesuportat", §6.3 step 7). A substream that goes out in another
// H.264 profile (R23) is recorded, and is a fallback, not a failure.
//
// Samples: `<cam>.firstFrameMs` (the main stream, when ok), `<cam>.subFirstFrameMs`
// (the substream fallback).
// Notes, per camera: `<cam>/<profile>: bound <codec>[, fallback profile <id>]` or
// `<cam>/<profile>: error <code>`, `<cam>/<profile>: no first frame within 4 s`,
// and the outcome `<cam>: ok|fallback|unsupported-profile`.
import type { Player } from '@ipcam/player';
import type { BandCode } from '../band';
import type { MessageKey } from '../i18n';
import type { Profile, SpikeSession } from '../session';
import { describeBound, PLAY_BEFORE_STOP_MS, startStream } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const FIRST_FRAME_MS = 4000;
const PAUSE_MS = 2000;

const PATHS: { cam: string; failure: 'fallback' | 'unsupported-profile'; label: MessageKey }[] = [
  { cam: 'mx-h265', failure: 'fallback', label: 'h265Unsupported' },
  { cam: 'mx-high51', failure: 'unsupported-profile', label: 'unsupportedProfile' },
];

/** Starts `cam`/`profile` on slot 0 and returns its first frame time, or undefined with a note. */
async function tryStream(
  run: Run,
  slot: { session: SpikeSession; player: Player; video: HTMLVideoElement; stale: BandCode | null },
  cam: string,
  profile: Profile,
): Promise<number | undefined> {
  const started = await startStream(run, { ...slot, slot: 0, cam, profile, timeoutMs: FIRST_FRAME_MS });
  if ('bindError' in started) {
    run.note(`${cam}/${profile}: error ${started.bindError}`);
    return undefined;
  }
  run.note(`${cam}/${profile}: bound ${describeBound(started.bound)}`);
  const { frame } = started;
  if (frame !== 'timeout' && 'ms' in frame) return frame.ms;
  const hint = frame !== 'timeout' && frame.failure.hint !== undefined ? ` (hint: ${frame.failure.hint})` : '';
  run.note(`${cam}/${profile}: no first frame within ${FIRST_FRAME_MS / 1000} s${hint}`);
  return undefined;
}

export const S9: ScenarioDefinition = {
  async run(run) {
    const { stage, t } = run.env;
    const session = await run.session(1);
    const [video] = stage.grid(1);
    if (!video) return;
    const player = run.player(video, session, 0);
    /** Stops what slot 0 shows, remembering its last frame (see isFrameOfStart). */
    const clear = (): { session: SpikeSession; player: Player; video: HTMLVideoElement; stale: BandCode | null } => {
      const stale = run.env.readBand(video);
      player.stop();
      session.unbind(0);
      return { session, player, video, stale };
    };
    for (const [index, path] of PATHS.entries()) {
      let slot = { session, player, video, stale: null as BandCode | null };
      if (index > 0) {
        await run.sleep(PLAY_BEFORE_STOP_MS);
        slot = clear();
        stage.say('');
        await run.sleep(PAUSE_MS);
      }
      const main = await tryStream(run, slot, path.cam, 'main');
      if (main !== undefined) {
        run.sample(`${path.cam}.firstFrameMs`, main);
        run.note(`${path.cam}: ok`);
        continue;
      }
      stage.say(t(path.label));
      const sub = await tryStream(run, clear(), path.cam, 'sub');
      if (sub !== undefined) run.sample(`${path.cam}.subFirstFrameMs`, sub);
      run.note(`${path.cam}: ${path.failure}`);
    }
  },
};
