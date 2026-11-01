import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { memorySource, readGoldens, sameBytes, sha256Hex } from './fixtures';
import { openAgeRange } from './index';

// Any range inside the plaintext reads as the same slice of the full decrypt.
// fast-check leans towards the edges (empty ranges, offset 0, the last byte),
// which the uniform ranges of golden.test.ts rarely hit.

const goldens = readGoldens();

describe('agerange properties', () => {
  it.each(goldens.map((g) => [g.vector.file, g] as const))(
    'agerange_property_anyRangeEqualsSlice %s',
    { timeout: 60_000 },
    async (_name, g) => {
      const r = await openAgeRange(memorySource(g.file), g.fileKey);
      const size = r.plaintextSize;
      const full = await r.read(0, size);
      expect(sha256Hex(full)).toBe(g.vector.plaintextSha256);

      const range = fc
        .integer({ min: 0, max: size })
        .chain((offset) => fc.tuple(fc.constant(offset), fc.integer({ min: 0, max: size - offset })));
      await fc.assert(
        fc.asyncProperty(range, async ([offset, length]) => {
          expect(sameBytes(await r.read(offset, length), full.subarray(offset, offset + length))).toBe(true);
        }),
        { numRuns: 200 },
      );
    },
  );
});
