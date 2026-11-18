// S3-4, S3-9, S3-16: MV3 (fluency), MV4 (hardware decoding), MV5 (heat and
// battery) and MV6 (CPU). A grid of 4, 9 or 16 substream tiles for 10 min, then
// the main stream of sim-01 alone in fullscreen, on the extra slot, for 2 min.
// getStats every 2 s, the shell's metrics every 10 s.
//
// MV4: Chrome reports a track's decoder only while the page holds a camera
// capture (SPIKE-ONLY, decoderinfo.ts). S3 holds a 1-pixel one in two short
// windows only (R47), each after the screen says why the camera light turns on:
// at most 10 s once the tiles play, before the grid's measurement starts, and
// at most 10 s after the fullscreen measurement, with the main stream still
// playing. Battery, heat, CPU and fps are never measured with the camera on.
//
// A phone on the charger measures neither battery nor heat (R48): on Android
// S3 does not start while the phone charges, and ends as interrupted when a
// reading says it charges.
//
// Samples, grid: `gridFps` (mean fps of the tiles over each 2 s), `tileFps`,
// `tileDroppedPct` (per tile over the 10 min), `tileHardware` (per tile, 1 when
// its decoder is power efficient, 0 when not; from the first capture window,
// absent without decoder info).
// Samples, fullscreen: `mainFps` (each 2 s), `mainDroppedPct`, `mainHardware`
// (from the second capture window).
// Shell metrics, every 10 s, as they come: `thermal` (index in THERMAL_LEVELS,
// 0 = NONE), `batteryPct`, `charging` (1 or 0), `memMb`, `cpuPctTotal`,
// `cpuPctApp`, `webview2Procs` (Windows: the WebView2 processes in cpuPctApp
// and memMb); the same with the prefix `main` (`mainCpuPctTotal`, ...) during
// the fullscreen part.
// The loss counters of the run: `rtpLost`, `nack`, `pli`, `framesDropped` (recordLoss).
// Notes: `decoders: <name> ×<tiles>`, `main decoder: <name>`, `decoder info: n/a`,
// `native metrics: n/a`, `tile <n>: ...` for a tile that did not start,
// `tile <n>: <player error> <s> s into the grid window` and
// `main: <player error> <s> s into the fullscreen window` for a player error
// (a STALLED, for example) while it is measured, `<loss counter>: n/a`.
import type { Player } from '@ipcam/player';
import type { TileStats } from '../metrics';
import { readNativeMetrics, THERMAL_LEVELS, type NativeMetrics } from '../native';
import type { Profile, SpikeSession } from '../session';
import type { ScenarioEnv } from './context';
import { describeBound, describeFailure, playerFailure, recordLoss, startStream } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const GRID_MS = 10 * 60_000;
const MAIN_MS = 2 * 60_000;
const STATS_EVERY_MS = 2000;
const NATIVE_EVERY_MS = 10_000;
const FIRST_FRAME_TIMEOUT_MS = 10_000;
/** The longest a capture window holds the camera (R47). */
const CAPTURE_MAX_MS = 10_000;

/** Whether the phone is on the charger, which S3 does not measure on (R48). Only the Android shell reports it. */
const onTheCharger = (env: ScenarioEnv, metrics: NativeMetrics | 'n/a'): boolean =>
  env.device.platform === 'android' && metrics !== 'n/a' && metrics.charging === true;

/**
 * Records the shell's metrics under `prefix` (`main` gives `mainCpuPctTotal`);
 * nothing without a shell. Returns what the shell answered.
 */
export async function recordNative(run: Run, prefix = ''): Promise<NativeMetrics | 'n/a'> {
  const native = run.env.native;
  if (!native) return 'n/a';
  const metrics = await readNativeMetrics(native, run.env.clock.setTimer);
  if (metrics === 'n/a') return metrics;
  const put = (key: string, value: number | undefined): void => {
    if (value !== undefined) run.sample(prefix ? `${prefix}${key[0]?.toUpperCase() ?? ''}${key.slice(1)}` : key, value);
  };
  put('thermal', metrics.thermal === undefined ? undefined : THERMAL_LEVELS.indexOf(metrics.thermal));
  put('batteryPct', metrics.batteryPct);
  put('charging', metrics.charging === undefined ? undefined : Number(metrics.charging));
  put('memMb', metrics.memMb);
  put('cpuPctTotal', metrics.cpuPctTotal);
  put('cpuPctApp', metrics.cpuPctApp);
  put('webview2Procs', metrics.webview2Procs);
  return metrics;
}

/** Starts a tile and notes what went wrong. */
async function startTile(
  run: Run,
  session: SpikeSession,
  player: Player,
  video: HTMLVideoElement,
  slot: number,
  cam: string,
  profile: Profile,
): Promise<void> {
  const started = await startStream(run, { session, player, video, slot, cam, profile, timeoutMs: FIRST_FRAME_TIMEOUT_MS });
  if ('bindError' in started) run.note(`tile ${slot}: bind error ${started.bindError}`);
  else if (started.frame === 'timeout') run.note(`tile ${slot}: no first frame (${describeBound(started.bound)})`);
  else if ('failure' in started.frame) run.note(`tile ${slot}: ${describeFailure(started.frame.failure)}`);
}

interface Totals {
  framesDecoded: number;
  framesDropped: number;
}

const totalsOf = (stats: Map<string, TileStats>, track: string): Totals =>
  stats.get(track) ?? { framesDecoded: 0, framesDropped: 0 };

/** A player whose errors S3 notes while it is measured, and how the note names it. */
interface Watched {
  player: Player;
  label: string;
}

/**
 * Samples the stats of `tracks` every 2 s for `durationMs`, and the shell every
 * 10 s; notes each error of the `watched` players (a STALLED, for example)
 * with its time in the window. Returns, per track, the fps and the dropped
 * percentage over the whole time.
 */
async function sampleTracks(
  run: Run,
  session: SpikeSession,
  tracks: string[],
  durationMs: number,
  window: { fpsKey: string; nativePrefix: string; name: string; watched: Watched[] },
): Promise<{ fps: number; droppedPct: number }[]> {
  const first = await run.env.sampleStats(session.pc);
  const firstAt = run.now();
  const offErrors = window.watched.map(({ player, label }) =>
    player.on('error', (data) => {
      const seconds = Math.round((run.now() - firstAt) / 1000);
      run.note(`${label}: ${describeFailure(playerFailure(data))} ${seconds} s into the ${window.name} window`);
    }),
  );
  try {
    let previous = first;
    let previousAt = firstAt;
    for (let elapsed = STATS_EVERY_MS; elapsed <= durationMs; elapsed += STATS_EVERY_MS) {
      await run.sleep(STATS_EVERY_MS);
      const stats = await run.env.sampleStats(session.pc);
      const at = run.now();
      const seconds = (at - previousAt) / 1000;
      const fps = tracks.map((track) => (totalsOf(stats, track).framesDecoded - totalsOf(previous, track).framesDecoded) / seconds);
      run.sample(window.fpsKey, fps.reduce((sum, value) => sum + value, 0) / Math.max(1, fps.length));
      if (elapsed % NATIVE_EVERY_MS === 0) {
        const metrics = await recordNative(run, window.nativePrefix);
        if (onTheCharger(run.env, metrics)) run.interrupt({ kind: 'charging', atS: run.elapsedS() });
      }
      previous = stats;
      previousAt = at;
    }
    const seconds = Math.max(0.001, (previousAt - firstAt) / 1000);
    return tracks.map((track) => {
      const decoded = totalsOf(previous, track).framesDecoded - totalsOf(first, track).framesDecoded;
      const dropped = totalsOf(previous, track).framesDropped - totalsOf(first, track).framesDropped;
      return { fps: decoded / seconds, droppedPct: decoded + dropped > 0 ? (100 * dropped) / (decoded + dropped) : 0 };
    });
  } finally {
    for (const off of offErrors) off();
  }
}

/** The decoder of a track, as far as the stats name it. */
type Decoder = Pick<TileStats, 'decoder' | 'powerEfficient'>;

/**
 * A capture window (R47): explains the camera light, holds the 1-pixel capture
 * and reads the decoders of `tracks` every 2 s until each one has reported
 * its decoder or CAPTURE_MAX_MS is near, then releases the capture. `n/a`
 * when the page cannot capture (no camera, refused, or the prompt unanswered).
 */
async function decoderWindow(run: Run, session: SpikeSession, tracks: string[]): Promise<Map<string, Decoder> | 'n/a'> {
  const { stage, t } = run.env;
  // The explanation stays on screen while the camera is held, that is while its light is on.
  // Only on the phone does a refusal cost a verdict (MV4 is an AND-1 item).
  stage.say(t(run.env.device.platform === 'android' ? 'mv4ExplainAndroid' : 'mv4Explain'));
  const release = await run.env.captureForDecoderInfo();
  if (release === 'n/a') {
    stage.say('');
    return 'n/a';
  }
  const decoders = new Map<string, Decoder>();
  try {
    const heldAt = run.now();
    for (;;) {
      const stats = await run.env.sampleStats(session.pc);
      for (const track of tracks) {
        const tile = stats.get(track);
        if (tile?.decoder !== undefined || tile?.powerEfficient !== undefined) {
          decoders.set(track, {
            ...(tile.decoder === undefined ? {} : { decoder: tile.decoder }),
            ...(tile.powerEfficient === undefined ? {} : { powerEfficient: tile.powerEfficient }),
          });
        }
      }
      if (decoders.size === tracks.length || run.now() - heldAt + STATS_EVERY_MS >= CAPTURE_MAX_MS) break;
      await run.sleep(STATS_EVERY_MS);
    }
  } finally {
    release();
    stage.say('');
  }
  return decoders;
}

/** The tiles' decoders as `name ×count`, most used first. */
function decoderSummary(names: string[]): string {
  const counts = new Map<string, number>();
  for (const name of names) counts.set(name, (counts.get(name) ?? 0) + 1);
  return [...counts]
    .sort((a, b) => b[1] - a[1])
    .map(([name, count]) => `${name} ×${count}`)
    .join(', ');
}

export function gridScenario(tiles: number): ScenarioDefinition {
  return {
    refuse: (env, metrics) => (onTheCharger(env, metrics) ? 's3Charging' : undefined),
    async run(run) {
      const { stage } = run.env;
      const sims = run.sims();
      const session = await run.session(tiles);
      if (!run.env.native) run.note('native metrics: n/a');

      const videos = stage.grid(tiles);
      const players = videos.map((video, slot) => run.player(video, session, slot));
      await Promise.all(
        players.map((player, slot) =>
          startTile(run, session, player, videos[slot] ?? stage.fullscreen(), slot, sims[slot % sims.length]?.id ?? '', 'sub'),
        ),
      );
      const tracks = players.map((_, slot) => session.track(slot).id);

      // MV4, first capture window: the tiles' decoders, before the grid is measured.
      const tileDecoders = await decoderWindow(run, session, tracks);
      if (tileDecoders === 'n/a') run.note('decoder info: n/a');

      const grid = await sampleTracks(run, session, tracks, GRID_MS, {
        fpsKey: 'gridFps',
        nativePrefix: '',
        name: 'grid',
        watched: players.map((player, slot) => ({ player, label: `tile ${slot}` })),
      });
      for (const tile of grid) {
        run.sample('tileFps', tile.fps);
        run.sample('tileDroppedPct', tile.droppedPct);
      }
      if (tileDecoders !== 'n/a') {
        for (const track of tracks) {
          const powerEfficient = tileDecoders.get(track)?.powerEfficient;
          if (powerEfficient !== undefined) run.sample('tileHardware', powerEfficient ? 1 : 0);
        }
        const names = tracks.flatMap((track) => {
          const name = tileDecoders.get(track)?.decoder;
          return name === undefined ? [] : [name];
        });
        if (names.length > 0) run.note(`decoders: ${decoderSummary(names)}`);
      }

      // A tap on a tile: the grid stops and the main stream fills the screen, on the extra slot.
      players.forEach((player, slot) => {
        player.stop();
        session.unbind(slot);
      });
      stage.grid(0);
      const big = stage.fullscreen();
      const fullscreen = run.player(big, session, tiles);
      await startTile(run, session, fullscreen, big, tiles, sims[0]?.id ?? '', 'main');
      const mainTrack = session.track(tiles).id;
      const [main] = await sampleTracks(run, session, [mainTrack], MAIN_MS, {
        fpsKey: 'mainFps',
        nativePrefix: 'main',
        name: 'fullscreen',
        watched: [{ player: fullscreen, label: 'main' }],
      });
      if (main) run.sample('mainDroppedPct', main.droppedPct);

      // MV4, second capture window: the main stream's decoder, after its measurement.
      if (tileDecoders !== 'n/a') {
        const mainDecoder = await decoderWindow(run, session, [mainTrack]);
        const decoder = mainDecoder === 'n/a' ? undefined : mainDecoder.get(mainTrack);
        if (decoder?.powerEfficient !== undefined) run.sample('mainHardware', decoder.powerEfficient ? 1 : 0);
        if (decoder?.decoder !== undefined) run.note(`main decoder: ${decoder.decoder}`);
      }
      await recordLoss(run, session);
      stage.exitFullscreen();
    },
  };
}
