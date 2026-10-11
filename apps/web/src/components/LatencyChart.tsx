import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';

import type { CheckResult } from '../api/types';
import { formatMs, formatTime } from '../lib/format';
import { formatAxisMs, nearestIndex, niceMax, ticks } from './charts';

const HEIGHT = 220;
const PAD = { top: 12, right: 16, bottom: 28, left: 48 };

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(640);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver((entries) => {
      const w = entries[0]?.contentRect.width;
      if (w) setWidth(Math.max(280, Math.round(w)));
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

/**
 * Response time over the last 24h. Reachable probes form the line; failed
 * probes are drawn as markers on the baseline (their "latency" is a timeout,
 * not a response time). Crosshair + tooltip on hover, arrow keys when focused.
 */
export function LatencyChart({ results, thresholdMs }: { results: CheckResult[]; thresholdMs: number | null }) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [active, setActive] = useState<number | null>(null);

  const points = useMemo(
    () =>
      [...results]
        .sort((a, b) => a.checkedAt.localeCompare(b.checkedAt))
        .map((r) => ({ ...r, t: new Date(r.checkedAt).getTime() })),
    [results],
  );

  if (points.length === 0) {
    return (
      <div ref={ref} className="flex h-40 items-center justify-center text-sm text-muted">
        No probes in the last 24 hours yet.
      </div>
    );
  }

  const innerW = width - PAD.left - PAD.right;
  const innerH = HEIGHT - PAD.top - PAD.bottom;
  const t0 = points[0]!.t;
  const t1 = Math.max(points[points.length - 1]!.t, t0 + 60_000);
  const reachable = points.filter((p) => p.outcome !== 'down');
  const maxLatency = Math.max(0, ...reachable.map((p) => p.latencyMs), thresholdMs ?? 0);
  const yMax = niceMax(maxLatency * 1.1);
  const x = (t: number) => PAD.left + ((t - t0) / (t1 - t0)) * innerW;
  const y = (ms: number) => PAD.top + innerH - (Math.min(ms, yMax) / yMax) * innerH;

  // Break the line where probes failed so gaps are visible.
  const segments: string[] = [];
  let current: string[] = [];
  for (const p of points) {
    if (p.outcome === 'down') {
      if (current.length) segments.push(current.join(' '));
      current = [];
    } else {
      current.push(`${x(p.t).toFixed(1)},${y(p.latencyMs).toFixed(1)}`);
    }
  }
  if (current.length) segments.push(current.join(' '));

  const xs = points.map((p) => x(p.t));
  const xTicks = Array.from({ length: 5 }, (_, i) => t0 + ((t1 - t0) * i) / 4);
  const hovered = active === null ? undefined : points[active];
  const failures = points.length - reachable.length;

  const onPointerMove = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = ((e.clientX - rect.left) / rect.width) * width;
    setActive(nearestIndex(xs, px));
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    setActive((i) => {
      const start = i ?? points.length - 1;
      return Math.min(points.length - 1, Math.max(0, start + (e.key === 'ArrowLeft' ? -1 : 1)));
    });
  };

  return (
    <div ref={ref} className="relative">
      <ul className="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted" aria-label="Chart legend">
        <li className="flex items-center gap-1.5">
          <span className="inline-block h-0.5 w-4 rounded bg-series-1" aria-hidden="true" />
          Response time
        </li>
        <li className="flex items-center gap-1.5">
          <span className="inline-block size-2 rounded-full bg-critical" aria-hidden="true" />
          Failed probe ({failures})
        </li>
        {thresholdMs !== null && (
          <li className="flex items-center gap-1.5">
            <span className="inline-block h-px w-4 bg-serious" aria-hidden="true" />
            Latency threshold
          </li>
        )}
      </ul>
      <div
        tabIndex={0}
        role="group"
        aria-label={`Response time chart, ${points.length} probes. Use left and right arrow keys to inspect probes.`}
        onKeyDown={onKeyDown}
        onBlur={() => setActive(null)}
        className="rounded-lg"
      >
        <svg
          width="100%"
          height={HEIGHT}
          viewBox={`0 0 ${width} ${HEIGHT}`}
          preserveAspectRatio="none"
          onPointerMove={onPointerMove}
          onPointerLeave={() => setActive(null)}
          className="block touch-pan-y overflow-visible"
          aria-hidden="true"
        >
          {ticks(yMax).map((v) => (
            <g key={v}>
              <line x1={PAD.left} x2={width - PAD.right} y1={y(v)} y2={y(v)} stroke="var(--color-line)" strokeWidth={1} />
              <text x={PAD.left - 8} y={y(v)} dy="0.32em" textAnchor="end" fontSize={11} fill="var(--color-axis)" className="tabular">
                {formatAxisMs(v)}
              </text>
            </g>
          ))}
          {xTicks.map((t, i) => (
            <text
              key={t}
              x={x(t)}
              y={HEIGHT - 8}
              textAnchor={i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}
              fontSize={11}
              fill="var(--color-axis)"
              className="tabular"
            >
              {formatTime(new Date(t))}
            </text>
          ))}
          {thresholdMs !== null && thresholdMs <= yMax && (
            <g>
              <line
                x1={PAD.left}
                x2={width - PAD.right}
                y1={y(thresholdMs)}
                y2={y(thresholdMs)}
                stroke="var(--color-serious)"
                strokeWidth={1}
              />
              <text
                x={width - PAD.right}
                y={y(thresholdMs) - 5}
                textAnchor="end"
                fontSize={11}
                fill="var(--color-ink-2)"
                stroke="var(--color-surface)"
                strokeWidth={4}
                paintOrder="stroke"
              >
                threshold {formatMs(thresholdMs)}
              </text>
            </g>
          )}
          {segments.map((pts, i) => (
            <polyline
              key={i}
              points={pts}
              fill="none"
              stroke="var(--color-series-1)"
              strokeWidth={2}
              strokeLinejoin="round"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {points.map((p) =>
            p.outcome === 'down' ? (
              <circle
                key={p.id}
                cx={x(p.t)}
                cy={PAD.top + innerH}
                r={4}
                fill="var(--color-critical)"
                stroke="var(--color-surface)"
                strokeWidth={2}
              />
            ) : null,
          )}
          {hovered && (
            <g>
              <line
                x1={x(hovered.t)}
                x2={x(hovered.t)}
                y1={PAD.top}
                y2={PAD.top + innerH}
                stroke="var(--color-line-strong)"
                strokeWidth={1}
              />
              <circle
                cx={x(hovered.t)}
                cy={hovered.outcome === 'down' ? PAD.top + innerH : y(hovered.latencyMs)}
                r={5}
                fill={hovered.outcome === 'down' ? 'var(--color-critical)' : 'var(--color-series-1)'}
                stroke="var(--color-surface)"
                strokeWidth={2}
              />
            </g>
          )}
        </svg>
      </div>
      {hovered && (
        <div
          role="tooltip"
          aria-live="polite"
          className="pointer-events-none absolute top-8 z-10 w-max max-w-60 rounded-lg border border-line bg-raised px-3 py-2 text-xs shadow-lg"
          style={{
            left: `clamp(0px, ${((x(hovered.t) / width) * 100).toFixed(2)}% - 7.5rem, calc(100% - 15rem))`,
          }}
        >
          <div className="text-sm font-semibold text-ink tabular">
            {hovered.outcome === 'down' ? 'Failed' : formatMs(hovered.latencyMs)}
          </div>
          <div className="text-muted tabular">{formatTime(hovered.checkedAt)}</div>
          {hovered.statusCode !== null && <div className="text-ink-2">HTTP {hovered.statusCode}</div>}
          {hovered.error && <div className="mt-0.5 break-words text-critical-ink">{hovered.error}</div>}
          {hovered.outcome === 'degraded' && <div className="text-warning-ink">Above latency threshold</div>}
        </div>
      )}
    </div>
  );
}
