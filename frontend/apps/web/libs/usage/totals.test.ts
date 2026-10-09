import { create } from '@bufbuild/protobuf';
import { JobKind, UsageTotalsSchema } from '@husonym/sdk';
import {
  durationLabel,
  formatCount,
  isUnknownJob,
  noRowsPerDayLine,
  noRunLine,
  rowsLabel,
  successRateLabel,
  usageNotes,
  usageTiles,
} from './totals';

type Count =
  | 'runs'
  | 'runsCompleted'
  | 'runsCanceled'
  | 'rowsRead'
  | 'rowsDiscarded'
  | 'runsWithUncountedRows';

// The totals of a period, the counts that are not given at zero.
function totals(counts: Partial<Record<Count, number>> = {}) {
  const of = (count: Count): bigint => BigInt(counts[count] ?? 0);
  return create(UsageTotalsSchema, {
    runs: of('runs'),
    runsCompleted: of('runsCompleted'),
    runsCanceled: of('runsCanceled'),
    rowsRead: of('rowsRead'),
    rowsDiscarded: of('rowsDiscarded'),
    runsWithUncountedRows: of('runsWithUncountedRows'),
  });
}

function runs(all: number, completed: number, canceled = 0) {
  return totals({
    runs: all,
    runsCompleted: completed,
    runsCanceled: canceled,
  });
}

describe('formatCount', () => {
  it('groups the digits', () => {
    expect(formatCount(BigInt(0))).toBe('0');
    expect(formatCount(BigInt(999))).toBe('999');
    expect(formatCount(BigInt(1234567))).toBe('1,234,567');
    expect(formatCount(1234567)).toBe('1,234,567');
  });

  it('keeps every digit of a number a float cannot hold', () => {
    expect(formatCount(BigInt('9007199254740993'))).toBe(
      '9,007,199,254,740,993'
    );
  });
});

describe('successRateLabel', () => {
  it('has nothing to say without a run that succeeded or failed', () => {
    expect(successRateLabel(undefined)).toBe('—');
    expect(successRateLabel(runs(0, 0))).toBe('—');
  });

  it('has nothing to say when every run was canceled', () => {
    expect(successRateLabel(runs(3, 0, 3))).toBe('—');
  });

  it('is 100% only when every run completed', () => {
    expect(successRateLabel(runs(3, 3))).toBe('100%');
    expect(successRateLabel(runs(200, 199))).toBe('99%');
    expect(successRateLabel(runs(100_000, 99_999))).toBe('99%');
  });

  it('is 0% when no run completed', () => {
    expect(successRateLabel(runs(3, 0))).toBe('0%');
  });

  it('rounds down', () => {
    expect(successRateLabel(runs(3, 2))).toBe('66%');
  });

  it('counts a canceled run neither as a success nor as a failure', () => {
    // 4 runs: 2 completed, 1 failed, 1 canceled.
    expect(successRateLabel(runs(4, 2, 1))).toBe('66%');
    // 3 runs: 2 completed, 1 canceled.
    expect(successRateLabel(runs(3, 2, 1))).toBe('100%');
  });
});

describe('durationLabel', () => {
  const label = (seconds: number) => durationLabel(BigInt(seconds));

  it('has nothing to say for no duration', () => {
    expect(durationLabel(undefined)).toBe('—');
  });

  it('reads in seconds under a minute', () => {
    expect(label(0)).toBe('0s');
    expect(label(59)).toBe('59s');
  });

  it('reads in minutes under an hour', () => {
    expect(label(60)).toBe('1m 0s');
    expect(label(200)).toBe('3m 20s');
    expect(label(3599)).toBe('59m 59s');
  });

  it('reads in hours under a day', () => {
    expect(label(3600)).toBe('1h 0m');
    expect(label(3900)).toBe('1h 5m');
    expect(label(86_399)).toBe('23h 59m');
  });

  it('reads in days from a day on', () => {
    expect(label(86_400)).toBe('1d 0h');
    expect(label(90_000)).toBe('1d 1h');
    expect(label(100_000_000)).toBe('1,157d 9h');
  });
});

describe('rowsLabel', () => {
  it('is the count of a job that counts rows', () => {
    expect(rowsLabel(JobKind.SYNC, BigInt(1140))).toBe('1,140');
    expect(rowsLabel(JobKind.GENERATE, BigInt(0))).toBe('0');
    expect(rowsLabel(JobKind.AI_GENERATE, BigInt(12))).toBe('12');
    expect(rowsLabel(JobKind.UNSPECIFIED, BigInt(0))).toBe('0');
    expect(rowsLabel(undefined, BigInt(7))).toBe('7');
  });

  it('is never a zero for a job that counts none', () => {
    expect(rowsLabel(JobKind.PII_DETECT, BigInt(0))).toBe('—');
  });

  it('is marked when not every row was counted', () => {
    expect(rowsLabel(JobKind.SYNC, BigInt(1140), BigInt(2))).toBe(
      '1,140 (incomplete)'
    );
    expect(rowsLabel(JobKind.SYNC, BigInt(0), BigInt(1))).toBe(
      '0 (incomplete)'
    );
    expect(rowsLabel(JobKind.SYNC, BigInt(1140), BigInt(0))).toBe('1,140');
  });

  it('is not marked for a job that counts none', () => {
    expect(rowsLabel(JobKind.PII_DETECT, BigInt(0), BigInt(3))).toBe('—');
  });
});

describe('usageTiles', () => {
  const full = totals({
    runs: 4,
    runsCompleted: 2,
    runsCanceled: 1,
    rowsRead: 1140,
    rowsDiscarded: 5,
    runsWithUncountedRows: 1,
  });

  it('gives the four tiles of an account', () => {
    expect(
      usageTiles({
        totals: full,
        duration: { label: 'Run time', seconds: BigInt(7500) },
      })
    ).toEqual([
      { label: 'Rows read', value: '1,140' },
      { label: 'Runs', value: '4', note: '1 canceled' },
      { label: 'Success rate', value: '66%' },
      { label: 'Run time', value: '2h 5m' },
    ]);
  });

  it('says nothing of canceled runs when there is none', () => {
    const tiles = usageTiles({
      totals: runs(3, 3),
      duration: { label: 'Run time', seconds: BigInt(12) },
    });
    expect(tiles[1]).toEqual({ label: 'Runs', value: '3' });
  });

  it('reads 0, 0, — and — for a period with no run', () => {
    for (const none of [undefined, totals()]) {
      expect(
        usageTiles({ totals: none, duration: { label: 'Run time' } })
      ).toEqual([
        { label: 'Rows read', value: '0' },
        { label: 'Runs', value: '0' },
        { label: 'Success rate', value: '—' },
        { label: 'Run time', value: '—' },
      ]);
    }
  });

  it('gives the tiles of a job, with its median', () => {
    expect(
      usageTiles({
        totals: full,
        kind: JobKind.SYNC,
        duration: { label: 'Median duration', seconds: BigInt(120) },
      })
    ).toEqual([
      { label: 'Rows read', value: '1,140' },
      { label: 'Runs', value: '4', note: '1 canceled' },
      { label: 'Success rate', value: '66%' },
      { label: 'Median duration', value: '2m 0s' },
    ]);
  });

  it('shows no row count for a job that counts none, and says why', () => {
    const tiles = usageTiles({
      totals: runs(2, 2),
      kind: JobKind.PII_DETECT,
      duration: { label: 'Median duration', seconds: BigInt(30) },
    });
    expect(tiles[0]).toEqual({
      label: 'Rows read',
      value: '—',
      note: 'PII detection jobs count no rows.',
    });
    expect(tiles[1]).toEqual({ label: 'Runs', value: '2' });
  });
});

describe('noRunLine', () => {
  it('says that no run ended in a period without one', () => {
    expect(noRunLine(undefined)).toBe('No run ended in this period.');
    expect(noRunLine(totals())).toBe('No run ended in this period.');
  });

  it('has nothing to say once a run is counted, whatever became of it', () => {
    expect(noRunLine(runs(1, 0, 1))).toBeUndefined();
    expect(noRunLine(runs(3, 3))).toBeUndefined();
  });
});

describe('noRowsPerDayLine', () => {
  it('says that a job counts no rows, with or without runs', () => {
    expect(noRowsPerDayLine(JobKind.PII_DETECT, runs(3, 3))).toBe(
      'PII detection jobs count no rows.'
    );
    expect(noRowsPerDayLine(JobKind.PII_DETECT, undefined)).toBe(
      'PII detection jobs count no rows.'
    );
  });

  it('says that no run ended for a job that counts rows and did not run', () => {
    expect(noRowsPerDayLine(JobKind.SYNC, totals())).toBe(
      'No run ended in this period.'
    );
    expect(noRowsPerDayLine(JobKind.GENERATE, undefined)).toBe(
      'No run ended in this period.'
    );
  });

  it('has nothing to say when there are rows to plot', () => {
    expect(noRowsPerDayLine(JobKind.SYNC, runs(1, 1))).toBeUndefined();
    expect(noRowsPerDayLine(JobKind.AI_GENERATE, runs(2, 0))).toBeUndefined();
  });
});

describe('isUnknownJob', () => {
  it('is a job the answer tells no kind of', () => {
    expect(isUnknownJob(JobKind.UNSPECIFIED)).toBe(true);
  });

  it('is not a job of a kind, whether or not it ran', () => {
    for (const kind of [
      JobKind.SYNC,
      JobKind.GENERATE,
      JobKind.AI_GENERATE,
      JobKind.PII_DETECT,
    ]) {
      expect(isUnknownJob(kind)).toBe(false);
    }
  });

  it('is not a job of a kind this page does not know yet', () => {
    expect(isUnknownJob(99 as JobKind)).toBe(false);
  });
});

describe('usageNotes', () => {
  it('has nothing to say when every row is counted and none set aside', () => {
    expect(usageNotes(undefined)).toEqual([]);
    expect(usageNotes(totals({ runs: 4 }))).toEqual([]);
  });

  it('says how many runs have rows that are not all counted', () => {
    expect(usageNotes(totals({ runs: 40, runsWithUncountedRows: 3 }))).toEqual([
      'The rows of 3 of 40 runs are not all counted.',
    ]);
    expect(usageNotes(totals({ runs: 1, runsWithUncountedRows: 1 }))).toEqual([
      'The rows of 1 of 1 run are not all counted.',
    ]);
  });

  it('says how many rows were set aside', () => {
    expect(usageNotes(totals({ runs: 2, rowsDiscarded: 1500 }))).toEqual([
      '1,500 rows set aside.',
    ]);
    expect(usageNotes(totals({ runs: 2, rowsDiscarded: 1 }))).toEqual([
      '1 row set aside.',
    ]);
  });

  it('says both, the rows not counted first', () => {
    expect(
      usageNotes(
        totals({ runs: 4, runsWithUncountedRows: 1, rowsDiscarded: 5 })
      )
    ).toEqual([
      'The rows of 1 of 4 runs are not all counted.',
      '5 rows set aside.',
    ]);
  });
});
