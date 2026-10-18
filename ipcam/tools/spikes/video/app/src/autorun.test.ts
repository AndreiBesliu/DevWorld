import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { autorun, parseAutorun } from './autorun';
import { ResultsQueue, type KeyValueStore, type ScenarioResult } from './results';
import { finish, setup, TEST_HUB } from './scenarios/fakes';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const memory = (): KeyValueStore => {
  const map = new Map<string, string>();
  return { getItem: (key) => map.get(key) ?? null, setItem: (key, value) => map.set(key, value) };
};

describe('autorun', () => {
  it('parseAutorun_readsTheScenarioAndTheHub', () => {
    expect(parseAutorun('?autorun=S1&hub=http://10.0.2.2:7080', 'http://localhost')).toEqual({
      scenario: 'S1',
      hub: 'http://10.0.2.2:7080',
    });
    // Without hub, the page's own origin: the web app served by the hub.
    expect(parseAutorun('?autorun=S3-9', 'http://localhost:7080')).toEqual({ scenario: 'S3-9', hub: 'http://localhost:7080' });
    expect(parseAutorun('', 'http://localhost:7080')).toBeUndefined();
    expect(parseAutorun('?autorun=S99', 'http://localhost:7080')).toBeUndefined();
  });

  it('autorun_runsScenarioAndExposesResults', async () => {
    const t = setup({ hub: { firstFrameMs: () => 280 } });
    const target: { __spikeResults?: ScenarioResult[] } = {};
    const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(new Response('{}', { status: 201 })));
    const queue = new ResultsQueue({ storage: memory(), fetch: fetchMock, hubUrl: () => TEST_HUB });
    const connected: string[] = [];

    const result = await finish(
      autorun(
        { scenario: 'S1', hub: TEST_HUB },
        {
          connect: (hub) => {
            connected.push(hub);
            return Promise.resolve(t.env);
          },
          queue,
          target,
        },
      ),
    );

    expect(connected).toEqual([TEST_HUB]);
    expect(target.__spikeResults).toEqual([result]);
    expect(result.scenario).toBe('S1');
    expect(result.samples.firstFrameMs).toEqual(Array<number>(20).fill(280));
    // And it went to the hub, which stores it in its results folder.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(queue.pending()).toBe(0);
  });
});
