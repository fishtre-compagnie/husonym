import { create } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { JobRunSchema, JobRunStatus } from '@husonym/sdk';
import {
  dailyOutcomes,
  heatLevel,
  niceMax,
  percentChange,
  successRate,
  weekHeatmap,
} from './dashboard-analytics';

// Thursday 19 November 2026, 15:00 local time.
const NOW = new Date(2026, 10, 19, 15, 0);

function run(
  id: string,
  jobId: string,
  status: JobRunStatus,
  start: Date,
  lastedMin?: number
) {
  return create(JobRunSchema, {
    id,
    jobId,
    status,
    startedAt: timestampFromDate(start),
    completedAt:
      lastedMin === undefined
        ? undefined
        : timestampFromDate(new Date(start.getTime() + lastedMin * 60_000)),
  });
}

describe('dailyOutcomes', () => {
  it('counts the finished runs of each day by outcome, today last', () => {
    const runs = [
      run('a', 'j1', JobRunStatus.COMPLETE, new Date(2026, 10, 19, 2, 0), 5),
      run('b', 'j1', JobRunStatus.FAILED, new Date(2026, 10, 19, 9, 0), 5),
      run('c', 'j2', JobRunStatus.CANCELED, new Date(2026, 10, 17, 9, 0), 5),
      run('d', 'j2', JobRunStatus.RUNNING, new Date(2026, 10, 19, 14, 0)),
      run('old', 'j2', JobRunStatus.COMPLETE, new Date(2026, 9, 1, 9, 0), 5),
    ];
    const days = dailyOutcomes(runs, 7, NOW);
    expect(days).toHaveLength(7);
    expect(days[6].day).toEqual(new Date(2026, 10, 19));
    expect(days[6]).toMatchObject({ succeeded: 1, failed: 1, canceled: 0 });
    expect(days[4]).toMatchObject({ succeeded: 0, failed: 0, canceled: 1 });
    expect(
      days.reduce((s, d) => s + d.succeeded + d.failed + d.canceled, 0)
    ).toBe(3);
  });
});

describe('weekHeatmap', () => {
  it('puts each run in the hour of its day of the week, Monday first, by job', () => {
    const runs = [
      // Two Thursdays at 2 h, one Monday at 9 h.
      run('a', 'nuit', JobRunStatus.COMPLETE, new Date(2026, 10, 19, 2, 10), 6),
      run('b', 'nuit', JobRunStatus.FAILED, new Date(2026, 10, 12, 2, 40), 2),
      run('c', 'dev', JobRunStatus.COMPLETE, new Date(2026, 10, 19, 2, 5), 1),
      run('d', 'dev', JobRunStatus.COMPLETE, new Date(2026, 10, 16, 9, 0), 1),
      run('out', 'dev', JobRunStatus.COMPLETE, new Date(2026, 8, 1, 9, 0), 1),
    ];
    const heat = weekHeatmap(runs, 30, NOW);
    expect(heat).toHaveLength(7);
    expect(heat[0]).toHaveLength(24);
    const thu2 = heat[3][2];
    expect(thu2).toMatchObject({ weekday: 3, hour: 2, runs: 3, failed: 1 });
    expect(thu2.jobs.map((j) => [j.jobId, j.runs, j.failed])).toEqual([
      ['nuit', 2, 1],
      ['dev', 1, 0],
    ]);
    expect(thu2.jobs[0].meanMs).toBe(4 * 60_000);
    expect(heat[0][9].runs).toBe(1);
    expect(heat.flat().reduce((s, c) => s + c.runs, 0)).toBe(4);
  });
});

describe('figures', () => {
  it('scales a slot to the busiest', () => {
    expect(heatLevel(0, 10)).toBe(0);
    expect(heatLevel(1, 10)).toBe(1);
    expect(heatLevel(10, 10)).toBe(5);
    expect(heatLevel(3, 0)).toBe(0);
  });

  it('compares with the period before', () => {
    expect(percentChange(120, 100)).toBe(20);
    expect(percentChange(80, 100)).toBe(-20);
    expect(percentChange(5, 0)).toBeUndefined();
  });

  it('leaves the canceled runs out of the success rate', () => {
    expect(successRate(10, 8, 0)).toBe(80);
    expect(successRate(10, 8, 2)).toBe(100);
    expect(successRate(2, 0, 2)).toBeUndefined();
  });

  it('rounds the top of an axis', () => {
    expect(niceMax(0)).toBe(1);
    expect(niceMax(17)).toBe(20);
    expect(niceMax(42)).toBe(50);
    expect(niceMax(100)).toBe(100);
    expect(niceMax(1_234_567)).toBe(2_000_000);
  });
});
