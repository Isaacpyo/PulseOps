import { useState } from 'react';

import type { UptimeBar } from '../api/types';
import { formatDay, formatPercent } from '../lib/format';
import { UPTIME_LEVELS, uptimeLevel, type UptimeLevel } from '../lib/status';
import { cx } from './ui';

const levelFill: Record<UptimeLevel, string> = {
  good: 'bg-good',
  warning: 'bg-warning',
  serious: 'bg-serious',
  critical: 'bg-critical',
  none: 'bg-line',
};

export function describeBar(bar: UptimeBar): string {
  const parts = [formatDay(bar.day)];
  parts.push(bar.uptime === null ? 'no data' : `${formatPercent(bar.uptime)} uptime`);
  if (bar.incidents > 0) parts.push(`${bar.incidents} incident${bar.incidents === 1 ? '' : 's'}`);
  return parts.join(', ');
}

/** Bars shown below the `sm` breakpoint, so each cell stays wide enough to hit. */
const MOBILE_DAYS = 30;

/**
 * 90-day uptime strip: one status-coloured cell per UTC day. Each cell is
 * focusable and announces its day/uptime, so the strip doubles as an
 * accessible list; the hover/focus tooltip adds the same detail visually.
 */
export function UptimeBars({ bars, label }: { bars: UptimeBar[]; label: string }) {
  const [active, setActive] = useState<number | null>(null);
  const hovered = active === null ? undefined : bars[active];
  const total = bars.length;

  return (
    <div className="relative" onPointerLeave={() => setActive(null)}>
      <ol aria-label={`${label}: daily uptime history`} className="flex h-8 items-stretch gap-[2px]">
        {bars.map((bar, i) => {
          const level = uptimeLevel(bar.uptime);
          return (
            <li
              key={bar.day}
              tabIndex={0}
              aria-label={describeBar(bar)}
              onPointerEnter={() => setActive(i)}
              onFocus={() => setActive(i)}
              onBlur={() => setActive(null)}
              className={cx(
                'min-w-0 flex-1 rounded-[2px] transition-opacity outline-offset-1',
                levelFill[level],
                active !== null && active !== i && 'opacity-60',
                i < total - MOBILE_DAYS && 'hidden sm:block',
              )}
            />
          );
        })}
      </ol>
      <div className="mt-1.5 flex justify-between text-xs text-muted">
        <span>
          <span className="sm:hidden">{MOBILE_DAYS} days ago</span>
          <span className="hidden sm:inline">{total} days ago</span>
        </span>
        <span>Today</span>
      </div>
      {hovered && active !== null && (
        <div
          role="tooltip"
          className="pointer-events-none absolute bottom-full z-10 mb-2 w-max max-w-56 -translate-x-1/2 rounded-lg border border-line bg-raised px-3 py-2 text-xs shadow-lg"
          style={{ left: tooltipLeft(active, total) }}
        >
          <div className="font-semibold text-ink tabular">
            {hovered.uptime === null ? 'No data' : `${formatPercent(hovered.uptime)} uptime`}
          </div>
          <div className="text-muted">{formatDay(hovered.day)}</div>
          {hovered.total > 0 && (
            <div className="mt-1 text-ink-2 tabular">
              {hovered.total.toLocaleString('en')} probes
              {hovered.degraded > 0 && ` · ${hovered.degraded} slow`}
            </div>
          )}
          {hovered.incidents > 0 && (
            <div className="text-ink-2">
              {hovered.incidents} incident{hovered.incidents === 1 ? '' : 's'} opened
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function tooltipLeft(index: number, total: number): string {
  // Keep the tooltip inside the strip near the edges.
  const pct = ((index + 0.5) / total) * 100;
  return `clamp(4.5rem, ${pct}%, calc(100% - 4.5rem))`;
}

export function UptimeLegend() {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted" aria-label="Uptime colour legend">
      {UPTIME_LEVELS.map(({ level, label }) => (
        <li key={level} className="flex items-center gap-1.5">
          <span className={cx('inline-block h-3 w-2 rounded-[2px]', levelFill[level])} aria-hidden="true" />
          {label}
        </li>
      ))}
    </ul>
  );
}
