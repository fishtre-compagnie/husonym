'use client';
import {
  PageHeader,
  Panel,
  PanelHeader,
  Segmented,
  StatusDot,
  rowClass,
} from '@/components/ds';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useDateFnsLocale } from '@/i18n/date-fns';
import {
  browserTimeZone,
  periodRange,
  previousRange,
} from '@/libs/usage/period';
import { cn } from '@/libs/utils';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import {
  createQueryOptions,
  useQuery,
  useTransport,
} from '@connectrpc/connect-query';
import {
  ConnectionService,
  GetAccountUsageResponse,
  Job,
  JobKind,
  JobRun,
  JobService,
  JobStatus,
  UsageService,
} from '@husonym/sdk';
import { useQueries } from '@tanstack/react-query';
import { useFormatter, useTranslations } from 'next-intl';
import Link from 'next/link';
import { ReactElement, useEffect, useMemo, useState } from 'react';
import { EmptyRow, LiveBar, rowLinkFocus } from './DashboardParts';
import {
  dailyOutcomes,
  percentChange,
  successRate,
  weekHeatmap,
} from './dashboard-analytics';
import {
  PERIODS_ORDER,
  Period,
  RelativeWords,
  activeRuns,
  formatAgo,
  formatIn,
  formatSpan,
  periodDays,
  runDuration,
  runOutcome,
  runStatusKey,
  toDate,
} from './dashboard-data';
import {
  ActivityChart,
  AreaSpark,
  JobRow,
  JobsTable,
  KpiTile,
  Ring,
  RunHeatmap,
} from './HomeCharts';

interface Props {
  accountId: string;
  accountName: string;
}

// How many scheduled runs the "Coming up" list shows.
const UPCOMING = 4;

const USAGE_PERIOD = {
  '7d': 'last-7-days',
  '30d': 'last-30-days',
  '90d': 'last-90-days',
} as const;

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
  const t = useTranslations('dashboard.home');
  const format = useFormatter();
  const dateLocale = useDateFnsLocale();
  const words: RelativeWords = useMemo(
    () => ({ locale: dateLocale, justNow: t('justNow') }),
    [dateLocale, t]
  );
  const [period, setPeriod] = useState<Period>('30d');
  const now = useNow(15_000);
  const transport = useTransport();
  const base = `/${accountName}`;
  const days = periodDays(period);

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
      // Faster while something runs, so the "Live" panel empties on its own.
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
  const { data: statusesData } = useQuery(
    JobService.method.getJobStatuses,
    { accountId },
    { enabled: !!accountId }
  );

  // The usage counters of the period and of the one before it, to say how things moved.
  // The day only changes the range, so the ranges are read from the day, not the minute.
  const timeZone = useMemo(() => browserTimeZone(), []);
  const today = now.toDateString();
  const range = useMemo(
    () => periodRange(USAGE_PERIOD[period], new Date(), timeZone),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [period, timeZone, today]
  );
  const before = useMemo(() => previousRange(range), [range]);
  const { data: usage, error: usageError } = useQuery(
    UsageService.method.getAccountUsage,
    { accountId, fromDay: range.fromDay, toDay: range.toDay, timeZone },
    { enabled: !!accountId, retry: false }
  );
  const { data: usageBefore } = useQuery(
    UsageService.method.getAccountUsage,
    { accountId, fromDay: before.fromDay, toDay: before.toDay, timeZone },
    { enabled: !!accountId, retry: false }
  );

  const jobs = useMemo(() => jobsData?.jobs ?? [], [jobsData?.jobs]);
  const runs = useMemo(() => runsData?.jobRuns ?? [], [runsData?.jobRuns]);
  const jobsById = useMemo(() => new Map(jobs.map((j) => [j.id, j])), [jobs]);
  const connNames = useMemo(
    () => new Map((connsData?.connections ?? []).map((c) => [c.id, c.name])),
    [connsData?.connections]
  );

  // A job with a schedule that is not paused: the ones the next runs come from.
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
  const nextRunsKey = nextRunQueries.map((q) => q.dataUpdatedAt).join(',');
  const upcoming = useMemo(() => {
    const out: { job: Job; at: Date }[] = [];
    nextRunQueries.forEach((q, i) => {
      const job = activeScheduled[i];
      const at = (q.data?.nextRuns?.nextRunTimes ?? [])
        .map((ts) => timestampDate(ts))
        .find((d) => d.getTime() > now.getTime());
      if (job && at) {
        out.push({ job, at });
      }
    });
    return out.sort((a, b) => a.at.getTime() - b.at.getTime());
    // nextRunQueries is a new array every render; its data changes with nextRunsKey.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nextRunsKey, now]);
  const nextRun = upcoming[0]?.at;

  const live = useMemo(() => activeRuns(runs), [runs]);
  const outcomes = useMemo(
    () => dailyOutcomes(runs, days, now),
    [runs, days, now]
  );
  const heat = useMemo(() => weekHeatmap(runs, days, now), [runs, days, now]);

  // The rows read on each day of the chart, matched on the calendar day.
  const rowsByDay = useMemo(() => {
    const byKey = new Map(
      (usage?.days ?? []).map((d) => [
        `${d.day?.year}-${d.day?.month}-${d.day?.day}`,
        Number(d.rowsRead),
      ])
    );
    return outcomes.map(
      (o) =>
        byKey.get(
          `${o.day.getFullYear()}-${o.day.getMonth() + 1}-${o.day.getDate()}`
        ) ?? 0
    );
  }, [usage?.days, outcomes]);

  const jobRows = useMemo(
    () => toJobRows(usage, runs, base, t),
    [usage, runs, base, t]
  );

  const isLoading = jobsLoading || runsLoading || connsLoading;
  if (isLoading) {
    return <DashboardSkeleton />;
  }

  const running = live.filter((r) => runOutcome(r.status) === 'running');
  const waiting = live.length - running.length;
  const subtitle = [
    running.length > 0
      ? t('subtitle.running', { count: running.length })
      : t('subtitle.noRunning'),
    waiting > 0 ? t('subtitle.waiting', { count: waiting }) : null,
    nextRun
      ? t('subtitle.nextRun', { when: formatIn(nextRun, now, dateLocale) })
      : scheduledJobs.length > 0 && statusesData && activeScheduled.length === 0
        ? t('subtitle.allPaused')
        : null,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <>
      <PageHeader
        title={t('title')}
        description={
          <span className="flex items-center gap-2">
            <StatusDot
              tone={running.length > 0 ? 'live' : 'muted'}
              pulse={running.length > 0}
            />
            {subtitle}
          </span>
        }
        actions={
          <Segmented
            ariaLabel={t('periodAria')}
            options={PERIODS_ORDER.map((p) => ({
              value: p,
              label: t(`periods.${p}`),
            }))}
            value={period}
            onChange={setPeriod}
          />
        }
      />

      {jobs.length === 0 ? (
        <NoJobs base={base} />
      ) : (
        <>
          <KeyFigures
            usage={usage}
            usageBefore={usageBefore}
            unavailable={!!usageError}
            days={days}
          />

          <div className="flex flex-wrap items-stretch gap-3.5">
            <ActivityChart outcomes={outcomes} rows={rowsByDay} />
            <Panel className="flex min-w-0 flex-[1_1_320px] flex-col">
              <PanelHeader
                leading={
                  <StatusDot
                    tone={live.length > 0 ? 'live' : 'muted'}
                    pulse={live.length > 0}
                  />
                }
                title={t('liveSection.title')}
                actions={
                  <Link
                    href={`${base}/runs`}
                    className="text-link hover:underline"
                  >
                    {t('liveSection.seeAll')}
                  </Link>
                }
              />
              {live.length === 0 ? (
                <EmptyRow>{t('liveSection.empty')}</EmptyRow>
              ) : (
                live.map((r) => (
                  <LiveRunRow
                    key={r.id}
                    run={r}
                    job={jobsById.get(r.jobId)}
                    connNames={connNames}
                    href={`${base}/runs/${r.id}`}
                    now={now}
                    words={words}
                  />
                ))
              )}
              <div className="flex flex-col px-5 pb-3 pt-4">
                <span className="mb-1 text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">
                  {t('liveSection.upcoming')}
                </span>
                {upcoming.length === 0 ? (
                  <span className="py-2 text-[13px] text-muted-foreground">
                    {t('liveSection.noUpcoming')}
                  </span>
                ) : (
                  upcoming.slice(0, UPCOMING).map((u) => (
                    <Link
                      key={u.job.id}
                      href={`${base}/jobs/${u.job.id}`}
                      className={cn(
                        'flex items-center gap-2.5 border-b border-line-soft py-[7px] last:border-b-0',
                        rowLinkFocus
                      )}
                    >
                      <span className="w-[86px] shrink-0 font-mono text-xs text-link">
                        {format.dateTime(u.at, {
                          weekday:
                            u.at.toDateString() === now.toDateString()
                              ? undefined
                              : 'short',
                          hour: '2-digit',
                          minute: '2-digit',
                        })}
                      </span>
                      <span className="min-w-0 grow truncate text-[13px]">
                        {u.job.name}
                      </span>
                      <span className="shrink-0 text-[11.5px] text-muted-foreground">
                        {formatIn(u.at, now, dateLocale)}
                      </span>
                    </Link>
                  ))
                )}
              </div>
            </Panel>
          </div>

          {!usageError && <JobsTable rows={jobRows} />}

          <RunHeatmap
            slots={heat}
            days={days}
            jobName={(id) => jobsById.get(id)?.name ?? id}
            runsHref={`${base}/runs`}
          />
        </>
      )}
    </>
  );
}

/** The four key figures of the period, each beside its change since the period before. */
function KeyFigures(props: {
  usage?: GetAccountUsageResponse;
  usageBefore?: GetAccountUsageResponse;
  unavailable: boolean;
  days: number;
}): ReactElement {
  const { usage, usageBefore, unavailable, days } = props;
  const t = useTranslations('dashboard.home.kpi');
  const format = useFormatter();

  if (unavailable) {
    return (
      <Panel className="px-5 py-4 text-[13px] text-warn-fg">
        {t('unavailable')}
      </Panel>
    );
  }
  if (!usage) {
    return (
      <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3.5">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[148px] w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  const cur = totals(usage);
  const prev = usageBefore ? totals(usageBefore) : undefined;
  const pct = (
    a: number,
    b?: number
  ): { text: string; up: boolean } | undefined => {
    const v = b === undefined ? undefined : percentChange(a, b);
    return v === undefined
      ? undefined
      : { text: t('deltaPercent', { value: v, days }), up: v >= 0 };
  };
  const rate = successRate(cur.runs, cur.completed, cur.canceled);
  const prevRate = prev
    ? successRate(prev.runs, prev.completed, prev.canceled)
    : undefined;
  const ratePts =
    rate !== undefined && prevRate !== undefined
      ? Math.round((rate - prevRate) * 10) / 10
      : undefined;
  const runsByDay = usage.days.map((d) => Number(d.runs));
  const rowsByDay = usage.days.map((d) => Number(d.rowsRead));
  const seconds =
    usage.durationTotalSeconds !== undefined
      ? Number(usage.durationTotalSeconds)
      : undefined;
  const prevSeconds =
    usageBefore?.durationTotalSeconds !== undefined
      ? Number(usageBefore.durationTotalSeconds)
      : undefined;

  return (
    <section
      aria-label={t('finished')}
      className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3.5"
    >
      <KpiTile
        label={t('finished')}
        value={format.number(cur.runs)}
        delta={pct(cur.runs, prev?.runs)}
        note={
          cur.runs === 0
            ? t('finishedNone')
            : t('finishedNote', {
                succeeded: format.number(cur.completed),
                failed: format.number(cur.runs - cur.completed - cur.canceled),
                canceled: format.number(cur.canceled),
              })
        }
        chart={
          <>
            <AreaSpark values={runsByDay} />
            <span className="sr-only">
              {t('series', { series: runsByDay.join(', ') })}
            </span>
          </>
        }
      />
      <KpiTile
        label={t('rate')}
        value={rate === undefined ? '—' : `${format.number(rate)} %`}
        delta={
          ratePts === undefined
            ? undefined
            : {
                text: t('deltaPoints', { value: ratePts, days }),
                up: ratePts >= 0,
              }
        }
        note={t('rateNote')}
        chart={rate !== undefined && <Ring percent={rate} />}
      />
      <KpiTile
        label={t('rows')}
        value={format.number(cur.rows, { notation: 'compact' })}
        delta={pct(cur.rows, prev?.rows)}
        note={t('rowsNote', {
          discarded: format.number(cur.discarded, { notation: 'compact' }),
        })}
        chart={
          <>
            <AreaSpark values={rowsByDay} />
            <span className="sr-only">
              {t('series', { series: rowsByDay.join(', ') })}
            </span>
          </>
        }
      />
      <KpiTile
        label={t('time')}
        value={seconds === undefined ? '—' : formatSpan(seconds * 1000)}
        delta={seconds === undefined ? undefined : pct(seconds, prevSeconds)}
        note={
          seconds === undefined || cur.runs === 0
            ? t('timeNone')
            : t('timeNote', {
                duration: formatSpan((seconds * 1000) / cur.runs),
              })
        }
      />
    </section>
  );
}

function totals(u: GetAccountUsageResponse): {
  runs: number;
  completed: number;
  canceled: number;
  rows: number;
  discarded: number;
} {
  return {
    runs: Number(u.totals?.runs ?? 0),
    completed: Number(u.totals?.runsCompleted ?? 0),
    canceled: Number(u.totals?.runsCanceled ?? 0),
    rows: Number(u.totals?.rowsRead ?? 0),
    discarded: Number(u.totals?.rowsDiscarded ?? 0),
  };
}

// The jobs of the period as the table lists them: the usage counters, and the last run.
function toJobRows(
  usage: GetAccountUsageResponse | undefined,
  runs: JobRun[],
  base: string,
  t: ReturnType<typeof useTranslations<'dashboard.home'>>
): JobRow[] {
  const lastByJob = new Map<string, JobRun>();
  for (const r of runs) {
    const o = runOutcome(r.status);
    if (o !== 'succeeded' && o !== 'failed' && o !== 'canceled') continue;
    const at = toDate(r.completedAt) ?? toDate(r.startedAt);
    const prev = lastByJob.get(r.jobId);
    const prevAt = prev
      ? (toDate(prev.completedAt) ?? toDate(prev.startedAt))
      : undefined;
    if (at && (!prevAt || at > prevAt)) {
      lastByJob.set(r.jobId, r);
    }
  }
  return (usage?.jobs ?? []).map((j) => {
    const last = lastByJob.get(j.jobId);
    const outcome = last ? runOutcome(last.status) : undefined;
    const runsN = Number(j.totals?.runs ?? 0);
    return {
      jobId: j.jobId,
      name: j.jobName,
      kind: t(`kinds.${kindKey(j.kind)}`),
      runs: runsN,
      rate: successRate(
        runsN,
        Number(j.totals?.runsCompleted ?? 0),
        Number(j.totals?.runsCanceled ?? 0)
      ),
      rows: Number(j.totals?.rowsRead ?? 0),
      medianSeconds:
        j.durationMedianSeconds !== undefined
          ? Number(j.durationMedianSeconds)
          : undefined,
      last:
        last &&
        (outcome === 'succeeded' ||
          outcome === 'failed' ||
          outcome === 'canceled')
          ? { outcome, label: t(`status.${runStatusKey(last.status)}`) }
          : undefined,
      href: `${base}/jobs/${j.jobId}`,
    };
  });
}

function kindKey(
  kind: JobKind
): 'sync' | 'generate' | 'aiGenerate' | 'piiDetect' | 'other' {
  switch (kind) {
    case JobKind.SYNC:
      return 'sync';
    case JobKind.GENERATE:
      return 'generate';
    case JobKind.AI_GENERATE:
      return 'aiGenerate';
    case JobKind.PII_DETECT:
      return 'piiDetect';
    default:
      return 'other';
  }
}

function LiveRunRow(props: {
  run: JobRun;
  job?: Job;
  connNames: Map<string, string>;
  href: string;
  now: Date;
  words: RelativeWords;
}): ReactElement {
  const { run, job, connNames, href, now, words } = props;
  const t = useTranslations('dashboard.home.live');
  const isPending = runOutcome(run.status) === 'pending';
  const srcId = job ? sourceConnectionId(job) : undefined;
  const src = srcId ? connNames.get(srcId) : undefined;
  const dests = (job?.destinations ?? [])
    .map((d) => connNames.get(d.connectionId))
    .filter((n): n is string => !!n);
  const route =
    src || dests.length > 0
      ? `${src ?? t('generated')} → ${dests.join(', ') || '–'}`
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
            ? t('waiting')
            : started
              ? t('started', { when: formatAgo(started, now, words) })
              : t('running')}
        </span>
      </div>
      <LiveBar
        label={
          isPending ? t('barWaiting', { name }) : t('barRunning', { name })
        }
      />
      {!isPending && (
        <div className="text-[13px] text-muted-foreground">
          {t.rich('runningFor', {
            duration: formatSpan(runDuration(run, now)),
            strong: (chunks) => (
              <strong className="font-semibold text-foreground">
                {chunks}
              </strong>
            ),
          })}
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

function NoJobs(props: { base: string }): ReactElement {
  const t = useTranslations('dashboard.home.noJobs');
  return (
    <Panel className="flex flex-col items-start gap-3 p-8">
      <h2 className="m-0 font-display text-lg font-semibold">{t('title')}</h2>
      <p className="m-0 max-w-prose text-sm text-muted-foreground">
        {t('description')}
      </p>
      <Button asChild>
        <Link href={`${props.base}/new/job`}>{t('newJob')}</Link>
      </Button>
    </Panel>
  );
}

function DashboardSkeleton(): ReactElement {
  return (
    <div className="flex flex-col gap-5" aria-busy="true">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-9 w-56" />
        <Skeleton className="h-5 w-80" />
      </div>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3.5">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[148px] w-full rounded-2xl" />
        ))}
      </div>
      <div className="flex flex-wrap gap-3.5">
        <Skeleton className="h-80 min-w-0 flex-[999_1_640px] rounded-2xl" />
        <Skeleton className="h-80 min-w-0 flex-[1_1_320px] rounded-2xl" />
      </div>
    </div>
  );
}
