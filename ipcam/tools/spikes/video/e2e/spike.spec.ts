// The browser end-to-end tests of plan Task 12, against the real hub and the
// real spike app. Each runs one scenario through the app's autorun (R5):
// `?autorun=<scenario>&hub=<url>` leaves the finished result in
// window.__spikeResults, or why there is none in window.__spikeErrors. Each
// test prints what it measured on a line that starts with "e2e: ", which the
// CI job turns into a notice.
import { expect, test, type Page } from '@playwright/test';
import type { BandCode } from '../app/src/band';
import { p } from '../app/src/percentile';
import type { ScenarioId, ScenarioResult } from '../app/src/results';

declare global {
  interface Window {
    /** The app's test hooks (autorun.ts, results.ts, band.ts). */
    __spikeResults?: ScenarioResult[];
    __spikeErrors?: unknown[];
    __spikeReadBand?: (video: HTMLVideoElement) => BandCode | null;
    /** What recordLabels and recordPeers record. */
    __e2eLabels?: string[];
    __e2ePeers?: RTCPeerConnection[];
    /** What the band test's sampler counted. */
    __e2eBand?: BandCounts;
  }
}

interface BandCounts {
  sampled: number;
  decoded: number;
  /** Decoded frames by the band's camera field, the media file (R1). */
  byCamera: Record<number, number>;
}

const HUB_URL = 'http://localhost:7080';

/**
 * Runs in the page before the app (addInitScript): records every text that the
 * stage shows over the video (the labels of S9), however briefly.
 */
function recordLabels(): void {
  const labels: string[] = [];
  window.__e2eLabels = labels;
  new MutationObserver(() => {
    const text = document.querySelector('.stage .message')?.textContent ?? '';
    if (text !== '' && labels[labels.length - 1] !== text) labels.push(text);
  }).observe(document, { subtree: true, childList: true, characterData: true });
}

/** Runs in the page before the app (addInitScript): records every RTCPeerConnection that the app opens, for its stats. */
function recordPeers(): void {
  const peers: RTCPeerConnection[] = [];
  window.__e2ePeers = peers;
  window.RTCPeerConnection = new Proxy(window.RTCPeerConnection, {
    construct(target, args: ConstructorParameters<typeof RTCPeerConnection>, newTarget) {
      const pc = Reflect.construct(target, args, newTarget) as RTCPeerConnection;
      peers.push(pc);
      return pc;
    },
  });
}

interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** Where the stage and its tiles are, in CSS pixels of the viewport. */
function stageLayout(page: Page): Promise<{ position: string; stage: Box; tiles: Box[]; viewport: { width: number; height: number } }> {
  return page.evaluate(() => {
    const box = (element: Element): Box => {
      const { left, top, right, bottom } = element.getBoundingClientRect();
      return { left: Math.round(left), top: Math.round(top), right: Math.round(right), bottom: Math.round(bottom) };
    };
    const stage = document.querySelector('.stage');
    if (!stage) throw new Error('the page has no stage');
    return {
      position: getComputedStyle(stage).position,
      stage: box(stage),
      tiles: [...document.querySelectorAll('.grid video')].map(box),
      viewport: { width: innerWidth, height: innerHeight },
    };
  });
}

const insideViewport = (box: Box, viewport: { width: number; height: number }): boolean =>
  box.left >= 0 && box.top >= 0 && box.right <= viewport.width && box.bottom <= viewport.height && box.right > box.left && box.bottom > box.top;

/** What the page says when a scenario gives no result: the errors hook and the connection panel. */
async function whyNoResult(page: Page): Promise<string> {
  const { errors, panel } = await page.evaluate(() => ({
    errors: window.__spikeErrors ?? [],
    panel: document.querySelector('.connection')?.textContent ?? '',
  }));
  return `__spikeErrors ${JSON.stringify(errors)}; the page says: ${panel === '' ? '(nothing)' : panel}`;
}

/** Opens the app with `?autorun=<scenario>`; the scenario starts at once. */
async function startAutorun(page: Page, scenario: ScenarioId): Promise<void> {
  const response = await page.goto(`/?autorun=${encodeURIComponent(scenario)}&hub=${encodeURIComponent(HUB_URL)}`);
  expect(response?.status(), 'the hub serves the app (built into cmd/spikehub/web)').toBe(200);
}

/** The result of the scenario that startAutorun started; fails with what the page says when there is none within `timeoutMs`. */
async function resultOf(page: Page, scenario: ScenarioId, timeoutMs: number): Promise<ScenarioResult> {
  await page
    .waitForFunction(() => (window.__spikeResults?.length ?? 0) > 0 || (window.__spikeErrors?.length ?? 0) > 0, undefined, {
      timeout: timeoutMs,
      polling: 500,
    })
    .catch(() => undefined); // the check below says what the page shows
  const [result] = await page.evaluate(() => window.__spikeResults ?? []);
  if (result === undefined) throw new Error(`${scenario} gave no result within ${timeoutMs / 1000} s: ${await whyNoResult(page)}`);
  expect(result.scenario).toBe(scenario);
  expect(result.interrupted, `${scenario} was interrupted`).toBe(false);
  return result;
}

const describeResult = (result: ScenarioResult): string =>
  `notes: ${result.notes.join('; ')}; samples: ${JSON.stringify(result.samples)}`;

/** The number of samples, their p50 and p90 or other quantiles (nearest rank, as the app counts them) and their max, in ms. */
function summary(values: number[], quantiles = [0.5, 0.9]): string {
  if (values.length === 0) return 'no samples';
  const ms = (value: number): string => `${Math.round(value * 10) / 10} ms`;
  const parts = quantiles.map((q) => `p${Math.round(q * 100)} ${ms(p(values, q))}`);
  return `${values.length} samples, ${parts.join(', ')}, max ${ms(Math.max(...values))}`;
}

/**
 * The video codecs the browser offers to receive, in offer order: the encoding
 * names, with the profile-level-id and packetization-mode of H.264. It is the
 * evidence for S9 (what the browser can be sent) and for expectH264.
 */
async function offeredVideoCodecs(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const pc = new RTCPeerConnection();
    pc.addTransceiver('video', { direction: 'recvonly' });
    const { sdp = '' } = await pc.createOffer();
    pc.close();
    const fmtp = new Map<string, string>();
    for (const [, pt = '', params = ''] of sdp.matchAll(/^a=fmtp:(\d+) (.*)$/gm)) fmtp.set(pt, params);
    const codecs: string[] = [];
    for (const [, pt = '', name = ''] of sdp.matchAll(/^a=rtpmap:(\d+) ([^/]+)\//gm)) {
      if (['rtx', 'red', 'ulpfec', 'flexfec-03'].includes(name.toLowerCase())) continue;
      const params = fmtp.get(pt) ?? '';
      const profile = /profile-level-id=([0-9a-f]{6})/i.exec(params)?.[1];
      const mode = /packetization-mode=(\d)/.exec(params)?.[1] ?? '0';
      codecs.push(profile === undefined ? name : `${name} ${profile.toLowerCase()} pm${mode}`);
    }
    return [...new Set(codecs)].join(', ');
  });
}

/** The project's browser and its version, for the measurement lines. */
const browserOf = (page: Page): string => `${test.info().project.name} ${page.context().browser()?.version() ?? ''}`.trim();

/**
 * The hub sends H.264 in packetization mode 1 (or H.265, which neither CI
 * browser offers), so a browser that offers no such H.264 gets no session.
 * Checked first, so that such a run fails at once and says why. Chrome has
 * H.264 (channel 'chrome', or Chrome for Testing locally, R33); Firefox has it
 * only with the OpenH264 plugin, which the run preinstalls (SPIKE_OPENH264,
 * install-openh264.sh, spec §14.7).
 */
function expectH264(page: Page, offered: string): void {
  expect(
    offered,
    `${browserOf(page)} offers no H.264 in packetization mode 1 to receive; Firefox needs the OpenH264 plugin (SPIKE_OPENH264, install-openh264.sh)`,
  ).toMatch(/\bH264 [0-9a-f]{6} pm1\b/);
}

test('S1 p90 first frame ≤ 1500 ms', { tag: '@chrome' }, async ({ page }) => {
  // HUB-06 / MV1: S1 binds sim-01's substream 20 times, each from an empty slot.
  await startAutorun(page, 'S1');
  const result = await resultOf(page, 'S1', 3 * 60_000);
  const starts = result.samples.firstFrameMs ?? [];
  console.log(`e2e: S1 first frame: ${summary(starts)}`);

  expect(starts, describeResult(result)).toHaveLength(20);
  expect(p(starts, 0.9)).toBeLessThanOrEqual(1500);
});

test('band decodes on ≥ 95 % of sampled frames', { tag: '@chrome' }, async ({ page }) => {
  // S2 measures latency by the band, on sim-01's substream (360 lines, 8 px
  // blocks) and then its main stream (1080 lines, 16 px blocks). While it runs,
  // the shown frame is read every 50 ms with the app's own band reader.
  await startAutorun(page, 'S2');
  await page.evaluate(() => {
    const read = window.__spikeReadBand;
    if (typeof read !== 'function') throw new Error('the app has no band reader hook (window.__spikeReadBand)');
    const counts: BandCounts = { sampled: 0, decoded: 0, byCamera: {} };
    window.__e2eBand = counts;
    setInterval(() => {
      const video = document.querySelector<HTMLVideoElement>('.grid video');
      if (!video || video.videoWidth === 0 || video.paused) return;
      counts.sampled += 1;
      const code = read(video);
      if (code === null) return;
      counts.decoded += 1;
      counts.byCamera[code.camera] = (counts.byCamera[code.camera] ?? 0) + 1;
    }, 50);
  });
  const result = await resultOf(page, 'S2', 3 * 60_000);
  const counts = await page.evaluate(() => window.__e2eBand);
  if (counts === undefined) throw new Error('the band sampler did not run');
  const rate = counts.sampled === 0 ? 0 : counts.decoded / counts.sampled;
  console.log(
    `e2e: band decoded on ${counts.decoded} of ${counts.sampled} sampled frames (${(rate * 100).toFixed(1)} %), by media file ${JSON.stringify(counts.byCamera)}`,
  );
  console.log(`e2e: S2 latency sub: ${summary(result.samples.subLatencyMs ?? [], [0.5, 0.95])}`);
  console.log(`e2e: S2 latency main: ${summary(result.samples.mainLatencyMs ?? [], [0.5, 0.95])}`);

  // About 40 s of video at 20 samples a second.
  expect(counts.sampled, describeResult(result)).toBeGreaterThanOrEqual(400);
  expect(rate).toBeGreaterThanOrEqual(0.95);
  // The band names the media file (R1): 1 is the substream, 2 the main stream.
  expect(Object.keys(counts.byCamera).sort()).toEqual(['1', '2']);
});

test('4-tile grid 60 s: fps ≥ 90 % of 12 and dropped ≤ 2 %', { tag: '@chrome' }, async ({ page }) => {
  // MV3 on 4 tiles: S3-4 puts the substreams of sim-01 to sim-04 in a 2×2 grid;
  // once all four play, the receivers' counters are read 60 s apart. (S3-4
  // itself measures for 10 min; the test leaves before.)
  await page.addInitScript(recordPeers);
  await startAutorun(page, 'S3-4');
  const playing = await page
    .waitForFunction(
      () => {
        const videos = [...document.querySelectorAll<HTMLVideoElement>('.grid video')];
        return videos.length === 4 && videos.every((video) => video.videoWidth > 0 && video.currentTime > 0);
      },
      undefined,
      { timeout: 60_000, polling: 250 },
    )
    .then(
      () => true,
      () => false,
    );
  if (!playing) throw new Error(`the 4 tiles did not all play within 60 s: ${await whyNoResult(page)}`);
  // client-stage-offscreen: while the run lasts, the stage covers the page and every tile is on screen,
  // so the compositor draws what MV3/MV5/MV6 measure.
  const layout = await stageLayout(page);
  console.log(`e2e: S3-4 stage ${JSON.stringify(layout)}`);
  expect(layout.position).toBe('fixed');
  expect(layout.stage).toEqual({ left: 0, top: 0, right: layout.viewport.width, bottom: layout.viewport.height });
  expect(layout.tiles).toHaveLength(4);
  for (const tile of layout.tiles) expect(insideViewport(tile, layout.viewport), JSON.stringify(tile)).toBe(true);
  await page.waitForTimeout(2000); // past the cached GOP that each tile starts with

  const counters = () =>
    page.evaluate(async () => {
      const tiles: Record<string, { decoded: number; dropped: number }> = {};
      for (const pc of window.__e2ePeers ?? []) {
        if (pc.connectionState !== 'connected') continue;
        (await pc.getStats()).forEach((stat: { type?: string; kind?: string; id: string; framesDecoded?: number; framesDropped?: number }) => {
          if (stat.type === 'inbound-rtp' && stat.kind === 'video') {
            tiles[stat.id] = { decoded: stat.framesDecoded ?? 0, dropped: stat.framesDropped ?? 0 };
          }
        });
      }
      return { at: performance.now(), tiles };
    });
  const before = await counters();
  await page.waitForTimeout(60_000);
  const after = await counters();
  const seconds = (after.at - before.at) / 1000;
  const tiles = Object.entries(after.tiles).map(([id, end]) => {
    const start = before.tiles[id] ?? { decoded: 0, dropped: 0 };
    const decoded = end.decoded - start.decoded;
    const dropped = end.dropped - start.dropped;
    return { fps: decoded / seconds, droppedPct: decoded + dropped > 0 ? (100 * dropped) / (decoded + dropped) : 0 };
  });
  const playingTiles = tiles.filter((tile) => tile.fps > 0);
  console.log(
    `e2e: 4-tile grid over ${seconds.toFixed(1)} s: fps ${playingTiles.map((t) => t.fps.toFixed(2)).join(', ')}; dropped ${playingTiles.map((t) => `${t.droppedPct.toFixed(2)} %`).join(', ')}`,
  );

  expect(playingTiles, `tiles: ${JSON.stringify(tiles)}`).toHaveLength(4);
  for (const tile of playingTiles) {
    expect(tile.fps).toBeGreaterThanOrEqual(0.9 * 12);
    expect(tile.droppedPct).toBeLessThanOrEqual(2);
  }
});

/** Runs S9 and returns its result with the labels the stage showed. */
async function runS9(page: Page): Promise<{ result: ScenarioResult; labels: string[]; offered: string }> {
  await page.addInitScript(recordLabels);
  await startAutorun(page, 'S9');
  const browser = browserOf(page);
  const offered = await offeredVideoCodecs(page);
  console.log(`e2e: S9 ${browser} offers to receive: ${offered}`);
  expectH264(page, offered);
  const result = await resultOf(page, 'S9', 2 * 60_000);
  const labels = await page.evaluate(() => window.__e2eLabels ?? []);
  const outcomes = result.notes.filter((note) => /^mx-[\w-]+: [a-z-]+$/.test(note));
  console.log(`e2e: S9 ${browser}: ${outcomes.join(', ')}; labels: ${labels.join(' | ')}`);
  return { result, labels, offered };
}

test('S9: H.265 not decodable on runner → fallback to sub, labelled', { tag: '@chrome' }, async ({ page }) => {
  // APP-05, negative branch: the runner's Chrome neither offers nor decodes H.265.
  const { result, labels, offered } = await runS9(page);
  const context = `${describeResult(result)}; offered: ${offered}`;

  expect(result.notes, context).toContain('mx-h265: fallback');
  expect(labels).toContain('calitate redusă: H.265 nesuportat');
  expect(result.samples['mx-h265.subFirstFrameMs'], context).toHaveLength(1);
});

test('Firefox × mx-high51 → "profil video nesuportat" label and sub fallback', { tag: '@firefox' }, async ({ page }) => {
  // APP-05, §6.3 step 7: Firefox offers no H.264 High, so the 4K High@5.1 main
  // stream cannot be bound; the app shows the label and plays the substream,
  // which the hub sends in the closest offered profile (R23).
  const { result, labels, offered } = await runS9(page);
  const context = `${describeResult(result)}; offered: ${offered}`;

  expect(result.notes, context).toContain('mx-high51: unsupported-profile');
  expect(labels).toContain('profil video nesuportat');
  expect(result.samples['mx-high51.subFirstFrameMs'], context).toHaveLength(1);
});

test('S10: selected pair is host or prflx', { tag: ['@chrome', '@firefox'] }, async ({ page }) => {
  // MV13 at http://localhost on the runner: both ends of the pair on the LAN, no relay.
  await startAutorun(page, 'S10');
  expectH264(page, await offeredVideoCodecs(page));
  const result = await resultOf(page, 'S10', 60_000);
  const pair = result.notes.find((note) => note.startsWith('pair: '));
  console.log(`e2e: S10 ${browserOf(page)}: ${pair ?? 'no pair note'}`);

  expect(pair, describeResult(result)).toMatch(/^pair: (host|prflx)\/(host|prflx)$/);
  // The run is over: the stage is off the page (an empty stage in the page's flow would only look hidden).
  expect(await page.evaluate(() => getComputedStyle(document.querySelector('.stage') ?? document.body).display)).toBe('none');
});
