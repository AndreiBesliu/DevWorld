import { describe, expect, it } from 'vitest';
import { CHUNK_SIZE, memorySource, readGoldens, sameBytes, sha256Hex, type Golden } from './fixtures';
import { AgeRangeError, headerLength, openAgeRange, type RangeSource } from './index';

const goldens = readGoldens();

function golden(plaintextSize: number): Golden {
  const g = goldens.find(({ vector }) => vector.plaintextSize === plaintextSize);
  if (!g) throw new Error(`no golden file of ${plaintextSize} bytes`);
  return g;
}

// The kind of the AgeRangeError that run throws or rejects with; 'none' if it
// succeeds. Any other error fails the test.
async function errorKind(run: () => unknown): Promise<string> {
  try {
    await run();
  } catch (err) {
    if (err instanceof AgeRangeError) return err.kind;
    throw err;
  }
  return 'none';
}

const latin1 = (bytes: Uint8Array): string => Buffer.from(bytes).toString('latin1');

describe('headerLength', () => {
  it.each(goldens.map((g) => [g.vector.file, g] as const))('agerange_headerLength_findsMacLine %s', async (_name, g) => {
    const h = headerLength(g.file);
    const header = latin1(g.file.subarray(0, h));
    expect(header.startsWith('age-encryption.org/v1\n')).toBe(true);
    expect(header.endsWith('\n')).toBe(true);
    expect(header.slice(header.lastIndexOf('\n', h - 2) + 1)).toMatch(/^--- [A-Za-z0-9+/]{43}\n$/);

    // The payload after it is the 16-byte nonce and one 16-byte tag per chunk.
    const size = g.vector.plaintextSize;
    expect(g.file.length).toBe(h + 16 + size + 16 * Math.max(1, Math.ceil(size / CHUNK_SIZE)));

    expect(headerLength(g.file.subarray(0, h))).toBe(h);
    // A prefix that ends before the header does: the caller reads more.
    expect(await errorKind(() => headerLength(g.file.subarray(0, h - 1)))).toBe('truncated');
  });

  it('agerange_headerLength_rejectsNonAgePrefix', async () => {
    const notAge = new TextEncoder().encode('this is no age file, though\n--- it has a line like a MAC line\n');
    expect(await errorKind(() => headerLength(notAge))).toBe('header');
  });

  it('agerange_headerLength_tellsAShortPrefixFromAFileThatIsNotAge', async () => {
    // A caller reads a prefix of the file, then hands typage's decryptHeader the
    // first headerLength bytes: `truncated` says to read more, `header` that no
    // amount of reading helps.
    const g = goldens[0]!;
    const h = headerLength(g.file);
    for (const n of [0, 5, 'age-encryption.org/v1\n'.length, h - 1]) {
      expect(await errorKind(() => headerLength(g.file.subarray(0, n))), `the first ${n} bytes`).toBe('truncated');
    }
    const notAge = new TextEncoder().encode('age-encryption.org/v2\n--- AAAA\n');
    expect(await errorKind(() => headerLength(notAge))).toBe('header');
    expect(await errorKind(() => headerLength(notAge.subarray(0, 21)))).toBe('header');
  });
});

describe('openAgeRange', () => {
  // Four chunks, the last one holding 8 bytes, after the header and the 16-byte nonce.
  const g = golden(3 * CHUNK_SIZE + 8);
  const payload = g.file.length - (3 * CHUNK_SIZE + 8) - 4 * 16;
  const h = payload - 16;

  it.each([
    ['inside the header', h - 1],
    ['inside the nonce', h + 8],
    ['after the nonce', payload],
    ['inside the second chunk', payload + CHUNK_SIZE + 16 + 8],
    ['inside the third chunk', payload + 3 * (CHUNK_SIZE + 16) - 10],
    ['at the final chunk', payload + 3 * (CHUNK_SIZE + 16)],
    ['one byte short', g.file.length - 1],
  ])('agerange_rejectsTruncatedFile cut %s', async (_name, cut) => {
    const kind = await errorKind(async () => {
      const r = await openAgeRange(memorySource(g.file.subarray(0, cut)), g.fileKey);
      await r.read(0, r.plaintextSize);
    });
    expect(['truncated', 'auth']).toContain(kind);
  });

  it('agerange_rejectsTruncatedFile source shorter than its size', async () => {
    const short = g.file.subarray(0, g.file.length - 100);
    const src: RangeSource = { size: g.file.length, read: async (offset, length) => short.slice(offset, offset + length) };
    expect(await errorKind(() => openAgeRange(src, g.fileKey))).toBe('truncated');
  });

  it.each([
    ['NaN', Number.NaN],
    ['-1', -1],
    ['fractional', g.file.length - 0.5],
    ['Infinity', Number.POSITIVE_INFINITY],
    ['undefined', undefined],
  ])('agerange_rejectsInvalidSourceSize %s', async (_name, size) => {
    // A source's size can come from outside, e.g. parseInt of an HTTP
    // Content-Range total that is missing or "*". openAgeRange checks it before
    // any read; this source fails after 100 reads, so a size that sends the
    // header search into an endless loop fails the test instead of hanging it.
    let reads = 0;
    const src = {
      size,
      read: async (offset: number, length: number) => {
        if (++reads > 100) throw new Error(`still reading after ${reads - 1} reads of length ${length}`);
        return g.file.slice(offset, offset + length);
      },
    } as unknown as RangeSource;
    expect(await errorKind(() => openAgeRange(src, g.fileKey))).toBe('truncated');
    expect(reads).toBe(0);
  });

  it.each([
    ['empty', 0],
    ['short', 15],
    ['long', 32],
  ])('agerange_rejectsAFileKeyThatIsNot16Bytes %s', async (_name, n) => {
    // age's file keys are 16 bytes. Another length is the caller's mistake, not
    // a file that fails to authenticate: a RangeError before any read.
    let reads = 0;
    const src: RangeSource = {
      size: g.file.length,
      read: async (offset, length) => {
        reads++;
        return g.file.slice(offset, offset + length);
      },
    };
    await expect(openAgeRange(src, new Uint8Array(n))).rejects.toThrow(RangeError);
    await expect(openAgeRange(src, new Uint8Array(n))).rejects.not.toBeInstanceOf(AgeRangeError);
    expect(reads).toBe(0);
  });

  it('agerange_rejectsWrongKey', async () => {
    const wrongKey = g.fileKey.slice();
    wrongKey[0]! ^= 1;
    expect(await errorKind(() => openAgeRange(memorySource(g.file), wrongKey))).toBe('auth');
  });

  it('agerange_rejectsReadOutsidePlaintext', async () => {
    const r = await openAgeRange(memorySource(g.file), g.fileKey);
    const size = r.plaintextSize;
    for (const [offset, length] of [[-1, 1], [0, size + 1], [size, 1], [size - 1, 2], [0.5, 1], [0, -1], [Number.NaN, 0]] as const) {
      expect(await errorKind(() => r.read(offset, length)), `read(${offset}, ${length})`).toBe('range');
    }
    expect(await r.read(size, 0)).toHaveLength(0);
  });

  it('agerange_largeHeader_readsEachByteOfTheHeaderOnce', async () => {
    // The header search reads only what it has not read yet, each time it
    // needs more (a remote source pays for every byte).
    const intro = 'age-encryption.org/v1\n'.length;
    const stanza = new TextEncoder().encode(`-> grease\n${`${'A'.repeat(64)}\n`.repeat(256)}\n`);
    const file = new Uint8Array(g.file.length + stanza.length);
    file.set(g.file.subarray(0, intro));
    file.set(stanza, intro);
    file.set(g.file.subarray(intro), intro + stanza.length);
    const header = h + stanza.length;
    const reads: [number, number][] = [];
    const src: RangeSource = {
      size: file.length,
      read: async (offset, length) => {
        reads.push([offset, length]);
        return file.slice(offset, offset + length);
      },
    };
    await openAgeRange(src, g.fileKey);
    const headerReads = reads.filter(([offset]) => offset < header);
    expect(headerReads.length).toBeGreaterThan(1);
    let next = 0;
    for (const [offset, length] of headerReads) {
      expect(offset, `the reads ${JSON.stringify(headerReads)}`).toBe(next);
      next = offset + length;
    }
  });

  it('agerange_largeHeader_readsPastFirstPrefix', async () => {
    // agerange does not check the header MAC (typage does), so a stanza added
    // to the header leaves the payload readable. This one pushes the MAC line
    // past the first bytes that openAgeRange reads.
    const intro = 'age-encryption.org/v1\n'.length;
    const stanza = new TextEncoder().encode(`-> grease\n${`${'A'.repeat(64)}\n`.repeat(256)}\n`);
    const file = new Uint8Array(g.file.length + stanza.length);
    file.set(g.file.subarray(0, intro));
    file.set(stanza, intro);
    file.set(g.file.subarray(intro), intro + stanza.length);

    expect(headerLength(file)).toBe(h + stanza.length);
    const r = await openAgeRange(memorySource(file), g.fileKey);
    expect(sha256Hex(await r.read(0, r.plaintextSize))).toBe(g.vector.plaintextSha256);
  });
});

describe('agerange reads', () => {
  // 17 chunks: 16 of 64 KiB and one of 17 bytes.
  const g = golden(16 * CHUNK_SIZE + 17);

  it('agerange_wholeFileRead_readsTheSourceInBoundedBatches', async () => {
    // A whole-segment read on a phone holds the output and one batch of sealed
    // chunks, not every sealed byte at once: the source is read in batches of
    // at most 16 sealed chunks (about 1 MiB), each byte once.
    const reads: [number, number][] = [];
    const src: RangeSource = {
      size: g.file.length,
      read: async (offset, length) => {
        reads.push([offset, length]);
        return g.file.slice(offset, offset + length);
      },
    };
    const r = await openAgeRange(src, g.fileKey);
    reads.length = 0;

    const plain = await r.read(0, r.plaintextSize);

    expect(sha256Hex(plain)).toBe(g.vector.plaintextSha256);
    const sealedChunk = CHUNK_SIZE + 16;
    const payloadStart = g.file.length - 16 * sealedChunk - (17 + 16);
    expect(reads).toEqual([
      [payloadStart, 16 * sealedChunk],
      [payloadStart + 16 * sealedChunk, 17 + 16],
    ]);
  });

  it('agerange_readInsideOneBatch_isOneSourceRead', async () => {
    const reads: [number, number][] = [];
    const src: RangeSource = {
      size: g.file.length,
      read: async (offset, length) => {
        reads.push([offset, length]);
        return g.file.slice(offset, offset + length);
      },
    };
    const r = await openAgeRange(src, g.fileKey);
    const full = await r.read(0, r.plaintextSize);
    reads.length = 0;

    // Chunks 1 to 3: a partial first chunk, a whole one, a partial last one.
    const part = await r.read(CHUNK_SIZE + 100, 2 * CHUNK_SIZE);

    expect(sameBytes(part, full.subarray(CHUNK_SIZE + 100, 3 * CHUNK_SIZE + 100))).toBe(true);
    expect(reads).toHaveLength(1);
    expect(reads[0]![1]).toBe(3 * (CHUNK_SIZE + 16));
  });
});
