import { describe, expect, it } from 'vitest';

import { formatAxisMs, nearestIndex, niceMax, ticks } from './charts';

describe('niceMax', () => {
  it.each([
    [0, 100],
    [-1, 100],
    [87, 100],
    [101, 200],
    [230, 250],
    [420, 500],
    [1300, 2000],
    [9000, 10000],
  ])('%i → %i', (v, want) => expect(niceMax(v)).toBe(want));
});

describe('ticks', () => {
  it('splits the axis evenly from zero', () => {
    expect(ticks(500)).toEqual([0, 100, 200, 300, 400, 500]);
    expect(ticks(2500)).toEqual([0, 500, 1000, 1500, 2000, 2500]);
    expect(ticks(200)).toEqual([0, 50, 100, 150, 200]);
    expect(ticks(1000)).toEqual([0, 250, 500, 750, 1000]);
    expect(ticks(1000, 2)).toEqual([0, 500, 1000]);
  });
});

describe('nearestIndex', () => {
  const xs = [0, 10, 20, 30];
  it.each([
    [-5, 0],
    [4, 0],
    [6, 1],
    [15, 1],
    [26, 3],
    [100, 3],
  ])('x=%i → %i', (x, want) => expect(nearestIndex(xs, x)).toBe(want));
  it('handles empty input', () => expect(nearestIndex([], 3)).toBe(-1));
});

it('formats axis labels compactly', () => {
  expect(formatAxisMs(250)).toBe('250ms');
  expect(formatAxisMs(2000)).toBe('2s');
});
