import { chacha20poly1305 } from '@noble/ciphers/chacha.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { AgeRangeError, findHeaderEnd } from './header';

/** Bytes read by range: a file, a Blob or an HTTP resource with range requests. */
export interface RangeSource {
  /** The byte count: a safe integer ≥ 0, else openAgeRange fails as `truncated` before any read. */
  readonly size: number;
  read(offset: number, length: number): Promise<Uint8Array>;
}

/** The plaintext of an age file, read by range. */
export interface AgeRange {
  readonly plaintextSize: number;
  read(offset: number, length: number): Promise<Uint8Array>;
}

// The age v1 payload (spec 10.2): a 16-byte nonce, then STREAM ChaCha20-Poly1305
// chunks of 64 KiB of plaintext, each sealed with a 16-byte tag. Only the final
// chunk may be shorter, and it is empty only when the whole plaintext is.
const NONCE_SIZE = 16;
const CHUNK_SIZE = 64 * 1024;
const TAG_SIZE = 16;
const SEALED_CHUNK_SIZE = CHUNK_SIZE + TAG_SIZE;
const PAYLOAD_INFO = new TextEncoder().encode('payload');

// The header is looked for in the first FIRST_HEADER_READ bytes (two X25519
// stanzas take about 250), then in twice as many, up to the 2 MiB header limit of
// filippo.io/age.
const FIRST_HEADER_READ = 4096;
const MAX_HEADER_SIZE = 2 << 20;

/**
 * Opens an age file for reads by plaintext range. fileKey is the file key that
 * typage's decryptHeader unwraps from the header (the first headerLength bytes).
 * This derives the payload key and authenticates the final chunk, which fixes the
 * plaintext size. Each read authenticates every chunk it touches.
 */
export async function openAgeRange(src: RangeSource, fileKey: Uint8Array): Promise<AgeRange> {
  const size = src.size;
  // A size from outside (say parseInt of a missing HTTP Content-Range total)
  // can be NaN, and with NaN the header search below would read forever.
  if (!Number.isSafeInteger(size) || size < 0) {
    throw new AgeRangeError('truncated', `the source's size ${String(size)} is not a byte count`);
  }
  const { headerSize, prefix } = await readHeader(src, size);
  const payloadStart = headerSize + NONCE_SIZE;
  if (size < payloadStart) throw new AgeRangeError('truncated', `the file ends inside the payload nonce, at ${size} bytes`);
  const nonce =
    prefix.length >= payloadStart ? prefix.subarray(headerSize, payloadStart) : await readExact(src, headerSize, NONCE_SIZE);
  const key = hkdf(sha256, fileKey, nonce, PAYLOAD_INFO, 32);
  const chunks = chunkCount(size - payloadStart);
  const plaintextSize = size - payloadStart - chunks * TAG_SIZE;

  // The sealed chunks first..last, in one read of the source.
  const readSealed = (first: number, last: number): Promise<Uint8Array> => {
    const start = payloadStart + first * SEALED_CHUNK_SIZE;
    return readExact(src, start, Math.min(payloadStart + (last + 1) * SEALED_CHUNK_SIZE, size) - start);
  };
  const openChunk = (index: number, sealed: Uint8Array): Uint8Array => {
    try {
      return chacha20poly1305(key, chunkNonce(index, index === chunks - 1)).decrypt(sealed);
    } catch {
      throw new AgeRangeError('auth', `chunk ${index} of ${chunks} does not authenticate under the file key`);
    }
  };

  // As in age's DecryptReaderAt: only an authentic final chunk proves the size.
  openChunk(chunks - 1, await readSealed(chunks - 1, chunks - 1));

  return {
    plaintextSize,
    async read(offset, length) {
      const inside = Number.isSafeInteger(offset) && Number.isSafeInteger(length) && offset >= 0 && length >= 0;
      if (!inside || offset + length > plaintextSize) {
        throw new AgeRangeError('range', `[${offset}, +${length}) is not inside the ${plaintextSize}-byte plaintext`);
      }
      const out = new Uint8Array(length);
      if (length === 0) return out;
      const first = Math.floor(offset / CHUNK_SIZE);
      const last = Math.floor((offset + length - 1) / CHUNK_SIZE);
      const sealed = await readSealed(first, last);
      for (let i = first; i <= last; i++) {
        const at = (i - first) * SEALED_CHUNK_SIZE;
        const chunk = openChunk(i, sealed.subarray(at, at + SEALED_CHUNK_SIZE));
        const chunkStart = i * CHUNK_SIZE;
        const from = Math.max(offset - chunkStart, 0);
        out.set(chunk.subarray(from, Math.min(offset + length - chunkStart, chunk.length)), chunkStart + from - offset);
      }
      return out;
    },
  };
}

async function readHeader(src: RangeSource, size: number): Promise<{ headerSize: number; prefix: Uint8Array }> {
  for (let n = Math.min(size, FIRST_HEADER_READ); ; n = Math.min(size, 2 * n)) {
    const prefix = await readExact(src, 0, n);
    const headerSize = findHeaderEnd(prefix);
    if (headerSize >= 0) return { headerSize, prefix };
    if (n === size || n >= MAX_HEADER_SIZE) {
      throw new AgeRangeError('header', `no complete "--- <mac>" line in the first ${n} bytes`);
    }
  }
}

async function readExact(src: RangeSource, offset: number, length: number): Promise<Uint8Array> {
  const bytes = await src.read(offset, length);
  if (bytes.length < length) {
    throw new AgeRangeError('truncated', `the source returned ${bytes.length} of the ${length} bytes at ${offset}`);
  }
  return bytes.subarray(0, length);
}

// The number of chunks in a payload of this size after the nonce, checked as
// filippo.io/age's EncryptedChunkCount does: only the final chunk may be short,
// and only a lone final chunk may be empty.
function chunkCount(payloadSize: number): number {
  const chunks = Math.ceil(payloadSize / SEALED_CHUNK_SIZE);
  const plaintextSize = payloadSize - chunks * TAG_SIZE;
  if (chunks !== (plaintextSize === 0 ? 1 : Math.ceil(plaintextSize / CHUNK_SIZE))) {
    throw new AgeRangeError('truncated', `no age payload has ${payloadSize} bytes after its nonce`);
  }
  return chunks;
}

// STREAM's nonce: the chunk index as an 11-byte big-endian counter, then the
// last-chunk flag.
function chunkNonce(index: number, last: boolean): Uint8Array {
  const nonce = new Uint8Array(12);
  const view = new DataView(nonce.buffer);
  view.setUint32(3, Math.floor(index / 2 ** 32));
  view.setUint32(7, index >>> 0);
  nonce[11] = last ? 1 : 0;
  return nonce;
}
