'use client';

import {
  ColumnDef,
  ColumnFiltersState,
  ColumnVisibilityState,
  RowData,
  SortingState,
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
import { useLocalStorage } from 'usehooks-ts';
import { DataTableToolbar, RunStatusGroup } from './data-table-toolbar';

interface DataTableProps<
  TData extends RowData,
  TAutoRefreshInterval extends string,
> {
  columns: ColumnDef<AppTableFeatures, TData>[];
  data: TData[];
  onRefreshClick(): void;
  refreshInterval: TAutoRefreshInterval;
  autoRefreshIntervalOptions: string[];
  onAutoRefreshIntervalChange(interval: string): void;
  isRefreshing: boolean;
  statusCounts: Record<RunStatusGroup, number>;
}

export function DataTable<
  TData extends RowData,
  TAutoRefreshInterval extends string,
>({
  columns,
  data,
  onRefreshClick,
  refreshInterval,
  autoRefreshIntervalOptions,
  onAutoRefreshIntervalChange,
  isRefreshing,
  statusCounts,
}: DataTableProps<TData, TAutoRefreshInterval>) {
  const [rowSelection, setRowSelection] = React.useState({});
  const [columnVisibility, setColumnVisibility] =
    React.useState<ColumnVisibilityState>({
      jobId: false,
      completedAt: false,
    });
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    []
  );
  const [sorting, setSorting] = React.useState<SortingState>([]);

  const [pagination, setPagination] = React.useState<number>(0);
  const [pageSize, setPageSize] = useLocalStorage<number>(
    'job-runs-table-page-size',
    10
  );

  const table = useTable({
    features: paginatedTableFeatures,
    data,
    columns,
    state: {
      sorting,
      columnVisibility,
      rowSelection,
      columnFilters,
      pagination: { pageIndex: pagination, pageSize: pageSize },
    },
    enableRowSelection: true,
    onRowSelectionChange: setRowSelection,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onColumnVisibilityChange: setColumnVisibility,
  });

  return (
    <div className="flex flex-col">
      <DataTableToolbar
        table={table}
        onRefreshClick={onRefreshClick}
        refreshInterval={refreshInterval}
        autoRefreshIntervalOptions={autoRefreshIntervalOptions}
        onAutoRefreshIntervalChange={onAutoRefreshIntervalChange}
        isRefreshing={isRefreshing}
        statusCounts={statusCounts}
      />
      <Table className="min-w-[640px]">
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => {
                return (
                  <TableHead
                    key={header.id}
                    className="h-10 px-5 text-xs font-medium text-muted-foreground"
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
            table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                data-state={row.getIsSelected() && 'selected'}
              >
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id} className="px-5 py-2.5">
                    <table.FlexRender cell={cell} />
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="h-24 text-center text-muted-foreground"
              >
                No run matches these filters.
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
