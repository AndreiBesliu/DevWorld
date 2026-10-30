import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { inflateSync } from 'node:zlib';
import { describe, expect, it } from 'vitest';
import { cryptoTestdata, fromHex, memorySource, sha256Hex } from './fixtures';
import { AgeRangeError, openAgeRange } from './index';

// C21: the C2SP CCTV age vectors that Task 2 copied (testdata/cctv/vectors).
// Every vector carries its file key, so agerange runs on each vector whose
// outcome depends on the payload. A vector that fails before the payload (the
// header, its MAC, no matching identity or the armor) is typage's job: it is
// counted and skipped.

const vectorsDir = join(cryptoTestdata, 'cctv', 'vectors');

interface Vector {
  name: string;
  expect: string;
  fileKey: Uint8Array;
  payloadSha256: string | undefined;
  file: Uint8Array;
}

const SKIPPED = new Set(['header failure', 'HMAC failure', 'no match', 'armor failure']);

// Each vector is "name: value" lines, a blank line, then the age file, which
// may be zlib-compressed and ASCII-armored (in that order).
function readVector(name: string): Vector {
  const data = readFileSync(join(vectorsDir, name));
  const split = data.indexOf('\n\n');
  const meta = new Map<string, string>();
  for (const line of data.subarray(0, split).toString('utf8').split('\n')) {
    const colon = line.indexOf(': ');
    meta.set(line.slice(0, colon), line.slice(colon + 2));
  }
  let file: Uint8Array = data.subarray(split + 2);
  if (meta.get('compressed') === 'zlib') file = inflateSync(file);
  if (meta.get('armored') === 'yes') file = dearmor(file);
  return {
    name,
    expect: meta.get('expect') ?? '',
    fileKey: fromHex(meta.get('file key') ?? ''),
    payloadSha256: meta.get('payload'),
    file,
  };
}

// A lenient armor decoder: the armor vectors that reach the payload are valid
// armor, and the strict checks belong to the armor failures, which are skipped.
function dearmor(armored: Uint8Array): Uint8Array {
  const text = Buffer.from(armored).toString('latin1');
  const begin = '-----BEGIN AGE ENCRYPTED FILE-----';
  const body = text.slice(text.indexOf(begin) + begin.length, text.indexOf('-----END AGE ENCRYPTED FILE-----'));
  return new Uint8Array(Buffer.from(body.replace(/\s+/g, ''), 'base64'));
}

const vectors = readdirSync(vectorsDir).sort().map(readVector);
const run = vectors.filter((v) => !SKIPPED.has(v.expect));

async function decryptAll(v: Vector): Promise<Uint8Array> {
  const r = await openAgeRange(memorySource(v.file), v.fileKey);
  return r.read(0, r.plaintextSize);
}

describe('agerange_cctvVectors', () => {
  it('runs every payload vector and skips the header-side ones', () => {
    const count = (expect: string): number => vectors.filter((v) => v.expect === expect).length;
    expect(vectors).toHaveLength(147);
    expect(count('success')).toBe(26);
    expect(count('payload failure')).toBe(19);
    expect(vectors.length - run.length).toBe(102);
    expect(run.every((v) => v.fileKey.length === 16)).toBe(true);
  });

  it.each(run.map((v) => [v.name, v] as const))('%s', { timeout: 30_000 }, async (_name, v) => {
    if (v.expect === 'success') {
      expect(sha256Hex(await decryptAll(v))).toBe(v.payloadSha256);
      return;
    }
    expect(v.expect).toBe('payload failure');
    const err: unknown = await decryptAll(v).then(
      () => undefined,
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(AgeRangeError);
    expect(['truncated', 'auth']).toContain((err as AgeRangeError).kind);
  });
});
