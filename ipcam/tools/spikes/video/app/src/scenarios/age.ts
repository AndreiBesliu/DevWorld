// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md
// AGE, MI4 part 3 (G24, ENC-08 (3)): agerange's speed in the WebView. The
// page encrypts a fixed-seed 32 MiB buffer with typage, unwraps its file key
// from the header with typage's decryptHeader, then times agerange
// (@ipcam/core) opening the file and reading its whole plaintext, from
// memory. The phone's run decides; agerange must reach 12 Mbps there, else
// F6 downloads and decrypts whole segments on that platform (G21).
//
// Result: `{scenario: "AGE", mbps, bytes, ms}`, the figures at the top of the
// result (Figures): `mbps` (the plaintext's megabits per second), `bytes` (the
// plaintext's size) and `ms` (the time agerange took), beside the device facts
// of every result, which tell the phone's WebView from a desktop run (G24). No
// samples.
import { headerLength, openAgeRange, type RangeSource } from '@ipcam/core/agerange';
import { Decrypter, Encrypter, generateX25519Identity, identityToRecipient } from 'age-encryption';
import type { ScenarioDefinition } from './runner';

/** agerange's floor on the phone, in megabits of plaintext per second (ENC-08 (3)). */
export const MIN_MBPS = 12;

/** The plaintext of the phone's run. */
export const BENCH_BYTES = 32 * 2 ** 20;

/** The seed of the plaintext, so that every run encrypts the same bytes. */
const SEED = 0x4d493461;

/** n bytes from a fixed seed (xorshift32): the same at every run, and not compressible. */
export function seededBytes(n: number, seed = SEED): Uint8Array {
  const bytes = new Uint8Array(n);
  let x = seed;
  for (let i = 0; i < n; i++) {
    x ^= x << 13;
    x ^= x >>> 17;
    x ^= x << 5;
    bytes[i] = x & 0xff;
  }
  return bytes;
}

/** The age file in memory, read by range without copies. */
function memorySource(cipher: Uint8Array): RangeSource {
  return { size: cipher.length, read: (offset, length) => Promise.resolve(cipher.subarray(offset, offset + length)) };
}

/**
 * Times agerange on `cipher`, an age file in memory, with its file key: from
 * before openAgeRange to the end of the read of the whole plaintext, on
 * `clock` (ms). The rate is the plaintext's megabits per second, and passes
 * at 12 Mbps or more. Rejects when the file does not decrypt with the key.
 */
export async function benchAgerange(
  cipher: Uint8Array,
  fileKey: Uint8Array,
  clock: () => number,
): Promise<{ mbps: number; pass: boolean }> {
  const start = clock();
  const file = await openAgeRange(memorySource(cipher), fileKey);
  const plaintext = await file.read(0, file.plaintextSize);
  const ms = clock() - start;
  const mbps = (plaintext.length * 8) / 1000 / ms;
  return { mbps, pass: mbps >= MIN_MBPS };
}

/** The AGE scenario on a plaintext of `bytes`: BENCH_BYTES on the phone, less in the tests. */
export function ageScenario(bytes: number): ScenarioDefinition {
  return {
    async run(run) {
      const plaintext = seededBytes(bytes);
      const identity = await generateX25519Identity();
      const encrypter = new Encrypter();
      encrypter.addRecipient(await identityToRecipient(identity));
      const cipher = await encrypter.encrypt(plaintext);
      const decrypter = new Decrypter();
      decrypter.addIdentity(identity);
      const fileKey = await decrypter.decryptHeader(cipher.subarray(0, headerLength(cipher)));
      const times: number[] = [];
      const { mbps } = await benchAgerange(cipher, fileKey, () => {
        const now = run.now();
        times.push(now);
        return now;
      });
      run.figure('mbps', mbps);
      run.figure('bytes', bytes);
      run.figure('ms', times[times.length - 1]! - times[0]!);
    },
  };
}

export const AGE: ScenarioDefinition = ageScenario(BENCH_BYTES);
