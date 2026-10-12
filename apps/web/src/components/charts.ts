/** Pure helpers shared by the SVG charts (unit-tested). */

/** Rounds a max value up to a "nice" axis bound (1, 2, 2.5, 5 × 10^n). */
export function niceMax(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 100;
  const exp = Math.floor(Math.log10(value));
  const base = 10 ** exp;
  for (const step of [1, 2, 2.5, 5, 10]) {
    if (value <= step * base) return step * base;
  }
  return 10 * base;
}

/**
 * Evenly spaced ticks from zero. For bounds produced by niceMax the count is
 * chosen so every step is itself a round number (e.g. 2.5s → 0.5s steps).
 */
export function ticks(max: number, count?: number): number[] {
  if (count === undefined) {
    const mantissa = max / 10 ** Math.floor(Math.log10(max));
    count = mantissa === 2.5 || mantissa === 5 ? 5 : 4;
  }
  const step = max / count;
  return Array.from({ length: count + 1 }, (_, i) => Math.round(i * step * 100) / 100);
}

/** Index of the point whose x is nearest to `x` (xs must be ascending). */
export function nearestIndex(xs: number[], x: number): number {
  if (xs.length === 0) return -1;
  let lo = 0;
  let hi = xs.length - 1;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if ((xs[mid] ?? 0) < x) lo = mid;
    else hi = mid;
  }
  return Math.abs((xs[lo] ?? 0) - x) <= Math.abs((xs[hi] ?? 0) - x) ? lo : hi;
}

export function formatAxisMs(ms: number): string {
  if (ms >= 1000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${ms}ms`;
}
