import { Phase, RunStatus } from '@/components/RunTImeline/events';
import { cn } from '@/libs/utils';
import { CheckIcon, Cross2Icon, MinusIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { RunFormat, useRunFormat } from '../../components/run-format';
import { useTranslations } from 'next-intl';

interface Props {
  phases: Phase[];
  // the run still goes on: its last step is the current one
  isLive: boolean;
}

/** The steps the run went through, as its events show them, in order. */
export default function RunPhases(props: Props): ReactElement | null {
  const { phases, isLive } = props;
  const t = useTranslations('runs.phases');
  const fmt = useRunFormat();
  if (phases.length === 0) {
    return null;
  }
  return (
    <ol
      aria-label={t('label')}
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
              {t(p.key)}
            </span>
            <span className="text-xs text-muted-foreground">
              {describe(p, current, t, fmt)}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function describe(
  p: Phase,
  current: boolean,
  t: ReturnType<typeof useTranslations<'runs.phases'>>,
  fmt: RunFormat
): string {
  const parts: string[] = [];
  if (p.key === 'tables' && p.tablesTotal > 0) {
    parts.push(t('tablesCount', { done: p.tablesDone, total: p.tablesTotal }));
  } else {
    parts.push(t('activitiesCount', { count: p.activities }));
  }
  if (current) {
    parts.push(t('inProgress'));
  } else if (p.status === 'failed') {
    parts.push(t('failed'));
  } else if (p.status === 'canceled' || p.status === 'terminated') {
    parts.push(t('stopped'));
  } else {
    parts.push(fmt.span(p.end.getTime() - p.start.getTime()));
  }
  return parts.join(' · ');
}

function PhaseMark(props: {
  status: RunStatus;
  current: boolean;
}): ReactElement {
  const { status, current } = props;
  const t = useTranslations('runs.phases');
  const base =
    'grid size-5 shrink-0 place-items-center rounded-full text-[11px]';
  if (current || status === 'running') {
    return (
      <span
        role="img"
        aria-label={t('markInProgress')}
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
        aria-label={t('markFailed')}
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
        aria-label={t('markStopped')}
        className={cn(base, 'bg-surface-2 text-muted-foreground')}
      >
        <MinusIcon className="h-3 w-3" />
      </span>
    );
  }
  return (
    <span
      role="img"
      aria-label={t('markDone')}
      className={cn(base, 'bg-ok-soft text-ok')}
    >
      <CheckIcon className="h-3 w-3" />
    </span>
  );
}
