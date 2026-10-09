import { create, MessageInitShape } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import {
  GateRefusalCountSchema,
  GetJobUsageResponseSchema,
  JobKind,
  JobRunStatus,
  JobUsageSchema,
  RunErrorCategory,
  RunErrorStep,
  RunUsageSchema,
  UsageErrorCountSchema,
  UsageTotalsSchema,
} from '@husonym/sdk';
import {
  errorRows,
  errorsEmptyLine,
  jobsEmptyLine,
  jobsTable,
  refusalRows,
  runsTable,
} from './rows';

const ORDERS_ID = '0b5c7d1e-0000-4000-8000-0000000000a1';
const SCAN_ID = '0b5c7d1e-0000-4000-8000-0000000000a2';

const orders = create(JobUsageSchema, {
  jobId: ORDERS_ID,
  jobName: 'orders',
  kind: JobKind.SYNC,
  totals: create(UsageTotalsSchema, {
    runs: BigInt(4),
    runsCompleted: BigInt(2),
    runsCanceled: BigInt(1),
    rowsRead: BigInt(1140),
  }),
  durationMedianSeconds: BigInt(120),
});

const scan = create(JobUsageSchema, {
  jobId: SCAN_ID,
  jobName: 'scan',
  kind: JobKind.PII_DETECT,
  totals: create(UsageTotalsSchema, {
    runs: BigInt(2),
    runsCompleted: BigInt(2),
  }),
});

function runsCounted(runs: number) {
  return create(UsageTotalsSchema, {
    runs: BigInt(runs),
    runsCompleted: BigInt(runs),
  });
}

function errorCount(category: RunErrorCategory, runs: number) {
  return create(UsageErrorCountSchema, { category, runs: BigInt(runs) });
}

function refusalCount(gate: string, refusals: number) {
  return create(GateRefusalCountSchema, { gate, refusals: BigInt(refusals) });
}

describe('jobsTable', () => {
  it('gives a row per job, each leading to the usage of the job', () => {
    expect(jobsTable([orders], 'acme')).toEqual({
      rows: [
        {
          id: ORDERS_ID,
          name: 'orders',
          href: `/acme/jobs/${ORDERS_ID}/usage`,
          rowsRead: '1,140',
          runs: '4',
          success: '66%',
          duration: '2m 0s',
        },
      ],
      note: undefined,
    });
  });

  it('shows no row count for a job that counts none, and says why once', () => {
    const table = jobsTable([scan, orders, scan], 'acme');
    expect(table.rows.map((row) => row.rowsRead)).toEqual(['—', '1,140', '—']);
    expect(table.note).toBe('PII detection jobs count no rows.');
  });

  it('has nothing to say of a median that no run gives', () => {
    expect(jobsTable([scan], 'acme').rows[0].duration).toBe('—');
  });

  it('reads a job without totals as a job without runs', () => {
    const bare = create(JobUsageSchema, { jobId: ORDERS_ID, jobName: 'bare' });
    expect(jobsTable([bare], 'acme').rows[0]).toMatchObject({
      rowsRead: '0',
      runs: '0',
      success: '—',
      duration: '—',
    });
  });

  it('keeps the order received', () => {
    expect(
      jobsTable([scan, orders], 'acme').rows.map((row) => row.name)
    ).toEqual(['scan', 'orders']);
  });

  it('gives nothing for no job', () => {
    expect(jobsTable([], 'acme')).toEqual({ rows: [], note: undefined });
  });
});

describe('jobsEmptyLine', () => {
  it('says that no job ran when the period has no run', () => {
    expect(jobsEmptyLine(undefined)).toBe('No job ran in this period.');
    expect(jobsEmptyLine(runsCounted(0))).toBe('No job ran in this period.');
  });

  it('does not say that no job ran when runs are counted', () => {
    expect(jobsEmptyLine(runsCounted(3))).toBe(
      'The jobs that ran in this period no longer exist.'
    );
  });
});

describe('errorRows', () => {
  it('names each category with its runs, in the order received', () => {
    expect(
      errorRows([
        errorCount(RunErrorCategory.CONSTRAINT_VIOLATED, 1200),
        errorCount(RunErrorCategory.TIMEOUT, 1),
      ])
    ).toEqual([
      { key: '4', label: 'Constraint violated', count: '1,200' },
      { key: '3', label: 'Timeout', count: '1' },
    ]);
  });

  it('reads a category it does not know as Other', () => {
    expect(errorRows([errorCount(99 as RunErrorCategory, 2)])).toEqual([
      { key: '99', label: 'Other', count: '2' },
    ]);
  });
});

describe('errorsEmptyLine', () => {
  it('says that no run ended when the period has none', () => {
    expect(errorsEmptyLine(undefined)).toBe('No run ended in this period.');
    expect(errorsEmptyLine(runsCounted(0))).toBe(
      'No run ended in this period.'
    );
  });

  it('says that every run completed when there are runs and no error', () => {
    expect(errorsEmptyLine(runsCounted(3))).toBe(
      'Every run of this period completed.'
    );
  });
});

const STARTED = new Date('2026-10-06T09:00:00Z');

// A run that completed: started at STARTED, ended 200 seconds later, 1,140 rows.
function run(fields: MessageInitShape<typeof RunUsageSchema> = {}) {
  return create(RunUsageSchema, {
    runId: 'r1',
    status: JobRunStatus.COMPLETE,
    startedAt: timestampFromDate(STARTED),
    endedAt: timestampFromDate(new Date(STARTED.getTime() + 200_000)),
    rowsRead: BigInt(1140),
    ...fields,
  });
}

function failed(fields: MessageInitShape<typeof RunUsageSchema> = {}) {
  return run({
    status: JobRunStatus.FAILED,
    errorCategory: RunErrorCategory.CONSTRAINT_VIOLATED,
    errorStep: RunErrorStep.TABLE_SYNC,
    ...fields,
  });
}

function jobUsage(fields: MessageInitShape<typeof GetJobUsageResponseSchema>) {
  return create(GetJobUsageResponseSchema, { kind: JobKind.SYNC, ...fields });
}

describe('runsTable', () => {
  it('gives a row per run, each leading to the page of the run', () => {
    expect(runsTable(jobUsage({ runs: [run()] }), 'acme').rows).toEqual([
      {
        id: 'r1',
        href: '/acme/runs/r1',
        startedAt: STARTED,
        status: JobRunStatus.COMPLETE,
        duration: '3m 20s',
        rowsRead: '1,140',
        error: '',
      },
    ]);
  });

  it('names what kept a run from completing, and the step it was at', () => {
    const [row] = runsTable(jobUsage({ runs: [failed()] }), 'acme').rows;
    expect(row.status).toBe(JobRunStatus.FAILED);
    expect(row.error).toBe('Constraint violated · Table sync');
  });

  it('leaves out a step that says nothing', () => {
    const errors = runsTable(
      jobUsage({
        runs: [
          failed({ errorStep: RunErrorStep.OTHER }),
          failed({ errorStep: RunErrorStep.UNSPECIFIED }),
          run({
            status: JobRunStatus.CANCELED,
            errorCategory: RunErrorCategory.CANCELED,
          }),
        ],
      }),
      'acme'
    ).rows.map((row) => row.error);
    expect(errors).toEqual([
      'Constraint violated',
      'Constraint violated',
      'Canceled',
    ]);
  });

  it('reads a run that did not complete and tells no category as Other', () => {
    const [row] = runsTable(
      jobUsage({
        runs: [
          failed({
            errorCategory: RunErrorCategory.UNSPECIFIED,
            errorStep: RunErrorStep.HOOKS,
          }),
        ],
      }),
      'acme'
    ).rows;
    expect(row.error).toBe('Other · Hooks');
  });

  it('shows no error for a run that completed, whatever it tells', () => {
    const [row] = runsTable(
      jobUsage({
        runs: [
          run({
            errorCategory: RunErrorCategory.TIMEOUT,
            errorStep: RunErrorStep.HOOKS,
          }),
        ],
      }),
      'acme'
    ).rows;
    expect(row.error).toBe('');
  });

  it('says that the rows of a run are not all counted', () => {
    const [row] = runsTable(
      jobUsage({
        runs: [failed({ rowsRead: BigInt(40), tablesUncounted: BigInt(1) })],
      }),
      'acme'
    ).rows;
    expect(row.rowsRead).toBe('40 (incomplete)');
  });

  it('shows no row count for a job that counts none', () => {
    const [row] = runsTable(
      jobUsage({
        kind: JobKind.PII_DETECT,
        runs: [run({ rowsRead: BigInt(0), tablesUncounted: BigInt(1) })],
      }),
      'acme'
    ).rows;
    expect(row.rowsRead).toBe('—');
  });

  it('has no duration for a run without a known end or a known start', () => {
    const rows = runsTable(
      jobUsage({
        runs: [run({ endedAt: undefined }), run({ startedAt: undefined })],
      }),
      'acme'
    ).rows;
    expect(rows.map((row) => row.duration)).toEqual(['—', '—']);
    expect(rows.map((row) => row.startedAt)).toEqual([STARTED, undefined]);
  });

  it('counts whole seconds, and never less than none', () => {
    const at = (ms: number) =>
      timestampFromDate(new Date(STARTED.getTime() + ms));
    const rows = runsTable(
      jobUsage({
        runs: [
          run({ endedAt: at(-5000) }),
          run({ endedAt: at(0) }),
          run({ endedAt: at(1999) }),
          run({ endedAt: at(3_600_000 + 5 * 60_000) }),
        ],
      }),
      'acme'
    ).rows;
    expect(rows.map((row) => row.duration)).toEqual([
      '0s',
      '0s',
      '1s',
      '1h 5m',
    ]);
  });

  it('keeps the order received', () => {
    expect(
      runsTable(
        jobUsage({ runs: [run({ runId: 'b' }), run({ runId: 'a' })] }),
        'acme'
      ).rows.map((row) => row.id)
    ).toEqual(['b', 'a']);
  });

  it('says so when the period has more runs than the list', () => {
    const twenty = Array.from({ length: 20 }, (_, i) =>
      run({ runId: `r${i}` })
    );
    expect(
      runsTable(jobUsage({ runs: twenty, totals: runsCounted(21) }), 'acme')
        .caption
    ).toBe('The 20 most recent runs of the period.');
  });

  it('has no caption when every run of the period is listed', () => {
    const two = [run({ runId: 'a' }), run({ runId: 'b' })];
    expect(
      runsTable(jobUsage({ runs: two, totals: runsCounted(2) }), 'acme').caption
    ).toBeUndefined();
    expect(runsTable(jobUsage({ runs: two }), 'acme').caption).toBeUndefined();
    expect(runsTable(jobUsage({}), 'acme')).toEqual({
      rows: [],
      caption: undefined,
    });
  });
});

describe('refusalRows', () => {
  it('names each gate with the times it refused, in the order received', () => {
    expect(
      refusalRows([refusalCount('rbac', 12), refusalCount('source_cap', 1500)])
    ).toEqual([
      { key: 'rbac', label: 'Member roles', count: '12' },
      { key: 'source_cap', label: 'Source limit', count: '1,500' },
    ]);
  });

  it('leaves out a gate that refused nothing', () => {
    expect(refusalRows([refusalCount('rbac', 0)])).toEqual([]);
    expect(refusalRows([])).toEqual([]);
  });

  it('reads a gate it does not know as Other', () => {
    expect(refusalRows([refusalCount('whatever', 2)])).toEqual([
      { key: 'whatever', label: 'Other', count: '2' },
    ]);
  });
});
