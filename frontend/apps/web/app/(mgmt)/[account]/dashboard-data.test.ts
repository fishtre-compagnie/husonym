import { create } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import {
  DatabaseColumnSchema,
  GetConnectionSchemaMapResponse,
  GetConnectionSchemaMapResponseSchema,
  GetConnectionSchemaResponseSchema,
  Job,
  JobRunSchema,
  JobRunStatus,
  JobSchema,
  PiiConfidence,
} from '@husonym/sdk';
import {
  activeRuns,
  categoryLabel,
  coverage,
  formatSpan,
  latestRuns,
  runStats,
} from './dashboard-data';

const NOW = new Date('2026-11-11T12:00:00Z');
const HOUR = 3_600_000;

function run(
  id: string,
  status: JobRunStatus,
  hoursAgo: number,
  lastedMin = 5
) {
  const start = new Date(NOW.getTime() - hoursAgo * HOUR);
  const done =
    status === JobRunStatus.RUNNING || status === JobRunStatus.PENDING
      ? undefined
      : new Date(start.getTime() + lastedMin * 60_000);
  return create(JobRunSchema, {
    id,
    jobId: 'j1',
    status,
    startedAt: timestampFromDate(start),
    completedAt: done ? timestampFromDate(done) : undefined,
  });
}

describe('runStats', () => {
  const runs = [
    run('a', JobRunStatus.COMPLETE, 2),
    run('b', JobRunStatus.FAILED, 30),
    run('c', JobRunStatus.CANCELED, 50),
    run('d', JobRunStatus.COMPLETE, 24 * 10),
    run('e', JobRunStatus.RUNNING, 1),
  ];

  it('counts only the finished runs of the period', () => {
    const s24 = runStats(runs, '24h', NOW);
    expect([s24.finished, s24.succeeded, s24.failed]).toEqual([1, 1, 0]);
    const s7 = runStats(runs, '7d', NOW);
    expect([s7.finished, s7.succeeded, s7.failed, s7.canceled]).toEqual([
      3, 1, 1, 1,
    ]);
    expect(s7.successRate).toBe(33.3);
    expect(runStats(runs, '30d', NOW).finished).toBe(4);
  });

  it('spreads them over the slices of the period', () => {
    const s = runStats(runs, '7d', NOW);
    expect(s.series).toHaveLength(7);
    expect(s.series.reduce((a, b) => a + b, 0)).toBe(3);
    expect(s.series[6]).toBe(1);
  });

  it('has no rate when nothing finished', () => {
    expect(runStats([], '24h', NOW).successRate).toBeNull();
  });
});

describe('latestRuns and activeRuns', () => {
  it('lists the finished runs of the period, the most recent first', () => {
    const runs = [
      run('old', JobRunStatus.COMPLETE, 30),
      run('new', JobRunStatus.ERROR, 3),
      run('live', JobRunStatus.RUNNING, 1),
    ];
    expect(latestRuns(runs, '7d', 5, NOW).map((r) => r.id)).toEqual([
      'new',
      'old',
    ]);
    expect(latestRuns(runs, '24h', 5, NOW).map((r) => r.id)).toEqual(['new']);
  });

  it('puts the running runs before the waiting ones', () => {
    const runs = [
      run('p', JobRunStatus.PENDING, 0),
      run('r', JobRunStatus.RUNNING, 1),
      run('done', JobRunStatus.COMPLETE, 1),
    ];
    expect(activeRuns(runs).map((r) => r.id)).toEqual(['r', 'p']);
  });
});

describe('coverage', () => {
  const job = create(JobSchema, {
    id: 'j1',
    name: 'sampling-crm',
    source: {
      options: {
        config: { case: 'mysql', value: { connectionId: 'c1' } },
      },
    },
    mappings: [
      {
        schema: 'app',
        table: 'users',
        column: 'email',
        transformer: {
          config: { config: { case: 'generateEmailConfig', value: {} } },
        },
      },
      {
        schema: 'app',
        table: 'users',
        column: 'iban',
        transformer: {
          config: { config: { case: 'passthroughConfig', value: {} } },
        },
      },
      { schema: 'app', table: 'users', column: 'id' },
      { schema: 'app', table: 'users', column: 'notes' },
      { schema: 'app', table: 'users', column: 'gone' },
    ],
  }) as Job;

  const schema: GetConnectionSchemaMapResponse = create(
    GetConnectionSchemaMapResponseSchema,
    {
      schemaMap: {
        'app.users': create(GetConnectionSchemaResponseSchema, {
          schemas: [
            create(DatabaseColumnSchema, {
              column: 'email',
              isSensitive: true,
              dataCategory: 'email',
            }),
            create(DatabaseColumnSchema, {
              column: 'iban',
              isSensitive: true,
              dataCategory: 'iban',
            }),
            create(DatabaseColumnSchema, { column: 'id' }),
            create(DatabaseColumnSchema, {
              column: 'notes',
              piiConfidence: PiiConfidence.NEEDS_REVIEW,
            }),
          ],
        }),
      },
    }
  );

  it('sorts the mapped columns as the GDPR cell of the mapping does', () => {
    const c = coverage([job], new Map([['c1', schema]]));
    expect(c).toMatchObject({
      anonymized: 1,
      inClear: 1,
      kept: 1,
      toConfirm: 1,
      total: 4,
      jobsCounted: 1,
    });
    expect(c.inClearColumns).toEqual([
      expect.objectContaining({ column: 'iban', category: 'iban' }),
    ]);
  });

  it('leaves out the jobs whose source schema is unknown', () => {
    const c = coverage([job], new Map());
    expect([c.total, c.jobsCounted]).toEqual([0, 0]);
  });
});

describe('formatting', () => {
  it('reads spans and categories', () => {
    expect(formatSpan(42_000)).toBe('42 s');
    expect(formatSpan(7 * 60_000 + 4_000)).toBe('7 min 04');
    expect(formatSpan(HOUR + 3 * 60_000)).toBe('1 h 03 min');
    expect(formatSpan(undefined)).toBe('–');
    expect(categoryLabel('birth_date')).toBe('Birth date');
  });
});
