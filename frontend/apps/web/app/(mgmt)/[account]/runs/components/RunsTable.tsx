import { PageHeader, Panel, Pill, StatusDot } from '@/components/ds';
import EmptyState, { EmptyStateLinkButton } from '@/components/EmptyState';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import {
  JobRunsAutoRefreshInterval,
  onJobRunsAutoRefreshInterval,
  onJobRunsPaused,
} from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import { JobService } from '@husonym/sdk';
import { ArrowTopRightIcon } from '@radix-ui/react-icons';
import { ReactElement, useMemo, useState } from 'react';
import { GoStack } from 'react-icons/go';
import { getColumns } from './JobRunsTable/columns';
import { DataTable } from './JobRunsTable/data-table';
import {
  RunStatusGroup,
  STATUS_GROUPS,
} from './JobRunsTable/data-table-toolbar';

const INTERVAL_SELECT_OPTIONS: JobRunsAutoRefreshInterval[] = [
  'off',
  '10s',
  '30s',
  '1m',
  '5m',
];

interface RunsTableProps {}

export default function RunsTable(props: RunsTableProps): ReactElement {
  const {} = props;
  const { account } = useAccount();
  const [refreshInterval, setAutoRefreshInterval] =
    useState<JobRunsAutoRefreshInterval>('1m');
  const {
    isLoading,
    data,
    refetch: mutate,
    isFetching: isValidating,
  } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'accountId', value: account?.id ?? '' } },
    {
      enabled() {
        return !!account?.id && !onJobRunsPaused(refreshInterval);
      },
      refetchInterval() {
        return onJobRunsAutoRefreshInterval(refreshInterval);
      },
    }
  );

  const {
    data: jobsData,
    refetch: jobsMutate,
    isLoading: isJobsLoading,
    isFetching: isJobsValidating,
  } = useQuery(
    JobService.method.getJobs,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );

  const jobs = jobsData?.jobs ?? [];

  // must be memoized otherwise it causes columns to re-render endlessly when hovering over links within the table
  const jobNameMap = useMemo(() => {
    return jobs.reduce(
      (prev, curr) => {
        return { ...prev, [curr.id]: curr.name };
      },
      {} as Record<string, string>
    );
  }, [isJobsLoading, isJobsValidating]);

  const columns = useMemo(
    () =>
      getColumns({
        onDeleted() {
          mutate();
        },
        accountName: account?.name ?? '',
        jobNameMap: jobNameMap,
      }),
    [account?.name ?? '', jobNameMap]
  );

  const runs = useMemo(() => data?.jobRuns ?? [], [data?.jobRuns]);

  const statusCounts = useMemo(() => {
    const counts: Record<RunStatusGroup, number> = {
      all: runs.length,
      live: 0,
      complete: 0,
      failed: 0,
      stopped: 0,
    };
    for (const run of runs) {
      for (const [group, statuses] of Object.entries(STATUS_GROUPS)) {
        if (statuses.includes(run.status)) {
          counts[group as Exclude<RunStatusGroup, 'all'>] += 1;
        }
      }
    }
    return counts;
  }, [runs]);

  const header = (
    <PageHeader
      title="Runs"
      description="Every execution of your jobs, with its status and how long it took."
      actions={
        statusCounts.live > 0 ? (
          <Pill tone="brand">
            <StatusDot tone="brand" pulse />
            {statusCounts.live} running now
          </Pill>
        ) : undefined
      }
    />
  );

  if (isLoading) {
    return (
      <>
        {header}
        <SkeletonTable />
      </>
    );
  }

  function refreshClick(): void {
    mutate();
    jobsMutate();
  }

  return (
    <>
      {header}
      {runs.length == 0 ? (
        <EmptyState
          title="No runs yet"
          description="A run is one execution of a job. Start a job to see its runs here."
          icon={<GoStack className="w-8 h-8 text-primary" />}
          extra={
            <EmptyStateLinkButton
              buttonText="Go to jobs"
              buttonIcon={<ArrowTopRightIcon />}
              buttonIconSide="right"
              href={`/${account?.name}/jobs`}
            />
          }
        />
      ) : (
        <Panel>
          <DataTable
            columns={columns}
            data={runs}
            refreshInterval={refreshInterval}
            onAutoRefreshIntervalChange={(newVal: JobRunsAutoRefreshInterval) =>
              setAutoRefreshInterval(newVal)
            }
            autoRefreshIntervalOptions={INTERVAL_SELECT_OPTIONS}
            onRefreshClick={refreshClick}
            isRefreshing={isValidating}
            statusCounts={statusCounts}
          />
        </Panel>
      )}
    </>
  );
}
