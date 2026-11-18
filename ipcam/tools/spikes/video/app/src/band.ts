/** The code a frame's band carries. `camera` is the media file id (1 sub, 2 main, 3 main-high51, 4 main-h265), not a simulated camera. */
export interface BandCode {
  camera: number;
  frame: number;
  /** Always 0 in the spike's media: the hub counts the replays (ruling R18). */
  loop: number;
}

/** The band is two rows of 48 blocks; the layout is documented in band/band.go and shared through band/vectors.json. */
const BLOCKS = 48;
const CODE_BYTES = BLOCKS / 8;

/** CRC-8/SMBUS: polynomial 0x07, initial value 0, no reflection, no final xor. */
function crc8(data: Uint8Array): number {
  let crc = 0;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) {
      crc = (crc & 0x80) !== 0 ? ((crc << 1) ^ 0x07) & 0xff : (crc << 1) & 0xff;
    }
  }
  return crc;
}

/**
 * Decodes the two rows of a band, left to right, true for a white block. Null
 * unless row 2 is the exact inverse of row 1 and the CRC of the first 40 bits
 * matches the last 8: a frame the decoder smeared, or one that shows no band,
 * must not yield a plausible wrong code.
 */
export function decodeBits(row1: boolean[], row2: boolean[]): BandCode | null {
  if (row1.length !== BLOCKS || row2.length !== BLOCKS) return null;
  const bytes = new Uint8Array(CODE_BYTES);
  for (let i = 0; i < BLOCKS; i += 1) {
    if (row1[i] === row2[i]) return null;
    if (row1[i] === true) bytes[i >> 3] = (bytes[i >> 3] ?? 0) | (0x80 >> (i & 7));
  }
  const [camera = 0, frameHigh = 0, frameLow = 0, loopHigh = 0, loopLow = 0, crc = 0] = bytes;
  if (crc8(bytes.subarray(0, CODE_BYTES - 1)) !== crc) return null;
  return { camera, frame: (frameHigh << 8) | frameLow, loop: (loopHigh << 8) | loopLow };
}

/** The block side Go paints: 16 px from 1080 lines up, else 8 px. */
const blockSide = (videoHeight: number): number => (videoHeight >= 1080 ? 16 : 8);

/** Rec. 601 luma. */
const luma = (r: number, g: number, b: number): number => 0.299 * r + 0.587 * g + 0.114 * b;

/** The bits of one row of the band: a block is white when the mean luma of its central half is at least 128. */
function readRow(pixels: Uint8ClampedArray, bandWidth: number, side: number, top: number): boolean[] {
  const margin = side / 4;
  const bits: boolean[] = [];
  for (let block = 0; block < BLOCKS; block += 1) {
    let sum = 0;
    let count = 0;
    for (let y = top + margin; y < top + side - margin; y += 1) {
      for (let x = block * side + margin; x < (block + 1) * side - margin; x += 1) {
        const at = (y * bandWidth + x) * 4;
        sum += luma(pixels[at] ?? 0, pixels[at + 1] ?? 0, pixels[at + 2] ?? 0);
        count += 1;
      }
    }
    bits.push(sum / count >= 128);
  }
  return bits;
}

/** What readBand needs of its canvas, which both kinds have (a union of the two has no common getContext signature). */
interface Canvas2D {
  width: number;
  height: number;
  getContext(
    contextId: '2d',
    options: CanvasRenderingContext2DSettings,
  ): CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D | null;
}

/**
 * Reads the band of the frame the video shows now. Only the top strip is drawn,
 * from the video's intrinsic resolution (not the size of the element), as
 * drawImage's source rectangle: the strip is two block rows high, 16 or 32
 * pixels, which is all the band needs. Null when the video has no picture yet
 * or the code does not check out (`decodeBits`).
 */
export function readBand(video: HTMLVideoElement, canvas: OffscreenCanvas | HTMLCanvasElement): BandCode | null {
  const surface: Canvas2D = canvas;
  const width = video.videoWidth;
  const height = video.videoHeight;
  const side = blockSide(height);
  const bandWidth = BLOCKS * side;
  const bandHeight = 2 * side;
  if (width < bandWidth || height < bandHeight) return null; // no picture yet, or one too small to carry the band

  if (surface.width !== bandWidth) surface.width = bandWidth;
  if (surface.height !== bandHeight) surface.height = bandHeight;
  const context = surface.getContext('2d', { willReadFrequently: true });
  if (!context) return null;
  context.drawImage(video, 0, 0, bandWidth, bandHeight, 0, 0, bandWidth, bandHeight);
  const { data } = context.getImageData(0, 0, bandWidth, bandHeight);
  return decodeBits(readRow(data, bandWidth, side, 0), readRow(data, bandWidth, side, side));
}

/** The test hook of the browser end-to-end tests (Task 12): `window.__spikeReadBand(video)`. */
export interface BandReaderTarget {
  __spikeReadBand?: (video: HTMLVideoElement) => BandCode | null;
}

/**
 * Exposes readBand, on a canvas of its own, to the end-to-end tests, which
 * check that the band survives the real hub, the browser's decoder and the
 * canvas, as S2's latency samples need.
 */
export function exposeBandReader(target: BandReaderTarget, canvas: OffscreenCanvas | HTMLCanvasElement): void {
  target.__spikeReadBand = (video) => readBand(video, canvas);
}
