/**
 * Why an age file cannot be read:
 * - `header`: the file does not start with an age v1 header (its version line,
 *   then a `--- <mac>` line);
 * - `truncated`: the file is shorter than its layout needs or has a size that no
 *   age payload has, the source's size is not a byte count (a safe integer
 *   ≥ 0), or the source returned fewer bytes than asked;
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
 */
export function headerLength(prefix: Uint8Array): number {
  const end = findHeaderEnd(prefix);
  if (end < 0) throw new AgeRangeError('header', `no complete "--- <mac>" line in the first ${prefix.length} bytes`);
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
