/**
 * Why an age file cannot be read:
 * - `header`: the file does not start with an age v1 header (its version line,
 *   then a `--- <mac>` line), or its header is larger than age's 2 MiB limit;
 * - `truncated`: the bytes end before the layout does: a prefix that ends
 *   before the header's `--- <mac>` line (headerLength: read more), a file
 *   shorter than its layout needs or with a size that no age payload has, the
 *   source's size is not a byte count (a safe integer ≥ 0), or the source
 *   returned fewer bytes than asked;
 * - `auth`: a chunk does not authenticate under the file key (a wrong key or
 *   changed bytes);
 * - `range`: a read outside the plaintext.
 */
export class AgeRangeError extends Error {
  override readonly name = 'AgeRangeError';

  constructor(
    readonly kind: 'header' | 'truncated' | 'auth' | 'range',
    message: string,
  ) {
    super(message);
  }
}

const INTRO = new TextEncoder().encode('age-encryption.org/v1\n');
const MAC_LINE_START = new TextEncoder().encode('--- ');
const LF = 0x0a;

/**
 * The length of the age header at the start of prefix: the bytes through the
 * newline that ends its `--- <mac>` line. It reads only the layout; the stanzas
 * and the MAC are typage's job (decryptHeader).
 *
 * prefix is the first bytes of the file: 4 KiB hold an X25519 header with two
 * recipients (about 250 bytes) many times over. When prefix ends before the
 * header does, it throws `truncated`: read twice as many bytes and call again,
 * up to the file's size or age's 2 MiB header limit. It throws `header` as soon
 * as the bytes cannot start an age v1 file, however many follow.
 */
export function headerLength(prefix: Uint8Array): number {
  const end = findHeaderEnd(prefix);
  if (end < 0) throw new AgeRangeError('truncated', `the header goes on past the first ${prefix.length} bytes`);
  return end;
}

/** Like headerLength, but -1 when prefix ends before the `--- <mac>` line does. */
export function findHeaderEnd(prefix: Uint8Array): number {
  // A prefix shorter than the version line is checked as far as it goes.
  if (!startsWith(prefix, INTRO.subarray(0, prefix.length), 0)) {
    throw new AgeRangeError('header', 'the file does not start with the age v1 version line');
  }
  // Stanza bodies are base64, which has no '-', so the first line that starts
  // with "--- " is the MAC line.
  for (let lf = prefix.indexOf(LF, INTRO.length - 1); lf >= 0; lf = prefix.indexOf(LF, lf + 1)) {
    if (startsWith(prefix, MAC_LINE_START, lf + 1)) {
      const end = prefix.indexOf(LF, lf + 1 + MAC_LINE_START.length);
      return end < 0 ? -1 : end + 1;
    }
  }
  return -1;
}

function startsWith(bytes: Uint8Array, pattern: Uint8Array, at: number): boolean {
  return at + pattern.length <= bytes.length && pattern.every((b, i) => bytes[at + i] === b);
}
