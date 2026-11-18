import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { decodeBits, exposeBandReader, readBand, type BandReaderTarget } from './band';

interface Vector {
  note: string;
  camera: number;
  frame: number;
  loop: number;
  row1: string;
  row2: string;
}

// The same file the Go tests read: the single source of truth for both decoders.
const vectors = (
  JSON.parse(readFileSync(join(import.meta.dirname, '../../band/vectors.json'), 'utf8')) as { vectors: Vector[] }
).vectors;

const bits = (row: string): boolean[] => [...row].map((c) => c === '1');
const flip = (row: boolean[], i: number): boolean[] => row.map((bit, j) => (j === i ? !bit : bit));

describe('decodeBits', () => {
  it('decodeBits_vectors', () => {
    expect(vectors.length).toBeGreaterThanOrEqual(20);
    for (const v of vectors) {
      expect(decodeBits(bits(v.row1), bits(v.row2)), v.note).toEqual({ camera: v.camera, frame: v.frame, loop: v.loop });
    }
  });

  it('decodeBits_crcFail_returnsNull', () => {
    const v = vectors.find((candidate) => candidate.camera === 2 && candidate.frame === 1199);
    if (!v) throw new Error('vector missing');
    const row1 = bits(v.row1);
    const row2 = bits(v.row2);

    // A data bit that is wrong in both rows: row 2 is still the inverse of row 1, so only the CRC can tell.
    expect(decodeBits(flip(row1, 12), flip(row2, 12))).toBeNull();
    // A wrong CRC bit.
    expect(decodeBits(flip(row1, 45), flip(row2, 45))).toBeNull();
  });

  it('decodeBits_everySingleBitFlipInBothRows_isRejected', () => {
    for (const v of vectors) {
      const row1 = bits(v.row1);
      const row2 = bits(v.row2);
      for (let i = 0; i < 48; i += 1) {
        expect(decodeBits(flip(row1, i), flip(row2, i)), `${v.note}, bit ${i}`).toBeNull();
      }
    }
  });

  it('decodeBits_row2NotTheInverse_returnsNull', () => {
    const v = vectors[8];
    if (!v) throw new Error('vector missing');
    const row1 = bits(v.row1);

    // Row 2 equals row 1 (a washed-out or double-exposed frame) or differs in one block only.
    expect(decodeBits(row1, row1)).toBeNull();
    expect(decodeBits(row1, flip(bits(v.row2), 7))).toBeNull();
  });

  it('decodeBits_wrongRowLength_returnsNull', () => {
    const v = vectors[8];
    if (!v) throw new Error('vector missing');

    expect(decodeBits(bits(v.row1).slice(1), bits(v.row2).slice(1))).toBeNull();
    expect(decodeBits([...bits(v.row1), true], [...bits(v.row2), false])).toBeNull();
    expect(decodeBits([], [])).toBeNull();
  });
});

type Rgb = [number, number, number];

/** The colour of a pixel of the fake video's frame. */
type Pixel = (x: number, y: number) => Rgb;

const gray = (level: number): Rgb => [level, level, level];

/**
 * The band of a vector as a frame would carry it at this height. The outer
 * quarter of every block, on each side, shows the opposite colour, like the
 * bleed that scaling and compression leave at block edges: only the central
 * half of a block tells its bit.
 */
function bandedFrame(v: Vector, height: number, on: Rgb = gray(255), off: Rgb = gray(0)): Pixel {
  const side = height >= 1080 ? 16 : 8;
  const row1 = bits(v.row1);
  return (x, y) => {
    if (y >= 2 * side || x >= 48 * side) return gray(120); // the picture
    const block = Math.floor(x / side);
    const bit = Math.floor(y / side) === 0 ? row1[block] : !row1[block];
    const edge = (x % side) * 4 < side || (x % side) * 4 >= 3 * side || (y % side) * 4 < side || (y % side) * 4 >= 3 * side;
    return bit !== edge ? on : off;
  };
}

interface FakeVideoElement {
  videoWidth: number;
  videoHeight: number;
  pixel: Pixel;
}

function video(width: number, height: number, pixel: Pixel): { fake: FakeVideoElement; element: HTMLVideoElement } {
  const fake = { videoWidth: width, videoHeight: height, pixel };
  return { fake, element: fake as unknown as HTMLVideoElement };
}

/** A canvas whose 2D context copies from the fake video, nearest neighbour, like drawImage does. */
class FakeCanvas {
  readonly drawCalls: { source: FakeVideoElement; args: number[] }[] = [];
  readonly contextCalls: unknown[][] = [];
  private pixels = new Uint8ClampedArray(0);
  private w = 300;
  private h = 150;

  get width(): number {
    return this.w;
  }
  set width(value: number) {
    this.w = value;
    this.pixels = new Uint8ClampedArray(this.w * this.h * 4);
  }
  get height(): number {
    return this.h;
  }
  set height(value: number) {
    this.h = value;
    this.pixels = new Uint8ClampedArray(this.w * this.h * 4);
  }

  get element(): HTMLCanvasElement {
    return this as unknown as HTMLCanvasElement;
  }

  getContext(...args: unknown[]): object {
    this.contextCalls.push(args);
    return {
      drawImage: (
        source: FakeVideoElement,
        sx: number,
        sy: number,
        sw: number,
        sh: number,
        dx: number,
        dy: number,
        dw: number,
        dh: number,
      ) => {
        this.drawCalls.push({ source, args: [sx, sy, sw, sh, dx, dy, dw, dh] });
        for (let y = 0; y < dh; y += 1) {
          for (let x = 0; x < dw; x += 1) {
            const [r, g, b] = source.pixel(sx + Math.floor((x * sw) / dw), sy + Math.floor((y * sh) / dh));
            this.pixels.set([r, g, b, 255], ((dy + y) * this.w + dx + x) * 4);
          }
        }
      },
      getImageData: (sx: number, sy: number, sw: number, sh: number) => {
        const data = new Uint8ClampedArray(sw * sh * 4);
        for (let y = 0; y < sh; y += 1) {
          const from = ((sy + y) * this.w + sx) * 4;
          data.set(this.pixels.subarray(from, from + sw * 4), y * sw * 4);
        }
        return { data, width: sw, height: sh };
      },
    };
  }
}

const sample = (camera: number, frame: number): Vector => {
  const v = vectors.find((candidate) => candidate.camera === camera && candidate.frame === frame);
  if (!v) throw new Error(`vector ${camera}/${frame} missing`);
  return v;
};

describe('readBand', () => {
  it.each([
    { name: 'sub 640x360', width: 640, height: 360, side: 8, v: sample(1, 719) },
    { name: 'main 1920x1080', width: 1920, height: 1080, side: 16, v: sample(2, 1199) },
    { name: 'main-high51 3840x2160', width: 3840, height: 2160, side: 16, v: sample(3, 399) },
    { name: 'main-h265 1920x1080', width: 1920, height: 1080, side: 16, v: sample(4, 600) },
    { name: '720p, below 1080 lines', width: 1280, height: 720, side: 8, v: sample(1, 5) },
    { name: '1079 lines', width: 1920, height: 1079, side: 8, v: sample(2, 12) },
  ])('readBand_$name_decodesAndDrawsOnlyTheBandAtIntrinsicResolution', ({ width, height, side, v }) => {
    const canvas = new FakeCanvas();
    const { fake, element } = video(width, height, bandedFrame(v, height));

    const code = readBand(element, canvas.element);

    expect(code).toEqual({ camera: v.camera, frame: v.frame, loop: v.loop });
    // One draw: the top strip, two rows of 48 blocks, source rectangle at the video's own resolution, no scaling.
    expect(canvas.drawCalls).toEqual([{ source: fake, args: [0, 0, 48 * side, 2 * side, 0, 0, 48 * side, 2 * side] }]);
    expect(canvas.width).toBe(48 * side);
    expect(canvas.height).toBe(2 * side);
  });

  it('readBand_everyVector_decodes', () => {
    const canvas = new FakeCanvas();
    for (const v of vectors) {
      const { element } = video(1920, 1080, bandedFrame(v, 1080));
      expect(readBand(element, canvas.element), v.note).toEqual({ camera: v.camera, frame: v.frame, loop: v.loop });
    }
  });

  it('readBand_limitedRangeLevels_decode', () => {
    // 235 and 16 are the luma levels the hub paints; a player that does not expand to full range shows them as they are.
    const v = sample(2, 1000);
    const { element } = video(1920, 1080, bandedFrame(v, 1080, gray(235), gray(16)));

    expect(readBand(element, new FakeCanvas().element)).toEqual({ camera: v.camera, frame: v.frame, loop: v.loop });
  });

  it('readBand_blockEdgesAreIgnored_onlyTheCentralHalfCounts', () => {
    // bandedFrame paints the outer quarter of every block in the wrong colour: a reader that averaged the whole block would flip bits.
    const v = sample(1, 0);
    const frame = bandedFrame(v, 360);
    expect(frame(0, 0)).not.toEqual(frame(3, 3)); // the edge and the centre of block 0 differ

    expect(readBand(video(640, 360, frame).element, new FakeCanvas().element)).toEqual({ camera: 1, frame: 0, loop: 0 });
  });

  it('readBand_colourVideo_averagesBt601Luma', () => {
    // Pure green has luma 0.587 * 255 = 150 (a 1), pure blue 0.114 * 255 = 29 (a 0).
    // A reader that looked at one channel, or at the plain RGB mean of 85 for both, would not see the code.
    const v = sample(2, 0);
    const { element } = video(640, 360, bandedFrame(v, 360, [0, 255, 0], [0, 0, 255]));

    expect(readBand(element, new FakeCanvas().element)).toEqual({ camera: 2, frame: 0, loop: 0 });
  });

  it('readBand_crcFails_returnsNull', () => {
    const v = sample(2, 1199);
    const frame = bandedFrame(v, 1080);
    // Block 12 of both rows is inverted: row 2 still mirrors row 1, the CRC does not match.
    const damaged: Pixel = (x, y) => {
      const [level] = frame(x, y);
      return x >= 12 * 16 && x < 13 * 16 && y < 32 ? gray(255 - level) : frame(x, y);
    };

    expect(readBand(video(1920, 1080, damaged).element, new FakeCanvas().element)).toBeNull();
  });

  it('readBand_noBandInThePicture_returnsNull', () => {
    expect(readBand(video(1920, 1080, () => gray(120)).element, new FakeCanvas().element)).toBeNull();
  });

  it('readBand_videoNotReady_returnsNullWithoutDrawing', () => {
    const canvas = new FakeCanvas();

    expect(readBand(video(0, 0, () => gray(255)).element, canvas.element)).toBeNull();
    expect(canvas.drawCalls).toEqual([]);
  });

  it('readBand_frameSmallerThanTheBand_returnsNull', () => {
    const canvas = new FakeCanvas();

    expect(readBand(video(320, 180, () => gray(255)).element, canvas.element)).toBeNull();
    expect(canvas.drawCalls).toEqual([]);
  });

  it('readBand_sameCanvasTwice_asksForAReadbackFriendlyContext', () => {
    const canvas = new FakeCanvas();
    const v = sample(1, 1);

    readBand(video(640, 360, bandedFrame(v, 360)).element, canvas.element);
    const second = readBand(video(640, 360, bandedFrame(v, 360)).element, canvas.element);

    expect(second).toEqual({ camera: 1, frame: 1, loop: 1 });
    expect(canvas.contextCalls[0]).toEqual(['2d', { willReadFrequently: true }]);
  });
});

describe('the band reader test hook', () => {
  it('exposeBandReader_readsTheShownFrameWithReadBandOnItsOwnCanvas', () => {
    const target: BandReaderTarget = {};
    const canvas = new FakeCanvas();
    const v = sample(1, 719);
    const { fake, element } = video(640, 360, bandedFrame(v, 360));

    exposeBandReader(target, canvas.element);

    expect(target.__spikeReadBand?.(element)).toEqual({ camera: v.camera, frame: v.frame, loop: v.loop });
    expect(canvas.drawCalls).toEqual([{ source: fake, args: [0, 0, 384, 16, 0, 0, 384, 16] }]);
    expect(target.__spikeReadBand?.(video(1920, 1080, () => gray(120)).element)).toBeNull();
  });
});
