import { useServerWaking } from '../lib/wake';
import { Spinner } from './ui';

/**
 * Shown while any API request is unusually slow — almost always the free-tier
 * API instance booting after being idle.
 */
export function WakeBanner() {
  const waking = useServerWaking();
  if (!waking) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed inset-x-0 top-0 z-50 flex justify-center px-4 pt-3"
    >
      <div className="flex max-w-xl items-start gap-3 rounded-xl border border-accent/30 bg-raised px-4 py-3 text-sm shadow-lg">
        <Spinner className="mt-0.5 shrink-0 text-accent" />
        <p className="text-ink">
          <span className="font-semibold">Waking the server…</span>{' '}
          <span className="text-ink-2">
            The demo API runs on a free instance that sleeps when idle. The first request can take up to a minute —
            everything after that is fast.
          </span>
        </p>
      </div>
    </div>
  );
}
