// The automatic run of post-release task A, end to end: the real hub and the
// real app in Chrome, with the user agent of Chrome on Windows, the device the
// guide runs it on. «Rulează tot pentru acest dispozitiv» takes Chrome's list
// from the device table and skips what spikehub already holds complete.
//
// The run is kept short by the hub's results, not by shortening a scenario:
// before the button is pressed, the test stores complete results of Chrome's
// long scenarios (S1, S2, S3-9, S3-16 and S4) through POST /spike/v1/results,
// as v0.1.0 stored the owner's, so the run measures S10, S7 and S9 for real.
// S7 is interrupted by a hidden page in the middle (the app's own visibility
// rule) and runs again when the page is visible.
import { expect, test, type Page } from '@playwright/test';
import type { ScenarioResult } from '../app/src/results';

declare global {
  interface Window {
    __spikeResults?: ScenarioResult[];
    /** The banner texts the stage showed (recordNotices). */
    __e2eNotices?: string[];
    /** Hides or shows the page, as a minimised or covered window does (pageVisibility). */
    __e2eVisible?: (visible: boolean) => void;
  }
}

const CHROME_WINDOWS =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';

/** Runs in the page before the app: a switch for document.visibilityState, and a record of the stage's banner. */
function pageVisibility(): void {
  let state: DocumentVisibilityState = 'visible';
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => state === 'hidden' });
  window.__e2eVisible = (visible) => {
    state = visible ? 'visible' : 'hidden';
    document.dispatchEvent(new Event('visibilitychange'));
  };
  const notices: string[] = [];
  window.__e2eNotices = notices;
  new MutationObserver(() => {
    const text = document.querySelector('.stage .notice')?.textContent ?? '';
    if (text !== '' && notices[notices.length - 1] !== text) notices.push(text);
  }).observe(document, { subtree: true, childList: true, characterData: true });
}

/** A complete result of Chrome on Windows, as the app uploads it. */
function storedResult(scenario: string): ScenarioResult {
  return {
    scenario,
    device: { platform: 'chrome', os: 'Windows', webview: 'Chrome/141.0.0.0', ua: CHROME_WINDOWS },
    startedAt: '2026-11-17T09:00:00.000Z',
    durationS: 60,
    interrupted: false,
    hubJitterMs: [],
    samples: scenario === 'S4' ? { tiles: [16] } : {},
    notes: [],
  } as ScenarioResult;
}

interface ProgressAnswer {
  complete: { key: string; scenarios: string[] }[];
  devices: { id: string; remaining: string[] }[];
}

const results = (page: Page): Promise<ScenarioResult[]> => page.evaluate(() => window.__spikeResults ?? []);

test('«Rulează tot pentru acest dispozitiv» runs Chrome’s list, skips what is complete and resumes a hidden scenario', { tag: '@chrome' }, async ({ browser, request }) => {
  test.setTimeout(6 * 60_000);
  const before = (await (await request.get('/spike/v1/progress')).json()) as ProgressAnswer;
  expect(
    before.complete.filter((group) => group.key === 'chrome'),
    'the hub already holds results of Chrome on Windows: start it with an empty results folder',
  ).toEqual([]);
  for (const scenario of ['S1', 'S2', 'S3-9', 'S3-16', 'S4']) {
    const stored = await request.post('/spike/v1/results', { data: storedResult(scenario) });
    expect(stored.status()).toBe(201);
  }

  const context = await browser.newContext({ userAgent: CHROME_WINDOWS });
  const page = await context.newPage();
  await page.addInitScript(pageVisibility);
  await page.goto('/');
  await expect(page.locator('.autorun .plan')).toContainText('Acest dispozitiv: Chrome.');
  await expect(page.locator('.autorun .advice')).toContainText('Windows + ←');
  await expect(page.locator('.autorun .remaining').first()).toHaveText(
    'Rămase pe acest PC: Chrome (S10, S7, S9), Edge, Firefox, ipcam-desktop',
  );

  await page.locator('.autorun .run-all').click();
  // The only question, at the start: S10's.
  await page.locator('.stage .question button', { hasText: 'Nu' }).click({ timeout: 60_000 });
  // S7 starts; the window is minimised in the middle of it, and comes back 5 s later.
  await page.waitForFunction(() => document.querySelector('.stage.active .status')?.textContent?.startsWith('S7:'), undefined, {
    timeout: 60_000,
  });
  await page.waitForTimeout(1500);
  await page.evaluate(() => window.__e2eVisible?.(false));
  await page.waitForFunction(() => (window.__spikeResults ?? []).some((result) => result.scenario === 'S7' && result.interrupted), undefined, {
    timeout: 30_000,
  });
  await page.waitForTimeout(5000);
  expect((await results(page)).map((result) => result.scenario)).toEqual(['S10', 'S7']); // paused while hidden
  // The run starts S7 again by itself: its row says why, without the start page's «Repetă».
  await expect(page.locator('.scenario[data-scenario="S7"] .status')).toHaveText('întrerupt: fereastra nu mai era vizibilă.');
  await page.evaluate(() => window.__e2eVisible?.(true));

  await expect(page.locator('.autorun .run-status')).toContainText('Gata: toate scenariile acestui dispozitiv au rezultate.', {
    timeout: 4 * 60_000,
  });
  const ran = await results(page);
  console.log(
    `e2e: automatic run: ${ran.map((result) => `${result.scenario}${result.interrupted ? ' (interrupted)' : ''}`).join(', ')}`,
  );
  expect(ran.map((result) => [result.scenario, result.interrupted])).toEqual([
    ['S10', false],
    ['S7', true],
    ['S7', false],
    ['S9', false],
  ]);
  for (const result of ran) {
    expect(result.appVersion).toBe('spike-video-v0.1.2');
    expect(result.notes.filter((note) => note.startsWith('failed'))).toEqual([]);
  }
  expect(ran[0]?.notes).toContain('prompt: no');
  expect(await page.evaluate(() => window.__e2eNotices ?? [])).toContain(
    'S7 s-a întrerupt (1 din 3): fereastra nu mai era vizibilă. A pornit din nou de la început, iar rularea continuă.',
  );

  // Chrome is complete now: the page and the hub say so.
  await expect(page.locator('.autorun .remaining').first()).toHaveText('Rămase pe acest PC: Edge, Firefox, ipcam-desktop');
  const after = (await (await request.get('/spike/v1/progress')).json()) as ProgressAnswer;
  expect(after.devices.find((device) => device.id === 'chrome')?.remaining).toEqual([]);

  // A second press runs nothing.
  await page.locator('.autorun .run-all').click();
  await expect(page.locator('.autorun .run-status')).toContainText('Totul e gata pe acest dispozitiv.');
  expect(await results(page)).toHaveLength(4);
  await context.close();
});

test('an unknown device gets no button and a line that says why', { tag: '@chrome' }, async ({ page }) => {
  // The test browser's own user agent: Chrome on Linux, which the guide does not run.
  await page.goto('/');

  await expect(page.locator('.autorun .unknown')).toContainText('Rularea automată nu e disponibilă aici');
  await expect(page.locator('.autorun .run-all')).toHaveCount(0);
});
