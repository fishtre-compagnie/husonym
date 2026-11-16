'use client';

import { DotsHorizontalIcon } from '@radix-ui/react-icons';
import { Row, RowData } from '@tanstack/react-table';

import { useAccount } from '@/components/providers/account-provider';
import { AppTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { AccountApiKey } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { useRouter } from 'next/navigation';
import RemoveAccountApiKeyButton from '../../[id]/components/RemoveAccountApiKeyButton';

interface DataTableRowActionsProps<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  onDeleted(): void;
}

export function DataTableRowActions<TData extends RowData>({
  row,
  onDeleted,
}: DataTableRowActionsProps<TData>) {
  const apikey = row.original as AccountApiKey;
  const router = useRouter();
  const { account } = useAccount();
  const t = useTranslations('settings.apiKeys.list');

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
      <DropdownMenuContent align="end" className="w-[160px]">
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() =>
            router.push(`/${account?.name}/settings/api-keys/${apikey.id}`)
          }
        >
          {t('viewKey')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <RemoveAccountApiKeyButton
          id={apikey.id}
          trigger={
            <DropdownMenuItem
              className="cursor-pointer"
              onSelect={(e) => e.preventDefault()}
            >
              {t('delete')}
            </DropdownMenuItem>
          }
          onDeleted={() => onDeleted()}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
