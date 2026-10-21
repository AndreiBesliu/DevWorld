import { argon2id } from 'hash-wasm';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup } from './fakes';
import { runScenario } from './index';

vi.mock('hash-wasm', () => ({
  argon2id: vi.fn(
    () =>
      new Promise<Uint8Array>((resolve) => {
        setTimeout(() => {
          resolve(new Uint8Array(32));
        }, 1500);
      }),
  ),
}));

beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(argon2id).mockClear();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S8', () => {
  it('S8_runsTenTimes', async () => {
    const t = setup();

    const result = await finish(runScenario('S8', t.env));

    expect(argon2id).toHaveBeenCalledTimes(10);
    for (const [options] of vi.mocked(argon2id).mock.calls) {
      // MV15: the parameters of spec §10.3, m = 64 MiB (in KiB), t = 3, p = 4, 32 bytes out.
      expect(options).toMatchObject({ memorySize: 65536, iterations: 3, parallelism: 4, hashLength: 32, outputType: 'binary' });
      expect(options.salt).toHaveLength(16);
    }
    expect(result.samples.argon2idMs).toEqual(Array<number>(10).fill(1500));
    expect(t.sessions).toHaveLength(0);
  });
});
