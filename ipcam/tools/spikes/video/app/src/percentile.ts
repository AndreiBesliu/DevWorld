/**
 * The nearest-rank percentile of `values` at quantile `q` (0 to 1): the smallest
 * sample such that at least a fraction `q` of the samples are at or below it.
 * The result is always one of the samples, never an interpolation, so
 * `p(samples, 0.9)` of 20 samples is the 18th smallest. NaN when there are no
 * samples. `values` is not modified.
 */
export function p(values: number[], q: number): number {
  if (!(q >= 0 && q <= 1)) throw new RangeError(`quantile ${q} is outside 0 to 1`);
  if (values.length === 0) return Number.NaN;
  const sorted = [...values].sort((a, b) => a - b);
  // The epsilon keeps rounding noise, 0.07 * 100 being 7.000000000000001, from rounding up to the next rank.
  const rank = Math.min(sorted.length, Math.max(1, Math.ceil(q * sorted.length - 1e-9)));
  return sorted[rank - 1] ?? Number.NaN;
}
