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
      toast.success('Job run triggered successfully!', {
        action: visible
          ? {
              label: 'View run',
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
      toast.error('Unable to trigger job run', {
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
      toast.success('Job removed successfully!');
      router.push(`/${account?.name}/jobs`);
    } catch (err) {
      console.error(err);
      toast.error('Unable to remove job', {
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
                Jobs
              </Link>
              <span aria-hidden="true" className="text-border">
                /
              </span>
              <h1 className="m-0 min-w-0 break-all font-display text-2xl font-semibold tracking-[-0.01em]">
                {job.name}
              </h1>
              <Pill tone="brand">{jobType}</Pill>
              {status && (
                <Pill tone={status.tone}>
                  <StatusDot tone={status.tone} />
                  {status.label}
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
                      <span aria-label="to">→</span>{' '}
                      {destinationNames.join(', ')}
                    </>
                  )}
                </span>
              )}
              <ResourceId
                labelText={job.id}
                copyText={job.id}
                onHoverText="Copy the Job ID"
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
                  <ButtonText leftIcon={<TrashIcon />} text="Delete" />
                </Button>
              }
              headerText="Are you sure you want to delete this job?"
              description="Deleting this job will also delete all job runs."
              onConfirm={async () => onDelete()}
            />
            <Button
              onClick={() =>
                hasPreflight(job) ? startWithPreflight() : onTriggerJobRun()
              }
            >
              <ButtonText leftIcon={<PlayIcon />} text="Trigger run" />
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
): { label: string; tone: Tone } | undefined {
  switch (status) {
    case JobStatus.ENABLED:
      return { label: 'Enabled', tone: 'ok' };
    case JobStatus.PAUSED:
      return { label: 'Paused', tone: 'warn' };
    case JobStatus.DISABLED:
      return { label: 'Disabled', tone: 'quiet' };
    default:
      return undefined;
  }
}

function getLabeledJobType(
  jobTypeConfig?: JobTypeConfig,
  options?: JobSourceOptions
): 'Sync Job' | 'Generate Job' | 'AI Generate Job' | 'PII Detect Job' {
  switch (options?.config.case) {
    case 'generate':
      return 'Generate Job';
    case 'aiGenerate':
      return 'AI Generate Job';
    default:
      switch (jobTypeConfig?.jobType.case) {
        case 'sync':
          return 'Sync Job';
        case 'piiDetect':
          return 'PII Detect Job';
        default:
          return 'Sync Job';
      }
  }
}

function useGetSidebarNavItems(job?: Job): JobTab[] {
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
    return [{ title: 'Overview', href: '' }];
  }
  const badgeText = getLabeledJobType(job.jobType, job.source?.options);
  const basePath = `/${account.name}/jobs/${job.id}`;
  // Every kind of job has its usage: it is read from the counters of the instance.
  const usage = { title: 'Usage', href: `${basePath}/usage` };
  const isJobHooksEnabled =
    !isSystemConfigLoading && systemAppConfigData?.isJobHooksEnabled;

  switch (badgeText) {
    case 'Sync Job': {
      const nav: JobTab[] = [
        { title: 'Overview', href: basePath },
        { title: 'Source & mapping', href: `${basePath}/source` },
        { title: 'Destinations', href: `${basePath}/destinations` },
        // Only sync jobs copy columns from a source, so only they can copy one in clear.
        {
          title: 'Review',
          href: `${basePath}/review`,
          count: pendingCount,
        },
      ];
      if (isJobSubsettable(job)) {
        nav.push({
          title: 'Subsets',
          href: `${basePath}/subsets`,
        });
      }
      nav.push(usage);
      if (isJobHooksEnabled) {
        nav.push({
          title: 'Hooks',
          href: `${basePath}/hooks`,
        });
      }

      return nav;
    }

    case 'Generate Job': {
      const nav = [
        { title: 'Overview', href: basePath },
        { title: 'Source', href: `${basePath}/source` },
        { title: 'Destinations', href: `${basePath}/destinations` },
        usage,
      ];
      if (isJobHooksEnabled) {
        nav.push({
          title: 'Hooks',
          href: `${basePath}/hooks`,
        });
      }

      return nav;
    }
    case 'AI Generate Job': {
      const nav = [
        { title: 'Overview', href: basePath },
        { title: 'Source', href: `${basePath}/source` },
        { title: 'Destinations', href: `${basePath}/destinations` },
        usage,
      ];
      if (isJobHooksEnabled) {
        nav.push({
          title: 'Hooks',
          href: `${basePath}/hooks`,
        });
      }
      return nav;
    }
    case 'PII Detect Job': {
      return [
        { title: 'Overview', href: basePath },
        { title: 'Source', href: `${basePath}/source` },
        usage,
      ];
    }
    default: {
      return [{ title: 'Overview', href: basePath }];
    }
  }
}
