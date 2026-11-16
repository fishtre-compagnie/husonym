import { JobKind } from '@husonym/sdk';
import type { UsageTotals } from '@husonym/sdk';
import { ENGLISH_USAGE_I18N, UsageI18n } from './i18n';
import { formatCount } from './numbers';

// What a tile or a cell shows when there is nothing to say.
const NO_VALUE = '—';

const ZERO = BigInt(0);
const HUNDRED = BigInt(100);
const MINUTE = BigInt(60);
const HOUR = BigInt(3600);
const DAY = BigInt(86_400);

type RunCounts = Pick<UsageTotals, 'runs' | 'runsCompleted' | 'runsCanceled'>;

// The share of the runs that completed, among those that either completed or did not:
// a canceled run is in neither term. Rounded down, so that 100% says that every run
// completed. Nothing when no run is left to divide by.
function successRate(totals: RunCounts): bigint | undefined {
  const settled = totals.runs - totals.runsCanceled;
  if (settled <= ZERO) {
    return undefined;
  }
  // The division of two bigint drops the remainder.
  return (totals.runsCompleted * HUNDRED) / settled;
}

export function successRateLabel(totals: RunCounts | undefined): string {
  const rate = totals ? successRate(totals) : undefined;
  return rate === undefined ? NO_VALUE : `${rate}%`;
}

// A duration in its two largest units: "59s", "3m 20s", "1h 5m", "1d 1h".
// The numbers are given as text: a number would have its digits grouped, a bigint
// would not be taken.
export function durationLabel(
  seconds: bigint | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  const { t, locale } = i18n;
  if (seconds === undefined) {
    return NO_VALUE;
  }
  if (seconds < MINUTE) {
    return t('duration.seconds', { s: String(seconds) });
  }
  if (seconds < HOUR) {
    return t('duration.minutes', {
      m: String(seconds / MINUTE),
      s: String(seconds % MINUTE),
    });
  }
  if (seconds < DAY) {
    return t('duration.hours', {
      h: String(seconds / HOUR),
      m: String((seconds % HOUR) / MINUTE),
    });
  }
  return t('duration.days', {
    d: formatCount(seconds / DAY, locale),
    h: String((seconds % DAY) / HOUR),
  });
}

// Whether the runs of a kind of job count the rows they read. Looking for personal
// data counts none: its zero would read as "read nothing".
function countsRows(kind: JobKind | undefined): boolean {
  return kind !== JobKind.PII_DETECT;
}

// The rows a job or a run read, or nothing to say for a job that counts none: never a
// zero. `uncounted` is how many of what the count adds up (the runs of a job, the
// tables of a run) told no count of their own: the count is then less than what was
// read, and is marked so that it is not taken for the whole.
export function rowsLabel(
  kind: JobKind | undefined,
  rowsRead: bigint,
  uncounted: bigint = ZERO,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  if (!countsRows(kind)) {
    return NO_VALUE;
  }
  const rows = formatCount(rowsRead, i18n.locale);
  return uncounted > ZERO ? i18n.t('incomplete', { rows }) : rows;
}

// What to say next to a row count that is not shown: why a job that looks for
// personal data shows none.
export function rowsHint(
  kind: JobKind | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string | undefined {
  return countsRows(kind) ? undefined : i18n.t('noRowsHint');
}

interface Tile {
  label: string;
  value: string;
  note?: string;
}

// The four tiles of a Usage page: rows read, runs, success rate, and a duration that
// each page names (the total run time of an account, the median of a job). A page of a
// job tells its kind; the tiles of an account are of every kind.
export function usageTiles(
  usage: {
    totals: UsageTotals | undefined;
    kind?: JobKind;
    duration: { label: string; seconds?: bigint };
  },
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): Tile[] {
  const { totals, kind, duration } = usage;
  const { t, locale } = i18n;
  const canceled = totals?.runsCanceled ?? ZERO;
  return [
    withNote(
      {
        label: t('tiles.rowsRead'),
        value: rowsLabel(kind, totals?.rowsRead ?? ZERO, ZERO, i18n),
      },
      rowsHint(kind, i18n)
    ),
    withNote(
      {
        label: t('tiles.runs'),
        value: formatCount(totals?.runs ?? ZERO, locale),
      },
      canceled > ZERO
        ? t('tiles.canceled', { count: formatCount(canceled, locale) })
        : undefined
    ),
    { label: t('tiles.successRate'), value: successRateLabel(totals) },
    { label: duration.label, value: durationLabel(duration.seconds, i18n) },
  ];
}

function withNote(tile: Tile, note: string | undefined): Tile {
  return note === undefined ? tile : { ...tile, note };
}

// What stands in place of what a period without a run cannot show. Nothing when the
// period has runs.
export function noRunLine(
  totals: UsageTotals | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string | undefined {
  return (totals?.runs ?? ZERO) > ZERO ? undefined : i18n.t('noRun');
}

// What stands in place of the rows per day of a job when there is nothing to plot: a
// job that counts no rows has no chart at all, a period without a run has an empty one.
// Nothing when there are rows to plot.
export function noRowsPerDayLine(
  kind: JobKind,
  totals: UsageTotals | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string | undefined {
  return rowsHint(kind, i18n) ?? noRunLine(totals, i18n);
}

// Whether the account has no such job: the API tells the kind of every job of the
// account, ran or not, and none for a job that was deleted or is of another account.
export function isUnknownJob(kind: JobKind): boolean {
  return kind === JobKind.UNSPECIFIED;
}

// What the tiles do not say: the runs whose rows are not all counted, and the rows that
// were set aside instead of being written. One line each, only when there is something
// to say.
export function usageNotes(
  totals: UsageTotals | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string[] {
  const { t, locale } = i18n;
  const notes: string[] = [];
  if (!totals) {
    return notes;
  }
  const { runs, runsWithUncountedRows: uncounted, rowsDiscarded } = totals;
  if (uncounted > ZERO) {
    notes.push(
      t('notes.uncounted', {
        uncounted: formatCount(uncounted, locale),
        runs: formatCount(runs, locale),
        runCount: Number(runs),
      })
    );
  }
  if (rowsDiscarded > ZERO) {
    notes.push(
      t('notes.discarded', {
        rows: formatCount(rowsDiscarded, locale),
        rowCount: Number(rowsDiscarded),
      })
    );
  }
  return notes;
}
