import { describe, expect, it } from 'vitest';
import { chromiumHeapBuckets, isBucketedHeap } from './heap';

const MB = 2 ** 20;

describe('the JS heap that Chromium reports without a site-locked renderer', () => {
  it('chromiumHeapBuckets_matchChromiumsQuantizeMemorySize', () => {
    // third_party/blink/renderer/core/timing/memory_info.cc: 100 buckets from
    // 10 MB to about 4 GB, each rounded down to 3 significant digits.
    const buckets = chromiumHeapBuckets();

    expect(buckets).toHaveLength(100);
    expect(buckets.slice(0, 6)).toEqual([10_000_000, 10_600_000, 11_200_000, 11_900_000, 12_700_000, 13_400_000]);
    expect(buckets.at(-1)).toBe(3_760_000_000);
    for (let i = 1; i < buckets.length; i += 1) expect(buckets[i]).toBeGreaterThan(buckets[i - 1] ?? 0);
  });

  it('isBucketedHeap_everyReadingOnABucketEdge', () => {
    // The probe in the final review: twelve readings of exactly 10,000,000 bytes.
    const series = [...Array<number>(40).fill(10_000_000 / MB), ...Array<number>(21).fill(10_600_000 / MB)];

    expect(isBucketedHeap(series)).toBe(true);
    expect(isBucketedHeap([10_000_000 / MB, 10_600_000 / MB, 11_200_000 / MB])).toBe(true);
  });

  it('isBucketedHeap_aValueThatNeverChangesOver20Readings', () => {
    expect(isBucketedHeap(Array<number>(20).fill(23.4))).toBe(true);
    expect(isBucketedHeap([12.1, ...Array<number>(20).fill(23.4), 24])).toBe(true);
  });

  it('isBucketedHeap_aPreciseHeap_isNot', () => {
    // Desktop Chrome with site isolation: the reading moves with every allocation.
    const precise = Array.from({ length: 61 }, (_, i) => (3_095_342 + i * 381_517) / MB);
    expect(isBucketedHeap(precise)).toBe(false);
    // Briefly steady, as between two garbage collections, but not for 20 readings.
    expect(isBucketedHeap([...Array<number>(19).fill(30.25), 31, 32])).toBe(false);
    // An edge now and then is chance, not bucketing.
    expect(isBucketedHeap([10_000_000 / MB, 9.87, 10.31, 10.5])).toBe(false);
    expect(isBucketedHeap([])).toBe(false);
    expect(isBucketedHeap([10_000_000 / MB])).toBe(false); // one reading says nothing
  });
});
