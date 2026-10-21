import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { NATIVE_METRICS_TIMEOUT_MS, readNativeMetrics, type SpikeNative } from './native';

const setTimer = (fn: () => void, ms: number): (() => void) => {
  const id = setTimeout(fn, ms);
  return () => {
    clearTimeout(id);
  };
};

const facade = (metrics: SpikeNative['metrics']): SpikeNative => ({ metrics });

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('readNativeMetrics', () => {
  it('readNativeMetrics_answers_returnsTheMetrics', async () => {
    await expect(readNativeMetrics(facade(() => Promise.resolve({ memMb: 412 })), setTimer)).resolves.toEqual({ memMb: 412 });
    expect(vi.getTimerCount()).toBe(0); // the timeout is cleared
  });

  it('readNativeMetrics_neverAnswers_naAfter3s', async () => {
    // A shell whose bridge hangs must not keep the page busy (Task 9 review).
    let outcome: unknown = 'pending';
    void readNativeMetrics(facade(() => new Promise(() => undefined)), setTimer).then((value) => {
      outcome = value;
    });

    await vi.advanceTimersByTimeAsync(NATIVE_METRICS_TIMEOUT_MS - 1);
    expect(outcome).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);
    expect(outcome).toBe('n/a');
    expect(NATIVE_METRICS_TIMEOUT_MS).toBe(3000);
  });

  it('readNativeMetrics_throwsOrRejects_na', async () => {
    const throwing = facade(() => {
      throw new Error('the bridge is gone');
    });
    await expect(readNativeMetrics(throwing, setTimer)).resolves.toBe('n/a');
    await expect(readNativeMetrics(facade(() => Promise.reject(new Error('denied'))), setTimer)).resolves.toBe('n/a');
    expect(vi.getTimerCount()).toBe(0);
  });

  it('readNativeMetrics_noShell_na', async () => {
    await expect(readNativeMetrics(undefined, setTimer)).resolves.toBe('n/a');
  });
});
