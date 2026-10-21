import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { autorun, parseAutorun, type AutorunError } from './autorun';
import { ConnectError } from './hubhttp';
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
  });

  it('parseAutorun_unknownScenario_isAnErrorWithoutAddresses', () => {
    expect(parseAutorun('?autorun=S99', 'http://localhost:7080')).toEqual({ scenario: 'S99', kind: 'unknown-scenario' });
    expect(parseAutorun('?autorun=http://192.168.1.20:7080/x', 'http://localhost:7080')).toEqual({
      scenario: '<url>',
      kind: 'unknown-scenario',
    });
  });

  it('autorun_hubUnreachable_exposesTheErrorWithoutTheUrl', async () => {
    // Tasks 10-12 read the error instead of waiting for a result that never comes.
    const target: { __spikeResults?: ScenarioResult[]; __spikeErrors?: AutorunError[] } = {};
    const queue = new ResultsQueue({ storage: memory(), hubUrl: () => TEST_HUB });

    const failed = autorun(
      { scenario: 'S1', hub: TEST_HUB },
      { connect: () => Promise.reject(new ConnectError('refused', TEST_HUB, 'Failed to fetch')), queue, target },
    );

    await expect(failed).rejects.toBeInstanceOf(ConnectError);
    expect(target.__spikeErrors).toEqual([{ scenario: 'S1', kind: 'connect', code: 'refused' }]);
    expect(target.__spikeResults ?? []).toEqual([]);
    expect(JSON.stringify(target.__spikeErrors)).not.toContain('192.168.1.20');
  });

  it('autorun_scenarioThrows_exposesTheError', async () => {
    const t = setup();
    t.env.holdWakeLock = () => Promise.reject(new TypeError('wake lock exploded'));
    const target: { __spikeErrors?: AutorunError[] } = {};
    const queue = new ResultsQueue({ storage: memory(), hubUrl: () => TEST_HUB });

    const failed = autorun({ scenario: 'S8', hub: TEST_HUB }, { connect: () => Promise.resolve(t.env), queue, target });

    await expect(failed).rejects.toBeInstanceOf(TypeError);
    expect(target.__spikeErrors).toEqual([{ scenario: 'S8', kind: 'scenario', code: 'TypeError' }]);
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
