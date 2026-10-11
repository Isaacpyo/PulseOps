import { useSyncExternalStore } from 'react';

/**
 * Tracks API requests that are taking unusually long. The production API runs
 * on Render's free tier, which spins down when idle; the first request after a
 * nap can take 30–60s while the instance boots. Any request slower than the
 * threshold flips the app into a "waking server" state so users see why.
 */

export const SLOW_REQUEST_MS = 2500;

let slow = 0;
const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

export function markSlow(): () => void {
  slow += 1;
  emit();
  let done = false;
  return () => {
    if (done) return;
    done = true;
    slow = Math.max(0, slow - 1);
    emit();
  };
}

/** Wraps a promise; if it is still pending after `thresholdMs` it counts as slow. */
export function trackRequest<T>(promise: Promise<T>, thresholdMs = SLOW_REQUEST_MS): Promise<T> {
  let release: (() => void) | undefined;
  const timer = setTimeout(() => {
    release = markSlow();
  }, thresholdMs);
  return promise.finally(() => {
    clearTimeout(timer);
    release?.();
  });
}

export function isServerWaking(): boolean {
  return slow > 0;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useServerWaking(): boolean {
  return useSyncExternalStore(subscribe, isServerWaking, () => false);
}

/** Test helper. */
export function resetWakeState() {
  slow = 0;
  emit();
}
