import { JobKind } from '@husonym/sdk';
import type { UsageTotals } from '@husonym/sdk';

// What a tile or a cell shows when there is nothing to say.
const NO_VALUE = '—';

// Why a job that looks for personal data shows no row count.
const NO_ROWS_HINT = 'PII detection jobs count no rows.';

const ZERO = BigInt(0);
const ONE = BigInt(1);
const HUNDRED = BigInt(100);
const MINUTE = BigInt(60);
const HOUR = BigInt(3600);
const DAY = BigInt(86_400);

// A count with its digits grouped. A bigint keeps every digit.
export function formatCount(value: bigint | number): string {
  return value.toLocaleString('en-US');
}

function plural(count: bigint, one: string, many: string): string {
  return count === ONE ? one : many;
}

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
export function durationLabel(seconds: bigint | undefined): string {
  if (seconds === undefined) {
    return NO_VALUE;
  }
  if (seconds < MINUTE) {
    return `${seconds}s`;
  }
  if (seconds < HOUR) {
    return `${seconds / MINUTE}m ${seconds % MINUTE}s`;
  }
  if (seconds < DAY) {
    return `${seconds / HOUR}h ${(seconds % HOUR) / MINUTE}m`;
  }
  return `${formatCount(seconds / DAY)}d ${(seconds % DAY) / HOUR}h`;
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
  uncounted: bigint = ZERO
): string {
  if (!countsRows(kind)) {
    return NO_VALUE;
  }
  const rows = formatCount(rowsRead);
  return uncounted > ZERO ? `${rows} (incomplete)` : rows;
}

// What to say next to a row count that is not shown.
export function rowsHint(kind: JobKind | undefined): string | undefined {
  return countsRows(kind) ? undefined : NO_ROWS_HINT;
}

interface Tile {
  label: string;
  value: string;
  note?: string;
}

// The four tiles of a Usage page: rows read, runs, success rate, and a duration that
// each page names (the total run time of an account, the median of a job). A page of a
// job tells its kind; the tiles of an account are of every kind.
export function usageTiles(usage: {
  totals: UsageTotals | undefined;
  kind?: JobKind;
  duration: { label: string; seconds?: bigint };
}): Tile[] {
  const { totals, kind, duration } = usage;
  const canceled = totals?.runsCanceled ?? ZERO;
  return [
    withNote(
      { label: 'Rows read', value: rowsLabel(kind, totals?.rowsRead ?? ZERO) },
      rowsHint(kind)
    ),
    withNote(
      { label: 'Runs', value: formatCount(totals?.runs ?? ZERO) },
      canceled > ZERO ? `${formatCount(canceled)} canceled` : undefined
    ),
    { label: 'Success rate', value: successRateLabel(totals) },
    { label: duration.label, value: durationLabel(duration.seconds) },
  ];
}

function withNote(tile: Tile, note: string | undefined): Tile {
  return note === undefined ? tile : { ...tile, note };
}

// What stands in place of what a period without a run cannot show. Nothing when the
// period has runs.
export function noRunLine(totals: UsageTotals | undefined): string | undefined {
  return (totals?.runs ?? ZERO) > ZERO
    ? undefined
    : 'No run ended in this period.';
}

// What stands in place of the rows per day of a job when there is nothing to plot: a
// job that counts no rows has no chart at all, a period without a run has an empty one.
// Nothing when there are rows to plot.
export function noRowsPerDayLine(
  kind: JobKind,
  totals: UsageTotals | undefined
): string | undefined {
  return rowsHint(kind) ?? noRunLine(totals);
}

// Whether the account has no such job: the API tells the kind of every job of the
// account, ran or not, and none for a job that was deleted or is of another account.
export function isUnknownJob(kind: JobKind): boolean {
  return kind === JobKind.UNSPECIFIED;
}

// What the tiles do not say: the runs whose rows are not all counted, and the rows that
// were set aside instead of being written. One line each, only when there is something
// to say.
export function usageNotes(totals: UsageTotals | undefined): string[] {
  const notes: string[] = [];
  if (!totals) {
    return notes;
  }
  const { runs, runsWithUncountedRows: uncounted, rowsDiscarded } = totals;
  if (uncounted > ZERO) {
    notes.push(
      `The rows of ${formatCount(uncounted)} of ${formatCount(runs)} ${plural(runs, 'run', 'runs')} are not all counted.`
    );
  }
  if (rowsDiscarded > ZERO) {
    notes.push(
      `${formatCount(rowsDiscarded)} ${plural(rowsDiscarded, 'row', 'rows')} set aside.`
    );
  }
  return notes;
}
