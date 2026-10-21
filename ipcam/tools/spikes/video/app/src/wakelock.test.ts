import { describe, expect, it, vi } from 'vitest';
import { holdWakeLock, type ScreenWakeLock } from './wakelock';

class FakeDoc extends EventTarget {
  visibilityState: DocumentVisibilityState = 'visible';
}

function screenWakeLock(fail = false) {
  const sentinels: { released: boolean; release: () => Promise<void> }[] = [];
  const api: ScreenWakeLock = {
    request: vi.fn(() => {
      if (fail) return Promise.reject(new DOMException('not allowed', 'NotAllowedError'));
      const sentinel = {
        released: false,
        release() {
          sentinel.released = true;
          return Promise.resolve();
        },
      };
      sentinels.push(sentinel);
      return Promise.resolve(sentinel);
    }),
  };
  return { api, sentinels };
}

describe('holdWakeLock', () => {
  it('wakeLock_usesTheScreenWakeLockAndTakesItAgainOnReturn', async () => {
    const { api, sentinels } = screenWakeLock();
    const doc = new FakeDoc();

    const lock = await holdWakeLock({ wakeLock: api, doc });
    expect(lock.kind).toBe('screen');
    expect(api.request).toHaveBeenCalledWith('screen');

    // The browser drops the lock while the page is hidden; it is asked for again on return.
    const first = sentinels[0];
    if (first) first.released = true;
    doc.visibilityState = 'visible';
    doc.dispatchEvent(new Event('visibilitychange'));
    await Promise.resolve();
    expect(api.request).toHaveBeenCalledTimes(2);

    await lock.release();
    expect(sentinels.every((s) => s.released)).toBe(true);
    doc.dispatchEvent(new Event('visibilitychange'));
    expect(api.request).toHaveBeenCalledTimes(2); // released for good
  });

  it('wakeLock_screenLockRefused_usesTheNativeFacade', async () => {
    const { api } = screenWakeLock(true);
    const keepAwake = vi.fn(() => Promise.resolve());

    const lock = await holdWakeLock({ wakeLock: api, native: { metrics: () => Promise.resolve({}), keepAwake }, doc: new FakeDoc() });

    expect(lock.kind).toBe('native');
    expect(keepAwake).toHaveBeenLastCalledWith(true);
    await lock.release();
    expect(keepAwake).toHaveBeenLastCalledWith(false);
  });

  it('wakeLock_neither_isNa', async () => {
    const lock = await holdWakeLock({ doc: new FakeDoc() });

    expect(lock.kind).toBe('n/a');
    await expect(lock.release()).resolves.toBeUndefined();
  });
});
