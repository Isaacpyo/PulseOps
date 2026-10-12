import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react';
import { useId } from 'react';

import type { Tone } from '../lib/status';

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';

const variants: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-on-accent hover:bg-accent-strong dark:hover:bg-accent/85 border-transparent',
  secondary: 'bg-raised text-ink border-line-strong hover:bg-wash',
  ghost: 'bg-transparent text-ink-2 border-transparent hover:bg-wash hover:text-ink',
  danger: 'bg-raised text-critical-ink border-line-strong hover:bg-critical-wash',
};

export function buttonStyles(variant: ButtonVariant = 'secondary', className?: string): string {
  return cx(
    'inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border px-4 py-2 text-sm font-medium',
    'transition-colors disabled:cursor-not-allowed disabled:opacity-60',
    variants[variant],
    className,
  );
}

export function Button({
  variant = 'secondary',
  loading = false,
  className,
  children,
  disabled,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; loading?: boolean }) {
  return (
    <button
      type="button"
      {...rest}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={buttonStyles(variant, className)}
    >
      {loading && <Spinner />}
      {children}
    </button>
  );
}

export function Spinner({ className }: { className?: string }) {
  return (
    <svg className={cx('size-4 animate-spin', className)} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.25" strokeWidth="3" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

export function Card({ className, children, ...rest }: { className?: string; children: ReactNode } & React.HTMLAttributes<HTMLElement>) {
  return (
    <section {...rest} className={cx('rounded-xl border border-line bg-surface', className)}>
      {children}
    </section>
  );
}

export function CardHeader({ title, description, actions }: { title: ReactNode; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-line px-4 py-3 sm:px-5">
      <div className="min-w-0">
        <h2 className="text-base font-semibold text-ink">{title}</h2>
        {description && <p className="mt-0.5 text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

const toneDot: Record<Tone, string> = {
  good: 'bg-good',
  warning: 'bg-warning',
  serious: 'bg-serious',
  critical: 'bg-critical',
  neutral: 'bg-line-strong',
  info: 'bg-accent',
};

const toneText: Record<Tone, string> = {
  good: 'text-good-ink',
  warning: 'text-warning-ink',
  serious: 'text-serious-ink',
  critical: 'text-critical-ink',
  neutral: 'text-ink-2',
  info: 'text-accent-strong',
};

/** Status label: colour is always paired with text, never used alone. */
export function StatusBadge({ tone, children, className }: { tone: Tone; children: ReactNode; className?: string }) {
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1.5 rounded-full border border-line bg-raised px-2.5 py-0.5 text-xs font-medium whitespace-nowrap',
        toneText[tone],
        className,
      )}
    >
      <span className={cx('size-2 rounded-full', toneDot[tone])} aria-hidden="true" />
      {children}
    </span>
  );
}

export function LoadingState({ label = 'Loading…' }: { label?: string }) {
  return (
    <div role="status" className="flex items-center justify-center gap-3 py-16 text-sm text-muted">
      <Spinner />
      {label}
    </div>
  );
}

export function EmptyState({ title, body, action }: { title: string; body?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 py-14 text-center">
      <svg viewBox="0 0 48 48" className="mb-3 size-10 text-line-strong" fill="none" aria-hidden="true">
        <rect x="4" y="8" width="40" height="32" rx="6" stroke="currentColor" strokeWidth="2.5" />
        <path d="M10 26h8l3-6 5 12 3-6h9" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <p className="font-medium text-ink">{title}</p>
      {body && <p className="mt-1 max-w-sm text-sm text-muted">{body}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex flex-col items-center px-6 py-12 text-center">
      <p className="font-medium text-critical-ink">Couldn’t load this</p>
      <p className="mt-1 max-w-md text-sm text-muted">{message}</p>
      {onRetry && (
        <Button className="mt-4" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}

export function InlineAlert({ tone = 'critical', children }: { tone?: 'critical' | 'info' | 'good'; children: ReactNode }) {
  const styles = {
    critical: 'border-critical/40 bg-critical-wash text-critical-ink',
    info: 'border-accent/30 bg-accent-wash text-ink',
    good: 'border-good/40 bg-good-wash text-good-ink',
  }[tone];
  return (
    <div role={tone === 'critical' ? 'alert' : 'status'} className={cx('rounded-lg border px-3 py-2 text-sm', styles)}>
      {children}
    </div>
  );
}

type FieldProps = {
  label: string;
  error?: string | undefined;
  hint?: ReactNode;
  children: (props: { id: string; 'aria-invalid'?: boolean; 'aria-describedby'?: string }) => ReactNode;
};

/** Label + control + hint/error wiring with proper aria attributes. */
export function Field({ label, error, hint, children }: FieldProps) {
  const id = useId();
  const describedBy = error ? `${id}-error` : hint ? `${id}-hint` : undefined;
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-ink">
        {label}
      </label>
      {children({ id, 'aria-invalid': error ? true : undefined, 'aria-describedby': describedBy })}
      {error ? (
        <p id={`${id}-error`} className="text-sm text-critical-ink">
          {error}
        </p>
      ) : hint ? (
        <p id={`${id}-hint`} className="text-xs text-muted">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

const controlClass =
  'min-h-10 w-full rounded-lg border border-line-strong bg-raised px-3 py-2 text-sm text-ink placeholder:text-axis ' +
  'aria-[invalid=true]:border-critical focus-visible:border-accent';

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cx(controlClass, props.className)} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={cx(controlClass, props.className)} />;
}

export function Kbd({ children }: { children: ReactNode }) {
  return <code className="rounded bg-wash px-1.5 py-0.5 font-mono text-[0.8em] text-ink-2">{children}</code>;
}
