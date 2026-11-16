'use client';

import { DotsHorizontalIcon } from '@radix-ui/react-icons';
import { Row, RowData } from '@tanstack/react-table';

import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
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
import { getErrorMessage } from '@/util/util';
import { create } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  Connection,
  ConnectionConfigSchema,
  ConnectionService,
} from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { getConnectionUrlSlugName } from '../../util';

interface DataTableRowActionsProps<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  onDeleted(): void;
}

export function DataTableRowActions<TData extends RowData>({
  row,
  onDeleted,
}: DataTableRowActionsProps<TData>) {
  const connection = row.original as Connection;
  const t = useTranslations('connections.table');
  const tDelete = useTranslations('connections.delete');
  const router = useRouter();
  const { account } = useAccount();
  const { mutateAsync } = useMutation(
    ConnectionService.method.deleteConnection
  );

  async function onDelete(): Promise<void> {
    try {
      await mutateAsync({ id: connection.id });
      toast.success(tDelete('removedList'));
      onDeleted();
    } catch (err) {
      console.error(err);
      toast.error(tDelete('error'), {
        description: getErrorMessage(err),
      });
    }
  }

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
            router.push(`/${account?.name}/connections/${connection.id}`)
          }
        >
          {t('view')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() =>
            router.push(
              `/${account?.name}/new/connection/${getConnectionUrlSlugName(
                connection.connectionConfig ??
                  create(ConnectionConfigSchema, {})
              )}?sourceId=${connection.id}`
            )
          }
        >
          {t('clone')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DeleteConfirmationDialog
          trigger={
            <DropdownMenuItem
              className="cursor-pointer"
              onSelect={(e) => e.preventDefault()}
            >
              {t('delete')}
            </DropdownMenuItem>
          }
          headerText={tDelete('confirmTitle')}
          description={tDelete('confirmDescription')}
          buttonText={tDelete('confirmButton')}
          onConfirm={async () => onDelete()}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
