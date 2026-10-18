// S3-4, S3-9, S3-16: MV3 (fluency), MV4 (hardware decoding), MV5 (heat and
// battery) and MV6 (CPU). A grid of 4, 9 or 16 substream tiles for 10 min, then
// the main stream of sim-01 alone in fullscreen, on the extra slot, for 2 min.
// getStats every 2 s, the shell's metrics every 10 s. For MV4 a 1-pixel camera
// capture is held (SPIKE-ONLY, decoderinfo.ts) after the screen says why the
// camera light turns on.
//
// Samples, grid: `gridFps` (mean fps of the tiles over each 2 s), `tileFps`,
// `tileDroppedPct` (per tile over the 10 min), `tileHardware` (per tile, 1 when
// its decoder is power efficient, 0 when not; absent without decoder info).
// Samples, fullscreen: `mainFps` (each 2 s), `mainDroppedPct`, `mainHardware`.
// Shell metrics, every 10 s, as they come: `thermal` (index in THERMAL_LEVELS,
// 0 = NONE), `batteryPct`, `memMb`, `cpuPctTotal`, `cpuPctApp`; the same with
// the prefix `main` (`mainCpuPctTotal`, ...) during the fullscreen part.
// Notes: `decoders: <name> ×<tiles>`, `main decoder: <name>`, `decoder info: n/a`,
// `native metrics: n/a`, `tile <n>: ...` for a tile that did not start.
import type { Player } from '@ipcam/player';
import type { TileStats } from '../metrics';
import { THERMAL_LEVELS, type NativeMetrics } from '../native';
import type { Profile, SpikeSession } from '../session';
import { describeBound, describeFailure, startStream } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const GRID_MS = 10 * 60_000;
const MAIN_MS = 2 * 60_000;
const STATS_EVERY_MS = 2000;
const NATIVE_EVERY_MS = 10_000;
const FIRST_FRAME_TIMEOUT_MS = 10_000;
const NATIVE_TIMEOUT_MS = 5000;

/** Records the shell's metrics under `prefix` (`main` gives `mainCpuPctTotal`); nothing without a shell. */
export async function recordNative(run: Run, prefix = ''): Promise<void> {
  const native = run.env.native;
  if (!native) return;
  const metrics: NativeMetrics | 'timeout' | undefined = await run.within(native.metrics(), NATIVE_TIMEOUT_MS).catch(() => undefined);
  if (metrics === undefined || metrics === 'timeout') return;
  const put = (key: string, value: number | undefined): void => {
    if (value !== undefined) run.sample(prefix ? `${prefix}${key[0]?.toUpperCase() ?? ''}${key.slice(1)}` : key, value);
  };
  put('thermal', metrics.thermal === undefined ? undefined : THERMAL_LEVELS.indexOf(metrics.thermal));
  put('batteryPct', metrics.batteryPct);
  put('memMb', metrics.memMb);
  put('cpuPctTotal', metrics.cpuPctTotal);
  put('cpuPctApp', metrics.cpuPctApp);
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

/**
 * Samples the stats of `tracks` every 2 s for `durationMs`, and the shell every
 * 10 s. Returns, per track, the fps and the dropped percentage over the whole
 * time and the last decoder seen.
 */
async function sampleTracks(
  run: Run,
  session: SpikeSession,
  tracks: string[],
  durationMs: number,
  fpsKey: string,
  nativePrefix: string,
): Promise<{ fps: number; droppedPct: number; decoder?: string; powerEfficient?: boolean }[]> {
  const first = await run.env.sampleStats(session.pc);
  const firstAt = run.now();
  let previous = first;
  let previousAt = firstAt;
  const decoders = new Map<string, TileStats>();
  for (let elapsed = STATS_EVERY_MS; elapsed <= durationMs; elapsed += STATS_EVERY_MS) {
    await run.sleep(STATS_EVERY_MS);
    const stats = await run.env.sampleStats(session.pc);
    const at = run.now();
    const seconds = (at - previousAt) / 1000;
    const fps = tracks.map((track) => (totalsOf(stats, track).framesDecoded - totalsOf(previous, track).framesDecoded) / seconds);
    run.sample(fpsKey, fps.reduce((sum, value) => sum + value, 0) / Math.max(1, fps.length));
    for (const track of tracks) {
      const tile = stats.get(track);
      if (tile?.decoder !== undefined || tile?.powerEfficient !== undefined) decoders.set(track, tile);
    }
    if (elapsed % NATIVE_EVERY_MS === 0) await recordNative(run, nativePrefix);
    previous = stats;
    previousAt = at;
  }
  const seconds = Math.max(0.001, (previousAt - firstAt) / 1000);
  return tracks.map((track) => {
    const decoded = totalsOf(previous, track).framesDecoded - totalsOf(first, track).framesDecoded;
    const dropped = totalsOf(previous, track).framesDropped - totalsOf(first, track).framesDropped;
    const decoder = decoders.get(track);
    return {
      fps: decoded / seconds,
      droppedPct: decoded + dropped > 0 ? (100 * dropped) / (decoded + dropped) : 0,
      ...(decoder?.decoder === undefined ? {} : { decoder: decoder.decoder }),
      ...(decoder?.powerEfficient === undefined ? {} : { powerEfficient: decoder.powerEfficient }),
    };
  });
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
    async run(run) {
      const { stage, t } = run.env;
      const sims = run.sims();
      const session = await run.session(tiles);
      if (!run.env.native) run.note('native metrics: n/a');

      // The explanation stays on screen while the camera is held, that is while its light is on.
      stage.say(t('mv4Explain'));
      const release = await run.env.captureForDecoderInfo();
      if (release === 'n/a') {
        run.note('decoder info: n/a');
        stage.say('');
      } else {
        run.atEnd(release);
      }

      const videos = stage.grid(tiles);
      const players = videos.map((video, slot) => run.player(video, session, slot));
      await Promise.all(
        players.map((player, slot) =>
          startTile(run, session, player, videos[slot] ?? stage.fullscreen(), slot, sims[slot % sims.length]?.id ?? '', 'sub'),
        ),
      );
      const tracks = players.map((_, slot) => session.track(slot).id);
      const grid = await sampleTracks(run, session, tracks, GRID_MS, 'gridFps', '');
      for (const tile of grid) {
        run.sample('tileFps', tile.fps);
        run.sample('tileDroppedPct', tile.droppedPct);
        if (tile.powerEfficient !== undefined) run.sample('tileHardware', tile.powerEfficient ? 1 : 0);
      }
      const names = grid.flatMap((tile) => (tile.decoder === undefined ? [] : [tile.decoder]));
      if (names.length > 0) run.note(`decoders: ${decoderSummary(names)}`);

      // A tap on a tile: the grid stops and the main stream fills the screen, on the extra slot.
      players.forEach((player, slot) => {
        player.stop();
        session.unbind(slot);
      });
      stage.grid(0);
      const big = stage.fullscreen();
      const fullscreen = run.player(big, session, tiles);
      await startTile(run, session, fullscreen, big, tiles, sims[0]?.id ?? '', 'main');
      const [main] = await sampleTracks(run, session, [session.track(tiles).id], MAIN_MS, 'mainFps', 'main');
      if (main) {
        run.sample('mainDroppedPct', main.droppedPct);
        if (main.powerEfficient !== undefined) run.sample('mainHardware', main.powerEfficient ? 1 : 0);
        if (main.decoder !== undefined) run.note(`main decoder: ${main.decoder}`);
      }
      stage.exitFullscreen();
    },
  };
}
