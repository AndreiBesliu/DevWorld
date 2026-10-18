// Autorun (R5): `?autorun=<scenario>&hub=<url>` runs one scenario without
// anyone at the screen and exposes its result to the test hook
// `window.__spikeResults`. The Android and Windows shells turn their intent
// extras and command-line arguments into these parameters (Tasks 10 and 11);
// the browser end-to-end tests read the hook (Task 12).
import { publish, SCENARIO_IDS, type ResultsQueue, type ResultsTarget, type ScenarioId, type ScenarioResult } from './results';
import type { ScenarioEnv } from './scenarios/context';
import { runScenario } from './scenarios/index';

export interface AutorunRequest {
  scenario: ScenarioId;
  hub: string;
}

const isScenarioId = (value: string): value is ScenarioId => (SCENARIO_IDS as readonly string[]).includes(value);

/** The request in the page's query string; the hub defaults to the page's origin (the app served by the hub). */
export function parseAutorun(search: string, origin: string): AutorunRequest | undefined {
  const params = new URLSearchParams(search);
  const scenario = params.get('autorun');
  if (scenario === null || !isScenarioId(scenario)) return undefined;
  return { scenario, hub: params.get('hub') || origin };
}

export interface AutorunDeps {
  /** Reaches the hub and builds the scenario environment; rejects with a ConnectError. */
  connect(hub: string): Promise<ScenarioEnv>;
  queue: ResultsQueue;
  target?: ResultsTarget;
}

/** Runs the requested scenario, exposes its result and uploads it. */
export async function autorun(request: AutorunRequest, deps: AutorunDeps): Promise<ScenarioResult> {
  const env = await deps.connect(request.hub);
  const result = await runScenario(request.scenario, env);
  await publish(result, deps.queue, deps.target);
  return result;
}
