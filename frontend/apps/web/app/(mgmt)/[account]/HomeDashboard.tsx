'use client';
import {
  Panel,
  PanelHeader,
  PageHeader,
  Pill,
  Segmented,
  StatCard,
  StatusDot,
  Tone,
  rowClass,
  toneTextClass,
} from '@/components/ds';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/libs/utils';
import { isPassthrough } from '@/util/mapping-changes';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import {
  createQueryOptions,
  useQuery,
  useTransport,
} from '@connectrpc/connect-query';
import {
  ConnectionDataService,
  ConnectionService,
  GetConnectionSchemaMapResponse,
  Job,
  JobMappingChangeKind,
  JobRun,
  JobService,
  JobStatus,
} from '@husonym/sdk';
import { useQueries } from '@tanstack/react-query';
import Image from 'next/image';
import Link from 'next/link';
import { ReactElement, ReactNode, useEffect, useMemo, useState } from 'react';
import {
  ArrowRight,
  EmptyRow,
  LegendRow,
  LiveBar,
  SegmentBar,
  Sparkline,
  rowLinkFocus,
} from './DashboardParts';
import {
  PERIOD_OPTIONS,
  Period,
  RunOutcome,
  activeRuns,
  categoryLabel,
  coverage,
  engineCounts,
  formatAgo,
  formatIn,
  formatSpan,
  latestRuns,
  periodLabel,
  plural,
  runDuration,
  runOutcome,
  runStats,
  runStatusLabel,
  runWhen,
  sqlSourceConnectionId,
  toDate,
} from './dashboard-data';

interface Props {
  accountId: string;
  accountName: string;
}

const OUTCOME_TONE: Record<RunOutcome, Tone> = {
  succeeded: 'ok',
  failed: 'danger',
  canceled: 'muted',
  running: 'brand',
  pending: 'warn',
  unknown: 'muted',
};

const SCHEMA_STALE_MS = 5 * 60_000;

/** The current time, ticking every `everyMs`, so that "3 min ago" stays true. */
function useNow(everyMs: number): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), everyMs);
    return () => clearInterval(t);
  }, [everyMs]);
  return now;
}

export default function HomeDashboard({
  accountId,
  accountName,
}: Props): ReactElement {
  const [period, setPeriod] = useState<Period>('7d');
  const now = useNow(15_000);
  const transport = useTransport();
  const base = `/${accountName}`;

  const { data: jobsData, isLoading: jobsLoading } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: runsData, isLoading: runsLoading } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'accountId', value: accountId } },
    {
      enabled: !!accountId,
      // Faster while something runs, so the "In progress" panel empties on its own.
      refetchInterval: (q) =>
        (q.state.data?.jobRuns ?? []).some((r) => {
          const o = runOutcome(r.status);
          return o === 'running' || o === 'pending';
        })
          ? 10_000
          : 60_000,
    }
  );
  const { data: connsData, isLoading: connsLoading } = useQuery(
    ConnectionService.method.getConnections,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: pendingData } = useQuery(
    JobService.method.getPendingMappingChanges,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: statusesData } = useQuery(
    JobService.method.getJobStatuses,
    { accountId },
    { enabled: !!accountId }
  );

  const jobs = useMemo(() => jobsData?.jobs ?? [], [jobsData?.jobs]);
  const runs = useMemo(() => runsData?.jobRuns ?? [], [runsData?.jobRuns]);
  const pending = useMemo(
    () => pendingData?.changes ?? [],
    [pendingData?.changes]
  );

  const jobsById = useMemo(() => new Map(jobs.map((j) => [j.id, j])), [jobs]);
  const connNames = useMemo(
    () => new Map((connsData?.connections ?? []).map((c) => [c.id, c.name])),
    [connsData?.connections]
  );

  // A job with a schedule that is not paused: the ones the next run can come from.
  const pausedJobIds = useMemo(
    () =>
      new Set(
        (statusesData?.statuses ?? [])
          .filter((s) => s.status !== JobStatus.ENABLED)
          .map((s) => s.jobId)
      ),
    [statusesData?.statuses]
  );
  const scheduledJobs = jobs.filter((j) => !!j.cronSchedule);
  const activeScheduled = statusesData
    ? scheduledJobs.filter((j) => !pausedJobIds.has(j.id))
    : [];

  const nextRunQueries = useQueries({
    queries: activeScheduled.map((j) => ({
      ...createQueryOptions(
        JobService.method.getJobNextRuns,
        { jobId: j.id },
        { transport }
      ),
      staleTime: 60_000,
    })),
  });
  const nextRun = useMemo(() => {
    let best: Date | undefined;
    for (const q of nextRunQueries) {
      for (const ts of q.data?.nextRuns?.nextRunTimes ?? []) {
        const d = timestampDate(ts);
        if (d.getTime() > now.getTime() && (!best || d < best)) {
          best = d;
        }
      }
    }
    return best;
  }, [nextRunQueries, now]);

  // The schema of each database a job reads from: it says which columns are personal.
  const sourceIds = useMemo(
    () =>
      [
        ...new Set(
          jobs
            .map((j) => sqlSourceConnectionId(j.source))
            .filter((id): id is string => !!id)
        ),
      ].sort(),
    [jobs]
  );
  const schemaQueries = useQueries({
    queries: sourceIds.map((id) => ({
      ...createQueryOptions(
        ConnectionDataService.method.getConnectionSchemaMap,
        { connectionId: id },
        { transport }
      ),
      staleTime: SCHEMA_STALE_MS,
      retry: false,
      refetchOnWindowFocus: false,
    })),
  });
  const schemasLoading = schemaQueries.some((q) => q.isLoading);
  const unreachable = sourceIds.filter((_, i) => schemaQueries[i]?.isError);
  const schemaDataKey = schemaQueries.map((q) => q.dataUpdatedAt).join(',');
  const cov = useMemo(() => {
    const schemas = new Map<string, GetConnectionSchemaMapResponse>();
    sourceIds.forEach((id, i) => {
      const data = schemaQueries[i]?.data;
      if (data) {
        schemas.set(id, data);
      }
    });
    return coverage(jobs, schemas);
    // schemaQueries is a new array every render; its data changes with dataUpdatedAt.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobs, sourceIds, schemaDataKey]);

  const stats = useMemo(() => runStats(runs, period, now), [runs, period, now]);
  const live = useMemo(() => activeRuns(runs), [runs]);
  const latest = useMemo(
    () => latestRuns(runs, period, 6, now),
    [runs, period, now]
  );

  const review = useMemo(() => {
    const byJob = new Map<string, { count: number; inClear: number }>();
    let added = 0;
    let removed = 0;
    let typeChanged = 0;
    let inClear = 0;
    for (const c of pending) {
      const e = byJob.get(c.jobId) ?? { count: 0, inClear: 0 };
      e.count++;
      const clear =
        !!c.piiCategory &&
        c.kind === JobMappingChangeKind.ADDED &&
        isPassthrough(c);
      if (clear) {
        e.inClear++;
        inClear++;
      }
      byJob.set(c.jobId, e);
      if (c.kind === JobMappingChangeKind.ADDED) added++;
      else if (c.kind === JobMappingChangeKind.REMOVED) removed++;
      else if (c.kind === JobMappingChangeKind.TYPE_CHANGED) typeChanged++;
    }
    // The job shipping the most personal data in clear first: that is the one to open.
    const firstJobId = [...byJob.entries()].sort(
      (a, b) => b[1].inClear - a[1].inClear || b[1].count - a[1].count
    )[0]?.[0];
    return {
      total: pending.length,
      inClear,
      jobs: byJob.size,
      firstJobId,
      added,
      removed,
      typeChanged,
    };
  }, [pending]);

  // Personal columns in clear: those a run just added first (they need a decision now),
  // then those the current mappings copy as they are.
  const clearList = useMemo(() => {
    const items: {
      key: string;
      column: string;
      sub: string;
      kind: string;
      href: string;
    }[] = [];
    const seen = new Set<string>();
    for (const c of [...pending].reverse()) {
      if (
        !c.piiCategory ||
        c.kind !== JobMappingChangeKind.ADDED ||
        !isPassthrough(c)
      ) {
        continue;
      }
      const key = `${c.jobId}|${c.column?.schema}.${c.column?.table}.${c.column?.column}`;
      if (seen.has(key)) continue;
      seen.add(key);
      const added = toDate(c.createdAt);
      items.push({
        key,
        column: `${c.column?.table}.${c.column?.column}`,
        sub: `${jobsById.get(c.jobId)?.name ?? c.jobId} · added ${formatAgo(added, now)}`,
        kind: categoryLabel(c.piiCategory),
        href: `${base}/jobs/${c.jobId}/review`,
      });
    }
    for (const c of cov.inClearColumns) {
      const key = `${c.jobId}|${c.schema}.${c.table}.${c.column}`;
      if (seen.has(key)) continue;
      seen.add(key);
      items.push({
        key,
        column: `${c.table}.${c.column}`,
        sub: c.jobName,
        kind: categoryLabel(c.category),
        href: `${base}/jobs/${c.jobId}/source`,
      });
    }
    return items;
  }, [pending, cov.inClearColumns, jobsById, base, now]);

  const isLoading = jobsLoading || runsLoading || connsLoading;
  if (isLoading) {
    return <DashboardSkeleton />;
  }

  const running = live.filter((r) => runOutcome(r.status) === 'running');
  const waiting = live.length - running.length;
  const subtitle = [
    running.length > 0
      ? `${plural(running.length, 'run')} in progress`
      : 'No run in progress',
    waiting > 0 ? `${waiting} waiting to start` : null,
    nextRun
      ? `next scheduled run ${formatIn(nextRun, now)}`
      : scheduledJobs.length > 0 && statusesData && activeScheduled.length === 0
        ? 'every schedule is paused'
        : null,
  ]
    .filter(Boolean)
    .join(' · ');

  const engines = engineCounts(jobs);
  const personal = cov.anonymized + cov.inClear;
  const protectedPct =
    personal > 0 ? Math.round((cov.anonymized / personal) * 1000) / 10 : null;

  return (
    <>
      <PageHeader
        title="Overview"
        description={subtitle}
        actions={
          <Segmented
            ariaLabel="Period"
            options={PERIOD_OPTIONS}
            value={period}
            onChange={setPeriod}
          />
        }
      />

      <section
        aria-label="Key figures"
        className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-4"
      >
        <StatCard
          label="Finished runs"
          value={stats.finished.toLocaleString('en-US')}
          note={
            stats.finished === 0
              ? `None in the ${periodLabel(period)}`
              : [
                  `${stats.succeeded} succeeded`,
                  `${stats.failed} failed`,
                  stats.canceled > 0 ? `${stats.canceled} canceled` : null,
                ]
                  .filter(Boolean)
                  .join(' · ')
          }
          noteTone={
            stats.finished === 0 ? 'muted' : stats.failed > 0 ? 'warn' : 'ok'
          }
          chart={
            <>
              <Sparkline values={stats.series} />
              <span className="sr-only">
                {`Finished runs over the ${periodLabel(period)}, oldest first: ${stats.series.join(', ')}.`}
              </span>
            </>
          }
          footer={
            stats.successRate !== null && (
              <span className="text-xs text-muted-foreground">
                {stats.successRate}% success rate
              </span>
            )
          }
        />

        <StatCard
          label="Jobs"
          value={jobs.length}
          note={
            jobs.length === 0
              ? 'No job yet'
              : `${plural(activeScheduled.length, 'active schedule')} · ${plural(connNames.size, 'connection')}`
          }
          chart={
            engines.length > 0 && (
              <div className="flex flex-col gap-2">
                <SegmentBar
                  label={engines.map((e) => `${e.name}: ${e.count}`).join(', ')}
                  segments={engines.map((e) => ({
                    key: e.name,
                    value: e.count,
                    className: engineSwatch(e.name),
                  }))}
                />
                <span className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  {engines.map((e) => (
                    <span key={e.name} className="flex items-center gap-1.5">
                      <span
                        className={cn(
                          'size-2 rounded-[2px]',
                          engineSwatch(e.name)
                        )}
                        aria-hidden="true"
                      />
                      {e.name} {e.count}
                    </span>
                  ))}
                </span>
              </div>
            )
          }
        />

        <StatCard
          label="Personal columns protected"
          value={
            schemasLoading && cov.jobsCounted === 0 ? (
              <span
                className="inline-block h-8 w-24 animate-pulse rounded-md bg-surface-2 align-middle"
                aria-label="Loading"
              />
            ) : protectedPct === null ? (
              '—'
            ) : (
              `${protectedPct}%`
            )
          }
          note={
            schemasLoading && cov.jobsCounted === 0
              ? 'Reading source schemas…'
              : sourceIds.length === 0
                ? 'No job reads from a database'
                : cov.jobsCounted === 0
                  ? 'Source schema unreachable'
                  : personal === 0
                    ? 'No personal column detected'
                    : `${cov.anonymized.toLocaleString('en-US')} of ${plural(personal, 'detected column')}`
          }
          noteTone={
            cov.jobsCounted === 0 && !schemasLoading && sourceIds.length > 0
              ? 'warn'
              : 'muted'
          }
          chart={
            personal > 0 && (
              <SegmentBar
                label={`${protectedPct}% of personal columns protected`}
                segments={[
                  { key: 'ok', value: cov.anonymized, className: 'bg-primary' },
                  { key: 'clear', value: cov.inClear, className: 'bg-danger' },
                ]}
              />
            )
          }
        />

        <StatCard
          label="To review"
          tone={review.total > 0 ? 'danger' : 'default'}
          value={review.total}
          note={
            review.total === 0
              ? 'Nothing waiting for a decision'
              : review.inClear > 0
                ? `${plural(review.inClear, 'new sensitive column')} in clear`
                : `${plural(review.total, 'mapping change')} in ${plural(review.jobs, 'job')}`
          }
          noteTone={review.inClear > 0 ? 'danger' : 'muted'}
          footer={
            review.firstJobId && (
              <Link
                href={`${base}/jobs/${review.firstJobId}/review`}
                className="flex w-fit items-center gap-1.5 rounded-sm text-[13px] font-semibold text-link hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                Open the review
                <ArrowRight />
              </Link>
            )
          }
        />
      </section>

      {jobs.length === 0 ? (
        <NoJobs base={base} />
      ) : (
        <div className="flex flex-wrap items-start gap-5">
          <section
            aria-label="Runs"
            className="flex min-w-0 flex-[3_1_520px] flex-col gap-5"
          >
            <Panel>
              <PanelHeader
                leading={
                  <StatusDot
                    tone={live.length > 0 ? 'brand' : 'muted'}
                    pulse={live.length > 0}
                  />
                }
                title="In progress"
                actions={
                  <Link
                    href={`${base}/runs`}
                    className="text-link hover:underline"
                  >
                    See all
                  </Link>
                }
              />
              {live.length === 0 ? (
                <EmptyRow>
                  No run in progress.{' '}
                  {nextRun
                    ? `The next scheduled run starts ${formatIn(nextRun, now)}.`
                    : 'Start one from a job.'}
                </EmptyRow>
              ) : (
                live.map((r) => (
                  <LiveRunRow
                    key={r.id}
                    run={r}
                    job={jobsById.get(r.jobId)}
                    connNames={connNames}
                    href={`${base}/runs/${r.id}`}
                    now={now}
                  />
                ))
              )}
            </Panel>

            <Panel>
              <PanelHeader
                title="Latest runs"
                actions={
                  <Link
                    href={`${base}/runs`}
                    className="text-link hover:underline"
                  >
                    All runs
                  </Link>
                }
              />
              {latest.length === 0 ? (
                <EmptyRow>
                  No run finished in the {periodLabel(period)}.
                </EmptyRow>
              ) : (
                <div className="overflow-x-auto">
                  {latest.map((r) => (
                    <LatestRunRow
                      key={r.id}
                      run={r}
                      jobName={jobsById.get(r.jobId)?.name ?? r.jobId}
                      href={`${base}/runs/${r.id}`}
                      now={now}
                    />
                  ))}
                </div>
              )}
            </Panel>
          </section>

          <section
            aria-label="Personal data"
            className="flex min-w-0 flex-[2_1_340px] flex-col gap-5"
          >
            <CoveragePanel
              loading={schemasLoading && cov.jobsCounted === 0}
              hasSources={sourceIds.length > 0}
              cov={cov}
              unreachable={unreachable.map((id) => connNames.get(id) ?? id)}
            />

            {clearList.length > 0 && (
              <Panel>
                <PanelHeader title="Sensitive columns in clear" />
                <ul className="m-0 list-none p-0">
                  {clearList.slice(0, 6).map((c) => (
                    <li key={c.key} className={rowClass}>
                      <Link
                        href={c.href}
                        className={cn(
                          'flex items-center gap-3 px-5 py-3',
                          rowLinkFocus
                        )}
                      >
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate font-mono text-[13px]">
                            {c.column}
                          </span>
                          <span className="truncate text-xs text-muted-foreground">
                            {c.sub}
                          </span>
                        </span>
                        <Pill tone="danger">{c.kind}</Pill>
                      </Link>
                    </li>
                  ))}
                </ul>
                {clearList.length > 6 && (
                  <p className="m-0 border-t border-line-soft px-5 py-3 text-xs text-muted-foreground">
                    And {plural(clearList.length - 6, 'other column')}, in the
                    mapping of each job.
                  </p>
                )}
              </Panel>
            )}

            {review.total > 0 && review.firstJobId && (
              <Panel className="flex items-center gap-4 p-5">
                <Image
                  src="/images/mascot.png"
                  alt=""
                  width={64}
                  height={64}
                  className="h-auto w-16 shrink-0"
                />
                <div className="flex min-w-0 flex-col gap-1">
                  <span className="font-semibold">
                    Huso spotted {plural(review.total, 'change')} in your
                    sources
                  </span>
                  <span className="text-[13px] text-muted-foreground">
                    {schemaChangeSummary(review)} across{' '}
                    {plural(review.jobs, 'job')}, found by their runs. Each one
                    waits for your review.
                  </span>
                </div>
              </Panel>
            )}
          </section>
        </div>
      )}
    </>
  );
}

function engineSwatch(name: string): string {
  switch (name) {
    case 'Athanor':
      return 'bg-primary';
    case 'Benthos':
      return 'bg-warn';
    default:
      return 'bg-quiet';
  }
}

function schemaChangeSummary(r: {
  added: number;
  removed: number;
  typeChanged: number;
}): string {
  const parts = [
    r.added > 0 ? plural(r.added, 'new column') : null,
    r.removed > 0 ? `${plural(r.removed, 'column')} removed` : null,
    r.typeChanged > 0 ? plural(r.typeChanged, 'type change') : null,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(', ') : 'Mapping changes';
}

function LiveRunRow(props: {
  run: JobRun;
  job?: Job;
  connNames: Map<string, string>;
  href: string;
  now: Date;
}): ReactElement {
  const { run, job, connNames, href, now } = props;
  const isPending = runOutcome(run.status) === 'pending';
  const srcId = job ? sourceConnectionId(job) : undefined;
  const src = srcId ? connNames.get(srcId) : undefined;
  const dests = (job?.destinations ?? [])
    .map((d) => connNames.get(d.connectionId))
    .filter((n): n is string => !!n);
  const route =
    src || dests.length > 0
      ? `${src ?? 'generated'} → ${dests.join(', ') || '–'}`
      : undefined;
  const started = toDate(run.startedAt);
  const name = job?.name ?? run.jobId;
  return (
    <Link
      href={href}
      className={cn(rowClass, 'flex flex-col gap-3 px-5 py-4', rowLinkFocus)}
    >
      <div className="flex flex-wrap items-baseline gap-x-2.5 gap-y-1">
        <span className="font-semibold">{name}</span>
        {route && (
          <span className="font-mono text-xs text-muted-foreground">
            {route}
          </span>
        )}
        <span className="ml-auto text-[13px] text-muted-foreground">
          {isPending
            ? 'Waiting to start'
            : started
              ? `Started ${formatAgo(started, now)}`
              : 'Running'}
        </span>
      </div>
      <LiveBar
        label={`${name}: ${isPending ? 'waiting to start' : 'running'}`}
      />
      {!isPending && (
        <div className="text-[13px] text-muted-foreground">
          Running for{' '}
          <strong className="font-semibold text-foreground">
            {formatSpan(runDuration(run, now))}
          </strong>
        </div>
      )}
    </Link>
  );
}

// The connection a job reads from, whatever its kind, for the route of a running run.
function sourceConnectionId(job: Job): string | undefined {
  const c = job.source?.options?.config;
  switch (c?.case) {
    case 'postgres':
    case 'mysql':
    case 'mssql':
    case 'awsS3':
    case 'mongodb':
    case 'dynamodb':
      return c.value.connectionId;
    case 'generate':
      return c.value.fkSourceConnectionId;
    case 'aiGenerate':
      return c.value.aiConnectionId;
    default:
      return undefined;
  }
}

function LatestRunRow(props: {
  run: JobRun;
  jobName: string;
  href: string;
  now: Date;
}): ReactElement {
  const { run, jobName, href, now } = props;
  const tone = OUTCOME_TONE[runOutcome(run.status)];
  const label = runStatusLabel(run.status);
  return (
    <Link
      href={href}
      className={cn(
        rowClass,
        'grid min-w-[520px] grid-cols-[18px_minmax(160px,1.6fr)_minmax(90px,0.8fr)_minmax(90px,0.8fr)_minmax(80px,0.6fr)] items-center gap-3 px-5 py-3',
        rowLinkFocus
      )}
    >
      <StatusDot tone={tone} label={label} />
      <span className="truncate font-medium">{jobName}</span>
      <span className={cn('text-[13px]', toneTextClass(tone))}>{label}</span>
      <span className="font-mono text-xs text-muted-foreground">
        {formatSpan(runDuration(run, now))}
      </span>
      <span className="text-right text-[13px] text-muted-foreground">
        {formatAgo(runWhen(run), now)}
      </span>
    </Link>
  );
}

function CoveragePanel(props: {
  loading: boolean;
  hasSources: boolean;
  cov: ReturnType<typeof coverage>;
  unreachable: string[];
}): ReactElement {
  const { loading, hasSources, cov, unreachable } = props;
  let body: ReactNode;
  if (!hasSources) {
    body = (
      <p className="m-0 text-sm text-muted-foreground">
        None of your jobs reads from a database, so there is no column to
        classify.
      </p>
    );
  } else if (loading) {
    body = (
      <div className="flex flex-col gap-3" aria-busy="true">
        <Skeleton className="h-3.5 w-full rounded-full" />
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    );
  } else if (cov.total === 0) {
    body = (
      <p className="m-0 text-sm text-muted-foreground">
        No mapped column could be matched with its source schema.
      </p>
    );
  } else {
    body = (
      <>
        <SegmentBar
          className="h-3.5"
          label={`Breakdown of ${plural(cov.total, 'mapped column')}`}
          segments={[
            { key: 'anon', value: cov.anonymized, className: 'bg-primary' },
            { key: 'kept', value: cov.kept, className: 'bg-quiet' },
            { key: 'confirm', value: cov.toConfirm, className: 'bg-warn' },
            { key: 'clear', value: cov.inClear, className: 'bg-danger' },
          ]}
        />
        <ul className="m-0 flex list-none flex-col gap-2.5 p-0 text-[13px]">
          <LegendRow
            swatch="bg-primary"
            label="Personal, anonymized"
            value={cov.anonymized.toLocaleString('en-US')}
          />
          <LegendRow
            swatch="bg-quiet"
            label="Not personal, kept"
            value={cov.kept.toLocaleString('en-US')}
          />
          <LegendRow
            swatch="bg-warn"
            label="To confirm"
            value={cov.toConfirm.toLocaleString('en-US')}
          />
          <LegendRow
            swatch="bg-danger"
            label="Personal, in clear"
            value={cov.inClear.toLocaleString('en-US')}
            valueClassName={cov.inClear > 0 ? 'text-danger-fg' : undefined}
          />
        </ul>
        <p className="m-0 text-xs text-muted-foreground">
          The mapped columns of {plural(cov.jobsCounted, 'job')}, classified
          from their names and types in the source schema.
        </p>
      </>
    );
  }
  return (
    <Panel className="flex flex-col gap-4 p-5">
      <h2 className="m-0 font-display text-base font-semibold">
        Personal data coverage
      </h2>
      {body}
      {!loading && unreachable.length > 0 && (
        <p className="m-0 text-xs text-warn-fg">
          Could not read the schema of {unreachable.join(', ')}: the jobs that
          read from {unreachable.length === 1 ? 'it' : 'them'} are not counted.
        </p>
      )}
    </Panel>
  );
}

function NoJobs(props: { base: string }): ReactElement {
  return (
    <Panel className="flex flex-col items-start gap-3 p-8">
      <h2 className="m-0 font-display text-lg font-semibold">No job yet</h2>
      <p className="m-0 max-w-prose text-sm text-muted-foreground">
        A job copies a source database to a destination, anonymizing its
        personal columns on the way. Its runs, and the coverage of personal
        data, will show up here.
      </p>
      <Button asChild>
        <Link href={`${props.base}/new/job`}>New job</Link>
      </Button>
    </Panel>
  );
}

function DashboardSkeleton(): ReactElement {
  return (
    <div className="flex flex-col gap-7" aria-busy="true">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-9 w-56" />
        <Skeleton className="h-5 w-80" />
      </div>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-36 w-full rounded-2xl" />
        ))}
      </div>
      <div className="flex flex-wrap gap-5">
        <Skeleton className="h-80 min-w-0 flex-[3_1_520px] rounded-2xl" />
        <Skeleton className="h-80 min-w-0 flex-[2_1_340px] rounded-2xl" />
      </div>
    </div>
  );
}
