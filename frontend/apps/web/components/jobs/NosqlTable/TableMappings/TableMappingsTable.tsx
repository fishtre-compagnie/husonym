'use client';
import React, { ReactElement } from 'react';

import { ColumnDef, RowData, useTable } from '@tanstack/react-table';

import { useVirtualizer } from '@tanstack/react-virtual';

import {
  AppTableFeatures,
  unpaginatedTableFeatures,
} from '@/components/table/features';
import { CardDescription, CardTitle } from '@/components/ui/card';
import {
  StickyHeaderTable,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { cn } from '@/libs/utils';
import { useFormatter, useTranslations } from 'next-intl';
import { GoWorkflow } from 'react-icons/go';

interface DataTableProps<TData extends RowData> {
  columns: ColumnDef<AppTableFeatures, TData>[];
  data: TData[];
}

export default function TableMappingsTable<TData extends RowData>({
  columns,
  data,
}: DataTableProps<TData>): ReactElement {
  const t = useTranslations('mapping.nosql.tableMappings');
  const format = useFormatter();
  const table = useTable({
    features: unpaginatedTableFeatures,
    data,
    columns,
    initialState: {
      // sorting: [{ id: 'schemaTable', desc: false }],
      // columnVisibility: {
      //   schema: false,
      //   table: false,
      // },
    },
  });

  const { rows } = table.getRowModel();

  const tableContainerRef = React.useRef<HTMLDivElement>(null);

  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    estimateSize: () => 33,
    getScrollElement: () => tableContainerRef.current,
    overscan: 5,
  });

  return (
    <div>
      <div className="flex flex-row items-center gap-2 pt-4 ">
        <div className="flex">
          <GoWorkflow className="h-4 w-4" />
        </div>
        <CardTitle>{t('tableTitle')}</CardTitle>
      </div>
      <CardDescription className="pt-2">
        {t('tableDescription')}
      </CardDescription>
      <div
        className={cn(
          'rounded-md border min-h-[145px] max-h-[500px] relative border-line-soft overflow-hidden rounded-2xl bg-card',
          rows.length > 0 && 'overflow-auto'
        )}
        ref={tableContainerRef}
      >
        <StickyHeaderTable>
          <TableHeader className="bg-surface-2 sticky top-0 z-10 px-2 grid">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow
                key={headerGroup.id}
                className="flex flex-row items-center justify-between"
              >
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead
                      key={header.id}
                      style={{ minWidth: `${header.column.getSize()}px` }}
                      colSpan={header.colSpan}
                      className="flex items-center"
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
          <TableBody
            className="grid"
            style={{
              height: `${rowVirtualizer.getTotalSize()}px`, //tells scrollbar how big the table is
            }}
          >
            {rows.length === 0 && (
              <TableRow className="flex justify-center items-center py-10 text-muted-foreground">
                <td>{t('empty')}</td>
              </TableRow>
            )}
            {rowVirtualizer.getVirtualItems().map((virtualRow) => {
              const row = rows[virtualRow.index];
              return (
                <TableRow
                  data-index={virtualRow.index} //needed for dynamic row height measurement
                  ref={(node) => {
                    rowVirtualizer.measureElement(node);
                  }} //measure dynamic row height
                  key={row.id}
                  style={{
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                  className="items-center flex absolute w-full justify-between px-2"
                >
                  {row.getVisibleCells().map((cell) => {
                    return (
                      <td
                        key={cell.id}
                        className="py-2"
                        style={{
                          minWidth: cell.column.getSize(),
                        }}
                      >
                        <table.FlexRender cell={cell} />
                      </td>
                    );
                  })}
                </TableRow>
              );
            })}
          </TableBody>
        </StickyHeaderTable>
      </div>
      <div className="text-xs text-muted-foreground pt-4">
        {t('rowCounts', {
          total: format.number(data.length),
          visible: format.number(table.getRowModel().rows.length),
        })}
      </div>
    </div>
  );
}
