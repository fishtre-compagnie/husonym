/**
 * The building blocks of the Husonym redesign, shared by every screen.
 *
 * Colors come from the tokens in app/globals.css (bg-card, border-line-soft,
 * text-ok, bg-danger-soft…), so every block follows the light and dark themes.
 * A screen that needs a variation builds it next to itself; only blocks used by
 * several screens belong here.
 */
import { cn } from '@/libs/utils';
import { ComponentPropsWithoutRef, ReactElement, ReactNode } from 'react';

/** The title of a page, what it says beneath it, and its actions on the right. */
export function PageHeader(props: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}): ReactElement {
  const { title, description, actions, className } = props;
  return (
    <div
      className={cn(
        'flex flex-wrap items-end justify-between gap-4',
        className
      )}
    >
      <div className="min-w-0">
        <h1 className="m-0 font-display text-[30px] font-semibold leading-tight tracking-[-0.02em]">
          {title}
        </h1>
        {description && (
          <p className="mt-1.5 text-muted-foreground">{description}</p>
        )}
      </div>
      {actions && (
        <div className="flex flex-wrap items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

/** The column every page lays its content in. */
export function PageBody(props: {
  children: ReactNode;
  className?: string;
}): ReactElement {
  return (
    <div
      className={cn(
        'mx-auto flex w-full max-w-[1840px] flex-col gap-7 px-4 py-8 sm:px-8',
        props.className
      )}
    >
      {props.children}
    </div>
  );
}

/** A surface: rounded, on the card color, with a soft line. */
export function Panel(
  props: ComponentPropsWithoutRef<'section'> & { tone?: 'default' | 'danger' }
): ReactElement {
  const { className, tone = 'default', ...rest } = props;
  return (
    <section
      className={cn(
        'overflow-hidden rounded-2xl border bg-card',
        tone === 'danger'
          ? 'border-danger-line shadow-[inset_0_3px_0_var(--danger)]'
          : 'border-line-soft',
        className
      )}
      {...rest}
    />
  );
}

/** The head of a Panel: its title and, on the right, a link or actions. */
export function PanelHeader(props: {
  title: ReactNode;
  leading?: ReactNode;
  actions?: ReactNode;
  className?: string;
}): ReactElement {
  const { title, leading, actions, className } = props;
  return (
    <div
      className={cn(
        'flex items-center gap-2.5 border-b border-line-soft px-5 py-4',
        className
      )}
    >
      {leading}
      <h2 className="m-0 font-display text-base font-semibold">{title}</h2>
      {actions && (
        <div className="ml-auto flex items-center gap-2 text-[13px]">
          {actions}
        </div>
      )}
    </div>
  );
}

/** A row of a Panel's list: a hover tint and a line beneath. */
export const rowClass =
  'border-b border-line-soft last:border-b-0 text-foreground transition-colors hover:bg-hover';

/** A number and what it counts, with an optional line and a small chart. */
export function StatCard(props: {
  label: ReactNode;
  value: ReactNode;
  note?: ReactNode;
  noteTone?: Tone;
  chart?: ReactNode;
  tone?: 'default' | 'danger';
  footer?: ReactNode;
}): ReactElement {
  const { label, value, note, noteTone = 'muted', chart, tone, footer } = props;
  return (
    <Panel tone={tone} className="flex flex-col gap-2.5 p-5">
      <span
        className={cn(
          'text-[13px]',
          tone === 'danger'
            ? 'font-medium text-danger-fg'
            : 'text-muted-foreground'
        )}
      >
        {label}
      </span>
      <span className="font-display text-[32px] font-semibold leading-none tracking-[-0.02em] tabular-nums">
        {value}
      </span>
      {note && (
        <span className={cn('text-[13px]', toneText[noteTone])}>{note}</span>
      )}
      {chart}
      {footer}
    </Panel>
  );
}

export type Tone = 'ok' | 'warn' | 'danger' | 'brand' | 'quiet' | 'muted';

const toneText: Record<Tone, string> = {
  ok: 'text-ok',
  warn: 'text-warn-fg',
  danger: 'text-danger-fg',
  brand: 'text-link',
  quiet: 'text-quiet-fg',
  muted: 'text-muted-foreground',
};
const toneDot: Record<Tone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  danger: 'bg-danger',
  brand: 'bg-link',
  quiet: 'bg-quiet',
  muted: 'bg-muted-foreground',
};
const tonePill: Record<Tone, string> = {
  ok: 'bg-ok-soft text-ok',
  warn: 'bg-warn-soft text-warn-fg',
  danger: 'bg-danger-soft text-danger-fg',
  brand: 'bg-brand-soft text-link',
  quiet: 'bg-quiet-soft text-quiet-fg',
  muted: 'bg-surface-2 text-muted-foreground',
};

export function toneTextClass(tone: Tone): string {
  return toneText[tone];
}

/** A small round mark of a state. `label` is read by screen readers. */
export function StatusDot(props: {
  tone: Tone;
  label?: string;
  pulse?: boolean;
  className?: string;
}): ReactElement {
  const { tone, label, pulse, className } = props;
  return (
    <span
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className={cn(
        'inline-block size-2 shrink-0 rounded-full',
        toneDot[tone],
        pulse && 'animate-pulse-soft',
        className
      )}
    />
  );
}

/** A rounded label on a soft tint of its tone. */
export function Pill(props: {
  tone?: Tone;
  children: ReactNode;
  className?: string;
}): ReactElement {
  const { tone = 'muted', children, className } = props;
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium',
        tonePill[tone],
        className
      )}
    >
      {children}
    </span>
  );
}

/** A key of the keyboard. */
export function Kbd(props: {
  children: ReactNode;
  className?: string;
}): ReactElement {
  return (
    <kbd
      className={cn(
        'rounded-[5px] border border-border px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground',
        props.className
      )}
    >
      {props.children}
    </kbd>
  );
}

/** A bar that fills to `value` (0–100); `live` adds a moving sheen. */
export function ProgressBar(props: {
  value: number;
  live?: boolean;
  tone?: 'brand' | 'ok' | 'danger' | 'warn';
  label?: string;
  className?: string;
}): ReactElement {
  const { value, live, tone = 'brand', label, className } = props;
  const pct = Math.max(0, Math.min(100, value));
  const fill = {
    brand: 'bg-primary',
    ok: 'bg-ok',
    danger: 'bg-danger',
    warn: 'bg-warn',
  }[tone];
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      aria-label={label}
      className={cn('h-2 overflow-hidden rounded-full bg-surface-2', className)}
    >
      <div
        className={cn('h-full rounded-full', fill, live && 'progress-flow')}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

/** A small label above a group, in capitals. */
export function Eyebrow(props: {
  children: ReactNode;
  className?: string;
}): ReactElement {
  return (
    <span
      className={cn(
        'text-[11px] uppercase tracking-[0.08em] text-muted-foreground',
        props.className
      )}
    >
      {props.children}
    </span>
  );
}

/** A segmented choice, such as a period: 24 h, 7 days, 30 days. */
export function Segmented<T extends string>(props: {
  options: { value: T; label: ReactNode }[];
  value: T;
  onChange: (value: T) => void;
  ariaLabel: string;
  className?: string;
}): ReactElement {
  const { options, value, onChange, ariaLabel, className } = props;
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn(
        'flex rounded-[10px] border border-border bg-card p-[3px]',
        className
      )}
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
          className={cn(
            'cursor-pointer rounded-[7px] px-3 py-1.5 text-sm transition-colors',
            o.value === value
              ? 'bg-surface-2 text-hi'
              : 'text-muted-foreground hover:text-foreground'
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
