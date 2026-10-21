import { afterEach, describe, expect, it, vi } from 'vitest';
import { defaultSetTimer } from './timers';

afterEach(() => {
  vi.useRealTimers();
});

describe('defaultSetTimer', () => {
  it('defaultSetTimer_firesOnceAfterTheDelay', () => {
    vi.useFakeTimers();
    const fn = vi.fn();

    defaultSetTimer(fn, 500);
    vi.advanceTimersByTime(499);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(fn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(5000);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('defaultSetTimer_cancelPreventsTheCall', () => {
    vi.useFakeTimers();
    const fn = vi.fn();

    const cancel = defaultSetTimer(fn, 500);
    cancel();
    vi.advanceTimersByTime(5000);

    expect(fn).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });
});
