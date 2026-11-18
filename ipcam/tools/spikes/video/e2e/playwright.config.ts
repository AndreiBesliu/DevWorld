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
// - SPIKE_OPENH264: the folder of Mozilla's OpenH264 media plugin that
//   install-openh264.sh prints. The `firefox` project hands it to Firefox as
//   MOZ_GMP_PATH (spec §14.7: Firefox runs with OpenH264 preinstalled). Without
//   it Playwright's Firefox offers no H.264 for WebRTC, and the Firefox tests
//   fail at their first check.
//
// Tests carry the tag of the browsers they run in: @chrome, @firefox. They run
// one at a time, without retries: they share one hub and measure time.
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, dirname, join } from 'node:path';
import { defineConfig, type LaunchOptions } from '@playwright/test';

const HUB_URL = 'http://localhost:7080';

const hub = process.env.SPIKE_HUB;
const media = process.env.SPIKE_MEDIA;
const chrome = process.env.SPIKE_CHROME;
const results = process.env.SPIKE_RESULTS ?? join(tmpdir(), 'spike-video-e2e-results');
const hubLog = process.env.SPIKE_HUB_LOG ?? join(tmpdir(), 'spike-video-e2e-hub.log');
const openh264 = process.env.SPIKE_OPENH264;

if (hub !== undefined && media === undefined) {
  throw new Error('SPIKE_HUB is set without SPIKE_MEDIA, the folder of cmd/mediagen');
}

/**
 * Firefox with the OpenH264 plugin in `folder`, the way Linux distributions
 * preinstall it: Firefox loads every plugin folder in MOZ_GMP_PATH when it
 * starts, and with the plugin enabled and its updates off it keeps that one.
 * Firefox names a plugin after its folder's parent (gmp-<id>), so the folder is
 * checked here and a wrong one fails the run at once.
 */
function firefoxWithOpenH264(folder: string): LaunchOptions {
  if (basename(dirname(folder)) !== 'gmp-gmpopenh264' || !existsSync(join(folder, 'gmpopenh264.info'))) {
    throw new Error(`SPIKE_OPENH264=${folder} is not a folder that install-openh264.sh made (gmp-gmpopenh264/<version> with gmpopenh264.info)`);
  }
  // Playwright gives the browser this environment instead of its own.
  const env: Record<string, string> = {};
  for (const [name, value] of Object.entries(process.env)) if (value !== undefined) env[name] = value;
  return {
    env: { ...env, MOZ_GMP_PATH: folder },
    firefoxUserPrefs: {
      'media.gmp-gmpopenh264.enabled': true,
      'media.gmp-gmpopenh264.autoupdate': false,
    },
  };
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
      use: {
        browserName: 'firefox',
        ...(openh264 === undefined ? {} : { launchOptions: firefoxWithOpenH264(openh264) }),
      },
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
