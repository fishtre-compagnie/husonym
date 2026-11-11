'use client';

import { ColumnDef } from '@tanstack/react-table';

import { Pill, StatusDot, Tone } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import { cn } from '@/libs/utils';
import { formatDateTime } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { JobRun, JobRunStatus } from '@husonym/sdk';
import { formatDistanceToNowStrict } from 'date-fns';
import NextLink from 'next/link';
import {
  isRunFailed,
  isRunLive,
  JOB_STATE_LABEL,
  JobRow,
  JobState,
  RECENT_RUNS,
} from '../job-rows';
import { DataTableRowActions } from './data-table-row-actions';
import { RunJobButton } from './run-job-button';
import { SortHeader } from './sort-header';

const STATE_TONE: Record<JobState, Tone> = {
  running: 'brand',
  active: 'ok',
  failing: 'danger',
  paused: 'muted',
  disabled: 'danger',
  unknown: 'muted',
};

interface GetColumnsProps {
  accountName: string;
  onDeleted(id: string): void;
  onStatusChanged(): void;
  onRunStarted(): void;
}

export function getColumns(
  props: GetColumnsProps
): ColumnDef<AppTableFeatures, JobRow>[] {
  const { onDeleted, onStatusChanged, onRunStarted, accountName } = props;
  return [
    {
      accessorKey: 'name',
      header: ({ column }) => <SortHeader column={column} title="Job" />,
      sortFn: 'alphanumeric',
      cell: ({ row }) => {
        const j = row.original;
        const state = JOB_STATE_LABEL[j.state];
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
              {j.type !== 'Sync' && <Pill className="shrink-0">{j.type}</Pill>}
            </span>
            <span className="truncate text-xs text-muted-foreground">
              {state} · {j.tables} {j.tables === 1 ? 'table' : 'tables'} ·{' '}
              <span className={cn(j.schedule.isCron && 'font-mono')}>
                {j.schedule.text}
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
        <SortHeader column={column} title="Source → destination" />
      ),
      sortFn: 'alphanumeric',
      cell: ({ row }) => {
        const j = row.original;
        const destinations = j.destinations.join(', ') || 'No destination';
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
              aria-label="to"
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
      header: ({ column }) => <SortHeader column={column} title="Columns" />,
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
                / {j.columns} transformed
              </span>
            </span>
            <span
              role="img"
              aria-label={`${j.transformed} of ${j.columns} columns transformed`}
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
                    ? `${j.pendingPersonal} personal in clear`
                    : `${j.pendingReviews} to review`}
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
        <SortHeader column={column} title={`Last ${RECENT_RUNS} runs`} />
      ),
      sortDescFirst: true,
      cell: ({ row }) => <RunHistory row={row.original} />,
    },
    {
      id: 'actions',
      header: () => <span className="block text-right">Action</span>,
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

function runTone(s: JobRunStatus): { bar: string; label: string } {
  if (s === JobRunStatus.COMPLETE) {
    return { bar: 'h-4 bg-ok', label: 'Completed' };
  }
  if (isRunFailed(s)) {
    return { bar: 'h-6 bg-danger', label: 'Failed' };
  }
  if (isRunLive(s)) {
    return {
      bar: 'h-4 bg-link animate-pulse-soft',
      label: s === JobRunStatus.PENDING ? 'Pending' : 'Running',
    };
  }
  if (s === JobRunStatus.CANCELED || s === JobRunStatus.TERMINATED) {
    return {
      bar: 'h-4 bg-quiet',
      label: s === JobRunStatus.CANCELED ? 'Canceled' : 'Terminated',
    };
  }
  return { bar: 'h-4 bg-muted-foreground', label: 'Unknown' };
}

function runTitle(r: JobRun, label: string): string {
  return r.startedAt
    ? `${label} · ${formatDateTime(timestampDate(r.startedAt))}`
    : label;
}

function RunHistory({ row }: { row: JobRow }) {
  const runs = row.recentRuns;
  const empty = RECENT_RUNS - runs.length;
  const failed = runs.filter((r) => isRunFailed(r.status)).length;
  const summary =
    runs.length === 0
      ? `${row.name} has never run`
      : `Last ${runs.length} runs of ${row.name}: ${failed} failed`;
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
          const t = runTone(r.status);
          return (
            <span
              key={r.id}
              title={runTitle(r, t.label)}
              className={cn('w-[9px] rounded-[2px]', t.bar)}
            />
          );
        })}
      </span>
      <span className="text-xs text-muted-foreground">
        {row.lastRunAt
          ? `${formatDistanceToNowStrict(row.lastRunAt, { addSuffix: true })}`
          : 'Never run'}
      </span>
    </span>
  );
}
