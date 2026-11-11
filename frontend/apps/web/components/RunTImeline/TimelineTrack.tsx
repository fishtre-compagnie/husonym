/**
 * The pieces of a run's time chart: an axis of elapsed time, a track with its
 * grid, and a bar whose fill tells the state of what it spans.
 */
import { cn } from '@/libs/utils';
import { ReactElement, ReactNode } from 'react';
import { RunStatus } from './events';

export const TIMELINE_GRID =
  'grid grid-cols-[minmax(150px,220px)_minmax(0,1fr)_88px] items-center gap-3';

const barFill: Record<RunStatus, string> = {
  completed: 'bg-quiet',
  running: 'bg-primary progress-flow',
  failed: 'bg-danger',
  canceled: 'bg-warn',
  terminated: 'bg-muted-foreground/60',
};

export function percentAt(t: Date, start: Date, total: number): number {
  if (total <= 0) return 0;
  return Math.max(
    0,
    Math.min(100, ((t.getTime() - start.getTime()) / total) * 100)
  );
}

export function TimelineBar(props: {
  status: RunStatus;
  start: Date;
  end: Date;
  spanStart: Date;
  spanTotal: number;
  className?: string;
}): ReactElement {
  const { status, start, end, spanStart, spanTotal, className } = props;
  const left = percentAt(start, spanStart, spanTotal);
  const right = percentAt(end, spanStart, spanTotal);
  return (
    <span
      className={cn(
        'relative block h-[18px] rounded bg-[repeating-linear-gradient(90deg,var(--line-soft)_0_1px,transparent_1px_25%)]',
        className
      )}
    >
      <span
        className={cn(
          'absolute bottom-[3px] top-[3px] min-w-[3px] rounded',
          barFill[status]
        )}
        style={{ left: `${left}%`, width: `${Math.max(right - left, 0)}%` }}
      />
    </span>
  );
}

/** The elapsed time beneath the chart's head: 0, then four steps to its end. */
export function TimelineAxis(props: {
  spanTotal: number;
  format(ms: number): string;
  trailing?: ReactNode;
  className?: string;
}): ReactElement {
  const { spanTotal, format, trailing, className } = props;
  const steps = [0, 0.25, 0.5, 0.75, 1];
  return (
    <div
      className={cn(
        TIMELINE_GRID,
        'px-5 pb-2 pt-1 font-mono text-[11px] text-muted-foreground',
        className
      )}
      aria-hidden="true"
    >
      <span />
      <span className="flex justify-between">
        {steps.map((s) => (
          <span key={s}>{s === 0 ? '0' : format(spanTotal * s)}</span>
        ))}
      </span>
      <span className="text-right">{trailing}</span>
    </div>
  );
}

export function TimelineLegend(props: { className?: string }): ReactElement {
  const items: { status: RunStatus; label: string; fill: string }[] = [
    { status: 'completed', label: 'Completed', fill: 'bg-quiet' },
    { status: 'running', label: 'Running', fill: 'bg-primary' },
    { status: 'failed', label: 'Failed', fill: 'bg-danger' },
    { status: 'canceled', label: 'Canceled', fill: 'bg-warn' },
  ];
  return (
    <div
      className={cn(
        'flex flex-wrap gap-4 text-xs text-muted-foreground',
        props.className
      )}
    >
      {items.map((i) => (
        <span key={i.status} className="flex items-center gap-1.5">
          <span className={cn('h-2 w-3.5 rounded-sm', i.fill)} />
          {i.label}
        </span>
      ))}
    </div>
  );
}
