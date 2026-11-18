import { describe, expect, it } from 'vitest';
import { p } from './percentile';

const oneTo = (n: number): number[] => Array.from({ length: n }, (_, i) => i + 1);

describe('p', () => {
  it('p_nearestRank', () => {
    expect(p(oneTo(20), 0.9)).toBe(18);
    expect(p(oneTo(20), 0.95)).toBe(19);
    expect(p(oneTo(20), 0.5)).toBe(10);
    expect(p(oneTo(100), 0.95)).toBe(95);
  });

  it('p_isAnElementOfTheSamples_neverInterpolated', () => {
    expect(p([10, 20, 30, 40], 0.5)).toBe(20);
    expect(p([10, 20, 30, 40], 0.75)).toBe(30);
    expect(p([10, 20, 30, 40], 0.76)).toBe(40);
  });

  it('p_unsortedInput_isSortedWithoutTouchingTheCaller', () => {
    const values = [5, 1, 4, 2, 3];

    expect(p(values, 0.8)).toBe(4);
    expect(values).toEqual([5, 1, 4, 2, 3]);
  });

  it('p_rankProductWithRoundingNoise_doesNotRoundUp', () => {
    // 0.07 * 100 is 7.000000000000001 in floating point: rank 7, not 8.
    expect(p(oneTo(100), 0.07)).toBe(7);
    expect(p(oneTo(10), 0.7)).toBe(7);
  });

  it('p_edges_giveTheSmallestAndTheLargest', () => {
    expect(p(oneTo(20), 0)).toBe(1);
    expect(p(oneTo(20), 1)).toBe(20);
    expect(p([42], 0.99)).toBe(42);
  });

  it('p_noSamples_isNaN', () => {
    expect(p([], 0.9)).toBeNaN();
  });

  it('p_quantileOutsideZeroToOne_throws', () => {
    expect(() => p([1, 2, 3], -0.1)).toThrow(RangeError);
    expect(() => p([1, 2, 3], 1.1)).toThrow(RangeError);
    expect(() => p([1, 2, 3], Number.NaN)).toThrow(RangeError);
  });
});
