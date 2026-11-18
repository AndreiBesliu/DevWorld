// S8, MV15: Argon2id in the WebView with the parameters of spec §10.3, ten
// runs: m = 64 MiB (65536 KiB), t = 3, p = 4, a 32-byte output, hash-wasm.
//
// Samples: `argon2idMs`.
import { argon2id } from 'hash-wasm';
import type { ScenarioDefinition } from './runner';

const RUNS = 10;
const RUN_TIMEOUT_MS = 120_000;

export const S8: ScenarioDefinition = {
  async run(run) {
    for (let i = 1; i <= RUNS; i += 1) {
      const t0 = run.now();
      const hash = await run.within(
        argon2id({
          password: crypto.getRandomValues(new Uint8Array(32)),
          salt: crypto.getRandomValues(new Uint8Array(16)),
          parallelism: 4,
          iterations: 3,
          memorySize: 65536,
          hashLength: 32,
          outputType: 'binary',
        }),
        RUN_TIMEOUT_MS,
      );
      if (hash === 'timeout') run.note(`run ${i}: no result within ${RUN_TIMEOUT_MS / 1000} s`);
      else run.sample('argon2idMs', run.now() - t0);
    }
  },
};
