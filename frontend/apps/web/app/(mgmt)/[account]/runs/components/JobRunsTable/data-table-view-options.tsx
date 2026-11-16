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
import { useTranslations } from 'next-intl';

// the column ids, to the keys of their names under runs.table.columns
const COLUMN_LABELS = {
  status: 'status',
  id: 'run',
  duration: 'duration',
  startedAt: 'started',
  completedAt: 'completed',
  jobId: 'jobId',
} as const;

function columnLabelKey(
  id: string
): (typeof COLUMN_LABELS)[keyof typeof COLUMN_LABELS] | undefined {
  return (
    COLUMN_LABELS as Record<
      string,
      (typeof COLUMN_LABELS)[keyof typeof COLUMN_LABELS]
    >
  )[id];
}

interface DataTableViewOptionsProps<TData extends RowData> {
  table: Table<AppTableFeatures, TData>;
}

export function DataTableViewOptions<TData extends RowData>({
  table,
}: DataTableViewOptionsProps<TData>) {
  const t = useTranslations('runs.table');
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="hidden h-9 lg:flex">
          <MixerHorizontalIcon className="h-4 w-4" />
          {t('columnsButton')}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[150px]">
        <DropdownMenuLabel>{t('toggleColumns')}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {table
          .getAllColumns()
          .filter(
            (column) =>
              typeof column.accessorFn !== 'undefined' && column.getCanHide()
          )
          .map((column) => {
            const labelKey = columnLabelKey(column.id);
            return (
              <DropdownMenuCheckboxItem
                key={column.id}
                checked={column.getIsVisible()}
                onCheckedChange={(value) => column.toggleVisibility(!!value)}
              >
                {labelKey ? t(`columns.${labelKey}`) : column.id}
              </DropdownMenuCheckboxItem>
            );
          })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
