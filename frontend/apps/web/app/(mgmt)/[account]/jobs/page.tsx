'use client';
import { PageBody, PageHeader } from '@/components/ds';
import EmptyState, { EmptyStateLinkButton } from '@/components/EmptyState';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import { Button } from '@/components/ui/button';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionService, JobService, JobStatus } from '@husonym/sdk';
import NextLink from 'next/link';
import { ReactElement, useMemo, useState } from 'react';
import { GrPowerCycle } from 'react-icons/gr';
import { getColumns } from './components/DataTable/columns';
import { DataTable } from './components/DataTable/data-table';
import { JobFilterTabs } from './components/JobFilterTabs';
import {
  buildJobRows,
  JobFilter,
  matchesFilter,
  matchesQuery,
} from './components/job-rows';

export default function Jobs(): ReactElement {
  return (
    <PageBody className="gap-6">
      <PageHeader
        title="Jobs"
        description="Each job copies a source to one or more destinations, anonymizing the data on the way."
        actions={<NewJobButton />}
      />
      <JobList />
    </PageBody>
  );
}

function JobList(): ReactElement {
  const { account } = useAccount();
  const accountId = account?.id;
  const {
    isLoading,
    data,
    refetch: refetchJobs,
  } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: statusData, refetch: refetchStatuses } = useQuery(
    JobService.method.getJobStatuses,
    { accountId },
    { enabled: !!accountId }
  );
  // The runs of the whole account, as the dashboard and the runs page read them: one
  // request gives every job its latest runs.
  const { data: runsData, refetch: refetchRuns } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'accountId', value: accountId ?? '' } },
    { enabled: !!accountId }
  );
  // Names and engines only: what the connections store is not needed here. The same
  // request as the pre-flight dialog of the Run button.
  const { data: connData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId, excludeSensitive: true },
    { enabled: !!accountId }
  );
  // The same query as the bell in the header, so the list costs nothing more and the two
  // counts cannot disagree.
  const { data: pendingData } = useQuery(
    JobService.method.getPendingMappingChanges,
    { accountId },
    { enabled: !!accountId }
  );

  const [filter, setFilter] = useState<JobFilter>('all');
  const [query, setQuery] = useState('');

  const accountName = account?.name ?? '';
  const columns = useMemo(
    () =>
      getColumns({
        accountName,
        onDeleted() {
          refetchJobs();
        },
        onStatusChanged() {
          refetchStatuses();
        },
        onRunStarted() {
          refetchRuns();
        },
      }),
    [accountName, refetchJobs, refetchStatuses, refetchRuns]
  );

  const rows = useMemo(
    () =>
      buildJobRows({
        jobs: data?.jobs ?? [],
        statuses: new Map<string, JobStatus>(
          (statusData?.statuses ?? []).map((s) => [s.jobId, s.status])
        ),
        runs: runsData?.jobRuns ?? [],
        connections: connData?.connections ?? [],
        pendingChanges: pendingData?.changes ?? [],
      }),
    [data, statusData, runsData, connData, pendingData]
  );

  const visible = useMemo(
    () =>
      rows.filter(
        (r) => matchesFilter(r.state, filter) && matchesQuery(r, query)
      ),
    [rows, filter, query]
  );

  if (isLoading) {
    return <SkeletonTable />;
  }

  if (rows.length === 0) {
    return (
      <EmptyState
        title="No jobs yet"
        description="Jobs are async workflows that transform data and sync it between source and destination systems."
        icon={<GrPowerCycle className="h-8 w-8 text-primary" />}
        extra={
          <EmptyStateLinkButton
            buttonText="Create your first job"
            href={`/${accountName}/new/job`}
          />
        }
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <JobFilterTabs rows={rows} value={filter} onChange={setFilter} />
        <label className="flex min-w-0 flex-[1_1_260px] items-center gap-2.5 rounded-[10px] border border-border bg-card px-3 py-[9px] text-muted-foreground focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-link">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <span className="sr-only">Filter jobs</span>
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter by name, connection, table…"
            className="min-w-0 flex-1 border-0 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
          />
        </label>
      </div>
      <DataTable
        columns={columns}
        data={visible}
        pageResetKey={`${filter}|${query}`}
        empty="No job matches this filter."
      />
    </div>
  );
}

function NewJobButton(): ReactElement {
  const { account } = useAccount();
  return (
    <Button asChild>
      <NextLink href={`/${account?.name}/new/job`}>
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <path d="M12 5v14M5 12h14" />
        </svg>
        New job
      </NextLink>
    </Button>
  );
}
