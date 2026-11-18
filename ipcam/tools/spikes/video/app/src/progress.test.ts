import { afterEach, describe, expect, it, vi } from 'vitest';
import { DEVICE_PLANS } from './deviceplan';
import { createI18n } from './i18n';
import { completeFor, fetchProgress, remainingLines, type Progress } from './progress';
import { SCENARIO_IDS } from './results';
import { ConnectError } from './session';

const HUB = 'http://192.168.1.20:7080';

/** What spikehub answers (cmd/spikehub/progress.go), with Chrome done by hand on 2026-11-17. */
const ANSWER = {
  version: 'spike-video-v0.1.1',
  complete: [
    { key: 'android', platform: 'android', model: 'Google sdk_gphone64_x86_64', scenarios: ['S1', 'S2'] },
    { key: 'android', platform: 'android', model: 'samsung SM-A245F', scenarios: ['S10'] },
    { key: 'chrome', platform: 'chrome', scenarios: ['S1', 'S2', 'S3-9', 'S3-16', 'S4', 'S7', 'S9', 'S10'] },
    { key: 'chrome-linux', platform: 'chrome', scenarios: ['S7'] },
    { key: 'firefox', platform: 'firefox', scenarios: ['S10', 'S1'] },
  ],
  devices: [
    { id: 'chrome', label: 'Chrome', pc: true, remaining: [] },
    { id: 'edge', label: 'Edge', pc: true, remaining: ['S10', 'S1', 'S2', 'S3-9', 'S9'] },
    { id: 'firefox', label: 'Firefox', pc: true, remaining: ['S2', 'S3-9', 'S9'] },
    { id: 'ipcam-desktop', label: 'ipcam-desktop', pc: true, remaining: ['S10', 'S1', 'S2', 'S3-16', 'S4', 'S7', 'S9'] },
    { id: 'phone', label: 'Galaxy A24', pc: false, remaining: ['S3-9', 'S3-4', 'S4', 'S5', 'S6', 'S7', 'S8', 'S9'] },
  ],
};

afterEach(() => {
  vi.unstubAllGlobals();
});

async function progress(): Promise<Progress> {
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json(ANSWER))));
  return fetchProgress(HUB);
}

describe('the hub progress', () => {
  it('fetchProgress_readsTheHubsAnswer', async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(Response.json({ ...ANSWER, complete: [...ANSWER.complete, { key: 'x', scenarios: ['S99', 7] }, 'junk'] })),
    );
    vi.stubGlobal('fetch', fetchMock);

    const read = await fetchProgress(`${HUB}/`);

    expect(fetchMock.mock.calls[0]?.[0]).toBe(`${HUB}/spike/v1/progress`);
    expect(read.version).toBe('spike-video-v0.1.1');
    expect(read.complete).toHaveLength(6);
    // Unknown scenario ids and entries that are not groups are left out.
    expect(read.complete[5]).toEqual({ key: 'x', platform: '', scenarios: [] });
    expect(read.devices).toHaveLength(5);
  });

  it('fetchProgress_oldHub_isRejected', async () => {
    // spikehub v0.1.0 has no /spike/v1/progress: its file server answers 404.
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(new Response('404 page not found', { status: 404 }))));

    const failure = await fetchProgress(HUB).catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(ConnectError);
    expect(failure).toMatchObject({ kind: 'rejected', url: HUB });
  });

  it('skip_completeForTheDevice_countsItsOwnResultsOnly', async () => {
    const read = await progress();

    // Chrome on Windows: everything of 2026-11-17, not the Linux runs.
    expect([...completeFor(read, { platform: 'chrome', os: 'Windows' })].sort()).toEqual(
      ['S1', 'S10', 'S2', 'S3-16', 'S3-9', 'S4', 'S7', 'S9'].sort(),
    );
    expect([...completeFor(read, { platform: 'edge', os: 'Windows' })]).toEqual([]);
    // The phone by its model, once the shell has told it; without it, every phone's.
    expect([...completeFor(read, { platform: 'android', os: 'Android' }, 'samsung SM-A245F')]).toEqual(['S10']);
    expect([...completeFor(read, { platform: 'android', os: 'Android' })].sort()).toEqual(['S1', 'S10', 'S2']);
  });

  it('remaining_listsThePcDevicesLeftAndThePhone', async () => {
    const read = await progress();
    const { t } = createI18n({ lang: 'ro' });

    expect(remainingLines(read, t)).toEqual([
      'Rămase pe acest PC: Edge, Firefox (S2, S3-9, S9), ipcam-desktop',
      'Rămase pe telefon: Galaxy A24 (S3-9, S3-4, S4, S5, S6, S7, S8, S9)',
    ]);

    const done = { ...read, devices: read.devices.map((device) => ({ ...device, remaining: [] })) };
    expect(remainingLines(done, t)).toEqual(['Rămase pe acest PC: nimic, totul e gata', 'Rămase pe telefon: nimic, totul e gata']);
  });

  it('remaining_onThePhone_countsOnlyItsModel', async () => {
    // The phone's run skips by its model (completeFor with the shell's model);
    // «Rămase pe telefon» on its page counts the same way. The emulator's runs
    // in the results folder do not make the A24's scenarios done.
    const read = await progress();
    const { t } = createI18n({ lang: 'ro' });
    const phone = DEVICE_PLANS.find((plan) => plan.id === 'phone');
    if (!phone) throw new Error('no phone plan');
    const emulatorDidAll = {
      ...read,
      complete: [...read.complete, { key: 'android', platform: 'android', model: 'Google sdk_gphone64_x86_64', scenarios: [...SCENARIO_IDS] }],
      devices: read.devices.map((device) => (device.id === 'phone' ? { ...device, remaining: [] } : device)),
    };
    const a24 = completeFor(emulatorDidAll, { platform: 'android', os: 'Android' }, 'samsung SM-A245F');

    expect(remainingLines(emulatorDidAll, t, { plan: phone, complete: a24 })).toEqual([
      'Rămase pe acest PC: Edge, Firefox (S2, S3-9, S9), ipcam-desktop',
      'Rămase pe telefon: Galaxy A24 (S1, S2, S3-9, S3-4, S4, S5, S6, S7, S8, S9)',
    ]);
    // Without its own device (a browser page knows no phone model), the hub's answer.
    expect(remainingLines(emulatorDidAll, t)[1]).toBe('Rămase pe telefon: nimic, totul e gata');
  });
});
