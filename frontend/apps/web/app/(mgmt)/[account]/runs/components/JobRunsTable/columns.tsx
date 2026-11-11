'use client';

import { ColumnDef } from '@tanstack/react-table';

import { StatusDot, toneTextClass } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import { cn } from '@/libs/utils';
import { formatDateTime } from '@/util/util';
import { JobRun } from '@husonym/sdk';
import NextLink from 'next/link';
import { ReactElement } from 'react';
import { getJobRunStatusMeta } from '../JobRunStatus';
import { formatAgo, formatSpan, runSpan, toDate, useNow } from '../run-format';
import { DataTableColumnHeader } from './data-table-column-header';
import { DataTableRowActions } from './data-table-row-actions';

interface GetColumnsProps {
  onDeleted(id: string): void;
  accountName: string;
  jobNameMap: Record<string, string>;
}

export function getColumns(
  props: GetColumnsProps
): ColumnDef<AppTableFeatures, JobRun>[] {
  const { onDeleted, accountName, jobNameMap } = props;
  return [
    {
      accessorKey: 'status',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Status" />
      ),
      cell: ({ row }) => {
        const meta = getJobRunStatusMeta(row.original.status);
        return (
          <span className="flex min-w-[104px] items-center gap-2.5">
            <StatusDot tone={meta.tone} pulse={meta.live} />
            <span className={cn('text-[13px]', toneTextClass(meta.tone))}>
              {meta.label}
            </span>
          </span>
        );
      },
      filterFn: (row, id, value) => {
        return value.includes(row.getValue(id));
      },
    },
    {
      accessorKey: 'jobName',
      accessorFn: (row) => jobNameMap[row.jobId] ?? '',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Job" />
      ),
      cell: ({ row }) => {
        const name = jobNameMap[row.original.jobId];
        return (
          <NextLink
            className="font-medium text-foreground hover:text-link"
            href={`/${accountName}/runs/${row.original.id}`}
          >
            {name || <span className="text-muted-foreground">Unknown job</span>}
          </NextLink>
        );
      },
      enableHiding: false,
    },
    {
      accessorKey: 'id',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Run" />
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
        <DataTableColumnHeader column={column} title="Duration" />
      ),
      cell: ({ row }) => <DurationCell run={row.original} />,
    },
    {
      accessorKey: 'startedAt',
      accessorFn: (row) => toDate(row.startedAt)?.getTime() ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Started" />
      ),
      cell: ({ row }) => {
        const started = toDate(row.original.startedAt);
        return (
          <span
            className="whitespace-nowrap text-[13px] text-muted-foreground"
            title={formatDateTime(started)}
          >
            {formatAgo(started)}
          </span>
        );
      },
    },
    {
      accessorKey: 'completedAt',
      accessorFn: (row) => toDate(row.completedAt)?.getTime() ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Completed" />
      ),
      cell: ({ row }) => {
        const completed = toDate(row.original.completedAt);
        return (
          <span className="whitespace-nowrap text-[13px] text-muted-foreground">
            {completed ? formatDateTime(completed) : '–'}
          </span>
        );
      },
    },
    {
      accessorKey: 'jobId',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Job Id" />
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
export function shortRunId(id: string, jobId?: string): string {
  if (jobId && id.startsWith(`${jobId}-`) && id.length > jobId.length + 1) {
    return id.slice(jobId.length + 1);
  }
  return id.length > 13 ? `${id.slice(0, 8)}…` : id;
}

function DurationCell({ run }: { run: JobRun }): ReactElement {
  const live =
    !run.completedAt && !!run.startedAt && getJobRunStatusMeta(run.status).live;
  const now = useNow(live);
  const span =
    run.completedAt || live
      ? runSpan(run.startedAt, run.completedAt, now)
      : undefined;
  return (
    <span className="whitespace-nowrap font-mono text-xs text-muted-foreground">
      {span === undefined ? '–' : formatSpan(span)}
    </span>
  );
}
