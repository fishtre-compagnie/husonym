/**
 * Small charts and pieces of the home dashboard. Every color is a token, so they follow
 * both themes.
 */
import { cn } from '@/libs/utils';
import { ReactElement, ReactNode } from 'react';

/** A line through `values`, scaled to its highest point. Decorative: say it in text too. */
export function Sparkline(props: {
  values: number[];
  className?: string;
}): ReactElement {
  const { values, className } = props;
  const max = Math.max(1, ...values);
  const n = values.length;
  const points = values
    .map((v, i) => {
      const x = n > 1 ? (i / (n - 1)) * 200 : 100;
      // 4 px of margin above and below, so the stroke is never cut.
      const y = 32 - (v / max) * 28;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
  return (
    <svg
      viewBox="0 0 200 36"
      preserveAspectRatio="none"
      className={cn('h-9 w-full text-link', className)}
      aria-hidden="true"
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

export interface Segment {
  key: string;
  value: number;
  /** A background class, such as bg-primary. */
  className: string;
}

/** A bar split into segments proportional to their values. */
export function SegmentBar(props: {
  segments: Segment[];
  label: string;
  className?: string;
}): ReactElement {
  const { segments, label, className } = props;
  const total = segments.reduce((s, x) => s + x.value, 0);
  return (
    <div
      role="img"
      aria-label={label}
      className={cn(
        'flex h-2 gap-0.5 overflow-hidden rounded-full bg-surface-2',
        className
      )}
    >
      {total > 0 &&
        segments
          .filter((s) => s.value > 0)
          .map((s) => (
            <span
              key={s.key}
              className={cn('h-full', s.className)}
              // At least a sliver, so that a single column in clear is seen.
              style={{
                width: `${(s.value / total) * 100}%`,
                minWidth: '3px',
              }}
            />
          ))}
    </div>
  );
}

/** A bar for work whose progress is not known: it moves, it does not fill. */
export function LiveBar(props: { label: string }): ReactElement {
  return (
    <div
      role="progressbar"
      aria-label={props.label}
      aria-valuetext="In progress"
      className="h-2 overflow-hidden rounded-full bg-surface-2"
    >
      <div className="progress-flow h-full w-full rounded-full bg-primary/60" />
    </div>
  );
}

/** A line of a legend: a swatch, what it stands for, and its count. */
export function LegendRow(props: {
  swatch: string;
  label: ReactNode;
  value: ReactNode;
  valueClassName?: string;
}): ReactElement {
  return (
    <li className="flex items-center gap-2.5">
      <span
        className={cn('size-2.5 shrink-0 rounded-[3px]', props.swatch)}
        aria-hidden="true"
      />
      <span className="flex-1">{props.label}</span>
      <span className={cn('font-mono tabular-nums', props.valueClassName)}>
        {props.value}
      </span>
    </li>
  );
}

/** What a list says when it has nothing to show. */
export function EmptyRow(props: { children: ReactNode }): ReactElement {
  return (
    <p className="m-0 px-5 py-6 text-sm text-muted-foreground">
      {props.children}
    </p>
  );
}

export function ArrowRight(): ReactElement {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  );
}

/** A link that reads as a row of a Panel, with a visible focus. */
export const rowLinkFocus =
  'outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring';
