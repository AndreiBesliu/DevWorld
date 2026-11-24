// The table of the automatic run (post-release task A): for each device of the
// owner's guide, the scenarios it runs, in order. deviceplan.json, in the Go
// package tools/spikes/video/deviceplan, is the single source: spikehub reads it
// to answer what each device still lacks, and a Go test checks the guide's
// table against it. It also names the release, which every result carries.
import table from '../../deviceplan/deviceplan.json';
import type { DeviceInfo, Platform } from './device';
import { SCENARIO_IDS, type ScenarioId } from './results';

/** A step of a device's run: a scenario, or a pause that lets the phone cool down. */
export type Step = { kind: 'scenario'; id: ScenarioId } | { kind: 'pause'; ms: number };

export interface DevicePlan {
  /** `chrome`, `edge`, `firefox`, `ipcam-desktop` or `phone`, as spikehub names it. */
  id: string;
  /** What the owner reads: Chrome, Edge, Firefox, ipcam-desktop, Galaxy A54. */
  label: string;
  platform: Platform;
  /** The owner's PC (a browser or ipcam-desktop); false for the phone. */
  pc: boolean;
  steps: readonly Step[];
}

/** The release of the app, spikehub and this table, such as `spike-video-v0.1.1`. */
export const APP_VERSION: string = table.version;

const PLATFORMS: readonly Platform[] = ['android', 'windows-tauri', 'chrome', 'edge', 'firefox'];

const isScenarioId = (value: string): value is ScenarioId => (SCENARIO_IDS as readonly string[]).includes(value);

function toPlan(device: (typeof table.devices)[number]): DevicePlan {
  const platform = PLATFORMS.find((known) => known === device.platform);
  if (platform === undefined) throw new Error(`deviceplan.json: unknown platform ${device.platform}`);
  return {
    id: device.id,
    label: device.label,
    platform,
    pc: device.pc,
    steps: device.steps.map((step): Step => {
      if (step === 'pause') return { kind: 'pause', ms: table.pauseMinutes * 60_000 };
      if (!isScenarioId(step)) throw new Error(`deviceplan.json: unknown scenario ${step}`);
      return { kind: 'scenario', id: step };
    }),
  };
}

/** The devices of the guide, in its order. */
export const DEVICE_PLANS: readonly DevicePlan[] = table.devices.map(toPlan);

/** The scenarios of a plan, without its pause. */
export function scenariosOf(plan: DevicePlan): ScenarioId[] {
  return plan.steps.flatMap((step) => (step.kind === 'scenario' ? [step.id] : []));
}

const WEB_PLATFORMS: readonly Platform[] = ['chrome', 'edge', 'firefox'];

/** The OS family as spikehub and the report read it: the first word, letters and digits, lower case. */
function osFamily(os: string | undefined): string {
  const first = (os ?? '').trim().split(' ')[0] ?? '';
  return first
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '')
    .slice(0, 16);
}

/**
 * The device that a result counts for, as spikehub and the report key it (Go
 * package resultrules, Key): the platform, except for a browser on another OS
 * family than Windows, which adds the family (`chrome-android`, hub-4). A copy
 * of the Go function, pinned by deviceplan.test.ts against the cases of its test.
 */
export function deviceKey(platform: string, os: string | undefined): string {
  const family = osFamily(os);
  if (!(WEB_PLATFORMS as readonly string[]).includes(platform) || family === '' || family === 'windows') return platform;
  return `${platform}-${family}`;
}

/**
 * The plan of the device the page runs on, or undefined when it is none of
 * the guide's: the browsers count only on Windows and only when the user agent
 * names them (detectDevice takes any other browser for Chrome), the phone only
 * in its app.
 */
export function planFor(device: DeviceInfo): DevicePlan | undefined {
  const plan = DEVICE_PLANS.find((candidate) => candidate.platform === device.platform);
  if (plan === undefined || !(WEB_PLATFORMS as readonly string[]).includes(device.platform)) return plan;
  return device.os === 'Windows' && device.webview !== undefined ? plan : undefined;
}
