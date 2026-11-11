'use client';

import { ColumnDef, Row } from '@tanstack/react-table';

import { SchemaColumnHeader } from '@/components/jobs/SchemaTable/SchemaColumnHeader';
import { AppTableFeatures } from '@/components/table/features';
import { formatDateTimeMilliseconds } from '@/util/util';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { JobRunEvent, JobRunEventTaskError } from '@husonym/sdk';
import { ExclamationTriangleIcon } from '@radix-ui/react-icons';
import { getJobSyncMetadata } from './data-table';
import { DataTableRowActions } from './data-table-row-actions';
interface GetColumnsProps {
  onViewSelectClicked(schema: string, table: string): void;
}

export function getColumns(
  props: GetColumnsProps
): ColumnDef<AppTableFeatures, JobRunEvent>[] {
  const { onViewSelectClicked } = props;

  return [
    {
      accessorKey: 'id',
      header: ({ column }) => <SchemaColumnHeader column={column} title="Id" />,
      cell: ({ row }) => <div>{row.getValue<number>('id').toString()}</div>,
      filterFn: 'includesString',
    },
    {
      accessorKey: 'scheduleTime',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Scheduled" />
      ),
      enableColumnFilter: false,
      cell: ({ row }) => {
        const scheduledTime = row.original.tasks?.find(
          (item) =>
            item.type === 'ActivityTaskScheduled' ||
            item.type === 'StartChildWorkflowExecutionInitiated'
        )?.eventTime;
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate font-mono text-xs text-muted-foreground">
              {scheduledTime &&
                formatDateTimeMilliseconds(timestampDate(scheduledTime))}
            </span>
          </div>
        );
      },
    },
    {
      accessorKey: 'startTime',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Start Time" />
      ),
      enableColumnFilter: false,
      cell: ({ row }) => {
        const startTime = row.getValue<Timestamp>('startTime');
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate font-mono text-xs text-muted-foreground">
              {startTime &&
                formatDateTimeMilliseconds(timestampDate(startTime))}
            </span>
          </div>
        );
      },
    },
    {
      accessorKey: 'closeTime',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Completed" />
      ),
      enableColumnFilter: false,
      cell: ({ row }) => {
        const closeTime =
          row.original.closeTime ??
          row.original.tasks?.find(
            (item) =>
              item.type === 'ActivityTaskCompleted' ||
              item.type === 'ActivityTaskFailed' ||
              item.type === 'ActivityTaskTerminated' ||
              item.type === 'ActivityTaskCanceled' ||
              item.type === 'ActivityTaskTimedOut' ||
              item.type === 'ChildWorkflowExecutionCompleted' ||
              item.type === 'ChildWorkflowExecutionFailed' ||
              item.type === 'ChildWorkflowExecutionTerminated' ||
              item.type === 'ChildWorkflowExecutionCanceled' ||
              item.type === 'ChildWorkflowExecutionTimedOut'
          )?.eventTime;

        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate font-mono text-xs text-muted-foreground">
              {closeTime
                ? formatDateTimeMilliseconds(timestampDate(closeTime))
                : 'N/A'}
            </span>
          </div>
        );
      },
    },
    {
      accessorKey: 'type',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Type" />
      ),
      cell: ({ row }) => {
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate text-[13px]">
              {row.getValue('type')}
            </span>
          </div>
        );
      },
    },
    {
      accessorKey: 'schema',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Schema" />
      ),
      cell: ({ row }) => {
        const metadata = getJobSyncMetadata(
          row.original.metadata // Use row.original to access the full row data
        );

        return (
          <div className="flex space-x-2">
            <span className="font-medium">
              {metadata?.schema && (
                <span className="font-mono text-xs">{metadata.schema}</span>
              )}
            </span>
          </div>
        );
      },
      filterFn: schemaColumnFilterFn,
    },
    {
      accessorKey: 'table',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Table" />
      ),
      cell: ({ row }) => {
        const metadata = getJobSyncMetadata(
          row.original.metadata // Use row.original to access the full row data
        );

        return (
          <div className="flex space-x-2">
            <span className="font-medium">
              {metadata?.table && (
                <span className="font-mono text-xs">{metadata.table}</span>
              )}
            </span>
          </div>
        );
      },
      filterFn: tableColumnFilterFn,
    },

    {
      accessorKey: 'error',
      accessorFn: (originalRow, _) =>
        originalRow.tasks.find((t) => t.error)?.error,
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title="Error" />
      ),
      enableColumnFilter: false,
      cell: ({ row }) => {
        const err = row.getValue<JobRunEventTaskError>('error');
        return (
          <div className="min-w-[220px] max-w-[420px]">
            {err && (
              <div className="rounded-lg border border-danger-line bg-danger-soft px-3 py-2 text-xs text-danger-fg">
                <p className="mb-1 flex items-center gap-1.5 font-medium">
                  <ExclamationTriangleIcon aria-hidden="true" />
                  Error
                </p>
                <pre className="whitespace-pre-wrap break-words font-mono">
                  {JSON.stringify(err, undefined, 2)}
                </pre>
              </div>
            )}
          </div>
        );
      },
    },
    {
      id: 'actions',
      cell: ({ row }) => {
        const metadata = getJobSyncMetadata(row.original.metadata);
        return metadata?.schema && metadata.table ? (
          <DataTableRowActions
            row={row}
            onViewSelectClicked={() =>
              onViewSelectClicked(metadata?.schema ?? '', metadata?.table ?? '')
            }
          />
        ) : (
          <div />
        );
      },
    },
  ];
}

function schemaColumnFilterFn(
  row: Row<AppTableFeatures, JobRunEvent>,
  columnId: string,
  filterValue: any // eslint-disable-line @typescript-eslint/no-explicit-any
): boolean {
  const metadata = getJobSyncMetadata(row.original.metadata);
  const value = metadata?.schema;
  if (!value) {
    return false;
  }
  return value.toLowerCase().includes(filterValue.toLowerCase());
}

function tableColumnFilterFn(
  row: Row<AppTableFeatures, JobRunEvent>,
  columnId: string,
  filterValue: any // eslint-disable-line @typescript-eslint/no-explicit-any
): boolean {
  const metadata = getJobSyncMetadata(row.original.metadata);
  const value = metadata?.table;
  if (!value) {
    return false;
  }
  return value.toLowerCase().includes(filterValue.toLowerCase());
}
