/**
 * What the home dashboard computes from the API, kept apart from the markup so it can be
 * tested: the outcome of a run, the runs going on, durations and dates as a person reads
 * them.
 */
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { JobRun, JobRunStatus } from '@husonym/sdk';
import { Locale as DateFnsLocale, formatDistanceStrict } from 'date-fns';

export type Period = '7d' | '30d' | '90d';

// How many days each period spans.
const PERIOD_DAYS: Record<Period, number> = { '7d': 7, '30d': 30, '90d': 90 };

/** The periods the dashboard offers, shortest first. */
export const PERIODS_ORDER: Period[] = ['7d', '30d', '90d'];

/** How many days each period spans. */
export function periodDays(period: Period): number {
  return PERIOD_DAYS[period];
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

export type RunStatusKey =
  | 'succeeded'
  | 'error'
  | 'failed'
  | 'timedOut'
  | 'canceled'
  | 'terminated'
  | 'running'
  | 'pending'
  | 'unknown';

/** The key of the word for a run's status, closer to the API than its outcome. */
export function runStatusKey(status: JobRunStatus): RunStatusKey {
  switch (status) {
    case JobRunStatus.COMPLETE:
      return 'succeeded';
    case JobRunStatus.ERROR:
      return 'error';
    case JobRunStatus.FAILED:
      return 'failed';
    case JobRunStatus.TIMED_OUT:
      return 'timedOut';
    case JobRunStatus.CANCELED:
      return 'canceled';
    case JobRunStatus.TERMINATED:
      return 'terminated';
    case JobRunStatus.RUNNING:
      return 'running';
    case JobRunStatus.PENDING:
      return 'pending';
    default:
      return 'unknown';
  }
}

export function toDate(ts?: Timestamp): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

// When a run counts for a period: when it ended, or when it started if it has not ended.
function runTime(run: JobRun): number | undefined {
  return (toDate(run.completedAt) ?? toDate(run.startedAt))?.getTime();
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

/** How relative times are written: the language, and the words for "just now". */
export interface RelativeWords {
  locale?: DateFnsLocale;
  justNow: string;
}

/** "3 minutes ago", in the language of `words`. */
export function formatAgo(
  date: Date | undefined,
  now: Date,
  words: RelativeWords
): string {
  if (!date) {
    return '–';
  }
  if (now.getTime() - date.getTime() < 60_000) {
    return words.justNow;
  }
  return formatDistanceStrict(date, now, {
    addSuffix: true,
    locale: words.locale,
  });
}

/** "in 3 hours", in the language of `locale`. */
export function formatIn(
  date: Date,
  now: Date,
  locale?: DateFnsLocale
): string {
  return formatDistanceStrict(date, now, { addSuffix: true, locale });
}
