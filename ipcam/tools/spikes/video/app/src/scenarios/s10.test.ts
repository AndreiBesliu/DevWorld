import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup } from './fakes';
import { runScenario } from './index';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S10', () => {
  it('S10_recordsThePairTypesAndThePromptAnswer', async () => {
    const asked: string[] = [];
    const t = setup({
      hub: { pair: { local: 'host', remote: 'prflx' } },
      ask: (question) => {
        asked.push(question);
        return Promise.resolve(false);
      },
    });

    const result = await finish(runScenario('S10', t.env));

    expect(result.notes).toEqual(expect.arrayContaining(['pair: host/prflx', 'prompt: no']));
    expect(asked).toEqual([t.env.t('s10Question')]);
  });

  it('S10_withoutAnyoneToAsk_recordsThePromptAsNa', async () => {
    // Autorun: nobody can answer, and no pair was reported either.
    const t = setup();

    const result = await finish(runScenario('S10', t.env));

    expect(result.notes).toEqual(expect.arrayContaining(['pair: n/a', 'prompt: n/a']));
  });
});
