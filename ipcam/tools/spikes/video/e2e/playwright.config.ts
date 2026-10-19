// Browser end-to-end tests of the Phase 0 (b) video spike (plan Task 12): the
// real hub, spikehub with the app embedded and 4 simulated cameras, and the
// real spike app, in Chrome and in Firefox.
//
//   pnpm -C ipcam --filter spike-video-e2e e2e
//
// Environment:
// - SPIKE_HUB: a spikehub binary built after the app's dist/ was copied into
//   cmd/spikehub/web. The run starts it on port 7080 with `-sims 4` and the
//   media in SPIKE_MEDIA (cmd/mediagen's output), keeps the results that the
//   app uploads in SPIKE_RESULTS, writes its log to SPIKE_HUB_LOG (both in the
//   temporary folder by default) and stops it at the end. Without SPIKE_HUB the
//   tests use a hub that already listens on port 7080.
// - SPIKE_CHROME: the Chrome binary of the `chrome` project. Locally that is
//   Chrome for Testing (R33: Playwright's bundled Chromium has no H.264);
//   without it, the installed Google Chrome (channel `chrome`), as on the CI
//   runners.
//
// Tests carry the tag of the browsers they run in: @chrome, @firefox. They run
// one at a time, without retries: they share one hub and measure time.
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { defineConfig } from '@playwright/test';

const HUB_URL = 'http://localhost:7080';

const hub = process.env.SPIKE_HUB;
const media = process.env.SPIKE_MEDIA;
const chrome = process.env.SPIKE_CHROME;
const results = process.env.SPIKE_RESULTS ?? join(tmpdir(), 'spike-video-e2e-results');
const hubLog = process.env.SPIKE_HUB_LOG ?? join(tmpdir(), 'spike-video-e2e-hub.log');

if (hub !== undefined && media === undefined) {
  throw new Error('SPIKE_HUB is set without SPIKE_MEDIA, the folder of cmd/mediagen');
}

export default defineConfig({
  testDir: '.',
  workers: 1,
  fullyParallel: false,
  retries: 0,
  forbidOnly: process.env.CI !== undefined,
  // S1 takes about 1 min and a grid measurement 1.5 min; a hung run fails after 5.
  timeout: 5 * 60_000,
  reporter: 'list',
  use: {
    baseURL: HUB_URL,
  },
  projects: [
    {
      name: 'chrome',
      grep: /@chrome/,
      use: {
        browserName: 'chromium',
        ...(chrome === undefined ? { channel: 'chrome' } : { launchOptions: { executablePath: chrome } }),
      },
    },
    {
      name: 'firefox',
      grep: /@firefox/,
      use: { browserName: 'firefox' },
    },
  ],
  ...(hub === undefined
    ? {}
    : {
        webServer: {
          command: `"${hub}" -media "${media}" -sims 4 -results "${results}" > "${hubLog}" 2>&1`,
          url: `${HUB_URL}/spike/v1/cameras`,
          reuseExistingServer: false,
          timeout: 60_000,
        },
      }),
});
