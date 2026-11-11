/**
 * What the home dashboard computes from the API, kept apart from the markup so it can be
 * tested: run counts over a period, the coverage of personal columns, durations and dates
 * as a person reads them.
 */
import { rgpdState } from '@/components/jobs/JobMappingTable/rgpd-state';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import {
  GetConnectionSchemaMapResponse,
  Job,
  JobEngine,
  JobMapping,
  JobRun,
  JobRunStatus,
  JobSource,
} from '@husonym/sdk';
import { formatDistanceStrict } from 'date-fns';

export type Period = '24h' | '7d' | '30d';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

// The length of each period, and how many points its sparkline has.
const PERIODS: Record<Period, { ms: number; buckets: number }> = {
  '24h': { ms: DAY, buckets: 24 },
  '7d': { ms: 7 * DAY, buckets: 7 },
  '30d': { ms: 30 * DAY, buckets: 30 },
};

export const PERIOD_OPTIONS: { value: Period; label: string }[] = [
  { value: '24h', label: '24 h' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
];

export function periodLabel(p: Period): string {
  switch (p) {
    case '24h':
      return 'last 24 hours';
    case '7d':
      return 'last 7 days';
    case '30d':
      return 'last 30 days';
  }
}

export type RunOutcome =
  'succeeded' | 'failed' | 'canceled' | 'running' | 'pending' | 'unknown';

export function runOutcome(status: JobRunStatus): RunOutcome {
  switch (status) {
    case JobRunStatus.COMPLETE:
      return 'succeeded';
    case JobRunStatus.ERROR:
    case JobRunStatus.FAILED:
    case JobRunStatus.TIMED_OUT:
      return 'failed';
    case JobRunStatus.CANCELED:
    case JobRunStatus.TERMINATED:
      return 'canceled';
    case JobRunStatus.RUNNING:
      return 'running';
    case JobRunStatus.PENDING:
      return 'pending';
    default:
      return 'unknown';
  }
}

/** The word for a run's status, closer to the API than its outcome. */
export function runStatusLabel(status: JobRunStatus): string {
  switch (status) {
    case JobRunStatus.COMPLETE:
      return 'Succeeded';
    case JobRunStatus.ERROR:
      return 'Error';
    case JobRunStatus.FAILED:
      return 'Failed';
    case JobRunStatus.TIMED_OUT:
      return 'Timed out';
    case JobRunStatus.CANCELED:
      return 'Canceled';
    case JobRunStatus.TERMINATED:
      return 'Terminated';
    case JobRunStatus.RUNNING:
      return 'Running';
    case JobRunStatus.PENDING:
      return 'Pending';
    default:
      return 'Unknown';
  }
}

export function toDate(ts?: Timestamp): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

// When a run counts for a period: when it ended, or when it started if it has not ended.
function runTime(run: JobRun): number | undefined {
  return (toDate(run.completedAt) ?? toDate(run.startedAt))?.getTime();
}

export interface RunStats {
  finished: number;
  succeeded: number;
  failed: number;
  canceled: number;
  /** Finished runs per slice of the period, oldest first. */
  series: number[];
  /** Succeeded out of finished, in percent; null when nothing finished. */
  successRate: number | null;
}

export function runStats(
  runs: JobRun[],
  period: Period,
  now: Date = new Date()
): RunStats {
  const { ms, buckets } = PERIODS[period];
  const end = now.getTime();
  const start = end - ms;
  const slice = ms / buckets;
  const series = new Array<number>(buckets).fill(0);
  let succeeded = 0;
  let failed = 0;
  let canceled = 0;
  for (const run of runs) {
    const outcome = runOutcome(run.status);
    if (
      outcome !== 'succeeded' &&
      outcome !== 'failed' &&
      outcome !== 'canceled'
    ) {
      continue;
    }
    const t = runTime(run);
    if (t === undefined || t < start || t > end) {
      continue;
    }
    if (outcome === 'succeeded') succeeded++;
    else if (outcome === 'failed') failed++;
    else canceled++;
    series[Math.min(buckets - 1, Math.floor((t - start) / slice))]++;
  }
  const finished = succeeded + failed + canceled;
  return {
    finished,
    succeeded,
    failed,
    canceled,
    series,
    successRate:
      finished > 0 ? Math.round((succeeded / finished) * 1000) / 10 : null,
  };
}

/** The finished runs of the period, the most recent first. */
export function latestRuns(
  runs: JobRun[],
  period: Period,
  limit: number,
  now: Date = new Date()
): JobRun[] {
  const start = now.getTime() - PERIODS[period].ms;
  return runs
    .filter((r) => {
      const o = runOutcome(r.status);
      const t = runTime(r);
      return (
        o !== 'running' && o !== 'pending' && t !== undefined && t >= start
      );
    })
    .sort((a, b) => (runTime(b) ?? 0) - (runTime(a) ?? 0))
    .slice(0, limit);
}

/** The runs going on now: running first, then waiting, the oldest first. */
export function activeRuns(runs: JobRun[]): JobRun[] {
  return runs
    .filter((r) => {
      const o = runOutcome(r.status);
      return o === 'running' || o === 'pending';
    })
    .sort((a, b) => {
      const ra = runOutcome(a.status) === 'running' ? 0 : 1;
      const rb = runOutcome(b.status) === 'running' ? 0 : 1;
      return ra - rb || (runTime(a) ?? 0) - (runTime(b) ?? 0);
    });
}

/** A span of time as a person reads it: 42 s, 7 min 42, 1 h 03 min. */
export function formatSpan(ms: number | undefined): string {
  if (ms === undefined || !Number.isFinite(ms) || ms < 0) {
    return '–';
  }
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) {
    return `${h} h ${String(m).padStart(2, '0')} min`;
  }
  if (m > 0) {
    return `${m} min ${String(s).padStart(2, '0')}`;
  }
  return `${s} s`;
}

export function runDuration(
  run: JobRun,
  now: Date = new Date()
): number | undefined {
  const start = toDate(run.startedAt);
  if (!start) {
    return undefined;
  }
  return (toDate(run.completedAt) ?? now).getTime() - start.getTime();
}

export function formatAgo(
  date: Date | undefined,
  now: Date = new Date()
): string {
  if (!date) {
    return '–';
  }
  if (now.getTime() - date.getTime() < 60_000) {
    return 'just now';
  }
  return `${formatDistanceStrict(date, now)} ago`;
}

export function formatIn(date: Date, now: Date = new Date()): string {
  return `in ${formatDistanceStrict(date, now)}`;
}

export function runWhen(run: JobRun): Date | undefined {
  return toDate(run.completedAt) ?? toDate(run.startedAt);
}

// ---------------------------------------------------------------------------------------
// Engines

export interface EngineCount {
  name: string;
  count: number;
}

export function engineCounts(jobs: Job[]): EngineCount[] {
  const counts = { Athanor: 0, Benthos: 0, Default: 0 };
  for (const j of jobs) {
    switch (j.workflowOptions?.engine) {
      case JobEngine.ATHANOR:
        counts.Athanor++;
        break;
      case JobEngine.BENTHOS:
        counts.Benthos++;
        break;
      default:
        counts.Default++;
    }
  }
  return Object.entries(counts)
    .map(([name, count]) => ({ name, count }))
    .filter((e) => e.count > 0);
}

// ---------------------------------------------------------------------------------------
// Personal data coverage

/**
 * The connection whose schema says which columns of a job are personal: the database a
 * sync job reads from. Generation jobs and file or document sources have no such schema.
 */
export function sqlSourceConnectionId(
  source: JobSource | undefined
): string | undefined {
  const c = source?.options?.config;
  if (c?.case === 'postgres' || c?.case === 'mysql' || c?.case === 'mssql') {
    return c.value.connectionId;
  }
  return undefined;
}

function anonymizes(m: JobMapping): boolean {
  const c = m.transformer?.config?.config.case;
  return !!c && c !== 'passthroughConfig';
}

export interface ClearColumn {
  jobId: string;
  jobName: string;
  schema: string;
  table: string;
  column: string;
  category: string;
}

export interface Coverage {
  /** Personal, and a transformer anonymizes it. */
  anonymized: number;
  /** Nothing known to be personal: copied as it is. */
  kept: number;
  /** A detection that is not proven, to confirm or dismiss. */
  toConfirm: number;
  /** Personal and copied as it is. */
  inClear: number;
  total: number;
  /** Jobs whose source schema could be read. */
  jobsCounted: number;
  inClearColumns: ClearColumn[];
}

/**
 * Sorts every mapped column of the jobs by what its source schema says about it, using the
 * same rule as the GDPR cell of the mapping table. Jobs whose source schema is not in
 * `schemas` are left out.
 */
export function coverage(
  jobs: Job[],
  schemas: Map<string, GetConnectionSchemaMapResponse>
): Coverage {
  const out: Coverage = {
    anonymized: 0,
    kept: 0,
    toConfirm: 0,
    inClear: 0,
    total: 0,
    jobsCounted: 0,
    inClearColumns: [],
  };
  for (const job of jobs) {
    const connId = sqlSourceConnectionId(job.source);
    const schema = connId ? schemas.get(connId) : undefined;
    if (!schema) {
      continue;
    }
    out.jobsCounted++;
    for (const m of job.mappings) {
      const col = schema.schemaMap[`${m.schema}.${m.table}`]?.schemas.find(
        (c) => c.column === m.column
      );
      if (!col) {
        // Mapped, but no longer in the source: nothing leaves it.
        continue;
      }
      out.total++;
      const state = rgpdState({
        isSensitive: col.isSensitive,
        confidence: col.piiConfidence,
        isAnonymized: anonymizes(m),
      });
      switch (state) {
        case 'compliant':
          out.anonymized++;
          break;
        case 'review':
        case 'not_analyzed':
          out.toConfirm++;
          break;
        case 'not_anonymized':
          out.inClear++;
          out.inClearColumns.push({
            jobId: job.id,
            jobName: job.name,
            schema: m.schema,
            table: m.table,
            column: m.column,
            category: col.dataCategory,
          });
          break;
        case 'none':
          out.kept++;
          break;
      }
    }
  }
  return out;
}

/** "birth_date" → "Birth date". */
export function categoryLabel(category: string): string {
  const s = category.replace(/[_-]+/g, ' ').trim();
  if (!s) {
    return 'Personal data';
  }
  return s.charAt(0).toUpperCase() + s.slice(1).toLowerCase();
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n.toLocaleString('en-US')} ${n === 1 ? one : many}`;
}
