import { useState, type FormEvent } from 'react';

import type { Check, CheckInput, CheckKind } from '../api/types';
import { ApiError, errorMessage } from '../lib/api';
import { Button, Field, InlineAlert, Select, TextInput } from './ui';

export type CheckFormValues = {
  name: string;
  kind: CheckKind;
  target: string;
  intervalSeconds: string;
  timeoutMs: string;
  expectedStatus: string;
  latencyThresholdMs: string;
  failureThreshold: string;
  recoveryThreshold: string;
  enabled: boolean;
  isPublic: boolean;
};

export const defaultValues: CheckFormValues = {
  name: '',
  kind: 'http',
  target: 'https://',
  intervalSeconds: '60',
  timeoutMs: '10000',
  expectedStatus: '',
  latencyThresholdMs: '1000',
  failureThreshold: '3',
  recoveryThreshold: '2',
  enabled: true,
  isPublic: true,
};

export function valuesFromCheck(c: Check): CheckFormValues {
  return {
    name: c.name,
    kind: c.kind,
    target: c.target,
    intervalSeconds: String(c.intervalSeconds),
    timeoutMs: String(c.timeoutMs),
    expectedStatus: c.expectedStatus === null ? '' : String(c.expectedStatus),
    latencyThresholdMs: c.latencyThresholdMs === null ? '' : String(c.latencyThresholdMs),
    failureThreshold: String(c.failureThreshold),
    recoveryThreshold: String(c.recoveryThreshold),
    enabled: c.enabled,
    isPublic: c.isPublic,
  };
}

type Errors = Partial<Record<keyof CheckInput, string>>;

function intField(raw: string, field: keyof CheckInput, min: number, max: number, errors: Errors, label = 'Value'): number {
  const n = Number(raw);
  if (raw.trim() === '' || !Number.isInteger(n)) {
    errors[field] = `${label} must be a whole number`;
  } else if (n < min || n > max) {
    errors[field] = `${label} must be between ${min.toLocaleString('en')} and ${max.toLocaleString('en')}`;
  }
  return n;
}

/**
 * Client-side validation mirroring the API's rules for instant feedback. The
 * API re-validates everything (including SSRF rules) and its field errors are
 * shown in the same places.
 */
export function validateCheckForm(v: CheckFormValues): { input: CheckInput | null; errors: Errors } {
  const errors: Errors = {};
  const name = v.name.trim();
  const target = v.target.trim();
  if (!name) errors.name = 'Name is required';
  else if (name.length > 80) errors.name = 'Name must be at most 80 characters';

  if (v.kind === 'http') {
    try {
      const u = new URL(target);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error();
      if (!u.hostname) throw new Error();
    } catch {
      errors.target = 'Enter a full http:// or https:// URL';
    }
  } else if (!/^(\[[0-9a-fA-F:]+\]|[^\s:/]+):\d{1,5}$/.test(target)) {
    errors.target = 'Use host:port, e.g. db.example.com:5432';
  }

  const intervalSeconds = intField(v.intervalSeconds, 'intervalSeconds', 30, 3600, errors, 'Interval');
  const timeoutMs = intField(v.timeoutMs, 'timeoutMs', 500, 30000, errors, 'Timeout');
  const failureThreshold = intField(v.failureThreshold, 'failureThreshold', 1, 10, errors, 'Threshold');
  const recoveryThreshold = intField(v.recoveryThreshold, 'recoveryThreshold', 1, 10, errors, 'Threshold');
  const expectedStatus =
    v.kind === 'http' && v.expectedStatus.trim() !== ''
      ? intField(v.expectedStatus, 'expectedStatus', 100, 599, errors, 'Status')
      : null;
  const latencyThresholdMs =
    v.latencyThresholdMs.trim() !== '' ? intField(v.latencyThresholdMs, 'latencyThresholdMs', 1, 60000, errors, 'Threshold') : null;

  if (Object.keys(errors).length > 0) return { input: null, errors };
  return {
    input: {
      name,
      kind: v.kind,
      target,
      intervalSeconds,
      timeoutMs,
      expectedStatus,
      latencyThresholdMs,
      failureThreshold,
      recoveryThreshold,
      enabled: v.enabled,
      isPublic: v.isPublic,
    },
    errors,
  };
}

export function CheckForm({
  initial,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  initial: CheckFormValues;
  submitLabel: string;
  onSubmit: (input: CheckInput) => Promise<unknown>;
  onCancel: () => void;
}) {
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<Errors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const set = <K extends keyof CheckFormValues>(key: K, value: CheckFormValues[K]) =>
    setValues((prev) => ({ ...prev, [key]: value }));

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setFormError(null);
    const { input, errors: errs } = validateCheckForm(values);
    setErrors(errs);
    if (!input) return;
    setSubmitting(true);
    try {
      await onSubmit(input);
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        setErrors(err.fields as Errors);
      } else {
        setFormError(errorMessage(err));
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-5">
      {formError && <InlineAlert>{formError}</InlineAlert>}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" error={errors.name}>
          {(p) => (
            <TextInput {...p} value={values.name} onChange={(e) => set('name', e.target.value)} placeholder="Checkout API" maxLength={80} />
          )}
        </Field>
        <Field label="Type" error={errors.kind}>
          {(p) => (
            <Select
              {...p}
              value={values.kind}
              onChange={(e) => {
                const kind = e.target.value as CheckKind;
                setValues((prev) => ({
                  ...prev,
                  kind,
                  target: kind === 'tcp' && prev.target === 'https://' ? '' : prev.target,
                }));
              }}
            >
              <option value="http">HTTP(S) — GET request</option>
              <option value="tcp">TCP — port connect</option>
            </Select>
          )}
        </Field>
      </div>

      <Field
        label={values.kind === 'http' ? 'URL' : 'Host and port'}
        error={errors.target}
        hint={values.kind === 'http' ? 'Must be publicly reachable. Redirects are followed.' : 'For example 1.1.1.1:53 or db.example.com:5432.'}
      >
        {(p) => (
          <TextInput
            {...p}
            value={values.target}
            onChange={(e) => set('target', e.target.value)}
            placeholder={values.kind === 'http' ? 'https://api.example.com/health' : 'db.example.com:5432'}
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
          />
        )}
      </Field>

      <fieldset className="grid gap-4 sm:grid-cols-3">
        <legend className="mb-2 text-sm font-semibold text-ink">Schedule</legend>
        <Field label="Interval (seconds)" error={errors.intervalSeconds} hint="30 – 3600">
          {(p) => <TextInput {...p} type="number" min={30} max={3600} value={values.intervalSeconds} onChange={(e) => set('intervalSeconds', e.target.value)} />}
        </Field>
        <Field label="Timeout (ms)" error={errors.timeoutMs} hint="500 – 30000">
          {(p) => <TextInput {...p} type="number" min={500} max={30000} value={values.timeoutMs} onChange={(e) => set('timeoutMs', e.target.value)} />}
        </Field>
        {values.kind === 'http' && (
          <Field label="Expected status" error={errors.expectedStatus} hint="Blank accepts any 2xx/3xx">
            {(p) => <TextInput {...p} type="number" min={100} max={599} value={values.expectedStatus} onChange={(e) => set('expectedStatus', e.target.value)} placeholder="Any 2xx/3xx" />}
          </Field>
        )}
      </fieldset>

      <fieldset className="grid gap-4 sm:grid-cols-3">
        <legend className="mb-2 text-sm font-semibold text-ink">Alert rules</legend>
        <Field label="Open after N failures" error={errors.failureThreshold} hint="Consecutive failed (or slow) probes">
          {(p) => <TextInput {...p} type="number" min={1} max={10} value={values.failureThreshold} onChange={(e) => set('failureThreshold', e.target.value)} />}
        </Field>
        <Field label="Latency threshold (ms)" error={errors.latencyThresholdMs} hint="Blank disables the latency rule">
          {(p) => <TextInput {...p} type="number" min={1} max={60000} value={values.latencyThresholdMs} onChange={(e) => set('latencyThresholdMs', e.target.value)} placeholder="Off" />}
        </Field>
        <Field label="Resolve after N good probes" error={errors.recoveryThreshold}>
          {(p) => <TextInput {...p} type="number" min={1} max={10} value={values.recoveryThreshold} onChange={(e) => set('recoveryThreshold', e.target.value)} />}
        </Field>
      </fieldset>

      <div className="flex flex-col gap-3 sm:flex-row sm:gap-6">
        <label className="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" className="size-4 accent-accent" checked={values.enabled} onChange={(e) => set('enabled', e.target.checked)} />
          Monitoring enabled
        </label>
        <label className="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" className="size-4 accent-accent" checked={values.isPublic} onChange={(e) => set('isPublic', e.target.checked)} />
          Show on public status page
        </label>
      </div>

      <div className="flex flex-col-reverse gap-2 border-t border-line pt-4 sm:flex-row sm:justify-end">
        <Button onClick={onCancel} disabled={submitting}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" loading={submitting}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
