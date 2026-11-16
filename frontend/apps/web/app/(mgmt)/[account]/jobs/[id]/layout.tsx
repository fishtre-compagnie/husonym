'use client';
import ButtonText from '@/components/ButtonText';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import ResourceId from '@/components/ResourceId';
import { PageBody, Pill, StatusDot, Tone } from '@/components/ds';
import JobNotFoundAlert from '@/components/jobs/JobNotFoundAlert';
import { hasPreflight } from '@/components/jobs/preflight/report';
import { usePreflightThenRun } from '@/components/jobs/preflight/usePreflightThenRun';
import { isJobSubsettable } from '@/components/jobs/subsets/utils';
import { useAccount } from '@/components/providers/account-provider';
import { LayoutProps } from '@/components/types';
import { Button } from '@/components/ui/button';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { getErrorMessage } from '@/util/util';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import {
  ConnectionService,
  Job,
  JobService,
  JobSourceOptions,
  JobStatus,
  JobTypeConfig,
} from '@husonym/sdk';
import { PlayIcon, TrashIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { use, useMemo } from 'react';
import { toast } from 'sonner';
import JobIdSkeletonForm from './JobIdSkeletonForm';
import JobCloneButton from './components/JobCloneButton';
import JobPauseButton from './components/JobPauseButton';
import JobTabs, { JobTab } from './components/JobTabs';
import { getConnectionIdFromSource } from './source/components/util';

export default function JobIdLayout(props: LayoutProps) {
  const params = use(props.params);

  const { children } = props;

  const id = params?.id ?? '';
  const t = useTranslations('jobs.job');
  const router = useRouter();
  const { account } = useAccount();
  const { data, isLoading, isPending } = useQuery(
    JobService.method.getJob,
    { id },
    { enabled: !!id }
  );
  const { data: jobStatus, refetch: mutateJobStatus } = useQuery(
    JobService.method.getJobStatus,
    { jobId: id },
    { enabled: !!id }
  );
  const { refetch: mutateRecentRuns } = useQuery(
    JobService.method.getJobRecentRuns,
    { jobId: id },
    { enabled: !!id }
  );
  const { refetch: mutateJobRunsByJob } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'jobId', value: id } },
    { enabled: !!id }
  );

  // The names of the connections, for the route of the job under its name.
  const { data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connectionNames = useMemo(
    () =>
      new Map((connectionsData?.connections ?? []).map((c) => [c.id, c.name])),
    [connectionsData?.connections]
  );

  const { mutateAsync: removeJob } = useMutation(JobService.method.deleteJob);
  const { mutateAsync: triggerJobRun } = useMutation(
    JobService.method.createJobRun
  );

  // The pre-flight check goes first, when the job has one: see usePreflightThenRun.
  const { start: startWithPreflight, dialog: preflightDialog } =
    usePreflightThenRun(data?.job, onTriggerJobRun);

  function refreshRuns(): void {
    mutateRecentRuns();
    mutateJobRunsByJob();
  }

  async function onTriggerJobRun(): Promise<void> {
    try {
      const { jobRun } = await triggerJobRun({ jobId: id });
      // The API answers with the run it started. One that comes back without its start
      // cannot be read yet: runs are found through an index that sees a new one a moment
      // late. Its page would not open, and the lists are read again once it has caught up.
      const visible = !!jobRun?.startedAt;
      toast.success(t('runTriggered'), {
        action: visible
          ? {
              label: t('viewRun'),
              onClick: () => router.push(`/${account?.name}/runs/${jobRun.id}`),
            }
          : undefined,
      });
      refreshRuns();
      if (!visible) {
        setTimeout(refreshRuns, 4000);
      }
    } catch (err) {
      console.error(err);
      toast.error(t('runError'), {
        description: getErrorMessage(err),
      });
    }
  }

  async function onDelete(): Promise<void> {
    if (!id) {
      return;
    }
    try {
      await removeJob({ id });
      toast.success(t('deleted'));
      router.push(`/${account?.name}/jobs`);
    } catch (err) {
      console.error(err);
      toast.error(t('deleteError'), {
        description: getErrorMessage(err),
      });
    }
  }

  function onNewStatus(_newStatus: JobStatus): void {
    mutateJobStatus();
  }

  const sidebarNavItems = useGetSidebarNavItems(data?.job);

  if (isLoading || isPending) {
    return (
      <PageBody>
        <JobIdSkeletonForm />
      </PageBody>
    );
  }

  if (!data?.job) {
    return (
      <PageBody>
        <JobNotFoundAlert />
      </PageBody>
    );
  }

  const job = data.job;
  const jobType = getLabeledJobType(job.jobType, job.source?.options);
  const sourceConnectionId = getConnectionIdFromSource(job.source);
  const sourceName = sourceConnectionId
    ? connectionNames.get(sourceConnectionId)
    : undefined;
  const destinationNames = job.destinations
    .map((d) => connectionNames.get(d.connectionId))
    .filter((n): n is string => !!n);
  const status = getStatusDisplay(jobStatus?.status);

  return (
    <PageBody className="gap-6">
      <header className="flex flex-col gap-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2.5">
              <Link
                href={`/${account?.name}/jobs`}
                className="text-muted-foreground hover:text-foreground"
              >
                {t('breadcrumb')}
              </Link>
              <span aria-hidden="true" className="text-border">
                /
              </span>
              <h1 className="m-0 min-w-0 break-all font-display text-2xl font-semibold tracking-[-0.01em]">
                {job.name}
              </h1>
              <Pill tone="brand">{t(`type.${jobType}`)}</Pill>
              {status && (
                <Pill tone={status.tone}>
                  <StatusDot tone={status.tone} />
                  {t(`status.${status.label}`)}
                </Pill>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              {(sourceName || destinationNames.length > 0) && (
                <span className="font-mono text-xs text-muted-foreground">
                  {sourceName ?? '—'}
                  {destinationNames.length > 0 && (
                    <>
                      {' '}
                      <span aria-label={t('to')}>→</span>{' '}
                      {destinationNames.join(', ')}
                    </>
                  )}
                </span>
              )}
              <ResourceId
                labelText={job.id}
                copyText={job.id}
                onHoverText={t('copyId')}
              />
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <JobCloneButton job={job} />
            <JobPauseButton
              jobId={id}
              status={jobStatus?.status}
              onNewStatus={onNewStatus}
            />
            <DeleteConfirmationDialog
              trigger={
                <Button
                  variant="outline"
                  className="text-danger-fg hover:bg-danger-soft"
                >
                  <ButtonText leftIcon={<TrashIcon />} text={t('delete')} />
                </Button>
              }
              headerText={t('deleteTitle')}
              description={t('deleteDescription')}
              onConfirm={async () => onDelete()}
            />
            <Button
              onClick={() =>
                hasPreflight(job) ? startWithPreflight() : onTriggerJobRun()
              }
            >
              <ButtonText leftIcon={<PlayIcon />} text={t('triggerRun')} />
            </Button>
          </div>
        </div>
        <JobTabs items={sidebarNavItems} />
      </header>
      <div className="min-w-0">{children}</div>
      {preflightDialog}
    </PageBody>
  );
}

function getStatusDisplay(
  status?: JobStatus
): { label: 'enabled' | 'paused' | 'disabled'; tone: Tone } | undefined {
  switch (status) {
    case JobStatus.ENABLED:
      return { label: 'enabled', tone: 'ok' };
    case JobStatus.PAUSED:
      return { label: 'paused', tone: 'warn' };
    case JobStatus.DISABLED:
      return { label: 'disabled', tone: 'quiet' };
    default:
      return undefined;
  }
}

function getLabeledJobType(
  jobTypeConfig?: JobTypeConfig,
  options?: JobSourceOptions
): 'sync' | 'generate' | 'aiGenerate' | 'piiDetect' {
  switch (options?.config.case) {
    case 'generate':
      return 'generate';
    case 'aiGenerate':
      return 'aiGenerate';
    default:
      switch (jobTypeConfig?.jobType.case) {
        case 'sync':
          return 'sync';
        case 'piiDetect':
          return 'piiDetect';
        default:
          return 'sync';
      }
  }
}

function useGetSidebarNavItems(job?: Job): JobTab[] {
  const t = useTranslations('jobs.job.tabs');
  const { account } = useAccount();
  const { data: systemAppConfigData, isLoading: isSystemConfigLoading } =
    useGetSystemAppConfig();
  // The same query as the review tab and the bell, so the three agree and refresh together.
  const { data: pendingData } = useQuery(
    JobService.method.getPendingMappingChanges,
    { accountId: account?.id, jobId: job?.id },
    { enabled: !!account?.id && !!job?.id }
  );
  const pendingCount = pendingData?.changes.length ?? 0;

  if (!account || !job) {
    return [{ title: t('overview'), href: '' }];
  }
  const badgeText = getLabeledJobType(job.jobType, job.source?.options);
  const basePath = `/${account.name}/jobs/${job.id}`;
  // Every kind of job has its usage: it is read from the counters of the instance.
  const usage = { title: t('usage'), href: `${basePath}/usage` };
  const isJobHooksEnabled =
    !isSystemConfigLoading && systemAppConfigData?.isJobHooksEnabled;

  switch (badgeText) {
    case 'sync': {
      const nav: JobTab[] = [
        { title: t('overview'), href: basePath },
        { title: t('source'), href: `${basePath}/source` },
        { title: t('mapping'), href: `${basePath}/mapping` },
        { title: t('destinations'), href: `${basePath}/destinations` },
        // Only sync jobs copy columns from a source, so only they can copy one in clear.
        {
          title: t('review'),
          href: `${basePath}/review`,
          count: pendingCount,
        },
      ];
      if (isJobSubsettable(job)) {
        nav.push({
          title: t('subsets'),
          href: `${basePath}/subsets`,
        });
      }
      nav.push(usage);
      if (isJobHooksEnabled) {
        nav.push({
          title: t('hooks'),
          href: `${basePath}/hooks`,
        });
      }

      return nav;
    }

    case 'generate': {
      const nav = [
        { title: t('overview'), href: basePath },
        { title: t('source'), href: `${basePath}/source` },
        { title: t('destinations'), href: `${basePath}/destinations` },
        usage,
      ];
      if (isJobHooksEnabled) {
        nav.push({
          title: t('hooks'),
          href: `${basePath}/hooks`,
        });
      }

      return nav;
    }
    case 'aiGenerate': {
      const nav = [
        { title: t('overview'), href: basePath },
        { title: t('source'), href: `${basePath}/source` },
        { title: t('destinations'), href: `${basePath}/destinations` },
        usage,
      ];
      if (isJobHooksEnabled) {
        nav.push({
          title: t('hooks'),
          href: `${basePath}/hooks`,
        });
      }
      return nav;
    }
    case 'piiDetect': {
      return [
        { title: t('overview'), href: basePath },
        { title: t('source'), href: `${basePath}/source` },
        usage,
      ];
    }
    default: {
      return [{ title: t('overview'), href: basePath }];
    }
  }
}
