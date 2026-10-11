import { Link } from 'react-router';

import { useChecks, useIncidents } from '../api/hooks';
import type { CheckSummary } from '../api/types';
import { Sparkline } from '../components/Sparkline';
import { buttonStyles, Card, CardHeader, EmptyState, ErrorState, LoadingState, StatusBadge, cx } from '../components/ui';
import { errorMessage } from '../lib/api';
import { formatMs, formatPercent, formatRelative } from '../lib/format';
import { checkStatusMeta, incidentStatusMeta } from '../lib/status';

export function summarise(checks: CheckSummary[]) {
  const enabled = checks.filter((c) => c.enabled);
  const count = (s: string) => enabled.filter((c) => c.status === s).length;
  let up = 0;
  let total = 0;
  let latencySum = 0;
  let latencyN = 0;
  for (const c of enabled) {
    if (c.uptime24h !== null) {
      // Weight each check equally for the headline figure.
      up += c.uptime24h;
      total += 1;
    }
    if (c.avgLatency24h !== null) {
      latencySum += c.avgLatency24h;
      latencyN += 1;
    }
  }
  return {
    total: checks.length,
    up: count('up'),
    degraded: count('degraded'),
    down: count('down'),
    paused: checks.length - enabled.length,
    uptime: total ? up / total : null,
    latency: latencyN ? Math.round(latencySum / latencyN) : null,
  };
}

function Stat({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div className="rounded-xl border border-line bg-surface px-4 py-4">
      <div className="text-sm text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-ink">{value}</div>
      {detail && <div className="mt-0.5 text-xs text-muted">{detail}</div>}
    </div>
  );
}

export function Overview() {
  const checks = useChecks();
  const incidents = useIncidents('active');

  if (checks.isPending) return <LoadingState label="Loading checks…" />;
  if (checks.isError) return <ErrorState message={errorMessage(checks.error)} onRetry={() => void checks.refetch()} />;

  const s = summarise(checks.data);
  const active = incidents.data ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">Overview</h1>
          <p className="text-sm text-muted">Live state of every check. Refreshes every 15 seconds.</p>
        </div>
        <Link to="/app/checks/new" className={buttonStyles('primary')}>
          New check
        </Link>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat
          label="Checks up"
          value={`${s.up} / ${s.total - s.paused}`}
          detail={[s.degraded && `${s.degraded} degraded`, s.down && `${s.down} down`, s.paused && `${s.paused} paused`]
            .filter(Boolean)
            .join(' · ') || 'All reachable'}
        />
        <Stat label="Active incidents" value={String(active.length)} detail={active.length ? 'Needs attention' : 'Nothing open'} />
        <Stat label="Uptime (24h)" value={formatPercent(s.uptime)} detail="Average across checks" />
        <Stat label="Avg response (24h)" value={formatMs(s.latency)} detail="Reachable probes only" />
      </div>

      {active.length > 0 && (
        <Card aria-labelledby="active-incidents">
          <CardHeader title={<span id="active-incidents">Active incidents</span>} />
          <ul className="divide-y divide-line">
            {active.map((inc) => (
              <li key={inc.id}>
                <Link
                  to={`/app/incidents/${inc.id}`}
                  className="flex flex-col gap-1 px-4 py-3 hover:bg-wash sm:flex-row sm:items-center sm:gap-3 sm:px-5"
                >
                  <span className="flex min-w-0 flex-1 items-center gap-3">
                    <StatusBadge tone={incidentStatusMeta[inc.status].tone}>{incidentStatusMeta[inc.status].label}</StatusBadge>
                    <span className="min-w-0 font-medium text-ink">{inc.title}</span>
                  </span>
                  <span className="text-sm text-muted">opened {formatRelative(inc.openedAt)}</span>
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      )}

      <Card aria-labelledby="checks-title">
        <CardHeader title={<span id="checks-title">Checks</span>} description={`${s.total} monitored targets`} />
        {checks.data.length === 0 ? (
          <EmptyState
            title="No checks yet"
            body="Add an HTTP endpoint or TCP port and PulseOps will start probing it right away."
            action={
              <Link to="/app/checks/new" className={buttonStyles('primary')}>
                Create your first check
              </Link>
            }
          />
        ) : (
          <CheckList checks={checks.data} />
        )}
      </Card>
    </div>
  );
}

function CheckList({ checks }: { checks: CheckSummary[] }) {
  return (
    <ul className="divide-y divide-line" aria-label="Checks">
      {checks.map((c) => {
        const meta = checkStatusMeta[c.enabled ? c.status : 'paused'];
        return (
          <li key={c.id}>
            <Link
              to={`/app/checks/${c.id}`}
              className="grid grid-cols-[1fr_auto] items-center gap-x-4 gap-y-2 px-4 py-3 hover:bg-wash sm:px-5 md:grid-cols-[minmax(0,2.2fr)_6.5rem_7.5rem_minmax(0,1fr)_minmax(0,1fr)]"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium text-ink">{c.name}</span>
                  <span className="rounded bg-wash px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-ink-2 uppercase">
                    {c.kind}
                  </span>
                </div>
                <div className="truncate text-xs text-muted">{c.target}</div>
              </div>
              <div className="justify-self-end md:justify-self-start">
                <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
              </div>
              <div className="col-span-2 flex items-center gap-4 md:col-span-1">
                <Sparkline points={c.recent} />
              </div>
              <dl className="col-span-2 flex gap-4 text-sm md:col-span-2 md:grid md:grid-cols-2">
                <div>
                  <dt className="text-xs text-muted">Uptime 24h</dt>
                  <dd className={cx('font-medium tabular', c.uptime24h !== null && c.uptime24h < 99 ? 'text-critical-ink' : 'text-ink')}>
                    {formatPercent(c.uptime24h)}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-muted">Last probe</dt>
                  <dd className="font-medium text-ink tabular">
                    {c.status === 'down' ? 'Failed' : formatMs(c.lastLatencyMs)}{' '}
                    <span className="text-xs font-normal text-muted">{formatRelative(c.lastCheckedAt)}</span>
                  </dd>
                </div>
              </dl>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}
