'use client';

import { ColumnDef } from '@tanstack/react-table';

import NextLink from 'next/link';

import ConnectionIcon from '@/components/connections/ConnectionIcon';
import { Pill } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import TruncatedText from '@/components/TruncatedText';
import { formatDateTime } from '@/util/util';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { Connection } from '@husonym/sdk';
import { getCategory } from '../../util';
import { DataTableColumnHeader } from './data-table-column-header';
import { DataTableRowActions } from './data-table-row-actions';

interface GetColumnsProps {
  onConnectionDeleted(id: string): void;
  accountName: string;
}

export function getColumns(
  props: GetColumnsProps
): ColumnDef<AppTableFeatures, Connection>[] {
  const { accountName, onConnectionDeleted } = props;
  return [
    {
      accessorKey: 'id',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Connection Id" />
      ),
      cell: ({ row }) => (
        <div>
          <NextLink
            className="hover:underline"
            href={`/${accountName}/connections/${row.getValue('id')}`}
          >
            <span>{row.getValue('id')}</span>
          </NextLink>
        </div>
      ),
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: 'name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Name" />
      ),
      cell: ({ row }) => {
        return (
          <NextLink
            className="flex min-w-0 max-w-[500px] items-center gap-3 text-foreground hover:text-hi hover:no-underline"
            href={`/${accountName}/connections/${row.getValue('id')}`}
          >
            <span className="grid size-9 shrink-0 place-items-center rounded-[10px] bg-surface-2 [&_svg]:size-5!">
              {row.original.connectionConfig?.config.case && (
                <ConnectionIcon
                  connectionType={row.original.connectionConfig.config.case}
                />
              )}
            </span>
            <span className="flex min-w-0 flex-col">
              <span className="truncate font-semibold">
                <TruncatedText text={row.getValue('name')} align="start" />
              </span>
              <span className="truncate font-mono text-xs text-muted-foreground">
                {row.getValue('id')}
              </span>
            </span>
          </NextLink>
        );
      },
    },
    {
      accessorKey: 'category',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Category" />
      ),
      cell: ({ row }) => {
        return <Pill>{getCategory(row.original.connectionConfig)}</Pill>;
      },
      filterFn: (row, id, value) => {
        return value.includes(row.getValue(id));
      },
    },
    {
      accessorKey: 'createdAt',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Created At" />
      ),
      cell: ({ row }) => {
        const ts = row.getValue<Timestamp>('createdAt') ?? {
          nanos: 0,
          seconds: 0,
        };
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate text-muted-foreground tabular-nums">
              {formatDateTime(timestampDate(ts))}
            </span>
          </div>
        );
      },
      filterFn: (row, id, value) => {
        return value.includes(row.getValue(id));
      },
    },
    {
      accessorKey: 'updatedAt',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Updated At" />
      ),
      cell: ({ row }) => {
        const ts = row.getValue<Timestamp>('updatedAt') ?? {
          nanos: 0,
          seconds: 0,
        };
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate text-muted-foreground tabular-nums">
              {formatDateTime(timestampDate(ts))}
            </span>
          </div>
        );
      },
      filterFn: (row, id, value) => {
        return value.includes(row.getValue(id));
      },
    },
    {
      id: 'actions',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Actions" />
      ),
      cell: ({ row }) => (
        <DataTableRowActions
          row={row}
          onDeleted={() => onConnectionDeleted(row.id)}
        />
      ),
    },
  ];
}
