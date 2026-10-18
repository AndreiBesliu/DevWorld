import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PENDING_KEY, ResultsQueue, publish, scrubText, type KeyValueStore, type ScenarioResult } from './results';
import { finish, setup, TEST_HUB } from './scenarios/fakes';
import { runScenario } from './scenarios/index';

/** localStorage as a Map. */
class MemoryStore implements KeyValueStore {
  readonly map = new Map<string, string>();

  getItem(key: string): string | null {
    return this.map.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.map.set(key, value);
  }
}

function result(scenario: ScenarioResult['scenario'], firstFrameMs = [310]): ScenarioResult {
  return {
    scenario,
    device: { platform: 'android', model: 'SM-A245F', ua: 'Mozilla/5.0 (Linux; Android 14; wv)' },
    startedAt: '2026-10-18T12:00:00.000Z',
    durationS: 52.5,
    interrupted: false,
    hubJitterMs: [3.1],
    samples: { firstFrameMs },
    notes: [],
  };
}

const created = (): Promise<Response> => Promise.resolve(new Response('{"file":"x.json"}', { status: 201 }));

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('ResultsQueue', () => {
  it('results_uploadFails_keptAndRetried', async () => {
    // The hub was closed or the Wi-Fi dropped (Review Focus 5).
    const storage = new MemoryStore();
    const fetchMock = vi.fn<typeof fetch>(() => Promise.reject(new TypeError('Failed to fetch')));
    const queue = new ResultsQueue({ storage, fetch: fetchMock, hubUrl: () => TEST_HUB });

    queue.add(result('S1'));
    expect(JSON.parse(storage.getItem(PENDING_KEY) ?? '[]')).toEqual([result('S1')]); // written before any upload
    expect(await queue.flush()).toBe(1);
    expect(JSON.parse(storage.getItem(PENDING_KEY) ?? '[]')).toEqual([result('S1')]);

    // The next launch reads what is pending, and the hub is back.
    fetchMock.mockImplementation(created);
    const next = new ResultsQueue({ storage, fetch: fetchMock, hubUrl: () => TEST_HUB });
    expect(next.pending()).toBe(1);
    expect(await next.flush()).toBe(0);
    expect(JSON.parse(storage.getItem(PENDING_KEY) ?? '[]')).toEqual([]);

    const [url, init] = fetchMock.mock.calls.at(-1) ?? [];
    expect(url).toBe(`${TEST_HUB}/spike/v1/results`);
    expect(init?.method).toBe('POST');
    // The hub reads device and scenario at the top level (Task 6).
    expect(JSON.parse(String(init?.body))).toEqual(result('S1'));
  });

  it('results_partialFailure_keepsOnlyWhatWasNotSent', async () => {
    const storage = new MemoryStore();
    let call = 0;
    const fetchMock = vi.fn<typeof fetch>(() =>
      call++ === 1 ? Promise.resolve(new Response('full', { status: 500 })) : created(),
    );
    const queue = new ResultsQueue({ storage, fetch: fetchMock, hubUrl: () => TEST_HUB });
    queue.add(result('S1', [1]));
    queue.add(result('S2', [2]));
    queue.add(result('S8', [3]));

    expect(await queue.flush()).toBe(1);

    expect(JSON.parse(storage.getItem(PENDING_KEY) ?? '[]')).toEqual([result('S2', [2])]);
  });

  it('results_resultAddedDuringAFlush_isKept', async () => {
    const storage = new MemoryStore();
    let answer: ((response: Response) => void) | undefined;
    const fetchMock = vi.fn<typeof fetch>(
      () =>
        new Promise<Response>((resolve) => {
          answer = resolve;
        }),
    );
    const queue = new ResultsQueue({ storage, fetch: fetchMock, hubUrl: () => TEST_HUB });
    queue.add(result('S1'));

    const flushing = queue.flush();
    expect(queue.flush()).toBe(flushing); // one upload at a time
    queue.add(result('S2'));
    await Promise.resolve();
    answer?.(new Response('{}', { status: 201 }));

    expect(await flushing).toBe(1);
    expect(JSON.parse(storage.getItem(PENDING_KEY) ?? '[]')).toEqual([result('S2')]);
  });

  it('results_storageUnavailable_keepsTheResultInMemory', async () => {
    // A private window or blocked site data: setItem throws.
    const storage: KeyValueStore = {
      getItem: () => null,
      setItem: () => {
        throw new DOMException('quota', 'QuotaExceededError');
      },
    };
    const fetchMock = vi.fn<typeof fetch>(() => Promise.reject(new TypeError('Failed to fetch')));
    const queue = new ResultsQueue({ storage, fetch: fetchMock, hubUrl: () => TEST_HUB });

    queue.add(result('S1'));

    expect(queue.pending()).toBe(1);
    expect(await queue.flush()).toBe(1);
  });

  it('publish_exposesTheResultAndUploadsIt', async () => {
    const target: { __spikeResults?: ScenarioResult[] } = {};
    const fetchMock = vi.fn<typeof fetch>(created);
    const queue = new ResultsQueue({ storage: new MemoryStore(), fetch: fetchMock, hubUrl: () => TEST_HUB });

    await publish(result('S1'), queue, target);
    await publish(result('S2'), queue, target);

    expect(target.__spikeResults?.map((r) => r.scenario)).toEqual(['S1', 'S2']);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(queue.pending()).toBe(0);
  });
});

describe('data minimisation (PH0-22)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it('results_containNoUrlOrIpLiteral', async () => {
    // Errors carry addresses in their messages: a browser's fetch error, a hub
    // error that names its own address. None of it may reach a result.
    const t = setup();
    t.env.openSession = () =>
      Promise.reject(new Error(`request to ${TEST_HUB}/spike/v1/offer failed, reason: connect ECONNRESET 192.168.1.20:7080`));

    const failed = await finish(runScenario('S2', t.env));
    const json = JSON.stringify(failed);

    expect(failed.notes.length).toBeGreaterThan(0); // the failure is recorded, without its addresses
    expect(json).not.toContain('192.168.1.20');
    expect(json).not.toContain('://');
    expect(json).not.toMatch(/\b\d{1,3}(\.\d{1,3}){3}\b/);
    expect(json).not.toMatch(/::|\b[0-9a-f]{1,4}(:[0-9a-f]{1,4}){3,7}\b/i); // IPv6
  });

  it('scrubText_removesUrlsAndIpLiterals', () => {
    expect(scrubText('GET http://192.168.1.20:7080/spike/v1/cameras failed')).toBe('GET <url> failed');
    expect(scrubText('connect ECONNREFUSED 10.0.0.5:7080')).toBe('connect ECONNREFUSED <ip>');
    expect(scrubText('no route to fe80::1c2b:3aff:fe4d:5e6f%en0 or [fd00::1]:7080')).toBe('no route to <ip> or <ip>');
    expect(scrubText('bind error CODEC_UNSUPPORTED at 12:30')).toBe('bind error CODEC_UNSUPPORTED at 12:30');
  });
});
