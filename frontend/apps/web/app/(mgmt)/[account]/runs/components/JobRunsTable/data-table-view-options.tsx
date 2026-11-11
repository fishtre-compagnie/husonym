'use client';

import { DropdownMenuTrigger } from '@radix-ui/react-dropdown-menu';
import { MixerHorizontalIcon } from '@radix-ui/react-icons';
import { RowData, Table } from '@tanstack/react-table';

import { AppTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu';

const COLUMN_LABELS: Record<string, string> = {
  status: 'Status',
  id: 'Run',
  duration: 'Duration',
  startedAt: 'Started',
  completedAt: 'Completed',
  jobId: 'Job Id',
};

interface DataTableViewOptionsProps<TData extends RowData> {
  table: Table<AppTableFeatures, TData>;
}

export function DataTableViewOptions<TData extends RowData>({
  table,
}: DataTableViewOptionsProps<TData>) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="hidden h-9 lg:flex">
          <MixerHorizontalIcon className="h-4 w-4" />
          Columns
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[150px]">
        <DropdownMenuLabel>Toggle columns</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {table
          .getAllColumns()
          .filter(
            (column) =>
              typeof column.accessorFn !== 'undefined' && column.getCanHide()
          )
          .map((column) => {
            return (
              <DropdownMenuCheckboxItem
                key={column.id}
                checked={column.getIsVisible()}
                onCheckedChange={(value) => column.toggleVisibility(!!value)}
              >
                {COLUMN_LABELS[column.id] ?? column.id}
              </DropdownMenuCheckboxItem>
            );
          })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
