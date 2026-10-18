// S4, MV7: stability over 30 min. The grid pages between 4 and 9 substream
// tiles every 60 s (each page binds its tiles to the next cameras), and the
// main stream of the first tile's camera goes fullscreen on the extra slot, or
// leaves it, every 90 s. Memory is read every 30 s. A player `error` while its
// tile is meant to play counts as unprovoked: the scenario stops a player
// before it unbinds its slot, so it causes none itself.
//
// Samples: `jsHeapMb` (Chromium only), `memMb` (the shell's), every 30 s from
// the start; `playerErrors`, `pageSwitches`, `fullscreenToggles` (one value each).
// Notes: `player error: <reason>[ (hint: <cause>)]`, `bind error <code> on slot <n>`.
import type { Player } from '@ipcam/player';
import type { HubCamera } from '../cameras';
import type { Profile } from '../session';
import { bindFailure, describeFailure, liveSource, playerFailure } from './frames';
import type { Run, ScenarioDefinition } from './runner';

const RUN_MS = 30 * 60_000;
const TICK_MS = 30_000;
const PAGE_EVERY_MS = 60_000;
const FULLSCREEN_EVERY_MS = 90_000;
const LAYOUTS = [4, 9] as const;
const TILE_SLOTS = 9;

async function sampleMemory(run: Run): Promise<void> {
  const heap = run.env.jsHeapMb();
  if (heap !== undefined) run.sample('jsHeapMb', heap);
  const native = run.env.native;
  if (!native) return;
  const metrics = await run.within(native.metrics(), 5000).catch(() => undefined);
  if (metrics !== undefined && metrics !== 'timeout' && metrics.memMb !== undefined) run.sample('memMb', metrics.memMb);
}

export const S4: ScenarioDefinition = {
  async run(run) {
    const { stage } = run.env;
    const sims = run.sims();
    const session = await run.session(TILE_SLOTS);
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
      const bind = session.bind(slot, cam.id, profile);
      playing.add(player);
      await player.play(liveSource(cam.id, profile));
      try {
        await bind;
      } catch (error) {
        playing.delete(player); // nothing to play: not an unprovoked error
        player.stop();
        run.note(`bind error ${bindFailure(error)} on slot ${slot}`);
      }
    };
    const showPage = async (page: number): Promise<void> => {
      const n = LAYOUTS[page % LAYOUTS.length] ?? 4;
      tilePlayers.forEach((player, slot) => {
        stopSlot(player, slot);
      });
      const videos = stage.grid(n);
      const starts = videos.map((video, slot) => {
        tilePlayers[slot] ??= track(run.player(video, session, slot));
        const cam = sims[(page * TILE_SLOTS + slot) % sims.length];
        const player = tilePlayers[slot];
        return player && cam ? startSlot(player, slot, cam, 'sub') : Promise.resolve();
      });
      await Promise.all(starts);
    };
    const setFullscreen = async (on: boolean, page: number): Promise<void> => {
      if (!on) {
        stopSlot(fullscreenPlayer, TILE_SLOTS);
        stage.exitFullscreen();
        return;
      }
      fullscreenPlayer ??= track(run.player(stage.fullscreen(), session, TILE_SLOTS));
      stage.fullscreen();
      const cam = sims[(page * TILE_SLOTS) % sims.length];
      if (cam) await startSlot(fullscreenPlayer, TILE_SLOTS, cam, 'main');
    };

    let page = 0;
    let fullscreen = false;
    let toggles = 0;
    await showPage(page);
    await sampleMemory(run);
    for (let elapsed = TICK_MS; elapsed <= RUN_MS; elapsed += TICK_MS) {
      await run.sleep(TICK_MS);
      await sampleMemory(run);
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
  },
};
