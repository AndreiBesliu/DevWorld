import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import type { RangeSource } from './index';

// Test fixtures shared by the agerange tests. The golden files and the CCTV
// copies are Task 2's, read in place from the hub's crypto testdata, so the TS
// reader is checked against the bytes the Go writer produced (ENC-07).

export const cryptoTestdata = resolve(import.meta.dirname, '../../../../hub/internal/crypto/testdata');

export const CHUNK_SIZE = 64 * 1024;

/** One range of testdata/golden/vectors.json. */
export interface GoldenRange {
  offset: number;
  length: number;
  sha256: string;
}

/** One entry of testdata/golden/vectors.json. */
export interface GoldenVector {
  file: string;
  fileKeyHex: string;
  plaintextSize: number;
  plaintextSha256: string;
  ranges: GoldenRange[];
}

export interface Golden {
  vector: GoldenVector;
  file: Uint8Array;
  fileKey: Uint8Array;
}

export function readGoldens(): Golden[] {
  const dir = join(cryptoTestdata, 'golden');
  const vectors = JSON.parse(readFileSync(join(dir, 'vectors.json'), 'utf8')) as GoldenVector[];
  return vectors.map((vector) => ({
    vector,
    file: new Uint8Array(readFileSync(join(dir, vector.file))),
    fileKey: fromHex(vector.fileKeyHex),
  }));
}

/** A RangeSource over bytes in memory. Each read returns a copy, as a fetch or file read would. */
export function memorySource(bytes: Uint8Array): RangeSource {
  return { size: bytes.length, read: async (offset, length) => bytes.slice(offset, offset + length) };
}

export const fromHex = (hex: string): Uint8Array => new Uint8Array(Buffer.from(hex, 'hex'));

// node:crypto, not the @noble/hashes that agerange uses, so the hashes are checked independently.
export const sha256Hex = (bytes: Uint8Array): string => createHash('sha256').update(bytes).digest('hex');

export const sameBytes = (a: Uint8Array, b: Uint8Array): boolean => Buffer.from(a.buffer, a.byteOffset, a.length).equals(b);
