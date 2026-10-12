import type { ResultPoint } from '../api/types';

const W = 112;
const H = 28;

/** Compact trend of recent probe latencies; failures are ticks on the baseline. */
export function Sparkline({ points }: { points: ResultPoint[] }) {
  if (points.length < 2) {
    return <span className="text-xs text-muted">No data yet</span>;
  }
  const max = Math.max(1, ...points.filter((p) => p.outcome !== 'down').map((p) => p.latencyMs));
  const step = W / (points.length - 1);
  const coords = points.map((p, i) => ({
    x: i * step,
    y: H - 3 - (Math.min(p.latencyMs, max) / max) * (H - 6),
    down: p.outcome === 'down',
  }));
  const line = coords
    .filter((c) => !c.down)
    .map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`)
    .join(' ');
  const failures = coords.filter((c) => c.down).length;
  const last = coords[coords.length - 1]!;

  return (
    <span className="inline-flex items-center">
      <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} aria-hidden="true" className="overflow-visible">
        <polyline points={line} fill="none" stroke="var(--color-axis)" strokeWidth={1.5} strokeLinejoin="round" />
        {coords.map((c, i) =>
          c.down ? <rect key={i} x={c.x - 1} y={H - 6} width={2} height={6} fill="var(--color-critical)" /> : null,
        )}
        {!last.down && <circle cx={last.x} cy={last.y} r={2.5} fill="var(--color-series-1)" />}
      </svg>
      <span className="sr-only">
        Last {points.length} probes: {points.length - failures} reachable, {failures} failed.
      </span>
    </span>
  );
}
