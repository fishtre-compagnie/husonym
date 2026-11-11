'use client';

import {
  ColumnDef,
  ColumnVisibilityState,
  useTable,
} from '@tanstack/react-table';
import * as React from 'react';

import { DataTablePagination } from '@/components/table/data-table-pagination';
import {
  AppTableFeatures,
  paginatedTableFeatures,
} from '@/components/table/features';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  JobRunEvent,
  JobRunEventMetadata,
  JobRunSyncMetadata,
} from '@husonym/sdk';
import { useLocalStorage } from 'usehooks-ts';

interface DataTableProps {
  columns: ColumnDef<AppTableFeatures, JobRunEvent>[];
  data: JobRunEvent[];
  isError: boolean;
  onViewSelectClicked(schema: string, table: string): void;
}

export function DataTable({ columns, data, isError }: DataTableProps) {
  const columnVisibility: ColumnVisibilityState = { error: isError };

  const [pagination, setPagination] = React.useState<number>(0);
  const [pageSize, setPageSize] = useLocalStorage<number>(
    'job-run-activity-table-page-size',
    10
  );

  const table = useTable({
    features: paginatedTableFeatures,
    data,
    columns,
    state: {
      columnVisibility,
      pagination: { pageIndex: pagination, pageSize: pageSize },
    },
  });

  return (
    <div className="flex flex-col">
      <Table className="min-w-[860px]">
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => {
                return (
                  <TableHead
                    key={header.id}
                    className="h-10 px-4 text-xs font-medium text-muted-foreground"
                  >
                    {header.isPlaceholder ? null : (
                      <table.FlexRender header={header} />
                    )}
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows?.length ? (
            table.getRowModel().rows.map((row) => {
              return (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id} className="px-4 py-2 align-top">
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              );
            })
          ) : (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="h-24 text-center text-muted-foreground"
              >
                No activities found
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="border-t border-line-soft px-5 py-3">
        <DataTablePagination
          table={table}
          setPagination={setPagination}
          setPageSize={setPageSize}
        />
      </div>
    </div>
  );
}

export function getJobSyncMetadata(
  metadata?: JobRunEventMetadata
): JobRunSyncMetadata | null {
  if (metadata?.metadata.case === 'syncMetadata') {
    const md = metadata.metadata.value;
    if (md.schema && md.table) {
      return md;
    }
  }
  return null;
}
