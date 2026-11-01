// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md
//
// MI9's Chrome leg (G26, CTR-12's «afișează»), for the recorder spike's mi9
// job: the page that mi9page serves at MI9_PAGE plays a recording made by
// ipcam-hub over the data channel. It asks the hub side for the plaintext
// fMP4 over bulk/<id> channels, appends it to MSE, and makes 20 timeline
// seeks, each timed from its request until the first IDR's bytes arrive
// (seekMs) and until the first requestVideoFrameCallback after it
// (displayMs). The test prints one line, "mi9: {"transport":"chrome",…}",
// which the job turns into a notice; it is skipped unless MI9_PAGE is set.
import { expect, test } from '@playwright/test';

/** What the MI9 page leaves when it is done. */
interface Mi9Result {
  seekMs: number[];
  displayMs: number[];
}

declare global {
  interface Window {
    /** The MI9 page's result, or why it has none (cmd/mi9page/web/index.html). */
    __mi9Result?: Mi9Result;
    __mi9Error?: string;
  }
}

const SEEKS = 20;
/** GitHub keeps an annotation of at most about 4,000 characters; each notice line stays within 3,500 bytes after annotate.sh's escaping (C32). */
const MAX_NOTICE_BYTES = 3500;

test('mi9: shows the first frame after each of 20 seeks @chrome', async ({ page }) => {
  const url = process.env.MI9_PAGE;
  test.skip(url === undefined, 'MI9_PAGE is unset: the recorder spike mi9 job sets it');
  await page.goto(url ?? '');
  await page.waitForFunction(() => window.__mi9Result !== undefined || window.__mi9Error !== undefined, null, { timeout: 4 * 60_000 });
  const error = await page.evaluate(() => window.__mi9Error ?? null);
  expect(error).toBeNull();
  const result = await page.evaluate(() => window.__mi9Result ?? null);
  expect(result).not.toBeNull();
  const { seekMs, displayMs } = result ?? { seekMs: [], displayMs: [] };
  expect(seekMs).toHaveLength(SEEKS);
  expect(displayMs).toHaveLength(SEEKS);
  for (const [i, shown] of displayMs.entries()) {
    const seek = seekMs[i] ?? Number.NaN;
    expect(seek).toBeGreaterThanOrEqual(0);
    expect(shown).toBeGreaterThanOrEqual(seek);
  }
  const line = JSON.stringify({ transport: 'chrome', displayMs, seekMs });
  expect(Buffer.byteLength(line.replaceAll('%', '%25'))).toBeLessThanOrEqual(MAX_NOTICE_BYTES);
  console.log(`mi9: ${line}`);
});
