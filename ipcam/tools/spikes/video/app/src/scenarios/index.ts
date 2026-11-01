// The scenarios by id, and the entry point that runs one.
import type { ScenarioId, ScenarioResult } from '../results';
import { AGE } from './age';
import type { ScenarioEnv } from './context';
import { runDefinition, type ScenarioDefinition } from './runner';
import { S1 } from './s1';
import { S10 } from './s10';
import { S2 } from './s2';
import { gridScenario } from './s3';
import { S4 } from './s4';
import { S5 } from './s5';
import { S6 } from './s6';
import { S7 } from './s7';
import { S8 } from './s8';
import { S9 } from './s9';

export const SCENARIOS: Record<ScenarioId, ScenarioDefinition> = {
  S1,
  S2,
  'S3-4': gridScenario(4),
  'S3-9': gridScenario(9),
  'S3-16': gridScenario(16),
  S4,
  S5,
  S6,
  S7,
  S8,
  S9,
  S10,
  AGE,
};

/** Runs scenario `id`; see runDefinition. */
export function runScenario(id: ScenarioId, env: ScenarioEnv): Promise<ScenarioResult> {
  return runDefinition(id, SCENARIOS[id], env);
}
