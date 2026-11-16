'use client';

import { ColumnDef } from '@tanstack/react-table';

import { StatusDot, toneTextClass } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import { cn } from '@/libs/utils';
import { JobRun } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import NextLink from 'next/link';
import { ReactElement } from 'react';
import { getJobRunStatusMeta } from '../JobRunStatus';
import { runSpan, toDate, useNow, useRunFormat } from '../run-format';
import { DataTableColumnHeader } from './data-table-column-header';
import { DataTableRowActions } from './data-table-row-actions';

interface GetColumnsProps {
  onDeleted(id: string): void;
  accountName: string;
  jobNameMap: Record<string, string>;
  t: ReturnType<typeof useTranslations<'runs.table'>>;
}

export function getColumns(
  props: GetColumnsProps
): ColumnDef<AppTableFeatures, JobRun>[] {
  const { onDeleted, accountName, jobNameMap, t } = props;
  return [
    {
      accessorKey: 'status',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.status')} />
      ),
      cell: ({ row }) => <StatusCell run={row.original} />,
      filterFn: (row, id, value) => {
        return value.includes(row.getValue(id));
      },
    },
    {
      accessorKey: 'jobName',
      accessorFn: (row) => jobNameMap[row.jobId] ?? '',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.job')} />
      ),
      cell: ({ row }) => {
        const name = jobNameMap[row.original.jobId];
        return (
          <NextLink
            className="font-medium text-foreground hover:text-link"
            href={`/${accountName}/runs/${row.original.id}`}
          >
            {name || (
              <span className="text-muted-foreground">{t('unknownJob')}</span>
            )}
          </NextLink>
        );
      },
      enableHiding: false,
    },
    {
      accessorKey: 'id',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.run')} />
      ),
      cell: ({ row }) => {
        const id = row.original.id;
        return (
          <NextLink
            className="font-mono text-xs text-muted-foreground hover:text-link"
            href={`/${accountName}/runs/${id}`}
            title={id}
          >
            {shortRunId(id, row.original.jobId)}
          </NextLink>
        );
      },
      enableSorting: false,
    },
    {
      id: 'duration',
      accessorFn: (row) => runSpan(row.startedAt, row.completedAt) ?? -1,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.duration')} />
      ),
      cell: ({ row }) => <DurationCell run={row.original} />,
    },
    {
      accessorKey: 'startedAt',
      accessorFn: (row) => toDate(row.startedAt)?.getTime() ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.started')} />
      ),
      cell: ({ row }) => <StartedCell run={row.original} />,
    },
    {
      accessorKey: 'completedAt',
      accessorFn: (row) => toDate(row.completedAt)?.getTime() ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.completed')} />
      ),
      cell: ({ row }) => <CompletedCell run={row.original} />,
    },
    {
      accessorKey: 'jobId',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.jobId')} />
      ),
      cell: ({ row }) => {
        return (
          <NextLink
            className="font-mono text-xs text-muted-foreground hover:text-link"
            href={`/${accountName}/jobs/${row.original.jobId}`}
          >
            {row.original.jobId}
          </NextLink>
        );
      },
    },
    {
      id: 'actions',
      cell: ({ row }) => (
        <div className="flex justify-end">
          <DataTableRowActions
            row={row}
            onDeleted={() => onDeleted(row.original.id)}
          />
        </div>
      ),
    },
  ];
}

/**
 * A run id short enough for a list. A run Temporal started on a schedule is named
 * after its job and its time (`<job id>-2026-11-10T14:58:54Z`): what follows the job id
 * tells it apart. Any other id is cut to its first block.
 */
function shortRunId(id: string, jobId?: string): string {
  if (jobId && id.startsWith(`${jobId}-`) && id.length > jobId.length + 1) {
    return id.slice(jobId.length + 1);
  }
  return id.length > 13 ? `${id.slice(0, 8)}…` : id;
}

function StatusCell({ run }: { run: JobRun }): ReactElement {
  const t = useTranslations('runs.status');
  const meta = getJobRunStatusMeta(run.status);
  return (
    <span className="flex min-w-[104px] items-center gap-2.5">
      <StatusDot tone={meta.tone} pulse={meta.live} />
      <span className={cn('text-[13px]', toneTextClass(meta.tone))}>
        {t(meta.label)}
      </span>
    </span>
  );
}

function StartedCell({ run }: { run: JobRun }): ReactElement {
  const fmt = useRunFormat();
  const started = toDate(run.startedAt);
  return (
    <span
      className="whitespace-nowrap text-[13px] text-muted-foreground"
      title={started ? fmt.date(started, 'dateTime') : undefined}
    >
      {fmt.ago(started)}
    </span>
  );
}

function CompletedCell({ run }: { run: JobRun }): ReactElement {
  const fmt = useRunFormat();
  const completed = toDate(run.completedAt);
  return (
    <span className="whitespace-nowrap text-[13px] text-muted-foreground">
      {completed ? fmt.date(completed, 'dateTime') : '–'}
    </span>
  );
}

function DurationCell({ run }: { run: JobRun }): ReactElement {
  const fmt = useRunFormat();
  const live =
    !run.completedAt && !!run.startedAt && getJobRunStatusMeta(run.status).live;
  const now = useNow(live);
  const span =
    run.completedAt || live
      ? runSpan(run.startedAt, run.completedAt, now)
      : undefined;
  return (
    <span className="whitespace-nowrap font-mono text-xs text-muted-foreground">
      {span === undefined ? '–' : fmt.span(span)}
    </span>
  );
}
