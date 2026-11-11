'use client';
import JobRunPreflight from '@/components/jobs/preflight/JobRunPreflight';
import { PageProps } from '@/components/types';

import ConfirmationDialog from '@/components/ConfirmationDialog';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import {
  PageBody,
  PageHeader,
  Panel,
  PanelHeader,
  Pill,
  ProgressBar,
  StatCard,
  StatusDot,
} from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import {
  getEventsSpan,
  getPhases,
  getTableProgress,
} from '@/components/RunTImeline/events';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { JobRunStatus as JobRunStatusEnum, JobService } from '@husonym/sdk';
import { TiCancel } from 'react-icons/ti';

import { CopyButton } from '@/components/CopyButton';
import PiiDetectionJobGuard from '@/components/guards/PiiDetectionJobGuard';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import {
  cn,
  refreshEventsWhenEventsIncomplete,
  refreshJobRunWhenJobRunning,
} from '@/libs/utils';
import { formatDateTime, getErrorMessage } from '@/util/util';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { Editor } from '@monaco-editor/react';
import {
  ArrowRightIcon,
  ChevronLeftIcon,
  Cross2Icon,
  ExclamationTriangleIcon,
  TrashIcon,
} from '@radix-ui/react-icons';
import { format } from 'date-fns';
import { useTheme } from 'next-themes';
import NextLink from 'next/link';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { format as formatSql } from 'sql-formatter';
import yaml from 'yaml';
import { getJobRunStatusMeta } from '../components/JobRunStatus';
import {
  formatAgo,
  formatSpan,
  runSpan,
  toDate,
  useNow,
} from '../components/run-format';
import JobRunActivityErrors from './components/JobRunActivityErrors';
import JobRunActivityTable from './components/JobRunActivityTable';
import JobRunLogs from './components/JobRunLogs';
import JobRunPiiDetectionTable from './components/JobRunPiiDetectionTable/JobRunPiiDetectionTable';
import RunPhases from './components/RunPhases';
import RunTablesPanel from './components/RunTablesPanel';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const { account } = useAccount();
  const accountId = account?.id || '';
  const id = decodeURIComponent(params?.id ?? '');
  const router = useRouter();
  const { data: systemAppConfigData, isLoading: isSystemAppConfigDataLoading } =
    useGetSystemAppConfig();
  const {
    data,
    isLoading,
    refetch: mutate,
  } = useQuery(
    JobService.method.getJobRun,
    { jobRunId: id, accountId: accountId },
    {
      enabled: !!id && !!accountId,
      refetchInterval(query) {
        return query.state.data
          ? refreshJobRunWhenJobRunning(query.state.data)
          : 0;
      },
    }
  );
  const jobRun = data?.jobRun;

  const {
    data: eventData,
    isFetching: isValidating,
    refetch: eventMutate,
  } = useQuery(
    JobService.method.getJobRunEvents,
    { jobRunId: id, accountId: accountId },
    {
      enabled: !!id && !!accountId,
      refetchInterval(query) {
        return query.state.data
          ? refreshEventsWhenEventsIncomplete(query.state.data)
          : 0;
      },
    }
  );

  // the job, for its name and the tables it maps (same query as the errors panel)
  const { data: jobData } = useQuery(
    JobService.method.getJob,
    { id: jobRun?.jobId },
    { enabled: !!jobRun?.jobId }
  );
  const job = jobData?.job;

  const { mutateAsync: removeJobRunAsync } = useMutation(
    JobService.method.deleteJobRun
  );
  const { mutateAsync: cancelJobRunAsync } = useMutation(
    JobService.method.cancelJobRun
  );
  const { mutateAsync: terminateJobRunAsync } = useMutation(
    JobService.method.terminateJobRun
  );
  const { mutateAsync: getRunContextAsync } = useMutation(
    JobService.method.getRunContext
  );

  const [isViewSelectDialogOpen, setIsSelectDialogOpen] =
    useState<boolean>(false);
  const [activeSelectQuery, setActiveSelectQuery] = useState<SelectQuery>({
    schema: '',
    table: '',
    select: '',
  });
  const [isRetrievingRunContext, setIsRetrievingRunContext] =
    useState<boolean>(false);

  async function onDelete(): Promise<void> {
    try {
      await removeJobRunAsync({ accountId: accountId, jobRunId: id });
      toast.success('Job run removed successfully!');
      router.push(`/${account?.name}/runs`);
    } catch (err) {
      console.error(err);
      toast.error('Unable to remove job run', {
        description: getErrorMessage(err),
      });
    }
  }

  async function onCancel(): Promise<void> {
    try {
      await cancelJobRunAsync({ accountId, jobRunId: id });
      toast.success('Job run canceled successfully!');
      mutate();
      eventMutate();
    } catch (err) {
      console.error(err);
      toast.error('Unable to cancel job run', {
        description: getErrorMessage(err),
      });
    }
  }

  async function onTerminate(): Promise<void> {
    try {
      await terminateJobRunAsync({ accountId, jobRunId: id });
      toast.success('Job run terminated successfully!');
      mutate();
      eventMutate();
    } catch (err) {
      console.error(err);
      toast.error('Unable to terminate job run', {
        description: getErrorMessage(err),
      });
    }
  }

  async function onViewSelectClicked(
    schema: string,
    table: string
  ): Promise<void> {
    if (isRetrievingRunContext) {
      return;
    }
    setIsRetrievingRunContext(true);
    try {
      const rcResp = await getRunContextAsync({
        id: {
          accountId: accountId,
          externalId: buildBenthosRunCtxExternalId(schema, table),
          jobRunId: id,
        },
      });
      const runCtx = parseUint8ArrayToYaml(rcResp.value);
      if (isValidRunContext(runCtx)) {
        setActiveSelectQuery({
          schema,
          table,
          select: runCtx.input.pooled_sql_raw.query,
        });
        setIsSelectDialogOpen(true);
      } else {
        toast.error('Unable to parse run context', {
          description: `Was unable to pull Select Query out of run context for ${schema}.${table}`,
        });
      }
    } catch (err) {
      console.error(err);
      toast.error('Unable to retrieve select for table', {
        description: getErrorMessage(err),
      });
    } finally {
      setIsRetrievingRunContext(false);
    }
  }

  const isRunning = useMemo(() => {
    return (
      jobRun?.status === JobRunStatusEnum.RUNNING ||
      jobRun?.status === JobRunStatusEnum.PENDING
    );
  }, [jobRun?.status]);

  // ticks every second while the run goes on, for its duration and its bars
  const now = useNow(isRunning);
  const statusMeta = getJobRunStatusMeta(jobRun?.status);
  const span =
    jobRun?.completedAt || isRunning
      ? runSpan(jobRun?.startedAt, jobRun?.completedAt, now)
      : undefined;
  const startedAt = toDate(jobRun?.startedAt);
  const completedAt = toDate(jobRun?.completedAt);

  const events = eventData?.events;
  const jobStatus = jobRun?.status;
  // computed on every render, not memoized: an activity still open ends at
  // "now", so these move each second while the run goes on
  const phases = events ? getPhases(events, jobStatus) : [];
  const tables = events ? getTableProgress(events, jobStatus) : [];
  const eventsSpan = events ? getEventsSpan(events, jobStatus) : undefined;
  // while the run goes on, the tables the job maps that it has not begun yet
  const begun = new Set(tables.map((t) => t.key));
  const waitingTables =
    isRunning && job
      ? Array.from(
          new Set(job.mappings.map((m) => `${m.schema}.${m.table}`))
        ).filter((k) => !begun.has(k))
      : [];

  const tablesDone = tables.filter((t) => t.status === 'completed').length;
  const tablesTotal = tables.length + waitingTables.length;
  const failedActivities =
    events?.filter((e) => e.tasks.some((t) => t.error)).length ?? 0;
  const pendingFailures =
    jobRun?.pendingActivities.filter((a) => a.lastFailure) ?? [];
  const showLogs =
    !isSystemAppConfigDataLoading && !!systemAppConfigData?.enableRunLogs;

  const progressTone =
    statusMeta.tone === 'danger'
      ? 'danger'
      : statusMeta.tone === 'ok'
        ? 'ok'
        : statusMeta.tone === 'warn'
          ? 'warn'
          : 'brand';

  return (
    <PageBody className="runs-page">
      <div className="flex flex-col gap-3">
        <nav
          aria-label="Breadcrumb"
          className="flex items-center gap-2 text-sm"
        >
          <NextLink
            href={`/${account?.name}/runs`}
            className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
          >
            <ChevronLeftIcon aria-hidden="true" />
            Runs
          </NextLink>
          {jobRun?.jobId && (
            <>
              <span className="text-border" aria-hidden="true">
                /
              </span>
              <NextLink
                href={`/${account?.name}/jobs/${jobRun.jobId}`}
                className="text-link hover:text-foreground"
              >
                {job?.name ?? 'Job'}
              </NextLink>
            </>
          )}
        </nav>
        <PageHeader
          title={
            startedAt ? `Run of ${format(startedAt, 'MMM d, HH:mm')}` : 'Run'
          }
          description={
            <span className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <Pill tone={statusMeta.tone} className="px-2.5 py-1">
                <StatusDot tone={statusMeta.tone} pulse={statusMeta.live} />
                {statusMeta.label}
                {span !== undefined && ` · ${formatSpan(span)}`}
              </Pill>
              {startedAt && (
                <span className="text-[13px]" title={formatDateTime(startedAt)}>
                  Started {formatAgo(startedAt)}
                </span>
              )}
              {jobRun?.id && (
                <span className="flex items-center gap-1 font-mono text-xs">
                  {jobRun.id}
                  <CopyButton
                    onHoverText="Copy the Run ID"
                    textToCopy={jobRun.id}
                    onCopiedText="Success!"
                    buttonVariant="ghost"
                  />
                </span>
              )}
            </span>
          }
          actions={
            <>
              {isRunning && (
                <>
                  <ConfirmationDialog
                    trigger={
                      <Button variant="outline">
                        <Cross2Icon />
                        Cancel run
                      </Button>
                    }
                    headerText="Cancel Job Run?"
                    description="Are you sure you want to cancel this job run?"
                    onConfirm={async () => onCancel()}
                    buttonText="Cancel"
                    buttonVariant="default"
                    buttonIcon={<Cross2Icon />}
                  />
                  <ConfirmationDialog
                    trigger={
                      <Button variant="outline">
                        <TiCancel />
                        Terminate
                      </Button>
                    }
                    headerText="Terminate Job Run?"
                    description="Are you sure you want to terminate this job run?"
                    onConfirm={async () => onTerminate()}
                    buttonText="Terminate"
                    buttonVariant="default"
                    buttonIcon={<Cross2Icon />}
                  />
                </>
              )}
              {jobRun?.jobId && (
                <Button
                  variant="outline"
                  onClick={() =>
                    router.push(`/${account?.name}/jobs/${jobRun.jobId}`)
                  }
                >
                  View job
                  <ArrowRightIcon />
                </Button>
              )}
              <DeleteConfirmationDialog
                trigger={
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Delete this run"
                    title="Delete this run"
                    className="text-danger-fg hover:text-danger-fg"
                  >
                    <TrashIcon />
                  </Button>
                }
                headerText="Delete Job Run?"
                description="Are you sure you want to delete this job run?"
                onConfirm={async () => onDelete()}
              />
            </>
          }
        />
      </div>

      {isLoading ? (
        <div className="flex flex-col gap-6">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-4">
            <Skeleton className="h-28 w-full rounded-2xl" />
            <Skeleton className="h-28 w-full rounded-2xl" />
            <Skeleton className="h-28 w-full rounded-2xl" />
            <Skeleton className="h-28 w-full rounded-2xl" />
          </div>
          <Skeleton className="h-72 w-full rounded-2xl" />
        </div>
      ) : (
        <>
          <RunPhases phases={phases} isLive={isRunning} />

          {tablesTotal > 0 && (
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-baseline justify-between gap-2 text-[13px]">
                <span className="text-muted-foreground">
                  <strong className="font-semibold text-foreground">
                    {tablesDone}
                  </strong>{' '}
                  of {tablesTotal} {tablesTotal === 1 ? 'table' : 'tables'}{' '}
                  copied
                </span>
                <span className="font-mono text-xs text-muted-foreground">
                  {Math.round((tablesDone / tablesTotal) * 100)}%
                </span>
              </div>
              <ProgressBar
                value={(tablesDone / tablesTotal) * 100}
                live={isRunning}
                tone={progressTone}
                label="Tables copied"
              />
            </div>
          )}

          <section
            aria-label="Run summary"
            className="grid grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-3"
          >
            <StatCard
              label="Duration"
              value={span !== undefined ? formatSpan(span) : '–'}
              note={
                isRunning
                  ? 'still running'
                  : completedAt
                    ? `ended ${format(completedAt, 'MMM d, HH:mm:ss')}`
                    : 'no end recorded'
              }
            />
            <StatCard
              label="Started"
              value={startedAt ? format(startedAt, 'HH:mm:ss') : '–'}
              note={
                startedAt ? format(startedAt, 'EEEE, MMM d yyyy') : undefined
              }
            />
            <StatCard
              label="Tables copied"
              value={
                tablesTotal > 0 ? (
                  <>
                    {tablesDone}
                    <span className="text-base text-muted-foreground">
                      {' '}
                      / {tablesTotal}
                    </span>
                  </>
                ) : (
                  '–'
                )
              }
              note={
                tablesTotal === 0
                  ? 'no table synced yet'
                  : tables.some((t) => t.status === 'failed')
                    ? `${tables.filter((t) => t.status === 'failed').length} failed`
                    : `${events?.length ?? 0} activities in all`
              }
              noteTone={
                tables.some((t) => t.status === 'failed') ? 'danger' : 'muted'
              }
            />
            <StatCard
              label="Activities with errors"
              value={failedActivities}
              tone={failedActivities > 0 ? 'danger' : 'default'}
              note={
                failedActivities > 0
                  ? 'an attempt failed; some may have succeeded on retry'
                  : 'none'
              }
            />
          </section>

          {pendingFailures.length > 0 && (
            <Panel tone="danger" aria-label="Failing activities">
              <PanelHeader
                leading={
                  <ExclamationTriangleIcon
                    aria-hidden="true"
                    className="h-4 w-4 text-danger-fg"
                  />
                }
                title="Failing activities"
              />
              <ul className="m-0 list-none p-0">
                {pendingFailures.map((a) => (
                  <li
                    key={a.activityName}
                    className="border-b border-line-soft px-5 py-3 last:border-b-0"
                  >
                    <p className="m-0 text-[13px] font-medium">
                      {a.activityName}
                    </p>
                    <p className="m-0 mt-1 whitespace-pre-wrap break-words font-mono text-xs text-danger-fg">
                      {a.lastFailure?.message || ''}
                    </p>
                  </li>
                ))}
              </ul>
            </Panel>
          )}

          <JobRunActivityErrors
            jobRunId={id}
            jobId={jobRun?.jobId ?? ''}
            accountId={accountId}
          />

          <JobRunPreflight
            jobRunId={id}
            jobId={jobRun?.jobId ?? ''}
            accountId={accountId}
            isRunning={isRunning}
          />

          {(tablesTotal > 0 || showLogs) && (
            <div className="flex flex-wrap items-start gap-5">
              {tablesTotal > 0 && (
                <RunTablesPanel
                  tables={tables}
                  waiting={waitingTables}
                  span={eventsSpan}
                  onViewSelectClicked={onViewSelectClicked}
                  className={cn(showLogs ? 'flex-[3_1_560px]' : 'flex-1')}
                />
              )}
              {showLogs && (
                <JobRunLogs
                  accountId={accountId}
                  runId={id}
                  isRunning={isRunning}
                  className="min-w-0 flex-[2_1_380px]"
                />
              )}
            </div>
          )}

          <PiiDetectionJobGuard jobId={jobRun?.jobId ?? ''}>
            <JobRunPiiDetectionTable jobRunId={id} isRunning={isRunning} />
          </PiiDetectionJobGuard>

          <JobRunActivityTable
            jobRunEvents={events}
            onViewSelectClicked={onViewSelectClicked}
            jobStatus={jobRun?.status}
            isRefreshing={isValidating}
          />

          <ViewSelectDialog
            isDialogOpen={isViewSelectDialogOpen}
            setIsDialogOpen={setIsSelectDialogOpen}
            query={activeSelectQuery}
          />
        </>
      )}
    </PageBody>
  );
}

// There is way more here, but this is currently all we care about
interface RunContext {
  input: {
    pooled_sql_raw: {
      query: string;
    };
  };
}

function isValidRunContext(input: unknown): input is RunContext {
  const typedInput = input as Partial<RunContext>;
  return !!typedInput?.input?.pooled_sql_raw?.query;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function parseUint8ArrayToYaml(data: Uint8Array): any {
  try {
    const yamlString = new TextDecoder().decode(data);
    const result = yaml.parse(yamlString);
    return result;
  } catch (error) {
    console.error('Error parsing YAML:', error);
    return null;
  }
}

function buildBenthosRunCtxExternalId(schema: string, table: string): string {
  return `benthosconfig-${schema}.${table}.insert`;
}

interface SelectQuery {
  schema: string;
  table: string;
  select: string;
}

interface ViewSelectDialogProps {
  isDialogOpen: boolean;
  setIsDialogOpen(open: boolean): void;
  query: SelectQuery;
}

function ViewSelectDialog(props: ViewSelectDialogProps): ReactElement {
  const { isDialogOpen, setIsDialogOpen, query } = props;
  const { resolvedTheme } = useTheme();

  const formattedQuery = useMemo(() => {
    // todo: maybe update this to explicitly pass in the driver type so it formats it according to the correct connection
    return formatSql(query.select);
  }, [query.schema, query.table, query.select]);

  return (
    <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
      <DialogContent className="lg:max-w-4xl">
        <DialogHeader>
          <DialogTitle>
            SQL Select Query - {query.schema}.{query.table}
          </DialogTitle>
          <DialogDescription>
            This was the query used to query the source database during the job
            run
          </DialogDescription>
        </DialogHeader>
        <div>
          <Editor
            height="50vh"
            width="100%"
            language="sql"
            value={formattedQuery}
            theme={resolvedTheme === 'dark' ? 'vs-dark' : 'cobalt'}
            options={{
              minimap: { enabled: false },
              readOnly: true,
              wordWrap: 'on',
            }}
          />
        </div>
        <DialogFooter className="md:justify-between">
          <Button
            type="button"
            variant="secondary"
            onClick={() => setIsDialogOpen(false)}
          >
            Close
          </Button>
          <CopyButton
            buttonVariant="default"
            onCopiedText="Success!"
            onHoverText="Copy the SELECT Query"
            textToCopy={query.select}
            buttonText="Copy"
          />
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
