'use client';

import { DotsHorizontalIcon } from '@radix-ui/react-icons';
import { Row, RowData } from '@tanstack/react-table';

import { AppTableFeatures } from '@/components/table/features';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

interface DataTableRowActionsProps<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  onDelete(data: Row<AppTableFeatures, TData>): void;
  onDuplicate(data: Row<AppTableFeatures, TData>): void;
}

export function DataTableRowActions<TData extends RowData>({
  row,
  onDelete,
  onDuplicate,
}: DataTableRowActionsProps<TData>) {
  const t = useTranslations('mapping.nosql.rowActions');
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="flex h-8 w-8 p-0 data-[state=open]:bg-muted"
        >
          <DotsHorizontalIcon className="h-4 w-4" />
          <span className="sr-only">{t('openMenu')}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[160px]">
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() => {
            onDuplicate(row);
          }}
        >
          {t('duplicate')}
        </DropdownMenuItem>
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() => onDelete(row)}
        >
          {t('remove')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
