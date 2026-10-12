import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { ApiError } from '../lib/api';
import { CheckForm, defaultValues, validateCheckForm, type CheckFormValues } from './CheckForm';

const valid: CheckFormValues = { ...defaultValues, name: 'API', target: 'https://api.example.com/health' };

describe('validateCheckForm', () => {
  it('parses a valid HTTP check', () => {
    const { input, errors } = validateCheckForm(valid);
    expect(errors).toEqual({});
    expect(input).toMatchObject({
      name: 'API',
      kind: 'http',
      intervalSeconds: 60,
      timeoutMs: 10000,
      expectedStatus: null,
      latencyThresholdMs: 1000,
      failureThreshold: 3,
    });
  });

  it('accepts TCP host:port and IPv6 literals, dropping expected status', () => {
    expect(validateCheckForm({ ...valid, kind: 'tcp', target: 'db.example.com:5432', expectedStatus: '200' }).input?.expectedStatus).toBeNull();
    expect(validateCheckForm({ ...valid, kind: 'tcp', target: '[2606:4700::1111]:53' }).errors).toEqual({});
  });

  it.each([
    [{ name: '  ' }, 'name'],
    [{ target: 'example.com' }, 'target'],
    [{ target: 'ftp://example.com' }, 'target'],
    [{ kind: 'tcp' as const, target: 'example.com' }, 'target'],
    [{ intervalSeconds: '10' }, 'intervalSeconds'],
    [{ intervalSeconds: '60.5' }, 'intervalSeconds'],
    [{ timeoutMs: '40000' }, 'timeoutMs'],
    [{ expectedStatus: '700' }, 'expectedStatus'],
    [{ latencyThresholdMs: '0' }, 'latencyThresholdMs'],
    [{ failureThreshold: '11' }, 'failureThreshold'],
  ])('rejects %o', (patch, field) => {
    const { input, errors } = validateCheckForm({ ...valid, ...patch });
    expect(input).toBeNull();
    expect(errors).toHaveProperty(field);
  });

  it('treats a blank latency threshold as "rule disabled"', () => {
    expect(validateCheckForm({ ...valid, latencyThresholdMs: '' }).input?.latencyThresholdMs).toBeNull();
  });
});

describe('<CheckForm>', () => {
  it('shows inline errors and does not submit invalid input', async () => {
    const onSubmit = vi.fn();
    render(<CheckForm initial={defaultValues} submitLabel="Create check" onSubmit={onSubmit} onCancel={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: 'Create check' }));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByText('Name is required')).toBeInTheDocument();
  });

  it('submits parsed values', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<CheckForm initial={defaultValues} submitLabel="Create check" onSubmit={onSubmit} onCancel={() => {}} />);
    await userEvent.type(screen.getByLabelText('Name'), 'Website');
    const url = screen.getByLabelText('URL');
    await userEvent.clear(url);
    await userEvent.type(url, 'https://example.com');
    await userEvent.click(screen.getByRole('button', { name: 'Create check' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ name: 'Website', target: 'https://example.com', kind: 'http' }));
  });

  it('maps API field errors back onto inputs', async () => {
    const onSubmit = vi.fn().mockRejectedValue(
      new ApiError(422, 'validation_failed', 'Some fields are invalid.', { target: 'must be a public address' }),
    );
    render(<CheckForm initial={valid} submitLabel="Save" onSubmit={onSubmit} onCancel={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('must be a public address')).toBeInTheDocument();
  });
});
