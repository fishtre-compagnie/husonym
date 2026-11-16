'use client';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Row, RowData } from '@tanstack/react-table';

import { AppTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import { DotsHorizontalIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';

interface DataTableRowActionsProps<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  onViewSelectClicked(): void;
}

export function DataTableRowActions<TData extends RowData>({
  onViewSelectClicked,
}: DataTableRowActionsProps<TData>) {
  const t = useTranslations('runs.activity');
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="flex h-8 w-8 p-0 data-[state=open]:bg-muted"
        >
          <DotsHorizontalIcon className="h-4 w-4" />
          <span className="sr-only">{t('openMenu')}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() => onViewSelectClicked()}
        >
          {t('showSelect')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
