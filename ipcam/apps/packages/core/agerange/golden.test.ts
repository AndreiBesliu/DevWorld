import { describe, expect, it } from 'vitest';
import { CHUNK_SIZE, memorySource, readGoldens, sameBytes, sha256Hex } from './fixtures';
import { openAgeRange } from './index';

// ENC-07, cross-language: the Go writer's golden files (Task 2) decrypt in TS to
// the plaintext and range hashes that vectors.json lists.

const goldens = readGoldens();
const RANDOM_RANGES = 10_000;

// mulberry32: a small seeded generator, so a failing range repeats.
function seededRandom(seed: number): (n: number) => number {
  let state = seed;
  return (n) => {
    state = (state + 0x6d2b79f5) | 0;
    let t = Math.imul(state ^ (state >>> 15), 1 | state);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return Math.floor((((t ^ (t >>> 14)) >>> 0) / 2 ** 32) * n);
  };
}

describe('agerange golden files from Go', () => {
  it('lists the six golden files', () => {
    // The sizes and their order of goldenSizes in Go's writer_test.go.
    const sizes = [1, CHUNK_SIZE - 1, CHUNK_SIZE + 8, CHUNK_SIZE + 1, 3 * CHUNK_SIZE + 8, 16 * CHUNK_SIZE + 17];
    expect(goldens.map(({ vector }) => vector.plaintextSize)).toEqual(sizes);
  });

  it.each(goldens.map((g) => [g.vector.file, g] as const))('agerange_goldenFromGo_matchesPlaintext %s', async (_name, g) => {
    const r = await openAgeRange(memorySource(g.file), g.fileKey);
    expect(r.plaintextSize).toBe(g.vector.plaintextSize);
    expect(sha256Hex(await r.read(0, r.plaintextSize))).toBe(g.vector.plaintextSha256);

    expect(g.vector.ranges).toHaveLength(64);
    for (const { offset, length, sha256 } of g.vector.ranges) {
      expect(sha256Hex(await r.read(offset, length)), `range [${offset}, +${length})`).toBe(sha256);
    }
  });

  // Ranges as Go's TestRecording_RandomRangesMatchStream draws them: a uniform
  // offset in [0, size] and a length of up to three chunks.
  it.each(goldens.map((g) => [g.vector.file, g] as const))(
    'agerange_randomRanges_matchFullDecrypt %s',
    { timeout: 120_000 },
    async (_name, g) => {
      const r = await openAgeRange(memorySource(g.file), g.fileKey);
      const size = r.plaintextSize;
      const full = await r.read(0, size);
      expect(sha256Hex(full)).toBe(g.vector.plaintextSha256);

      const random = seededRandom(size);
      for (let i = 0; i < RANDOM_RANGES; i++) {
        const offset = random(size + 1);
        const length = random(Math.min(size - offset, 3 * CHUNK_SIZE) + 1);
        const got = await r.read(offset, length);
        if (!sameBytes(got, full.subarray(offset, offset + length))) {
          expect.fail(`range ${i}: [${offset}, +${length}) differs from the full decrypt`);
        }
      }
    },
  );
});
