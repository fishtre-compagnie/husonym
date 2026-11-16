'use client';

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
import { useMutation } from '@connectrpc/connect-query';
import { TransformersService, UserDefinedTransformer } from '@husonym/sdk';
import { DotsHorizontalIcon } from '@radix-ui/react-icons';
import { Row, RowData } from '@tanstack/react-table';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

interface DataTableRowActionsProps<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  onDeleted(): void;
}

export function DataTableRowActions<TData extends RowData>({
  row,
  onDeleted,
}: DataTableRowActionsProps<TData>) {
  const transformer = row.original as UserDefinedTransformer;
  const t = useTranslations('connections.transformers.table');
  const tDelete = useTranslations('connections.transformers.delete');
  const router = useRouter();
  const { account } = useAccount();
  const { mutateAsync: removeTransformer } = useMutation(
    TransformersService.method.deleteUserDefinedTransformer
  );

  async function onDelete(): Promise<void> {
    try {
      await removeTransformer({ transformerId: transformer.id });
      toast.success(tDelete('removedList'));
      onDeleted();
    } catch (err) {
      toast.error(tDelete('error'), {
        description: getErrorMessage(err),
      });
    }
  }

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
          onClick={() =>
            router.push(`/${account?.name}/transformers/${transformer.id}`)
          }
        >
          {t('view')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DeleteConfirmationDialog
          trigger={
            <DropdownMenuItem
              className="cursor-pointer"
              onSelect={(e) => e.preventDefault()} // needed for the delete modal to not automatically close
            >
              {t('delete')}
            </DropdownMenuItem>
          }
          headerText={tDelete('confirmTitle')}
          description={tDelete('confirmDescription')}
          buttonText={tDelete('confirmButton')}
          onConfirm={() => onDelete()}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
