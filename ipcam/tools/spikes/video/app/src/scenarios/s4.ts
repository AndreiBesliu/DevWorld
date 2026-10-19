// S4, MV7: stability over 30 min. The grid pages between two layouts every
// 60 s: 4 and 9 substream tiles on Android, 9 and 16 on the desktop paths
// (R45: G6 judges MV7 with 16 tiles on WIN-1). Each page moves every tile to
// another camera: the cameras advance by a step that is coprime with the
// number of sims (pageStep), so this holds with 16 tiles on the hub's 16 sims
// too, and over the pages each tile shows every sim. The main stream of the
// first tile's camera of the current page goes fullscreen on the extra slot,
// or leaves it, every 90 s. Memory is read every 30 s. A
// player `error` while its tile is meant to play counts as unprovoked: the
// scenario stops a player before it unbinds its slot, so it causes none itself.
//
// Samples: `jsHeapMb` (Chromium only), `memMb` and `webview2Procs` (the
// shell's), every 30 s from the start; `tiles` (the largest layout, 9 or 16),
// `playerErrors`, `pageSwitches`, `fullscreenToggles` (one value each); the
// loss counters of the run, `rtpLost`, `nack`, `pli`, `framesDropped`.
// Notes: `player error: <reason>[ (hint: <cause>)]`, `bind error <code> on slot <n>`,
// `jsHeap: bucketed` (R46: Chromium rounded the JS heap to its buckets, as in
// the Android WebView, so MV7 rests on memMb), `<loss counter>: n/a`.
import type { Player } from '@ipcam/player';
import type { HubCamera } from '../cameras';
import { isBucketedHeap } from '../heap';
import { readNativeMetrics } from '../native';
import type { Profile } from '../session';
import { bindFailure, describeFailure, liveSource, playerFailure, recordLoss } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const RUN_MS = 30 * 60_000;
const TICK_MS = 30_000;
const PAGE_EVERY_MS = 60_000;
const FULLSCREEN_EVERY_MS = 90_000;
/** The phone's grid has at most 9 tiles (A6); every other path is a desktop one. */
const PHONE_LAYOUTS = [4, 9] as const;
const DESKTOP_LAYOUTS = [9, 16] as const;

function gcd(a: number, b: number): number {
  return b === 0 ? a : gcd(b, a % b);
}

/**
 * How many cameras a page moves each tile on: the number of tile slots (the
 * next cameras, when there are more sims than tiles) or the first larger
 * number that is coprime with `sims`. Coprime, so that a page change moves
 * every tile to another camera and a tile goes through every sim over the
 * pages: with 16 tiles on 16 sims, a step of 16 would bind each tile to the
 * same camera on every page. With one sim there is no other camera.
 */
export function pageStep(tileSlots: number, sims: number): number {
  if (sims <= 1) return 1;
  let step = Math.max(1, tileSlots);
  while (gcd(step, sims) !== 1) step += 1;
  return step;
}

async function sampleMemory(run: Run, heap: number[]): Promise<void> {
  const heapMb = run.env.jsHeapMb();
  if (heapMb !== undefined) {
    heap.push(heapMb);
    run.sample('jsHeapMb', heapMb);
  }
  const native = run.env.native;
  if (!native) return;
  const metrics = await readNativeMetrics(native, run.env.clock.setTimer);
  if (metrics === 'n/a') return;
  if (metrics.memMb !== undefined) run.sample('memMb', metrics.memMb);
  if (metrics.webview2Procs !== undefined) run.sample('webview2Procs', metrics.webview2Procs);
}

export const S4: ScenarioDefinition = {
  async run(run) {
    const { stage } = run.env;
    const sims = run.sims();
    const layouts = run.env.device.platform === 'android' ? PHONE_LAYOUTS : DESKTOP_LAYOUTS;
    const tileSlots = Math.max(...layouts);
    const step = pageStep(tileSlots, sims.length);
    const session = await run.session(tileSlots);
    run.sample('tiles', tileSlots);
    if (!run.env.native) run.note('native metrics: n/a');
    const tilePlayers: Player[] = [];
    let fullscreenPlayer: Player | undefined;
    const playing = new Set<Player>();
    let errors = 0;

    const track = (player: Player): Player => {
      player.on('error', (data) => {
        if (!playing.has(player)) return;
        errors += 1;
        run.note(`player error: ${describeFailure(playerFailure(data))}`);
      });
      return player;
    };
    const stopSlot = (player: Player | undefined, slot: number): void => {
      if (player) {
        playing.delete(player);
        player.stop();
      }
      session.unbind(slot);
    };
    const startSlot = async (player: Player, slot: number, cam: HubCamera, profile: Profile): Promise<void> => {
      // Handled from the start: if play() throws, nobody else waits for the answer.
      const bind = session.bind(slot, cam.id, profile).then(
        () => undefined,
        (error: unknown) => ({ error }),
      );
      playing.add(player);
      await player.play(liveSource(cam.id, profile));
      const refused = await bind;
      if (refused) {
        playing.delete(player); // nothing to play: not an unprovoked error
        player.stop();
        run.note(`bind error ${bindFailure(refused.error)} on slot ${slot}`);
      }
    };
    const showPage = async (page: number): Promise<void> => {
      const n = layouts[page % layouts.length] ?? 4;
      tilePlayers.forEach((player, slot) => {
        stopSlot(player, slot);
      });
      const videos = stage.grid(n);
      const starts = videos.map((video, slot) => {
        tilePlayers[slot] ??= track(run.player(video, session, slot));
        const cam = sims[(page * step + slot) % sims.length];
        const player = tilePlayers[slot];
        return player && cam ? startSlot(player, slot, cam, 'sub') : Promise.resolve();
      });
      await Promise.all(starts);
    };
    const setFullscreen = async (on: boolean, page: number): Promise<void> => {
      if (!on) {
        stopSlot(fullscreenPlayer, tileSlots);
        stage.exitFullscreen();
        return;
      }
      fullscreenPlayer ??= track(run.player(stage.fullscreen(), session, tileSlots));
      stage.fullscreen();
      const cam = sims[(page * step) % sims.length];
      if (cam) await startSlot(fullscreenPlayer, tileSlots, cam, 'main');
    };

    let page = 0;
    let fullscreen = false;
    let toggles = 0;
    const heap: number[] = [];
    await showPage(page);
    await sampleMemory(run, heap);
    for (let elapsed = TICK_MS; elapsed <= RUN_MS; elapsed += TICK_MS) {
      await run.sleep(TICK_MS);
      await sampleMemory(run, heap);
      if (elapsed === RUN_MS) break;
      if (elapsed % PAGE_EVERY_MS === 0) {
        page += 1;
        await showPage(page);
      }
      if (elapsed % FULLSCREEN_EVERY_MS === 0) {
        fullscreen = !fullscreen;
        toggles += 1;
        await setFullscreen(fullscreen, page);
      }
    }
    playing.clear();
    run.sample('playerErrors', errors);
    run.sample('pageSwitches', page);
    run.sample('fullscreenToggles', toggles);
    if (isBucketedHeap(heap)) run.note('jsHeap: bucketed');
    await recordLoss(run, session);
  },
};
