// S2, MV2: glass-to-glass latency, 100 samples on the substream of sim-01 and
// 100 on its main stream. A presented frame's band names its frame; the hub's
// `sent` records say when it sent that frame, on the hub's clock; the clock
// offset from ClockSync (re-estimated every 30 s) puts both on one scale, so
// the device's wall clock never enters (Review Focus 3).
//
// Matching (R3, R18, R26): a frame is matched on (bound camera number, profile,
// frame) to the most recent `sent` record of that frame; the record's loop is
// the hub's replay counter and is not compared (the band's loop is always 0).
// Records that predate the bind that started the measurement belong to the GOP
// the hub had cached and are discarded (predatesBind), with that bind's own
// BindResult. A frame waits up to 3 s for its record (they come every 500 ms).
// A record that would make a sample of more than 10 s is the frame's previous
// showing, one loop earlier (the files loop every 20 to 60 s): the frame waits
// for its own record instead.
//
// Samples: `subLatencyMs`, `mainLatencyMs`; `subReliable`, `mainReliable` (1 or
// 0 per sample: the clock estimate at that sample was reliable); `clockRttMs`
// (the round trip of each clock estimate).
// Notes: `<profile>: <n> of 100 samples within 60 s`, `<profile>: bind error <code>`.
import { ClockSync, glassToGlassMs, predatesBind, type ClockEstimate } from '../clock';
import type { HubCamera } from '../cameras';
import type { Player } from '@ipcam/player';
import type { BindResult, Profile, SpikeSession } from '../session';
import { bindFailure, liveSource, onPresentedFrames } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const SAMPLES = 100;
const PROFILE_TIMEOUT_MS = 60_000;
/** At most one sample per 200 ms, so that 100 samples span about 20 s of video. */
const SAMPLE_SPACING_MS = 200;
const MATCH_WAIT_MS = 3000;
/** Half the shortest loop of the hub's files (20 s): a longer "latency" is a match one loop off (R18). */
const MAX_LATENCY_MS = 10_000;
const CLOCK_EVERY_MS = 30_000;
const POLL_MS = 250;

/** The band's camera field is the media file (R1): 1 is the substream, 2 the main stream. */
const MEDIA_FILE: Record<Profile, number> = { sub: 1, main: 2 };

interface Display {
  frame: number;
  displayPerfMs: number;
}

async function measure(
  run: Run,
  session: SpikeSession,
  player: Player,
  video: HTMLVideoElement,
  cam: HubCamera,
  profile: Profile,
  clock: () => ClockEstimate,
): Promise<void> {
  const records = new Map<number, number[]>(); // frame -> hubMs of its sent records
  const waiting: Display[] = [];
  let bound: BindResult | undefined;
  let taken = 0;
  let lastCandidateAt = Number.NEGATIVE_INFINITY;

  const match = (): void => {
    if (bound === undefined) return;
    const { offsetMs, reliable } = clock();
    const since = bound;
    for (let i = 0; i < waiting.length; ) {
      const display = waiting[i];
      if (display === undefined) break;
      const sent = (records.get(display.frame) ?? []).filter(
        (hubMs) => !predatesBind(hubMs, since.requestedPerfMs, offsetMs, performance.timeOrigin),
      );
      const latencyMs =
        sent.length > 0 ? glassToGlassMs(display.displayPerfMs, Math.max(...sent), offsetMs, performance.timeOrigin) : Infinity;
      if (latencyMs <= MAX_LATENCY_MS) {
        if (taken < SAMPLES) {
          run.sample(`${profile}LatencyMs`, latencyMs);
          run.sample(`${profile}Reliable`, reliable ? 1 : 0);
          taken += 1;
        }
        waiting.splice(i, 1);
      } else if (run.now() - display.displayPerfMs > MATCH_WAIT_MS) {
        waiting.splice(i, 1); // a cached-GOP frame, or a record that never came
      } else {
        i += 1;
      }
    }
  };

  const stopSent = session.onSent((camera, sentProfile, recs) => {
    if (camera !== cam.camera || sentProfile !== profile) return;
    for (const [frame, , hubMs] of recs) {
      const list = records.get(frame) ?? [];
      list.push(hubMs);
      records.set(frame, list);
    }
    match();
  });
  const stopFrames = onPresentedFrames(video, (displayPerfMs) => {
    if (displayPerfMs - lastCandidateAt < SAMPLE_SPACING_MS) return;
    const code = run.env.readBand(video);
    if (code === null || code.camera !== MEDIA_FILE[profile]) return;
    lastCandidateAt = displayPerfMs;
    waiting.push({ frame: code.frame, displayPerfMs });
    match();
  });
  try {
    const bind = session.bind(0, cam.id, profile);
    await player.play(liveSource(cam.id, profile));
    try {
      bound = await bind;
    } catch (error) {
      run.note(`${profile}: bind error ${bindFailure(error)}`);
      return;
    }
    const deadline = run.now() + PROFILE_TIMEOUT_MS;
    while (taken < SAMPLES && run.now() < deadline) {
      await run.sleep(POLL_MS);
      match(); // also expires the frames whose record never came
    }
    if (taken < SAMPLES) run.note(`${profile}: ${taken} of ${SAMPLES} samples within ${PROFILE_TIMEOUT_MS / 1000} s`);
  } finally {
    stopFrames();
    stopSent();
    player.stop();
    session.unbind(0);
  }
}

export const S2: ScenarioDefinition = {
  async run(run) {
    const [cam] = run.sims();
    const session = await run.session(1);
    const [video] = run.env.stage.grid(1);
    if (!cam || !video) return;
    const player = run.player(video, session, 0);
    const sync = new ClockSync(() => session.ping());
    let estimate = await sync.estimate();
    run.sample('clockRttMs', estimate.rttMs);
    run.every(CLOCK_EVERY_MS, () => {
      sync.estimate().then(
        (next) => {
          estimate = next;
          run.sample('clockRttMs', next.rttMs);
        },
        () => {
          run.note('a clock re-estimate failed: no ping was answered');
        },
      );
    });
    for (const profile of ['sub', 'main'] as const) {
      await measure(run, session, player, video, cam, profile, () => estimate);
    }
  },
};
