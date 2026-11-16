'use client';
import ConfirmationDialog from '@/components/ConfirmationDialog';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Panel, PanelHeader } from '@/components/ds';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { formatDateTime, getErrorMessage } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { JobRunStatus as JobRunStatusEnum, JobService } from '@husonym/sdk';
import {
  Cross2Icon,
  DotsHorizontalIcon,
  ReloadIcon,
} from '@radix-ui/react-icons';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { toast } from 'sonner';
import JobRunStatus from '../../../runs/components/JobRunStatus';

interface Props {
  jobId: string;
}

export default function JobRecentRuns({ jobId }: Props): ReactElement {
  const t = useTranslations('jobs.overview.recentRuns');
  const { account } = useAccount();
  const {
    data: recentRunsData,
    isLoading,
    error: recentRunsError,
    refetch: recentRunsMutate,
    isFetching: isValidating,
  } = useQuery(
    JobService.method.getJobRecentRuns,
    { jobId },
    { enabled: !!jobId }
  );
  const {
    data: jobRunsData,
    isLoading: jobRunsLoading,
    isFetching: jobRunsValidating,
    refetch: jobRunsMutate,
  } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'jobId', value: jobId } },
    { enabled: !!jobId }
  );
  const { mutateAsync: removeJobRunAsync } = useMutation(
    JobService.method.deleteJobRun
  );
  const { mutateAsync: cancelJobRunAsync } = useMutation(
    JobService.method.cancelJobRun
  );
  const { mutateAsync: terminateJobRunAsync } = useMutation(
    JobService.method.terminateJobRun
  );

  const recentRuns = (recentRunsData?.recentRuns ?? []).toReversed();
  const jobRunsIdMap = new Map(
    (jobRunsData?.jobRuns ?? []).map((jr) => [jr.id, jr])
  );
  const router = useRouter();

  if (isLoading || jobRunsLoading) {
    return <Skeleton className="w-full h-full" />;
  }

  function onRefreshClick(): void {
    recentRunsMutate();
    jobRunsMutate();
  }

  async function onDelete(runId: string): Promise<void> {
    try {
      await removeJobRunAsync({ accountId: account?.id, jobRunId: runId });
      onRefreshClick();
      toast.success(t('deleting'));
    } catch (err) {
      console.error(err);
      toast.error(t('deleteError'), {
        description: getErrorMessage(err),
      });
    }
  }

  async function onCancel(runId: string): Promise<void> {
    try {
      await cancelJobRunAsync({ accountId: account?.id, jobRunId: runId });
      toast.success(t('canceling'));
      onRefreshClick();
    } catch (err) {
      console.error(err);
      toast.error(t('cancelError'), {
        description: getErrorMessage(err),
      });
    }
  }

  async function onTerminate(runId: string): Promise<void> {
    try {
      await terminateJobRunAsync({ accountId: account?.id, jobRunId: runId });
      toast.success(t('terminating'));
      onRefreshClick();
    } catch (err) {
      console.error(err);
      toast.error(t('terminateError'), {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <Panel>
      {recentRunsError ? (
        <Alert variant="destructive">
          <AlertTitle>{t('loadError')}</AlertTitle>
        </Alert>
      ) : (
        <div>
          <PanelHeader
            title={t('title')}
            actions={
              <Button
                className={
                  isValidating || jobRunsValidating ? 'animate-spin' : ''
                }
                disabled={isValidating || jobRunsValidating}
                variant="ghost"
                size="icon"
                aria-label={t('refresh')}
                onClick={() => onRefreshClick()}
              >
                <ReloadIcon className="h-4 w-4" />
              </Button>
            }
          />
          {recentRuns.length === 0 && (
            <p className="px-5 py-8 text-center text-sm text-muted-foreground">
              {t('empty')}
            </p>
          )}
          <div className="overflow-x-auto">
            <Table className={recentRuns.length === 0 ? 'hidden' : undefined}>
              <TableHeader className="bg-surface-2">
                <TableRow>
                  <TableHead className="px-6">{t('columns.runId')}</TableHead>
                  <TableHead className="px-6">{t('columns.started')}</TableHead>
                  <TableHead className="px-6">
                    {t('columns.completed')}
                  </TableHead>
                  <TableHead className="px-6">{t('columns.status')}</TableHead>
                  <TableHead className="px-6">{t('columns.action')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recentRuns.map((r) => {
                  const jobRun = jobRunsIdMap.get(r.jobRunId);
                  return (
                    <TableRow key={r.jobRunId}>
                      <TableCell className="px-6">
                        {jobRun ? (
                          <Link
                            className="font-mono text-[13px] text-link hover:underline"
                            href={`/${account?.name}/runs/${r.jobRunId}`}
                          >
                            {r.jobRunId}
                          </Link>
                        ) : (
                          <span className="font-mono text-[13px]">
                            {r.jobRunId}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="px-6">
                        <span className="tabular-nums text-muted-foreground">
                          {formatDateTime(
                            r.startTime ? timestampDate(r.startTime) : undefined
                          )}
                        </span>
                      </TableCell>
                      <TableCell className="px-6">
                        <span className="tabular-nums text-muted-foreground">
                          {jobRun &&
                            formatDateTime(
                              jobRun.completedAt
                                ? timestampDate(jobRun.completedAt)
                                : undefined
                            )}
                        </span>
                      </TableCell>
                      <TableCell className="px-6">
                        <JobRunStatus status={jobRun?.status} />
                      </TableCell>
                      <TableCell className="px-6">
                        {jobRun && (
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
                            <DropdownMenuContent
                              align="end"
                              className="w-[160px]"
                            >
                              <DropdownMenuItem
                                className="cursor-pointer"
                                onClick={() =>
                                  router.push(
                                    `/${account?.name}/runs/${jobRun.id}`
                                  )
                                }
                              >
                                {t('view')}
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              {(jobRun.status === JobRunStatusEnum.RUNNING ||
                                jobRun.status === JobRunStatusEnum.PENDING) && (
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
                                    onConfirm={async () => onCancel(jobRun.id)}
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
                                    onConfirm={async () =>
                                      onTerminate(jobRun.id)
                                    }
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
                                onConfirm={async () => onDelete(jobRun.id)}
                              />
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </div>
      )}
    </Panel>
  );
}
