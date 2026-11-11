'use client';

import {
  ColumnDef,
  RowData,
  SortingState,
  useTable,
} from '@tanstack/react-table';
import * as React from 'react';

import { rowClass } from '@/components/ds';
import { DataTablePagination } from '@/components/table/data-table-pagination';
import {
  AppTableFeatures,
  paginatedTableFeatures,
} from '@/components/table/features';
import { cn } from '@/libs/utils';
import { useLocalStorage } from 'usehooks-ts';

interface DataTableProps<TData extends RowData> {
  columns: ColumnDef<AppTableFeatures, TData>[];
  data: TData[];
  // Changes when what the list shows is filtered anew: the list goes back to its first page.
  pageResetKey: string;
  // What the body says when no row is left.
  empty: React.ReactNode;
}

// The widths of the columns, in their order: job, connections, columns, runs, actions.
const COLUMN_WIDTHS = ['30%', '26%', '16%', '16%', '132px'];

export function DataTable<TData extends RowData>({
  columns,
  data,
  pageResetKey,
  empty,
}: DataTableProps<TData>) {
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [pagination, setPagination] = React.useState<number>(0);
  const [pageSize, setPageSize] = useLocalStorage<number>(
    'jobs-table-page-size',
    10
  );
  const [lastResetKey, setLastResetKey] = React.useState(pageResetKey);
  if (lastResetKey !== pageResetKey) {
    setLastResetKey(pageResetKey);
    setPagination(0);
  }

  const table = useTable({
    features: paginatedTableFeatures,
    data,
    columns,
    state: {
      sorting,
      pagination: { pageIndex: pagination, pageSize: pageSize },
    },
    onSortingChange: setSorting,
  });

  const rows = table.getRowModel().rows;

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-x-auto rounded-2xl border border-line-soft bg-card">
        <table className="w-full min-w-[940px] table-fixed border-collapse text-left">
          <colgroup>
            {columns.map((c, i) => (
              <col
                key={c.id ?? i}
                style={{ width: COLUMN_WIDTHS[i] ?? undefined }}
              />
            ))}
          </colgroup>
          <thead>
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id} className="border-b border-line-soft">
                {headerGroup.headers.map((header) => {
                  const sorted = header.column.getIsSorted();
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={
                        sorted === 'asc'
                          ? 'ascending'
                          : sorted === 'desc'
                            ? 'descending'
                            : undefined
                      }
                      className="px-2 py-3 text-xs font-normal uppercase tracking-[0.06em] text-muted-foreground first:pl-5 last:pr-5"
                    >
                      {header.isPlaceholder ? null : (
                        <table.FlexRender header={header} />
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {rows.length ? (
              rows.map((row) => (
                <tr key={row.id} className={rowClass}>
                  {row.getVisibleCells().map((cell) => (
                    <td
                      key={cell.id}
                      className="px-2 py-3.5 align-middle first:pl-5 last:pr-5"
                    >
                      <table.FlexRender cell={cell} />
                    </td>
                  ))}
                </tr>
              ))
            ) : (
              <tr>
                <td
                  colSpan={columns.length}
                  className="px-5 py-12 text-center text-muted-foreground"
                >
                  {empty}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <div className={cn(data.length <= 10 && 'hidden')}>
        <DataTablePagination
          table={table}
          setPagination={setPagination}
          setPageSize={setPageSize}
        />
      </div>
    </div>
  );
}
