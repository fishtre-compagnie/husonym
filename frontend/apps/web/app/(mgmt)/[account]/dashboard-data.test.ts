import { create } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { JobRunSchema, JobRunStatus } from '@husonym/sdk';
import { activeRuns, formatSpan } from './dashboard-data';

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

describe('activeRuns', () => {
  it('puts the running runs before the waiting ones', () => {
    const runs = [
      run('p', JobRunStatus.PENDING, 0),
      run('r', JobRunStatus.RUNNING, 1),
      run('done', JobRunStatus.COMPLETE, 1),
    ];
    expect(activeRuns(runs).map((r) => r.id)).toEqual(['r', 'p']);
  });
});

describe('formatting', () => {
  it('reads spans', () => {
    expect(formatSpan(42_000)).toBe('42 s');
    expect(formatSpan(7 * 60_000 + 4_000)).toBe('7 min 04');
    expect(formatSpan(HOUR + 3 * 60_000)).toBe('1 h 03 min');
    expect(formatSpan(undefined)).toBe('–');
  });
});
