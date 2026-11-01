// Autorun (R5): `?autorun=<scenario>&hub=<url>` runs one scenario without
// anyone at the screen and exposes its result to the test hook
// `window.__spikeResults`, or why there is none to `window.__spikeErrors`. The
// Android and Windows shells turn their intent extras and command-line
// arguments into these parameters (Tasks 10 and 11); the browser end-to-end
// tests read the hooks (Task 12).
//
// On Android only a cold start honours the extras. The metrics plugin reads
// them once, when the Bridge is created, and the Activity's saved state keeps
// the scenario until the page has been given it, so an Activity that the system
// creates again before that still runs it. An Activity restored after its page
// had the scenario (created again, or started again after its process died,
// from the launch Intent that the system kept with its extras) ignores them,
// and so does a warm start: the scenario does not run again, and an
// interrupted run is repeated by starting the app again. The
// spike's Activity is exported and takes any `hub` URL, so any app on the
// device can start a run against a hub of its choosing. That is by design for
// a spike that is never published (SPIKE-ONLY); the results hold no addresses
// or serial numbers (PH0-22).
import { ConnectError } from './hubhttp';
import {
  publish,
  SCENARIO_IDS,
  scrubText,
  type ResultsQueue,
  type ResultsTarget,
  type ScenarioId,
  type ScenarioResult,
} from './results';
import type { ScenarioEnv } from './scenarios/context';
import { runScenario } from './scenarios/index';

export interface AutorunRequest {
  scenario: ScenarioId;
  hub: string;
}

/** Why an autorun gives no result. It never names an address (PH0-22). */
export interface AutorunError {
  /** The scenario the query string asked for. */
  scenario: string;
  /**
   * `unknown-scenario`: there is no such scenario; `connect`: the hub could
   * not be reached (a ConnectError, also from within the scenario);
   * `scenario`: the scenario threw.
   */
  kind: 'unknown-scenario' | 'connect' | 'scenario';
  /** The ConnectError's kind (`timeout`, `refused`, `rejected`), else the name of the error thrown. */
  code?: string;
}

/** The test hook of autorun failures (`window.__spikeErrors`). */
export interface ErrorsTarget {
  __spikeErrors?: AutorunError[];
}

const isScenarioId = (value: string): value is ScenarioId => (SCENARIO_IDS as readonly string[]).includes(value);

/**
 * The request in the page's query string, the hub defaulting to the page's
 * origin (the app served by the hub); an AutorunError for a scenario that does
 * not exist; undefined without `autorun`.
 */
export function parseAutorun(search: string, origin: string): AutorunRequest | AutorunError | undefined {
  const params = new URLSearchParams(search);
  const scenario = params.get('autorun');
  if (scenario === null) return undefined;
  if (!isScenarioId(scenario)) return { scenario: scrubText(scenario), kind: 'unknown-scenario' };
  return { scenario, hub: params.get('hub') || origin };
}

/** Exposes an autorun failure to the test hook. */
export function reportAutorunError(error: AutorunError, target: ErrorsTarget = globalThis as ErrorsTarget): void {
  (target.__spikeErrors ??= []).push(error);
}

/** `code` of an AutorunError: what kind of ConnectError, or which error. */
function codeOf(error: unknown): { code?: string } {
  if (error instanceof ConnectError) return { code: error.kind };
  return error instanceof Error ? { code: scrubText(error.name) } : {};
}

export interface AutorunDeps {
  /** Reaches the hub and builds the scenario environment; rejects with a ConnectError. */
  connect(hub: string): Promise<ScenarioEnv>;
  queue: ResultsQueue;
  target?: ResultsTarget & ErrorsTarget;
}

/**
 * Runs the requested scenario, exposes its result and uploads it. When the hub
 * cannot be reached or the scenario throws, the failure goes to the errors
 * hook and the promise rejects.
 */
export async function autorun(request: AutorunRequest, deps: AutorunDeps): Promise<ScenarioResult> {
  const fail = (kind: AutorunError['kind'], error: unknown): void => {
    reportAutorunError({ scenario: request.scenario, kind, ...codeOf(error) }, deps.target);
  };
  let env: ScenarioEnv;
  try {
    env = await deps.connect(request.hub);
  } catch (error) {
    fail('connect', error);
    throw error;
  }
  let result: ScenarioResult;
  try {
    result = await runScenario(request.scenario, env);
  } catch (error) {
    fail(error instanceof ConnectError ? 'connect' : 'scenario', error);
    throw error;
  }
  await publish(result, deps.queue, deps.target);
  return result;
}
