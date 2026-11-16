'use client';

import { ColumnDef } from '@tanstack/react-table';

import { Pill, StatusDot, Tone } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import { cn } from '@/libs/utils';
import { useDateFnsLocale } from '@/i18n/date-fns';
import { formatDateTime } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { JobRun, JobRunStatus } from '@husonym/sdk';
import { formatDistanceToNowStrict } from 'date-fns';
import { useTranslations } from 'next-intl';
import NextLink from 'next/link';
import {
  isRunFailed,
  isRunLive,
  JobRow,
  JobState,
  RECENT_RUNS,
} from '../job-rows';
import { ScheduleText } from '../ScheduleText';
import { DataTableRowActions } from './data-table-row-actions';
import { RunJobButton } from './run-job-button';
import { SortHeader } from './sort-header';

const STATE_TONE: Record<JobState, Tone> = {
  running: 'live',
  active: 'ok',
  failing: 'danger',
  paused: 'muted',
  disabled: 'danger',
  unknown: 'muted',
};

// The translator of the list's messages.
type JobListT = ReturnType<typeof useTranslations<'jobs.list'>>;

interface GetColumnsProps {
  accountName: string;
  t: JobListT;
  onDeleted(id: string): void;
  onStatusChanged(): void;
  onRunStarted(): void;
}

export function getColumns(
  props: GetColumnsProps
): ColumnDef<AppTableFeatures, JobRow>[] {
  const { onDeleted, onStatusChanged, onRunStarted, accountName, t } = props;
  return [
    {
      accessorKey: 'name',
      header: ({ column }) => (
        <SortHeader column={column} title={t('columns.job')} />
      ),
      sortFn: 'alphanumeric',
      cell: ({ row }) => {
        const j = row.original;
        const state = t(`state.${j.state}`);
        return (
          <NextLink
            href={`/${accountName}/jobs/${j.id}`}
            className="group flex min-w-0 flex-col gap-1 rounded-md text-foreground"
          >
            <span className="flex min-w-0 items-center gap-2">
              <StatusDot
                tone={STATE_TONE[j.state]}
                pulse={j.state === 'running'}
                label={state}
              />
              <span className="truncate font-semibold group-hover:underline">
                {j.name}
              </span>
              {j.type !== 'sync' && (
                <Pill className="shrink-0">{t(`type.${j.type}`)}</Pill>
              )}
            </span>
            <span className="truncate text-xs text-muted-foreground">
              {state} · {t('tables', { count: j.tables })} ·{' '}
              <span className={cn(j.schedule.isCron && 'font-mono')}>
                <ScheduleText label={j.schedule} />
              </span>
            </span>
          </NextLink>
        );
      },
    },
    {
      id: 'connections',
      accessorFn: (j) => j.source,
      header: ({ column }) => (
        <SortHeader column={column} title={t('columns.connections')} />
      ),
      sortFn: 'alphanumeric',
      cell: ({ row }) => {
        const j = row.original;
        const destinations = j.destinations.join(', ') || t('noDestination');
        return (
          <span className="flex min-w-0 items-center gap-2 text-[13px]">
            {j.engine && (
              <span className="shrink-0 rounded-md bg-surface-2 px-[7px] py-[3px] font-mono text-[11px] text-link">
                {j.engine}
              </span>
            )}
            <span className="truncate" title={j.source}>
              {j.source}
            </span>
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-label={t('to')}
              role="img"
              className="shrink-0 text-muted-foreground"
            >
              <path d="M5 12h14M13 6l6 6-6 6" />
            </svg>
            <span
              className="truncate text-muted-foreground"
              title={destinations}
            >
              {destinations}
            </span>
          </span>
        );
      },
    },
    {
      accessorKey: 'transformed',
      header: ({ column }) => (
        <SortHeader column={column} title={t('columns.columns')} />
      ),
      sortDescFirst: true,
      cell: ({ row }) => {
        const j = row.original;
        const pct = j.columns ? (j.transformed / j.columns) * 100 : 0;
        return (
          <span className="flex flex-col gap-1.5">
            <span className="text-[13px]">
              <span className="font-mono">{j.transformed}</span>
              <span className="text-muted-foreground">
                {' '}
                {t('transformedOf', { count: j.columns })}
              </span>
            </span>
            <span
              role="img"
              aria-label={t('transformedAria', {
                transformed: j.transformed,
                count: j.columns,
              })}
              className="flex h-1.5 overflow-hidden rounded-full bg-surface-2"
            >
              <span className="bg-primary" style={{ width: `${pct}%` }} />
            </span>
            {j.pendingReviews > 0 && (
              <NextLink
                href={`/${accountName}/jobs/${j.id}/review`}
                className="w-fit rounded-full"
              >
                <Pill tone={j.pendingPersonal > 0 ? 'danger' : 'warn'}>
                  {j.pendingPersonal > 0
                    ? t('personalInClear', { count: j.pendingPersonal })
                    : t('toReview', { count: j.pendingReviews })}
                </Pill>
              </NextLink>
            )}
          </span>
        );
      },
    },
    {
      id: 'lastRun',
      accessorFn: (j) => j.lastRunAt?.getTime() ?? 0,
      header: ({ column }) => (
        <SortHeader
          column={column}
          title={t('columns.lastRuns', { count: RECENT_RUNS })}
        />
      ),
      sortDescFirst: true,
      cell: ({ row }) => <RunHistory row={row.original} />,
    },
    {
      id: 'actions',
      header: () => (
        <span className="block text-right">{t('columns.action')}</span>
      ),
      enableSorting: false,
      cell: ({ row }) => (
        <span className="flex justify-end gap-1.5">
          <RunJobButton job={row.original.job} onStarted={onRunStarted} />
          <DataTableRowActions
            job={row.original.job}
            status={row.original.status}
            onDeleted={() => onDeleted(row.original.id)}
            onStatusChanged={onStatusChanged}
          />
        </span>
      ),
    },
  ];
}

type RunToneLabel =
  | 'completed'
  | 'failed'
  | 'pending'
  | 'running'
  | 'canceled'
  | 'terminated'
  | 'unknown';

function runTone(s: JobRunStatus): { bar: string; label: RunToneLabel } {
  if (s === JobRunStatus.COMPLETE) {
    return { bar: 'h-4 bg-ok', label: 'completed' };
  }
  if (isRunFailed(s)) {
    return { bar: 'h-6 bg-danger', label: 'failed' };
  }
  if (isRunLive(s)) {
    return {
      bar:
        s === JobRunStatus.PENDING
          ? 'h-4 bg-pending animate-pulse-soft'
          : 'h-4 bg-live animate-pulse-soft',
      label: s === JobRunStatus.PENDING ? 'pending' : 'running',
    };
  }
  if (s === JobRunStatus.CANCELED || s === JobRunStatus.TERMINATED) {
    return {
      bar: 'h-4 bg-stopped',
      label: s === JobRunStatus.CANCELED ? 'canceled' : 'terminated',
    };
  }
  return { bar: 'h-4 bg-muted-foreground', label: 'unknown' };
}

function runTitle(r: JobRun, label: string): string {
  return r.startedAt
    ? `${label} · ${formatDateTime(timestampDate(r.startedAt))}`
    : label;
}

function RunHistory({ row }: { row: JobRow }) {
  const t = useTranslations('jobs.list.runs');
  const dateLocale = useDateFnsLocale();
  const runs = row.recentRuns;
  const empty = RECENT_RUNS - runs.length;
  const failed = runs.filter((r) => isRunFailed(r.status)).length;
  const summary =
    runs.length === 0
      ? t('neverRanAria', { name: row.name })
      : t('summary', { count: runs.length, name: row.name, failed });
  return (
    <span className="flex flex-col gap-1">
      <span
        role="img"
        aria-label={summary}
        className="flex h-6 items-end gap-[3px]"
      >
        {Array.from({ length: empty }, (_, i) => (
          <span
            key={`empty-${i}`}
            className="h-1.5 w-[9px] rounded-[2px] bg-border"
          />
        ))}
        {runs.map((r) => {
          const tone = runTone(r.status);
          return (
            <span
              key={r.id}
              title={runTitle(r, t(`status.${tone.label}`))}
              className={cn('w-[9px] rounded-[2px]', tone.bar)}
            />
          );
        })}
      </span>
      <span className="text-xs text-muted-foreground">
        {row.lastRunAt
          ? `${formatDistanceToNowStrict(row.lastRunAt, {
              addSuffix: true,
              locale: dateLocale,
            })}`
          : t('neverRun')}
      </span>
    </span>
  );
}
