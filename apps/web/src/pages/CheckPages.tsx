import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';

import { useCheck, useCreateCheck, useDeleteCheck, useDrill, useRunCheck, useUpdateCheck } from '../api/hooks';
import type { Check, CheckDetail as CheckDetailData } from '../api/types';
import { CheckForm, defaultValues, valuesFromCheck } from '../components/CheckForm';
import { LatencyChart } from '../components/LatencyChart';
import { UptimeBars, UptimeLegend } from '../components/UptimeBars';
import {
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  InlineAlert,
  LoadingState,
  StatusBadge,
  buttonStyles,
} from '../components/ui';
import { ApiError, errorMessage } from '../lib/api';
import { formatDateTime, formatDuration, formatMs, formatPercent, formatRelative, formatTime } from '../lib/format';
import { checkStatusMeta, incidentStatusMeta } from '../lib/status';

function useCheckId(): number {
  const { id } = useParams();
  return Number(id);
}

function BackLink({ to, children }: { to: string; children: string }) {
  return (
    <Link to={to} className="text-sm font-medium text-accent-strong hover:underline">
      ← {children}
    </Link>
  );
}

export function NewCheckPage() {
  const navigate = useNavigate();
  const create = useCreateCheck();
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <BackLink to="/app">Overview</BackLink>
      <Card>
        <CardHeader title="New check" description="PulseOps probes the target immediately, then on every interval." />
        <div className="p-4 sm:p-5">
          <CheckForm
            initial={defaultValues}
            submitLabel="Create check"
            onCancel={() => navigate('/app')}
            onSubmit={async (input) => {
              const check = await create.mutateAsync(input);
              navigate(`/app/checks/${check.id}`);
            }}
          />
        </div>
      </Card>
    </div>
  );
}

export function EditCheckPage() {
  const id = useCheckId();
  const navigate = useNavigate();
  const detail = useCheck(id);
  const update = useUpdateCheck(id);

  if (detail.isPending) return <LoadingState />;
  if (detail.isError) return <ErrorState message={errorMessage(detail.error)} onRetry={() => void detail.refetch()} />;

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <BackLink to={`/app/checks/${id}`}>{detail.data.check.name}</BackLink>
      <Card>
        <CardHeader title={`Edit ${detail.data.check.name}`} />
        <div className="p-4 sm:p-5">
          <CheckForm
            initial={valuesFromCheck(detail.data.check)}
            submitLabel="Save changes"
            onCancel={() => navigate(`/app/checks/${id}`)}
            onSubmit={async (input) => {
              await update.mutateAsync(input);
              navigate(`/app/checks/${id}`);
            }}
          />
        </div>
      </Card>
    </div>
  );
}

export function CheckDetailPage() {
  const id = useCheckId();
  const detail = useCheck(id);

  if (detail.isPending) return <LoadingState label="Loading check…" />;
  if (detail.isError) {
    if (detail.error instanceof ApiError && detail.error.status === 404) {
      return <EmptyState title="Check not found" body="It may have been deleted." action={<BackLink to="/app">Back to overview</BackLink>} />;
    }
    return <ErrorState message={errorMessage(detail.error)} onRetry={() => void detail.refetch()} />;
  }
  return <CheckDetailView data={detail.data} />;
}

function CheckDetailView({ data }: { data: CheckDetailData }) {
  const { check, results, uptime, incidents } = data;
  const meta = checkStatusMeta[check.enabled ? check.status : 'paused'];

  return (
    <div className="flex flex-col gap-6">
      <BackLink to="/app">Overview</BackLink>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-2xl font-semibold break-words text-ink">{check.name}</h1>
            <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
          </div>
          <p className="mt-1 text-sm break-all text-muted">
            <span className="font-semibold uppercase">{check.kind}</span> · {check.target}
          </p>
        </div>
        <CheckActions check={check} />
      </div>

      <DrillBanner check={check} />
      {check.lastError && check.status === 'down' && <InlineAlert>Last probe failed: {check.lastError}</InlineAlert>}

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          ['Last probe', check.status === 'down' ? 'Failed' : formatMs(check.lastLatencyMs), formatRelative(check.lastCheckedAt)],
          ['Uptime (90d)', formatPercent(uptime.overall), 'Probe-weighted'],
          ['Interval', `${check.intervalSeconds}s`, `timeout ${formatMs(check.timeoutMs)}`],
          [
            'Alert rule',
            `${check.failureThreshold} failures`,
            check.latencyThresholdMs ? `or ${check.failureThreshold} probes > ${formatMs(check.latencyThresholdMs)}` : 'latency rule off',
          ],
        ].map(([label, value, detail]) => (
          <div key={label} className="rounded-xl border border-line bg-surface px-4 py-3">
            <dt className="text-sm text-muted">{label}</dt>
            <dd className="mt-0.5 text-lg font-semibold text-ink">{value}</dd>
            <dd className="text-xs text-muted">{detail}</dd>
          </div>
        ))}
      </dl>

      <Card>
        <CardHeader title="Response time" description="Last 24 hours" />
        <div className="p-4 sm:p-5">
          <LatencyChart results={results} thresholdMs={check.latencyThresholdMs} />
        </div>
      </Card>

      <Card>
        <CardHeader title="Uptime" description={`Last 90 days · ${formatPercent(uptime.overall)} overall`} />
        <div className="flex flex-col gap-3 p-4 sm:p-5">
          <UptimeBars bars={uptime.bars} label={check.name} />
          <UptimeLegend />
        </div>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader title="Incidents" description="Most recent first" />
          {incidents.length === 0 ? (
            <EmptyState title="No incidents" body="This check hasn’t tripped an alert rule." />
          ) : (
            <ul className="divide-y divide-line">
              {incidents.map((inc) => (
                <li key={inc.id}>
                  <Link to={`/app/incidents/${inc.id}`} className="flex flex-wrap items-center gap-2 px-4 py-3 hover:bg-wash sm:px-5">
                    <StatusBadge tone={incidentStatusMeta[inc.status].tone}>{incidentStatusMeta[inc.status].label}</StatusBadge>
                    <span className="min-w-0 flex-1 text-sm font-medium text-ink">{inc.title}</span>
                    <span className="text-xs text-muted">
                      {formatDateTime(inc.openedAt)}
                      {inc.resolvedAt && ` · ${formatDuration(new Date(inc.resolvedAt).getTime() - new Date(inc.openedAt).getTime())}`}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card>
          <CardHeader title="Recent probes" description="Table view of the chart above" />
          {results.length === 0 ? (
            <EmptyState title="No probes yet" body="Results appear within one interval." />
          ) : (
            <div className="max-h-96 overflow-auto">
              <table className="w-full text-left text-sm">
                <caption className="sr-only">Most recent probe results for {check.name}</caption>
                <thead className="sticky top-0 bg-surface text-xs text-muted">
                  <tr>
                    <th scope="col" className="px-4 py-2 font-medium sm:px-5">Time</th>
                    <th scope="col" className="px-2 py-2 font-medium">Result</th>
                    <th scope="col" className="px-2 py-2 text-right font-medium">Latency</th>
                    <th scope="col" className="px-4 py-2 font-medium sm:px-5">Detail</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {results.slice(0, 50).map((r) => (
                    <tr key={r.id}>
                      <td className="px-4 py-2 whitespace-nowrap text-ink-2 tabular sm:px-5">{formatTime(r.checkedAt)}</td>
                      <td className="px-2 py-2">
                        <StatusBadge tone={checkStatusMeta[r.outcome].tone}>{checkStatusMeta[r.outcome].label}</StatusBadge>
                      </td>
                      <td className="px-2 py-2 text-right text-ink tabular">{r.outcome === 'down' ? '—' : formatMs(r.latencyMs)}</td>
                      <td className="px-4 py-2 text-xs break-words text-muted sm:px-5">
                        {r.error ?? (r.statusCode ? `HTTP ${r.statusCode}` : 'Connected')}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}

function DrillBanner({ check }: { check: Check }) {
  const drill = useDrill(check.id);
  if (!check.drillUntil || new Date(check.drillUntil) <= new Date()) return null;
  return (
    <InlineAlert tone="info">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>
          <strong>Chaos drill running</strong> until {formatTime(check.drillUntil)} — probes report a simulated outage so you
          can watch an incident open, notify and resolve. Probes run every 10s during the drill.
        </span>
        <Button variant="secondary" className="min-h-8 py-1" loading={drill.isPending} onClick={() => drill.mutate(null)}>
          Stop drill
        </Button>
      </div>
    </InlineAlert>
  );
}

function CheckActions({ check }: { check: Check }) {
  const navigate = useNavigate();
  const run = useRunCheck(check.id);
  const drill = useDrill(check.id);
  const update = useUpdateCheck(check.id);
  const del = useDeleteCheck();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [message, setMessage] = useState<{ tone: 'good' | 'critical'; text: string } | null>(null);

  const report = (p: Promise<unknown>, ok: string) =>
    p.then(
      () => setMessage({ tone: 'good', text: ok }),
      (err: unknown) => setMessage({ tone: 'critical', text: errorMessage(err) }),
    );

  const drilling = check.drillUntil !== null && new Date(check.drillUntil) > new Date();

  return (
    <div className="flex w-full flex-col gap-2 sm:w-auto sm:items-end">
      <div className="flex flex-wrap gap-2">
        <Button
          loading={run.isPending}
          disabled={!check.enabled}
          onClick={() => void report(run.mutateAsync(), 'Probe queued — results appear in a few seconds.')}
        >
          Run now
        </Button>
        <Button
          loading={update.isPending}
          onClick={() =>
            void report(
              update.mutateAsync({ ...valuesToInput(check), enabled: !check.enabled }),
              check.enabled ? 'Monitoring paused.' : 'Monitoring resumed.',
            )
          }
        >
          {check.enabled ? 'Pause' : 'Resume'}
        </Button>
        {!drilling && (
          <Button
            loading={drill.isPending}
            disabled={!check.enabled}
            title="Simulate an outage for 2 minutes to exercise alerting end to end"
            onClick={() => void report(drill.mutateAsync(120), 'Chaos drill started for 2 minutes.')}
          >
            Simulate outage
          </Button>
        )}
        <Link to={`/app/checks/${check.id}/edit`} className={buttonStyles('secondary')}>
          Edit
        </Link>
        {!confirmDelete ? (
          <Button variant="danger" onClick={() => setConfirmDelete(true)}>
            Delete
          </Button>
        ) : (
          <span className="flex items-center gap-2" role="group" aria-label="Confirm delete">
            <Button
              variant="danger"
              loading={del.isPending}
              onClick={() =>
                del.mutate(check.id, {
                  onSuccess: () => navigate('/app'),
                  onError: (err) => setMessage({ tone: 'critical', text: errorMessage(err) }),
                })
              }
            >
              Confirm delete
            </Button>
            <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
          </span>
        )}
      </div>
      {message && (
        <p role="status" className={message.tone === 'good' ? 'text-sm text-good-ink' : 'text-sm text-critical-ink'}>
          {message.text}
        </p>
      )}
    </div>
  );
}

function valuesToInput(c: Check) {
  return {
    name: c.name,
    kind: c.kind,
    target: c.target,
    intervalSeconds: c.intervalSeconds,
    timeoutMs: c.timeoutMs,
    expectedStatus: c.expectedStatus,
    latencyThresholdMs: c.latencyThresholdMs,
    failureThreshold: c.failureThreshold,
    recoveryThreshold: c.recoveryThreshold,
    enabled: c.enabled,
    isPublic: c.isPublic,
  };
}
