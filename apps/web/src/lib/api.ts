import { markSlow, trackRequest } from './wake';

export const API_URL = (import.meta.env.VITE_API_URL || 'http://localhost:8080').replace(/\/+$/, '');

/** Error thrown for non-2xx API responses, carrying the API's error envelope. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: Record<string, string>;

  constructor(status: number, code: string, message: string, fields: Record<string, string> = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

/** Thrown when the API could not be reached at all (after retries). */
export class NetworkError extends Error {
  constructor() {
    super('Could not reach the PulseOps API. Check your connection and try again.');
    this.name = 'NetworkError';
  }
}

type ApiConfig = {
  getToken: () => string | null;
  onUnauthorized: () => void;
};

let config: ApiConfig = { getToken: () => null, onUnauthorized: () => {} };

export function configureApi(next: ApiConfig) {
  config = next;
}

export type RequestOptions = {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
  /** Send the bearer token (default true). */
  auth?: boolean;
  /**
   * Retry while the server is unreachable or still booting. Defaults to true
   * for GET; mutations opt in only when replaying them is harmless.
   */
  retry?: boolean;
};

/** Gateway statuses Render returns while a sleeping instance boots. */
const WAKING_STATUSES = new Set([502, 503, 504]);
/** Total time to keep retrying a cold start before giving up. */
export const WAKE_BUDGET_MS = 90_000;

const sleep = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    const t = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => {
      clearTimeout(t);
      reject(signal.reason);
    });
  });

export function retryDelay(attempt: number): number {
  return Math.min(1000 * 2 ** attempt, 8000);
}

async function parseError(res: Response): Promise<ApiError> {
  try {
    const data = (await res.json()) as { error?: { code?: string; message?: string; fields?: Record<string, string> } };
    if (data.error) {
      return new ApiError(res.status, data.error.code ?? 'error', data.error.message ?? res.statusText, data.error.fields);
    }
  } catch {
    // fall through
  }
  return new ApiError(res.status, 'http_error', `Request failed (${res.status}).`);
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method ?? 'GET';
  const retry = opts.retry ?? method === 'GET';
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (opts.auth !== false) {
    const token = config.getToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }

  const started = Date.now();
  let releaseSlow: (() => void) | undefined;
  try {
    for (let attempt = 0; ; attempt++) {
      let res: Response;
      try {
        res = await trackRequest(
          fetch(`${API_URL}${path}`, {
            method,
            headers,
            body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
            signal: opts.signal,
          }),
        );
      } catch (err) {
        if (opts.signal?.aborted) throw err;
        if (!retry || Date.now() - started > WAKE_BUDGET_MS) throw new NetworkError();
        releaseSlow ??= markSlow();
        await sleep(retryDelay(attempt), opts.signal);
        continue;
      }

      if (retry && WAKING_STATUSES.has(res.status) && Date.now() - started < WAKE_BUDGET_MS) {
        releaseSlow ??= markSlow();
        await sleep(retryDelay(attempt), opts.signal);
        continue;
      }
      if (res.status === 401 && opts.auth !== false) {
        config.onUnauthorized();
      }
      if (!res.ok) throw await parseError(res);
      if (res.status === 204) return undefined as T;
      return (await res.json()) as T;
    }
  } finally {
    releaseSlow?.();
  }
}

/** Fire-and-forget request that starts waking a sleeping API early. */
export function prewarm() {
  void request<{ status: string }>('/healthz', { auth: false }).catch(() => {});
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError || err instanceof NetworkError) return err.message;
  return 'Something unexpected went wrong.';
}
