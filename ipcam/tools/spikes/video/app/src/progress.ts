// What spikehub already holds (GET /spike/v1/progress, cmd/spikehub/progress.go):
// the scenarios each device has completed, which the automatic run skips, and
// what each device of the guide still lacks, which the page lists under
// «Rămase pe acest PC:». It reads every release's results, v0.1.0's included.
// Only the device table's scenarios count (TABLE_SCENARIOS): one that the app
// lists outside the table, the recorder spike's AGE, is left out of the answer.
import { DEVICE_PLANS, deviceKey, scenariosOf, TABLE_SCENARIOS, type DevicePlan } from './deviceplan';
import { requestHub } from './hubhttp';
import type { Translate } from './i18n';
import type { ScenarioId } from './results';

/** What one device (a key, and a model for the shells) has completed. */
export interface ProgressGroup {
  key: string;
  platform: string;
  model?: string;
  scenarios: ScenarioId[];
}

/** A device of the guide and the scenarios of its run that are left, in order. */
export interface ProgressDevice {
  id: string;
  label: string;
  pc: boolean;
  remaining: ScenarioId[];
}

export interface Progress {
  /** spikehub's release. */
  version: string;
  complete: ProgressGroup[];
  devices: ProgressDevice[];
}

const PROGRESS_TIMEOUT_MS = 10_000;

/** A scenario of the device table; an unknown id, or one run only from the list (AGE), is not. */
const isTableScenario = (value: unknown): value is ScenarioId => (TABLE_SCENARIOS as readonly unknown[]).includes(value);
const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const scenarios = (value: unknown): ScenarioId[] => (Array.isArray(value) ? value.filter(isTableScenario) : []);
const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null;

function readProgress(body: unknown): Progress {
  if (!isObject(body) || !Array.isArray(body.complete) || !Array.isArray(body.devices)) {
    throw new Error('the progress is not an object with complete and devices');
  }
  return {
    version: text(body.version),
    complete: body.complete.filter(isObject).map((group) => ({
      key: text(group.key),
      platform: text(group.platform),
      ...(typeof group.model === 'string' && group.model !== '' ? { model: group.model } : {}),
      scenarios: scenarios(group.scenarios),
    })),
    devices: body.devices.filter(isObject).map((device) => ({
      id: text(device.id),
      label: text(device.label),
      pc: device.pc === true,
      remaining: scenarios(device.remaining),
    })),
  };
}

/**
 * What the hub holds. Rejects with a ConnectError like the camera list; a hub
 * older than v0.1.1 has no such route and answers 404 (`rejected`).
 */
export function fetchProgress(hubUrl: string): Promise<Progress> {
  return requestHub(
    hubUrl.replace(/\/+$/, ''),
    '/spike/v1/progress',
    { cache: 'no-store' },
    async (response) => readProgress(await response.json()),
    PROGRESS_TIMEOUT_MS,
  );
}

/**
 * The scenarios of the device table that this device has completed: the
 * results of its key (its platform, and the OS family for a browser elsewhere
 * than on Windows), and of its model when the shell reports one, as the hub
 * stores it. A scenario outside the table (AGE) is never complete here.
 */
export function completeFor(progress: Progress, device: { platform: string; os?: string }, model?: string): Set<ScenarioId> {
  const key = deviceKey(device.platform, device.os);
  const done = new Set<ScenarioId>();
  for (const group of progress.complete) {
    if (group.key !== key || (model !== undefined && group.model !== undefined && group.model !== model)) continue;
    for (const id of group.scenarios) if (isTableScenario(id)) done.add(id);
  }
  return done;
}

/** A device, and the scenarios left in brackets when part of its run is done. */
function describeLeft(device: ProgressDevice): string {
  const plan = DEVICE_PLANS.find((candidate) => candidate.id === device.id);
  const whole = plan !== undefined && device.remaining.length >= scenariosOf(plan).length;
  return whole ? device.label : `${device.label} (${device.remaining.join(', ')})`;
}

/**
 * «Rămase pe acest PC:» with the PC's devices that lack complete results, and
 * the same line for the phone. `own` is the device the page runs on, with the
 * scenarios that it has completed as its run counts them (completeFor, with
 * the shell's model): they replace the hub's answer for it, which merges every
 * model of its platform (another phone's or the emulator's runs).
 */
export function remainingLines(
  progress: Progress,
  t: Translate,
  own?: { plan: DevicePlan; complete: ReadonlySet<ScenarioId> },
): string[] {
  const guide = progress.devices.map((device) =>
    own !== undefined && device.id === own.plan.id
      ? { ...device, remaining: scenariosOf(own.plan).filter((id) => !own.complete.has(id)) }
      : device,
  );
  return [true, false].map((pc) => {
    const left = guide.filter((device) => device.pc === pc && device.remaining.length > 0);
    const devices = left.length === 0 ? t('remainingNone') : left.map(describeLeft).join(', ');
    return t(pc ? 'remainingPc' : 'remainingPhone', { devices });
  });
}
