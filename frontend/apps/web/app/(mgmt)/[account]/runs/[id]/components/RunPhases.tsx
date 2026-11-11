import { Phase, RunStatus } from '@/components/RunTImeline/events';
import { cn } from '@/libs/utils';
import { CheckIcon, Cross2Icon, MinusIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { formatSpan } from '../../components/run-format';

interface Props {
  phases: Phase[];
  // the run still goes on: its last step is the current one
  isLive: boolean;
}

/** The steps the run went through, as its events show them, in order. */
export default function RunPhases(props: Props): ReactElement | null {
  const { phases, isLive } = props;
  if (phases.length === 0) {
    return null;
  }
  return (
    <ol
      aria-label="Run phases"
      className="m-0 grid list-none grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-2.5 p-0"
    >
      {phases.map((p) => {
        const current = isLive && p.status === 'running';
        return (
          <li
            key={p.key}
            aria-current={current ? 'step' : undefined}
            className={cn(
              'flex flex-col gap-2 rounded-xl border p-3.5',
              current
                ? 'border-primary bg-brand-soft'
                : p.status === 'failed'
                  ? 'border-danger-line bg-danger-soft'
                  : 'border-line-soft bg-card'
            )}
          >
            <span className="flex items-center gap-2 font-medium">
              <PhaseMark status={p.status} current={current} />
              {p.label}
            </span>
            <span className="text-xs text-muted-foreground">
              {describe(p, current)}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function describe(p: Phase, current: boolean): string {
  const parts: string[] = [];
  if (p.key === 'tables' && p.tablesTotal > 0) {
    parts.push(
      `${p.tablesDone} / ${p.tablesTotal} ${p.tablesTotal === 1 ? 'table' : 'tables'}`
    );
  } else {
    parts.push(
      `${p.activities} ${p.activities === 1 ? 'activity' : 'activities'}`
    );
  }
  if (current) {
    parts.push('in progress');
  } else if (p.status === 'failed') {
    parts.push('failed');
  } else if (p.status === 'canceled' || p.status === 'terminated') {
    parts.push('stopped');
  } else {
    parts.push(formatSpan(p.end.getTime() - p.start.getTime()));
  }
  return parts.join(' · ');
}

function PhaseMark(props: {
  status: RunStatus;
  current: boolean;
}): ReactElement {
  const { status, current } = props;
  const base =
    'grid size-5 shrink-0 place-items-center rounded-full text-[11px]';
  if (current || status === 'running') {
    return (
      <span
        role="img"
        aria-label="In progress"
        className={cn(base, 'bg-primary text-primary-foreground')}
      >
        <span className="size-1.5 animate-pulse-soft rounded-full bg-primary-foreground" />
      </span>
    );
  }
  if (status === 'failed') {
    return (
      <span
        role="img"
        aria-label="Failed"
        className={cn(base, 'bg-danger text-primary-foreground')}
      >
        <Cross2Icon className="h-3 w-3" />
      </span>
    );
  }
  if (status === 'canceled' || status === 'terminated') {
    return (
      <span
        role="img"
        aria-label="Stopped"
        className={cn(base, 'bg-surface-2 text-muted-foreground')}
      >
        <MinusIcon className="h-3 w-3" />
      </span>
    );
  }
  return (
    <span
      role="img"
      aria-label="Done"
      className={cn(base, 'bg-ok-soft text-ok')}
    >
      <CheckIcon className="h-3 w-3" />
    </span>
  );
}
