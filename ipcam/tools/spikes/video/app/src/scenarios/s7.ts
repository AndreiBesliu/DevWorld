// S7, MV10: the 10 min HLS recording that the hub serves. Twenty times: start
// it (timed to the player's first frame), then seek to a random point of it
// (timed from the seek to the element's `seeked` event, R30). No WebRTC.
//
// Samples: `startMs`, `seekMs`.
// Notes: `start <n>: ...`, `seek <n>: no seeked within 10 s`.
import type { PlayerSource } from '@ipcam/player';
import { describeFailure, watchStart } from './frames';
import type { ScenarioDefinition } from './runner';

const STARTS = 20;
const RECORDING_S = 600;
/** Seeks stay this far from the end, where a seek would land on the last segment's tail. */
const END_MARGIN_S = 10;
const TIMEOUT_MS = 10_000;
/** The wall-clock time of media position 0 (R30): seeks are times of day, as in the app. */
const RECORDING_FROM = new Date(Date.UTC(2026, 9, 18, 12, 0, 0));

/** Resolves at the element's next `seeked` event. */
function nextSeeked(video: HTMLVideoElement): { seeked: Promise<void>; stop(): void } {
  let stop: () => void = () => undefined;
  const seeked = new Promise<void>((resolve) => {
    const onSeeked = (): void => {
      video.removeEventListener('seeked', onSeeked);
      resolve();
    };
    video.addEventListener('seeked', onSeeked);
    stop = () => {
      video.removeEventListener('seeked', onSeeked);
    };
  });
  return { seeked, stop };
}

export const S7: ScenarioDefinition = {
  async run(run) {
    const [video] = run.env.stage.grid(1);
    if (!video) return;
    const player = run.player(video);
    const source: PlayerSource = {
      kind: 'recording',
      cameraId: 'recording',
      profile: 'main',
      url: `${run.env.hubUrl.replace(/\/+$/, '')}/media/hls/index.m3u8`,
      from: RECORDING_FROM,
    };
    for (let i = 1; i <= STARTS; i += 1) {
      player.stop();
      const watch = watchStart(run, player, video, run.now());
      await player.play(source);
      const start = await watch.wait(TIMEOUT_MS);
      if (start === 'timeout' || 'failure' in start) {
        run.note(`start ${i}: ${start === 'timeout' ? 'no first frame within 10 s' : describeFailure(start.failure)}`);
        continue;
      }
      run.sample('startMs', start.ms);

      const offsetS = run.env.random() * (RECORDING_S - END_MARGIN_S);
      const seek = nextSeeked(video);
      const seekAt = run.now();
      await player.seek(new Date(RECORDING_FROM.getTime() + offsetS * 1000));
      const seeked = await run.within(seek.seeked, TIMEOUT_MS).finally(seek.stop);
      if (seeked === 'timeout') run.note(`seek ${i}: no seeked within 10 s`);
      else run.sample('seekMs', run.now() - seekAt);
    }
  },
};
