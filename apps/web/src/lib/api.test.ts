import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError, NetworkError, configureApi, request, retryDelay } from './api';
import { isServerWaking, resetWakeState } from './wake';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('request', () => {
  const fetchMock = vi.fn<typeof fetch>();
  const onUnauthorized = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock);
    configureApi({ getToken: () => 'tok', onUnauthorized });
    resetWakeState();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    fetchMock.mockReset();
    onUnauthorized.mockReset();
  });

  it('sends the bearer token and JSON body', async () => {
    fetchMock.mockResolvedValue(json(200, { ok: true }));
    await expect(request('/api/x', { method: 'POST', body: { a: 1 } })).resolves.toEqual({ ok: true });
    const [, init] = fetchMock.mock.calls[0]!;
    expect(init?.headers).toMatchObject({ Authorization: 'Bearer tok', 'Content-Type': 'application/json' });
    expect(init?.body).toBe('{"a":1}');
  });

  it('omits the token for unauthenticated calls', async () => {
    fetchMock.mockResolvedValue(json(200, {}));
    await request('/api/public', { auth: false });
    expect(fetchMock.mock.calls[0]![1]?.headers).not.toHaveProperty('Authorization');
  });

  it('parses the error envelope into an ApiError', async () => {
    fetchMock.mockResolvedValue(
      json(422, { error: { code: 'validation_failed', message: 'Some fields are invalid.', fields: { name: 'is required' } } }),
    );
    const err = await request('/api/checks', { method: 'POST', body: {} }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 422, code: 'validation_failed', fields: { name: 'is required' } });
  });

  it('signals 401s so the session can be cleared', async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: 'unauthenticated', message: 'Sign in' } }));
    await expect(request('/api/me')).rejects.toBeInstanceOf(ApiError);
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });

  it('returns undefined for 204 responses', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await expect(request('/api/checks/1', { method: 'DELETE' })).resolves.toBeUndefined();
  });

  it('retries GETs through a cold start and reports the waking state', async () => {
    vi.useFakeTimers();
    fetchMock
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(new Response('booting', { status: 503 }))
      .mockResolvedValueOnce(json(200, { checks: [] }));

    const p = request<{ checks: unknown[] }>('/api/checks');
    await vi.advanceTimersByTimeAsync(0);
    expect(isServerWaking()).toBe(true);
    await vi.advanceTimersByTimeAsync(retryDelay(0) + retryDelay(1));
    await expect(p).resolves.toEqual({ checks: [] });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(isServerWaking()).toBe(false);
  });

  it('does not replay mutations unless asked to', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
    await expect(request('/api/checks', { method: 'POST', body: {} })).rejects.toBeInstanceOf(NetworkError);
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('flags requests that are merely slow as waking', async () => {
    vi.useFakeTimers();
    let resolve!: (r: Response) => void;
    fetchMock.mockReturnValue(new Promise<Response>((r) => (resolve = r)));
    const p = request('/api/checks');
    await vi.advanceTimersByTimeAsync(2600);
    expect(isServerWaking()).toBe(true);
    resolve(json(200, {}));
    await p;
    expect(isServerWaking()).toBe(false);
  });
});

it('backs off exponentially up to 8s', () => {
  expect([0, 1, 2, 3, 4, 10].map(retryDelay)).toEqual([1000, 2000, 4000, 8000, 8000, 8000]);
});
