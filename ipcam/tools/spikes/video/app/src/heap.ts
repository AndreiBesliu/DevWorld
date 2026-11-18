// R46: Chromium reports the JS heap precisely only to a renderer locked to one
// site (desktop Chrome and Edge with site isolation, WebView2). Elsewhere, the
// Android WebView among them, `performance.memory` rounds the heap to one of
// 100 bucket sizes and refreshes it at most every 20 minutes, so it cannot show
// a leak over S4's 30 minutes. This file tells such a series apart.

/**
 * The bucket sizes in bytes, as Chromium computes them
 * (third_party/blink/renderer/core/timing/memory_info.cc, QuantizeMemorySize):
 * from 10,000,000 to about 4 GB in 100 steps of the same ratio, in single
 * precision, each rounded down to 3 significant digits.
 */
export function chromiumHeapBuckets(): number[] {
  const f = Math.fround;
  const buckets: number[] = [];
  let next = f(10_000_000);
  const scaling = f(Math.exp(Math.log(f(4_000_000_000) / next) / 100));
  let nextPowerOfTen = 10 ** (Math.floor(Math.log10(next)) + 1);
  let granularity = nextPowerOfTen / 1000;
  for (let i = 0; i < 100; i += 1) {
    const current = Math.trunc(next);
    buckets.push(current - (current % granularity));
    next = f(next * scaling);
    if (next >= nextPowerOfTen) {
      nextPowerOfTen *= 10;
      granularity *= 10;
    }
  }
  return buckets;
}

const BUCKETS = new Set(chromiumHeapBuckets());

/** The readings a value must stay the same for, while the app allocates, to count as cached. */
export const UNCHANGED_READINGS = 20;

/**
 * Whether a series of JS heap readings in MiB (as `jsHeapMb` gives them) is
 * Chromium's bucketed value rather than the heap: every reading, at least two,
 * is a bucket size, or one value repeats for UNCHANGED_READINGS readings in a
 * row (S4 pages its tiles in between, so a precise heap moves).
 */
export function isBucketedHeap(seriesMb: readonly number[]): boolean {
  if (seriesMb.length >= 2 && seriesMb.every((mb) => BUCKETS.has(Math.round(mb * 2 ** 20)))) return true;
  let run = 0;
  for (let i = 0; i < seriesMb.length; i += 1) {
    run = i > 0 && seriesMb[i] === seriesMb[i - 1] ? run + 1 : 1;
    if (run >= UNCHANGED_READINGS) return true;
  }
  return false;
}
