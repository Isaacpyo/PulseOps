import { useEffect } from 'react';
import { Link, useParams } from 'react-router';

import { usePublicStatus } from '../api/hooks';
import type { ComponentState, PublicIncident } from '../api/types';
import { Logo } from '../components/Logo';
import { UptimeBars, UptimeLegend } from '../components/UptimeBars';
import { EmptyState, ErrorState, LoadingState, StatusBadge, cx } from '../components/ui';
import { ApiError, errorMessage } from '../lib/api';
import { formatDateTime, formatDuration, formatPercent, formatRelative } from '../lib/format';
import { componentStateMeta, overallHeadline } from '../lib/status';

const bannerStyle: Record<ComponentState, string> = {
  operational: 'border-good/40 bg-good-wash text-good-ink',
  degraded: 'border-warning/50 bg-warning-wash text-warning-ink',
  partial_outage: 'border-serious/50 bg-critical-wash text-serious-ink',
  major_outage: 'border-critical/50 bg-critical-wash text-critical-ink',
  paused: 'border-line bg-wash text-ink-2',
  pending: 'border-line bg-wash text-ink-2',
};

function BannerIcon({ state }: { state: ComponentState }) {
  const ok = state === 'operational';
  return (
    <svg viewBox="0 0 24 24" className="size-6 shrink-0" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
      <circle cx="12" cy="12" r="10" />
      {ok ? <path d="m7.5 12.5 3 3 6-6.5" strokeLinecap="round" strokeLinejoin="round" /> : <path d="M12 7v6m0 3.5v.5" strokeLinecap="round" />}
    </svg>
  );
}

export function StatusPage() {
  const slug = useParams().slug ?? '';
  const status = usePublicStatus(slug);

  useEffect(() => {
    if (status.data) document.title = `${status.data.workspace.name} status`;
    return () => {
      document.title = 'PulseOps — monitoring & incident response';
    };
  }, [status.data]);

  return (
    <div className="min-h-dvh">
      <main className="mx-auto flex max-w-4xl flex-col gap-6 px-4 py-8 sm:py-12">
        {status.isPending ? (
          <LoadingState label="Loading status…" />
        ) : status.isError ? (
          status.error instanceof ApiError && status.error.status === 404 ? (
            <EmptyState title="Status page not found" body={`There is no status page called “${slug}”.`} />
          ) : (
            <ErrorState message={errorMessage(status.error)} onRetry={() => void status.refetch()} />
          )
        ) : (
          <>
            <header className="flex flex-wrap items-center justify-between gap-3">
              <h1 className="text-2xl font-semibold text-ink sm:text-3xl">{status.data.workspace.name} status</h1>
              <span className="text-sm text-muted">Updated {formatRelative(status.data.generatedAt)}</span>
            </header>

            <div className={cx('flex items-center gap-3 rounded-xl border px-4 py-4 sm:px-5', bannerStyle[status.data.state])}>
              <BannerIcon state={status.data.state} />
              <p className="text-lg font-semibold">{overallHeadline[status.data.state]}</p>
            </div>

            {status.data.activeIncidents.length > 0 && (
              <section aria-labelledby="active-heading" className="flex flex-col gap-3">
                <h2 id="active-heading" className="text-lg font-semibold text-ink">
                  Active incidents
                </h2>
                {status.data.activeIncidents.map((inc) => (
                  <IncidentItem key={inc.id} inc={inc} />
                ))}
              </section>
            )}

            <section aria-labelledby="components-heading" className="rounded-xl border border-line bg-surface">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3 sm:px-5">
                <h2 id="components-heading" className="font-semibold text-ink">
                  Components
                </h2>
                <UptimeLegend />
              </div>
              {status.data.components.length === 0 ? (
                <EmptyState title="Nothing to show yet" body="No checks are published to this status page." />
              ) : (
                <ul className="divide-y divide-line">
                  {status.data.components.map((c) => {
                    const meta = componentStateMeta[c.state];
                    return (
                      <li key={c.id} className="flex flex-col gap-3 px-4 py-4 sm:px-5">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <span className="font-medium text-ink">{c.name}</span>
                          <div className="flex items-center gap-3">
                            <span className="text-sm text-muted tabular">{formatPercent(c.uptime90)} uptime</span>
                            <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
                          </div>
                        </div>
                        <UptimeBars bars={c.bars} label={c.name} />
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>

            <section aria-labelledby="history-heading" className="flex flex-col gap-3">
              <h2 id="history-heading" className="text-lg font-semibold text-ink">
                Past incidents <span className="text-sm font-normal text-muted">· last 14 days</span>
              </h2>
              {status.data.recentIncidents.length === 0 ? (
                <p className="rounded-xl border border-line bg-surface px-4 py-6 text-center text-sm text-muted">
                  No incidents in the last 14 days.
                </p>
              ) : (
                status.data.recentIncidents.map((inc) => <IncidentItem key={inc.id} inc={inc} />)
              )}
            </section>
          </>
        )}
        <footer className="mt-4 flex items-center justify-center gap-2 text-sm text-muted">
          <Logo className="size-5" />
          Powered by{' '}
          <Link to="/" className="font-medium text-accent-strong hover:underline">
            PulseOps
          </Link>
        </footer>
      </main>
    </div>
  );
}

function IncidentItem({ inc }: { inc: PublicIncident }) {
  const resolved = inc.status === 'resolved';
  const duration = formatDuration(
    (inc.resolvedAt ? new Date(inc.resolvedAt) : new Date()).getTime() - new Date(inc.openedAt).getTime(),
  );
  return (
    <article className="rounded-xl border border-line bg-surface px-4 py-3 sm:px-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-medium text-ink">{inc.title}</h3>
        <StatusBadge tone={resolved ? 'good' : inc.status === 'acknowledged' ? 'warning' : 'critical'}>
          {resolved ? 'Resolved' : inc.status === 'acknowledged' ? 'Investigating' : 'Identified'}
        </StatusBadge>
      </div>
      <p className="mt-1 text-sm text-muted">
        {inc.cause === 'down' ? 'Outage' : 'Degraded performance'} · started {formatDateTime(inc.openedAt)} ·{' '}
        {resolved ? `lasted ${duration}` : `ongoing for ${duration}`}
      </p>
    </article>
  );
}
