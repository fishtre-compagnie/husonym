'use client';

import { Cross2Icon, DotsHorizontalIcon } from '@radix-ui/react-icons';
import { Row, RowData } from '@tanstack/react-table';

import ConfirmationDialog from '@/components/ConfirmationDialog';
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
import {
  JobRun,
  JobRunStatus as JobRunStatusEnum,
  JobService,
} from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { toast } from 'sonner';
import { useTranslations } from 'next-intl';

interface DataTableRowActionsProps<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  onDeleted(): void;
}

export function DataTableRowActions<TData extends RowData>({
  row,
  onDeleted,
}: DataTableRowActionsProps<TData>) {
  const run = row.original as JobRun;
  const t = useTranslations('runs.actions');
  const router = useRouter();
  const { account } = useAccount();
  const { mutateAsync: removeJobRunAsync } = useMutation(
    JobService.method.deleteJobRun
  );
  const { mutateAsync: cancelJobRunAsync } = useMutation(
    JobService.method.cancelJobRun
  );
  const { mutateAsync: terminateJobRunAsync } = useMutation(
    JobService.method.terminateJobRun
  );

  async function onDelete(): Promise<void> {
    try {
      await removeJobRunAsync({ accountId: account?.id, jobRunId: run.id });
      toast.success(t('removing'));
      onDeleted();
    } catch (err) {
      console.error(err);
      toast.error(t('removeFailed'), {
        description: getErrorMessage(err),
      });
    }
  }

  async function onCancel(): Promise<void> {
    try {
      await cancelJobRunAsync({ accountId: account?.id, jobRunId: run.id });
      toast.success(t('canceling'));
      onDeleted();
    } catch (err) {
      console.error(err);
      toast.error(t('cancelFailed'), {
        description: getErrorMessage(err),
      });
    }
  }

  async function onTerminate(): Promise<void> {
    try {
      await terminateJobRunAsync({ accountId: account?.id, jobRunId: run.id });
      toast.success(t('terminating'));
      onDeleted();
    } catch (err) {
      console.error(err);
      toast.error(t('terminateFailed'), {
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
          onClick={() => router.push(`/${account?.name}/runs/${run.id}`)}
        >
          {t('view')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {(run.status === JobRunStatusEnum.RUNNING ||
          run.status === JobRunStatusEnum.PENDING) && (
          <div>
            <ConfirmationDialog
              trigger={
                <DropdownMenuItem
                  className="cursor-pointer"
                  onSelect={(e) => e.preventDefault()}
                >
                  {t('cancel')}
                </DropdownMenuItem>
              }
              headerText={t('cancelTitle')}
              description={t('cancelDescription')}
              onConfirm={async () => onCancel()}
              buttonText={t('cancel')}
              buttonVariant="default"
              buttonIcon={<Cross2Icon />}
            />
            <DropdownMenuSeparator />
            <ConfirmationDialog
              trigger={
                <DropdownMenuItem
                  className="cursor-pointer"
                  onSelect={(e) => e.preventDefault()}
                >
                  {t('terminate')}
                </DropdownMenuItem>
              }
              headerText={t('terminateTitle')}
              description={t('terminateDescription')}
              onConfirm={async () => onTerminate()}
              buttonText={t('terminate')}
              buttonVariant="default"
              buttonIcon={<Cross2Icon />}
            />
            <DropdownMenuSeparator />
          </div>
        )}

        <DeleteConfirmationDialog
          trigger={
            <DropdownMenuItem
              className="cursor-pointer"
              onSelect={(e) => e.preventDefault()}
            >
              {t('delete')}
            </DropdownMenuItem>
          }
          headerText={t('deleteTitle')}
          description={t('deleteDescription')}
          buttonText={t('delete')}
          onConfirm={async () => {
            await onDelete();
          }}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
