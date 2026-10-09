import { create } from '@bufbuild/protobuf';
import {
  GateRefusalCountSchema,
  JobKind,
  JobUsageSchema,
  RunErrorCategory,
  UsageErrorCountSchema,
  UsageTotalsSchema,
} from '@husonym/sdk';
import {
  errorRows,
  errorsEmptyLine,
  jobsEmptyLine,
  jobsTable,
  refusalRows,
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
